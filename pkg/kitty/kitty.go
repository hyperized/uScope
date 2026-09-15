// Package kitty turns one RGBA frame into the escape sequences a
// Kitty-compatible terminal draws as an image.
//
// The Kitty graphics protocol lets a program hand a terminal raw pixels
// instead of relying on block characters or Sixel, which is how uScope
// puts a real picture on screen in Ghostty, kitty, and WezTerm. This
// package only builds the escape sequences: it touches no terminal, no
// files, and makes no syscall. A separate package owns the terminal and
// writes the bytes this one produces to it.
//
// The protocol itself is documented at
// https://sw.kovidgoyal.net/kitty/graphics-protocol/.
package kitty

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"io"
	"strconv"
)

// bytesPerPixel is the stride of image.RGBA: R, G, B, A, one byte each.
const bytesPerPixel = 4

// esc is the byte that opens and closes every escape sequence this package
// emits.
const esc = 0x1b

// defaultFirstImageID and defaultSecondImageID are the two image ids Frame
// alternates between when the caller does not supply WithIDs. Kitty image
// ids share one namespace across every program drawing to the same
// terminal, and most of them start counting from 1, so a pair of
// five-digit numbers keeps uScope out of their way.
const (
	defaultFirstImageID  uint32 = 42001
	defaultSecondImageID uint32 = 42002
)

// minChunkSize, maxChunkSize, and base64Quantum bound what WithChunkSize
// accepts. Below minChunkSize a single base64 quantum would not fit in one
// chunk; base64Quantum is the number of base64 characters that decode to
// one group of input bytes, so a chunk boundary that is not a multiple of
// it would split a quantum across two escape sequences, and Kitty
// concatenates chunk payloads by byte rather than decoding each on its own.
const (
	minChunkSize     = 4
	maxChunkSize     = 4096
	base64Quantum    = 4
	defaultChunkSize = maxChunkSize
)

// decimalBase is the base writeUint renders numbers in, named so the call
// site reads as a base rather than a stray number.
const decimalBase = 10

// ErrImage is returned by Frame for a nil image or one with no pixels.
// image.Rect happily builds an empty rectangle, and encoding zero pixels
// would send the terminal a well-formed frame that draws nothing, which is
// worth catching here rather than debugging later on screen.
var ErrImage = errors.New("kitty: image is nil or has no pixels")

// ErrCells is returned by Frame when cols or rows is not positive. The
// terminal scales the image into that cell area, so a non-positive value
// asks it to draw into zero or negative space.
var ErrCells = errors.New("kitty: cols and rows must be positive")

// Option configures an Encoder built by New.
//
// An Option that receives an invalid value leaves the current default in
// place instead of returning an error: a bad id pair or chunk size is a
// caller bug worth defaulting past, not a runtime condition worth
// threading an error return through every call site that builds an
// Encoder.
type Option func(*Encoder)

// WithIDs overrides the pair of image ids Frame alternates between. Both
// ids must be non-zero and different from each other; a call that breaks
// either rule is ignored and the defaults stay in effect.
func WithIDs(first, second uint32) Option {
	return func(enc *Encoder) {
		if first == 0 || second == 0 || first == second {
			return
		}

		enc.firstID = first
		enc.secondID = second
	}
}

// WithChunkSize overrides how many base64 characters Frame puts in each
// escape sequence chunk. Values from 4 to 4096 are accepted and rounded
// down to a multiple of 4 so a chunk boundary never splits a base64
// quantum; anything outside that range is ignored and the default of 4096
// stays in effect.
func WithChunkSize(size int) Option {
	return func(enc *Encoder) {
		if size < minChunkSize || size > maxChunkSize {
			return
		}

		enc.chunkSize = size - size%base64Quantum
	}
}

// Encoder turns image.RGBA frames into Kitty graphics protocol escape
// sequences.
//
// Encoder is not safe for concurrent use: it keeps the compression,
// base64, and framing buffers a call to Frame reuses instead of
// reallocating, plus the image id state that lets successive frames
// alternate ids. Use one Encoder per output stream, from one goroutine.
type Encoder struct {
	firstID  uint32
	secondID uint32

	chunkSize int

	zlibBuf bytes.Buffer
	zw      *zlib.Writer

	b64Buf []byte

	frameBuf bytes.Buffer

	displayed bool
	lastID    uint32
}

