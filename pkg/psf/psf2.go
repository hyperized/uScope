package psf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

// Byte offsets of the PSF2 header fields, which are all little-endian
// uint32s after the four magic bytes.
const (
	offHeaderSize = 8
	offFlags      = 12
	offLength     = 16
	offCharsize   = 20
	offHeight     = 24
	offWidth      = 28
)

// psf2Header is the fixed part of a PSF2 file after every field has been
// range-checked, so the parser below can use plain ints without checking
// again.
type psf2Header struct {
	headerSize int
	count      int
	charsize   int
	height     int
	width      int
	unicode    bool
}

// parsePSF2 reads the modern format: a 32-byte little-endian header, the
// glyph bitmaps at headerSize, and an optional UTF-8 unicode table after
// them.
func parsePSF2(data []byte) (*Font, error) {
	head, err := readPSF2Header(data)
	if err != nil {
		return nil, err
	}

	stride := (head.width + bitsPerByte - 1) / bitsPerByte
	if head.charsize < stride*head.height {
		return nil, fmt.Errorf("%w: charsize %d cannot hold %d rows of %d bytes",
			ErrTruncated, head.charsize, head.height, stride)
	}

	end := head.headerSize + head.count*head.charsize
	if end > len(data) {
		return nil, fmt.Errorf("%w: %d glyphs of %d bytes need %d bytes, file has %d",
			ErrTruncated, head.count, head.charsize, end, len(data))
	}

	font := &Font{
		data:     data[head.headerSize:end],
		width:    head.width,
		height:   head.height,
		stride:   stride,
		charsize: head.charsize,
		count:    head.count,
	}

	if head.unicode {
		lookup, err := parseUTF8Table(data[end:], head.count)
		if err != nil {
			return nil, err
		}

		font.lookup = lookup
	}

	font.fallback = font.fallbackIndex()

	return font, nil
}

// readPSF2Header reads and range-checks the header.
//
// headerSize is checked against the file rather than against a constant: the
// format allows a longer header than 32 bytes so a future version can add
// fields, and the glyphs start wherever it says they do.
func readPSF2Header(data []byte) (psf2Header, error) {
	if len(data) < psf2HeaderSize {
		return psf2Header{}, fmt.Errorf("%w: %d bytes is less than a %d byte PSF2 header",
			ErrTruncated, len(data), psf2HeaderSize)
	}

	headerSize := le32(data, offHeaderSize)
	if headerSize < psf2HeaderSize || uint64(headerSize) > uint64(len(data)) {
		return psf2Header{}, fmt.Errorf("%w: headersize %d is outside %d to %d",
			ErrTruncated, headerSize, psf2HeaderSize, len(data))
	}

	count, err := bounded(le32(data, offLength), "glyph count", 0, MaxGlyphs)
	if err != nil {
		return psf2Header{}, err
	}

	charsize, err := bounded(le32(data, offCharsize), "charsize", 1, maxCharsize)
	if err != nil {
		return psf2Header{}, err
	}

	height, err := bounded(le32(data, offHeight), "height", 1, MaxHeight)
	if err != nil {
		return psf2Header{}, err
	}

	width, err := bounded(le32(data, offWidth), "width", 1, MaxWidth)
	if err != nil {
		return psf2Header{}, err
	}

	return psf2Header{
		headerSize: int(headerSize),
		count:      count,
		charsize:   charsize,
		height:     height,
		width:      width,
		unicode:    le32(data, offFlags)&psf2Unicode != 0,
	}, nil
}

// le32 reads one little-endian header field.
func le32(data []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(data[offset:])
}

// bounded range-checks a header field before it is narrowed to an int.
//
// The check happens on the uint32 on purpose. Converting first and comparing
// afterwards is the bug this function exists to avoid: on a 32-bit build a
// field of 0xFFFFFFFF lands as -1, which passes an upper-bound test and then
// indexes wherever it likes.
func bounded(raw uint32, name string, low, high uint32) (int, error) {
	if raw < low || raw > high {
		return 0, fmt.Errorf("%w: %s %d is outside %d to %d", ErrTooLarge, name, raw, low, high)
	}

	return int(raw), nil
}

// parseUTF8Table reads PSF2's unicode table: for each glyph in turn, the
// UTF-8 encoded code points that should render as it, then 0xFF.
//
// A 0xFE opens a list of multi-code-point sequences, which a console uses for
// combining accents and uScope has no use for, so everything from there to
// the terminator is dropped.
//
// Where two glyphs claim the same code point the first one keeps it, which
// matches what setfont loads into the kernel.
func parseUTF8Table(table []byte, count int) (map[rune]int, error) {
	lookup := make(map[rune]int, count)
	pos := 0

	for index := range count {
		end := bytes.IndexByte(table[pos:], psf2Terminator)
		if end < 0 {
			return nil, fmt.Errorf("%w: unicode table has no terminator for glyph %d", ErrTruncated, index)
		}

		entry := table[pos : pos+end]
		pos += end + 1

		if cut := bytes.IndexByte(entry, psf2SeqStart); cut >= 0 {
			entry = entry[:cut]
		}

		recordUTF8(lookup, entry, index)
	}

	return lookup, nil
}

// recordUTF8 adds every code point in one table entry to the lookup.
//
// A byte that does not start a valid sequence ends the entry rather than
// failing the parse: the rest of the table is still readable, and refusing a
// whole font over one bad glyph mapping would be the wrong trade.
func recordUTF8(lookup map[rune]int, entry []byte, index int) {
	for len(entry) > 0 {
		value, size := utf8.DecodeRune(entry)
		if value == utf8.RuneError && size <= 1 {
			return
		}

		if _, taken := lookup[value]; !taken {
			lookup[value] = index
		}

		entry = entry[size:]
	}
}
