package text

import (
	"encoding/binary"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
)

// PSF2 header layout, mirrored here so the test can build a font without
// reaching into package psf's internals. There is no unicode table: a
// glyph's index is its own code point, which is all pen.glyph and runSize
// need.
const (
	psf2Magic0    = 0x72
	psf2Magic1    = 0xB5
	psf2Magic2    = 0x4A
	psf2Magic3    = 0x86
	psf2HeaderLen = 32

	offHeaderSize = 8
	offGlyphCount = 16
	offCharsize   = 20
	offHeight     = 24
	offWidth      = 28

	pixelsPerByte = 8

	glyphWidth  = 2
	glyphHeight = 2
)

// toUint32 narrows a fixture-controlled, non-negative int to uint32, so the
// header fields below do not repeat an unchecked conversion at every field.
func toUint32(t *testing.T, value int) uint32 {
	t.Helper()

	if value < 0 || value > math.MaxUint32 {
		t.Fatalf("value %d does not fit in uint32", value)
	}

	return uint32(value) //nolint:gosec // range-checked immediately above
}

// buildFont assembles a minimal PSF2 font from glyph rows and parses it,
// failing the test if the fixture itself does not parse.
func buildFont(t *testing.T, width, height int, glyphs []byte) *psf.Font {
	t.Helper()

	stride := (width + pixelsPerByte - 1) / pixelsPerByte
	charsize := stride * height
	count := len(glyphs) / charsize

	header := make([]byte, psf2HeaderLen)
	header[0], header[1], header[2], header[3] = psf2Magic0, psf2Magic1, psf2Magic2, psf2Magic3

	binary.LittleEndian.PutUint32(header[offHeaderSize:], psf2HeaderLen)
	binary.LittleEndian.PutUint32(header[offGlyphCount:], toUint32(t, count))
	binary.LittleEndian.PutUint32(header[offCharsize:], toUint32(t, charsize))
	binary.LittleEndian.PutUint32(header[offHeight:], toUint32(t, height))
	binary.LittleEndian.PutUint32(header[offWidth:], toUint32(t, width))

	raw := make([]byte, 0, len(header)+len(glyphs))
	raw = append(raw, header...)
	raw = append(raw, glyphs...)

	font, err := psf.Parse(raw)
	if err != nil {
		t.Fatalf("psf.Parse: %v", err)
	}

	return font
}

// tinyGlyphs returns the bitmap for one 2x2 glyph whose top row is fully
// inked and bottom row is blank. That is enough to drive pen.glyph at any
// scale without needing a real font's worth of characters.
func tinyGlyphs() []byte {
	const (
		bothInk = 0xC0
		noInk   = 0x00
	)

	return []byte{bothInk, noInk}
}

func TestClamp(t *testing.T) {
	t.Parallel()

	const (
		low  = 2
		high = 8
	)

	for _, testCase := range []struct {
		name  string
		value int
		want  int
	}{
		{name: "below range", value: 0, want: low},
		{name: "inside range", value: 5, want: 5},
		{name: "above range", value: 20, want: high},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := clamp(testCase.value, low, high); got != testCase.want {
				t.Errorf("clamp(%d, %d, %d) = %d, want %d", testCase.value, low, high, got, testCase.want)
			}
		})
	}
}

func TestSettleNoOptions(t *testing.T) {
	t.Parallel()

	want := options{scale: MinScale}

	if got := settle(nil); got != want {
		t.Errorf("settle(nil) = %+v, want %+v", got, want)
	}
}

// TestSettleAppliesOptionsInOrder checks that the last option in the slice
// wins, since settle applies each one over whatever the previous one left.
func TestSettleAppliesOptionsInOrder(t *testing.T) {
	t.Parallel()

	const (
		firstScale  = 2
		secondScale = 3
	)

	got := settle([]Option{WithScale(firstScale), WithScale(secondScale)})

	if got.scale != secondScale {
		t.Errorf("settle() scale = %d, want %d (the last option applied)", got.scale, secondScale)
	}
}

// TestRunSizeZeroCases drives the two guard clauses runSize opens with: a
// nil font, and a font with nothing to measure.
func TestRunSizeZeroCases(t *testing.T) {
	t.Parallel()

	font := buildFont(t, glyphWidth, glyphHeight, tinyGlyphs())

	for _, testCase := range []struct {
		name  string
		font  *psf.Font
		value string
	}{
		{name: "nil font", font: nil, value: "hello"},
		{name: "empty string", font: font, value: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			width, height := runSize(testCase.font, testCase.value, options{scale: MinScale})
			if width != 0 || height != 0 {
				t.Errorf("runSize() = (%d, %d), want (0, 0)", width, height)
			}
		})
	}
}

// TestPenGlyphScale1 drives pen.glyph directly at MinScale, which takes the
// single-pixel Set path rather than FillRect.
func TestPenGlyphScale1(t *testing.T) {
	t.Parallel()

	font := buildFont(t, glyphWidth, glyphHeight, tinyGlyphs())

	canv, err := canvas.New(glyphWidth, glyphHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	glyph, ok := font.Glyph(0)
	if !ok {
		t.Fatal("font.Glyph(0) not found")
	}

	drawPen := pen{dst: canv, font: font, ink: canvas.Red, set: options{scale: MinScale}}
	drawPen.glyph(glyph, 0, 0)

	if got := canv.Image().RGBAAt(0, 0); got != canvas.Red {
		t.Errorf("pixel (0, 0) = %v, want %v", got, canvas.Red)
	}

	if got := canv.Image().RGBAAt(0, 1); got != (color.RGBA{}) {
		t.Errorf("pixel (0, 1) = %v, want zero value", got)
	}
}

// TestPenGlyphScaleAboveOne drives pen.glyph at a scale above one, which
// takes the FillRect path and expands each ink pixel into a scale by scale
// block.
func TestPenGlyphScaleAboveOne(t *testing.T) {
	t.Parallel()

	const scale = 2

	font := buildFont(t, glyphWidth, glyphHeight, tinyGlyphs())

	canv, err := canvas.New(glyphWidth*scale, glyphHeight*scale)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	glyph, ok := font.Glyph(0)
	if !ok {
		t.Fatal("font.Glyph(0) not found")
	}

	drawPen := pen{dst: canv, font: font, ink: canvas.Blue, set: options{scale: scale}}
	drawPen.glyph(glyph, 0, 0)

	for pixelY := range glyphHeight * scale {
		for pixelX := range glyphWidth * scale {
			want := color.RGBA{}
			if pixelY/scale == 0 { // only the glyph's top source row is inked
				want = canvas.Blue
			}

			if got := canv.Image().RGBAAt(pixelX, pixelY); got != want {
				t.Errorf("pixel (%d, %d) = %v, want %v", pixelX, pixelY, got, want)
			}
		}
	}
}