// New builds an Encoder ready to encode frames, applying any options over
// the defaults.
func New(opts ...Option) *Encoder {
	enc := &Encoder{
		firstID:   defaultFirstImageID,
		secondID:  defaultSecondImageID,
		chunkSize: defaultChunkSize,
	}
	enc.zw = zlib.NewWriter(&enc.zlibBuf)

	for _, opt := range opts {
		opt(enc)
	}

	return enc
}

// Frame encodes one RGBA image and writes it to w as a single Kitty
// graphics protocol transmission.
//
// img is compressed with zlib and split across one or more escape sequence
// chunks, each shaped ESC _ G <control data> ; <base64 payload> ESC \. The
// control data on the first chunk carries the whole picture:
//
//	a=T   transmit the image and display it at the cursor
//	f=32  the payload is 32-bit RGBA
//	o=z   the payload is zlib-compressed
//	q=2   suppress the terminal's response; uScope never reads it back
//	i     the image id, alternating between two values across calls
//	s, v  the image's pixel width and height
//	c, r  the cell area the terminal should scale the image into
//	m     1 if another chunk follows, 0 on the last chunk
//
// Later chunks carry only m, since the rest of the control data does not
// change within one image. After the last chunk, if a previous frame is
// still on screen, Frame appends a delete block for its id, so the
// terminal does not accumulate images it will never show again.
//
// The whole frame is assembled in the Encoder's own buffer and handed to w
// in a single Write, so a terminal never draws half a frame and a slow
// writer never sees one call interleaved with the next.
//
//nolint:varnamelen // w is the conventional name for an io.Writer parameter.
func (e *Encoder) Frame(w io.Writer, img *image.RGBA, cols, rows int) error {
	if img == nil || img.Bounds().Empty() {
		return ErrImage
	}

	if cols <= 0 || rows <= 0 {
		return ErrCells
	}

	bounds := img.Bounds()
	e.compress(img, bounds)
	e.encodeBase64()

	imageID := e.nextID()

	e.frameBuf.Reset()
	e.writeChunks(imageID, bounds, cols, rows)

	if e.displayed {
		e.writeDelete(e.lastID)
	}

	e.displayed = true
	e.lastID = imageID

	if _, err := w.Write(e.frameBuf.Bytes()); err != nil {
		return fmt.Errorf("kitty: writing frame: %w", err)
	}

	return nil
}

// Close emits a delete block for both image ids Encoder ever uses, first
// then second, in one Write, and clears the "a frame is on screen" state
// so a later Frame does not try to delete an id the terminal has already
// forgotten.
//
//nolint:varnamelen // w is the conventional name for an io.Writer parameter.
func (e *Encoder) Close(w io.Writer) error {
	e.frameBuf.Reset()
	e.writeDelete(e.firstID)
	e.writeDelete(e.secondID)
	e.displayed = false

	if _, err := w.Write(e.frameBuf.Bytes()); err != nil {
		return fmt.Errorf("kitty: closing stream: %w", err)
	}

	return nil
}

// compress resets e.zlibBuf and e.zw, then feeds img's pixels through zlib
// row by row using PixOffset, so a subimage whose Stride is wider than its
// own width still compresses only its own pixels.
func (e *Encoder) compress(img *image.RGBA, bounds image.Rectangle) {
	e.zlibBuf.Reset()
	e.zw.Reset(&e.zlibBuf)

	rowLen := bounds.Dx() * bytesPerPixel

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		offset := img.PixOffset(bounds.Min.X, y)

		// e.zlibBuf is an in-memory buffer, which never rejects a write.
		_, _ = e.zw.Write(img.Pix[offset : offset+rowLen])
	}

	// Same reasoning: flushing the final block into an in-memory buffer
	// cannot fail either.
	_ = e.zw.Close()
}

// encodeBase64 base64-encodes e.zlibBuf into e.b64Buf, growing the
// destination slice only when its capacity falls short, so a steady-state
// Encoder never allocates once it has processed one full-sized frame.
func (e *Encoder) encodeBase64() {
	need := base64.StdEncoding.EncodedLen(e.zlibBuf.Len())

	if cap(e.b64Buf) < need {
		e.b64Buf = make([]byte, need)
	} else {
		e.b64Buf = e.b64Buf[:need]
	}

	base64.StdEncoding.Encode(e.b64Buf, e.zlibBuf.Bytes())
}

