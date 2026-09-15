// Package blocks is the lowest common denominator renderer for uScope.
//
// It shows the canvas in any terminal that speaks 24-bit colour, by printing
// one U+2580 (upper half block) glyph per character cell: the top pixel
// becomes the glyph's foreground colour, the bottom pixel its background
// colour, so one cell carries two vertically stacked pixels. It touches no
// terminal and no syscalls, only an io.Writer, so it is testable anywhere
// and leaves the terminal itself to a separate package.
package blocks

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"strconv"
)

// bytesPerPixel is the stride of image.RGBA: R, G, B, A, one byte each.
const bytesPerPixel = 4

// esc is the byte that starts every control sequence blocks emits.
const esc = 0x1b

// cursorHome moves the cursor to the top left without clearing the screen.
// Frames overwrite in place, so the renderer never scrolls and never clears.
const cursorHome = "[H"

// rowReset ends every row so a following row (or the shell prompt) starts
// from a clean SGR state rather than inheriting the last cell's colours.
const rowReset = "[0m"

// rowSeparator moves to the next row without touching the column.
//
// It is CR LF, not a bare LF, on purpose: the caller puts the terminal in
// raw mode with OPOST cleared, so a lone LF only moves down a line and never
// returns the carriage, and every row after the first would stair-step to
// the right.
const rowSeparator = "\r\n"

// glyph is U+2580, upper half block, the one character this package ever
// prints.
const glyph = "▀"

// sgrForeground and sgrBackground are the true-colour SGR parameter
// prefixes: 38 selects the foreground, 48 the background, and 2 says the
// colour that follows is three literal RGB numbers rather than a palette
// index.
const (
	sgrForeground = "38;2;"
	sgrBackground = "48;2;"
)

// ErrCells is returned when cols or rows is zero or negative. A grid with no
// cells has nothing to draw into.
var ErrCells = errors.New("blocks: cols and rows must be positive")

// ErrSize is returned when img is not exactly cols pixels wide and 2*rows
// pixels tall. This renderer never scales: the caller sizes the canvas to
// match the cell grid, so a mismatch is a caller bug and gets reported
// rather than papered over.
var ErrSize = errors.New("blocks: image size must be cols wide and 2*rows tall")

// Renderer draws an image.RGBA to a terminal as half-block glyphs, two
// pixels per character cell.
//
// Renderer is not safe for concurrent use: it keeps a reused byte buffer
// across calls to avoid allocating one every frame, so callers need one
// Renderer per output stream.
type Renderer struct {
	buf bytes.Buffer
}

// New returns a Renderer ready to draw frames.
func New() *Renderer {
	return &Renderer{}
}

// Frame writes one frame of img to w as a grid of cols by rows character
// cells, each cell showing two vertically stacked pixels under a half-block
// glyph.
//
// The whole frame is assembled in the Renderer's own buffer first and
// handed to w in a single Write, so a terminal never draws half a frame and
// a slow writer never sees one call interleaved with the next.
//
//nolint:varnamelen // w is the conventional name for an io.Writer parameter.
func (r *Renderer) Frame(w io.Writer, img *image.RGBA, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return ErrCells
	}

	if img == nil || img.Bounds().Dx() != cols || img.Bounds().Dy() != rows*2 {
		return ErrSize
	}

	r.buf.Reset()
	r.buf.WriteByte(esc)
	r.buf.WriteString(cursorHome)

	for row := range rows {
		r.writeRow(img, cols, row)

		if row < rows-1 {
			r.buf.WriteString(rowSeparator)
		}
	}

	if _, err := w.Write(r.buf.Bytes()); err != nil {
		return fmt.Errorf("blocks: writing frame: %w", err)
	}

	return nil
}

// writeRow appends one terminal row to the buffer: cols cells followed by
// the row reset, without a trailing separator.
//
// A colour escape is only written when it changes from the previous cell,
// which is what turns a run of same-coloured cells into one escape sequence
// instead of one per cell. The first cell always gets both colours, because
// the row reset before it means there is no previous cell to compare with.
func (r *Renderer) writeRow(img *image.RGBA, cols, row int) {
	var previousForeground, previousBackground [3]uint8

	top := row * 2
	bottom := top + 1

	for col := range cols {
		foreground := pixelAt(img, col, top)
		background := pixelAt(img, col, bottom)

		foregroundChanged := col == 0 || foreground != previousForeground
		backgroundChanged := col == 0 || background != previousBackground

		r.writeColorChange(foregroundChanged, backgroundChanged, foreground, background)
		r.buf.WriteString(glyph)

		previousForeground, previousBackground = foreground, background
	}

	r.buf.WriteByte(esc)
	r.buf.WriteString(rowReset)
}

// writeColorChange appends the SGR sequence for a cell, covering only the
// channels that actually changed: both, one, or neither, in which case it
// writes nothing at all.
//
//nolint:revive // fg/bg-changed are run-length flags computed by the caller, not a mode switch.
func (r *Renderer) writeColorChange(foregroundChanged, backgroundChanged bool, foreground, background [3]uint8) {
	if !foregroundChanged && !backgroundChanged {
		return
	}

	r.buf.WriteByte(esc)
	r.buf.WriteByte('[')

	if foregroundChanged {
		r.buf.WriteString(sgrForeground)
		r.writeRGB(foreground)
	}

	if foregroundChanged && backgroundChanged {
		r.buf.WriteByte(';')
	}

	if backgroundChanged {
		r.buf.WriteString(sgrBackground)
		r.writeRGB(background)
	}

	r.buf.WriteByte('m')
}

// writeRGB appends "R;G;B" for one colour, using the buffer's own spare
// capacity so a steady-state Renderer never allocates for it.
func (r *Renderer) writeRGB(rgb [3]uint8) {
	r.writeUint(rgb[0])
	r.buf.WriteByte(';')
	r.writeUint(rgb[1])
	r.buf.WriteByte(';')
	r.writeUint(rgb[2])
}

// decimalBase is the radix writeUint renders channel values in: plain
// decimal, the only base an SGR true-colour parameter accepts.
const decimalBase = 10

// writeUint appends the decimal digits of v to the buffer. It appends into
// the buffer's available capacity rather than building a temporary string,
// so a warmed-up Renderer draws every frame without allocating.
func (r *Renderer) writeUint(v uint8) {
	b := r.buf.AvailableBuffer()
	b = strconv.AppendUint(b, uint64(v), decimalBase)
	r.buf.Write(b)
}

// pixelAt reads the RGB channels of one pixel, ignoring alpha: the terminal
// has no alpha channel of its own, so anything translucent would only be
// flattened silently anyway. PixOffset is used rather than indexing by hand
// so a subimage with a wider Stride than its width still reads correctly.
func pixelAt(img *image.RGBA, col, pixelRow int) [3]uint8 {
	offset := img.PixOffset(col, pixelRow)
	pix := img.Pix[offset : offset+bytesPerPixel : offset+bytesPerPixel]

	return [3]uint8{pix[0], pix[1], pix[2]}
}
