// Package psf reads PC Screen Font files, the bitmap console fonts the Linux
// kernel loads into a virtual terminal.
//
// Both versions of the format are here because the Terminus builds Debian
// ships are a mix: the 16-pixel faces are PSF1 and the 12- and 32-pixel ones
// are PSF2. The difference that matters to a caller is the unicode table,
// which maps code points to glyph indices. PSF1 stores it as UCS-2, PSF2 as
// UTF-8, and a font without one is indexed by code point directly.
//
// Nothing here trusts its input. Parse is fed an embedded file today and
// could be fed a file off a disk tomorrow, so every length a header claims is
// checked against the bytes that actually follow and against a cap before
// anything is sliced or allocated. A malformed file comes back as ErrMagic,
// ErrTruncated or ErrTooLarge; it never panics and never allocates from an
// untrusted length.
//
// A Font is read-only once parsed, so any number of goroutines may draw with
// the same one.
package psf

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// Caps on what a header may claim. These are not format limits, they are the
// point past which a file is likelier corrupt or hostile than real. The
// largest console font anyone ships is 16x32, so 64x128 leaves plenty of
// room without letting four bytes of header ask for a gigabyte of glyphs.
const (
	// MaxGlyphs is the most glyphs a font may declare.
	MaxGlyphs = 65536

	// MaxWidth is the widest glyph, in pixels.
	MaxWidth = 64

	// MaxHeight is the tallest glyph, in pixels.
	MaxHeight = 128

	// maxCharsize follows from the two size caps: the widest glyph row is
	// MaxWidth/8 bytes and there are at most MaxHeight rows.
	maxCharsize = MaxWidth / bitsPerByte * MaxHeight
)

// Walking a glyph row. PSF packs pixels most significant bit first, so the
// leftmost pixel of a row is the top bit of its first byte.
const (
	bitsPerByte = 8
	highBit     = 0x80
)

// Parse failures. Each is a sentinel so a caller can tell a file that is not
// a font from one that is a font and got cut short.
var (
	// ErrMagic means the first bytes match neither PSF version.
	ErrMagic = errors.New("psf: not a PSF font")

	// ErrTruncated means a header or table promised bytes the file does not
	// contain.
	ErrTruncated = errors.New("psf: font data ends early")

	// ErrTooLarge means a header claimed a size past the caps above.
	ErrTooLarge = errors.New("psf: header is outside the accepted range")
)

// Format constants from the kernel's Documentation and from console-setup's
// own reader. The magics are strings rather than byte slices so they can be
// constants instead of package-level variables.
const (
	psf2Magic      = "\x72\xb5\x4a\x86"
	psf2HeaderSize = 32
	psf2Unicode    = 1

	// In a PSF2 unicode table 0xFE opens a list of multi-code-point
	// sequences and 0xFF ends the entry. Neither byte can appear inside
	// valid UTF-8, which is what makes scanning for them safe.
	psf2SeqStart   byte = 0xFE
	psf2Terminator byte = 0xFF

	psf1Magic      = "\x36\x04"
	psf1HeaderSize = 4
	psf1Width      = 8
	psf1Mode512    = 0x01

	// Bit 1 and bit 2 both mean "unicode table follows"; the second is the
	// older spelling and some fonts set only it.
	psf1ModeTable = 0x06

	psf1Glyphs    = 256
	psf1Glyphs512 = 512

	psf1SeqStart   = 0xFFFE
	psf1Terminator = 0xFFFF
)

// Font is a parsed console font: a fixed-size bitmap per glyph, plus the
// mapping from code points onto them.
type Font struct {
	// data is the glyph bitmaps only, sliced out of the bytes handed to
	// Parse. Nothing is copied, so the caller's slice must not be written
	// to afterwards.
	data []byte

	// lookup is nil for a font with no unicode table, which is the signal
	// to index glyphs by code point instead.
	lookup map[rune]int

	width    int
	height   int
	stride   int
	charsize int
	count    int
	fallback int
}

// Glyph is one character's bitmap.
//
// It is a view into the font rather than a copy, so taking one costs nothing
// and the font has to outlive it. The zero Glyph is blank, which is what a
// font with no glyphs at all hands back.
type Glyph struct {
	bits   []byte
	width  int
	height int
	stride int
}

// Set reports whether the pixel at (x, y) is ink.
//
// Coordinates outside the glyph are not, so a caller can loop over a fixed
// cell size without first asking how big this particular glyph is.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (g Glyph) Set(x, y int) bool {
	if x < 0 || y < 0 || x >= g.width || y >= g.height {
		return false
	}

	return g.bits[y*g.stride+x/bitsPerByte]&(highBit>>(x%bitsPerByte)) != 0
}

// Parse reads a font from PSF1 or PSF2 bytes.
//
// The returned Font aliases data. Parse does not copy the glyph bitmaps
// because the caller is usually holding an embedded, immutable slice and a
// copy per font would be pure waste.
func Parse(data []byte) (*Font, error) {
	switch {
	case startsWith(data, psf2Magic):
		return parsePSF2(data)
	case startsWith(data, psf1Magic):
		return parsePSF1(data)
	default:
		return nil, fmt.Errorf("%w: first bytes are % x", ErrMagic, data[:min(len(data), len(psf2Magic))])
	}
}

// Width returns the glyph width in pixels. Every glyph in a PSF font is the
// same size, which is what makes a console font a console font.
func (f *Font) Width() int { return f.width }

// Height returns the glyph height in pixels.
func (f *Font) Height() int { return f.height }

// Len returns how many glyphs the font holds.
func (f *Font) Len() int { return f.count }

// Glyph returns the bitmap for a code point, and whether the font has one.
func (f *Font) Glyph(value rune) (Glyph, bool) {
	index, ok := f.indexOf(value)
	if !ok {
		return Glyph{}, false
	}

	return f.glyphAt(index), true
}

// Fallback returns the glyph to draw in place of a code point the font does
// not have: the replacement character if it is there, a question mark if it
// is not, and failing both the first glyph, which on a console font is
// usually blank but is at least a fixed-width nothing.
func (f *Font) Fallback() Glyph {
	return f.glyphAt(f.fallback)
}

// startsWith reports whether data opens with magic, without converting the
// whole slice to a string.
func startsWith(data []byte, magic string) bool {
	return len(data) >= len(magic) && string(data[:len(magic)]) == magic
}

// indexOf resolves a code point to a glyph index.
//
// With a unicode table the answer is whatever the table says. Without one the
// index is the code point itself, which is how the kernel treats a font that
// ships no table.
func (f *Font) indexOf(value rune) (int, bool) {
	if f.lookup != nil {
		index, ok := f.lookup[value]

		return index, ok
	}

	if value < 0 || int(value) >= f.count {
		return 0, false
	}

	return int(value), true
}

// glyphAt slices one glyph's rows out of the font data. An index outside the
// font gives the blank glyph rather than a panic, which is what makes
// Fallback safe on a font with nothing in it.
func (f *Font) glyphAt(index int) Glyph {
	if index < 0 || index >= f.count {
		return Glyph{}
	}

	start := index * f.charsize
	end := start + f.stride*f.height

	return Glyph{
		bits:   f.data[start:end:end],
		width:  f.width,
		height: f.height,
		stride: f.stride,
	}
}

// fallbackIndex picks the glyph Fallback hands out, once, at parse time.
func (f *Font) fallbackIndex() int {
	for _, candidate := range [...]rune{utf8.RuneError, '?'} {
		if index, ok := f.indexOf(candidate); ok {
			return index
		}
	}

	if f.count > 0 {
		return 0
	}

	return -1
}