// nextID reports the image id the frame about to be written should use,
// alternating away from whichever id the previous frame is holding on
// screen.
func (e *Encoder) nextID() uint32 {
	if e.displayed && e.lastID == e.firstID {
		return e.secondID
	}

	return e.firstID
}

// writeChunks appends one or more escape sequences to e.frameBuf, covering
// all of e.b64Buf. The first chunk carries the full control data described
// on Frame; every later chunk carries only the m flag, since the rest does
// not change within one image.
func (e *Encoder) writeChunks(imageID uint32, bounds image.Rectangle, cols, rows int) {
	total := len(e.b64Buf)
	start := 0

	for {
		end := min(start+e.chunkSize, total)

		moreFlag := byte('0')
		if end < total {
			moreFlag = '1'
		}

		e.writeStart()

		if start == 0 {
			e.writeFirstControl(imageID, bounds, cols, rows, moreFlag)
		} else {
			e.writeContinuationControl(moreFlag)
		}

		e.frameBuf.WriteByte(';')
		e.frameBuf.Write(e.b64Buf[start:end])
		e.writeEnd()

		start = end
		if start >= total {
			break
		}
	}
}

// writeFirstControl appends a frame's full control data in the order
// Kitty expects: a=T,f=32,o=z,q=2,i=<id>,s=<width>,v=<height>,c=<cols>,
// r=<rows>,m=<moreFlag>.
func (e *Encoder) writeFirstControl(imageID uint32, bounds image.Rectangle, cols, rows int, moreFlag byte) {
	e.frameBuf.WriteString("a=T,f=32,o=z,q=2,i=")
	e.writeUint(uint64(imageID))
	e.frameBuf.WriteString(",s=")
	e.writeUint(uint64(bounds.Dx())) //nolint:gosec // image.Bounds() is never negative.
	e.frameBuf.WriteString(",v=")
	e.writeUint(uint64(bounds.Dy())) //nolint:gosec // image.Bounds() is never negative.
	e.frameBuf.WriteString(",c=")
	e.writeUint(uint64(cols)) //nolint:gosec // Frame already validated cols > 0.
	e.frameBuf.WriteString(",r=")
	e.writeUint(uint64(rows)) //nolint:gosec // Frame already validated rows > 0.
	e.frameBuf.WriteString(",m=")
	e.frameBuf.WriteByte(moreFlag)
}

// writeContinuationControl appends the control data for every chunk after
// the first: just the m flag, since the rest of the control data already
// reached the terminal in the first chunk.
func (e *Encoder) writeContinuationControl(moreFlag byte) {
	e.frameBuf.WriteString("m=")
	e.frameBuf.WriteByte(moreFlag)
}

// writeDelete appends a delete block for imageID with no payload. d=I
// frees the image data as well as the placement, and q=2 keeps the
// terminal quiet about it, matching every other block this package writes.
func (e *Encoder) writeDelete(imageID uint32) {
	e.writeStart()
	e.frameBuf.WriteString("a=d,d=I,i=")
	e.writeUint(uint64(imageID))
	e.frameBuf.WriteString(",q=2")
	e.writeEnd()
}

// writeStart appends the APC opener every escape sequence in this package
// begins with: ESC _ G.
func (e *Encoder) writeStart() {
	e.frameBuf.WriteByte(esc)
	e.frameBuf.WriteString("_G")
}

// writeEnd appends the APC terminator every escape sequence in this
// package ends with: ESC \.
func (e *Encoder) writeEnd() {
	e.frameBuf.WriteByte(esc)
	e.frameBuf.WriteByte('\\')
}

// writeUint appends the base-10 digits of v to e.frameBuf, using the
// buffer's own spare capacity so a steady-state Encoder never allocates for
// it.
func (e *Encoder) writeUint(v uint64) {
	b := e.frameBuf.AvailableBuffer()
	b = strconv.AppendUint(b, v, decimalBase)
	e.frameBuf.Write(b)
}
