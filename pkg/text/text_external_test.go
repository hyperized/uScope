package text_test

import (
	"encoding/binary"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// The synthetic fonts in this file are all 2x2 pixels, small enough that
// every pixel Draw touches can be checked by hand.
const (
	glyphWidth  = 2
	glyphHeight = 2

	pixelsPerByte = 8

	psf2Magic0    = 0x72
	psf2Magic1    = 0xB5
	psf2Magic2    = 0x4A
	psf2Magic3    = 0x86
	psf2HeaderLen = 32

	offHeaderSize = 8
	offFlags      = 12
	offGlyphCount = 16
	offCharsize   = 20
	offHeight     = 24
	offWidth      = 28

	unicodeFlag = 1

	// Row bytes for a 2-pixel-wide glyph only use the top two bits.
	rowBothInk = 0xC0
	rowLeftInk = 0x80
	rowNoInk   = 0x00
)

// Rune sequences drawn against basicFont, named once so a value like
// "\x00\x01" reused across many test functions does not become a goconst
// violation.
const (
	oneGlyphValue   = "\x00"
	twoGlyphValue   = "\x00\x01"
	threeGlyphValue = "\x00\x01\x02"
	nilFontProbe    = "hello"
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

// buildFont assembles a minimal PSF2 font from raw glyph rows and an
// optional UTF-8 unicode table (nil for none), then parses it.
func buildFont(t *testing.T, glyphs []byte, table []byte) *psf.Font {
	t.Helper()

	stride := (glyphWidth + pixelsPerByte - 1) / pixelsPerByte
	charsize := stride * glyphHeight
	count := len(glyphs) / charsize

	header := make([]byte, psf2HeaderLen)
	header[0], header[1], header[2], header[3] = psf2Magic0, psf2Magic1, psf2Magic2, psf2Magic3

	var flags uint32
	if table != nil {
		flags = unicodeFlag
	}

	binary.LittleEndian.PutUint32(header[offHeaderSize:], psf2HeaderLen)
	binary.LittleEndian.PutUint32(header[offFlags:], flags)
	binary.LittleEndian.PutUint32(header[offGlyphCount:], toUint32(t, count))
	binary.LittleEndian.PutUint32(header[offCharsize:], toUint32(t, charsize))
	binary.LittleEndian.PutUint32(header[offHeight:], glyphHeight)
	binary.LittleEndian.PutUint32(header[offWidth:], glyphWidth)

	raw := make([]byte, 0, len(header)+len(glyphs)+len(table))
	raw = append(raw, header...)
	raw = append(raw, glyphs...)
	raw = append(raw, table...)

	font, err := psf.Parse(raw)
	if err != nil {
		t.Fatalf("psf.Parse: %v", err)
	}

	return font
}

// basicFont has three glyphs and no unicode table, so a glyph's index is
// its own code point: rune 0 is glyph 0, rune 1 is glyph 1, rune 2 is
// glyph 2. Neither U+FFFD nor '?' fits inside a 3 glyph font, so its
// Fallback always settles on glyph 0.
func basicFont(t *testing.T) *psf.Font {
	t.Helper()

	glyphs := []byte{
		rowBothInk, rowNoInk, // glyph 0: top row inked, bottom row blank
		rowLeftInk, rowBothInk, // glyph 1: top row left pixel, bottom row inked
		rowNoInk, rowLeftInk, // glyph 2: top row blank, bottom row left pixel
	}

	return buildFont(t, glyphs, nil)
}

// mappedFont carries a PSF2 unicode table mapping 'A' to glyph 0, '?' to
// glyph 1, and U+FFFD (the replacement character) to glyph 2, so its
// Fallback is glyph 2 rather than whatever glyph 0 happens to be.
func mappedFont(t *testing.T) *psf.Font {
	t.Helper()

	glyphs := []byte{
		rowBothInk, rowBothInk, // glyph 0 ('A'): fully inked
		rowLeftInk, rowLeftInk, // glyph 1 ('?'): left column inked
		rowNoInk, rowBothInk, // glyph 2 (U+FFFD): bottom row inked
	}

	table := []byte{'A', 0xFF, '?', 0xFF, 0xEF, 0xBF, 0xBD, 0xFF}

	return buildFont(t, glyphs, table)
}

// assertPixel fails the test if the pixel at (x, y) is not want.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func assertPixel(t *testing.T, canv *canvas.Canvas, x, y int, want color.RGBA) {
	t.Helper()

	if got := canv.Image().RGBAAt(x, y); got != want {
		t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, want)
	}
}

// assertCanvasUntouched fails the test if any pixel on canv differs from
// the zero value, which is what a freshly made canvas starts as.
func assertCanvasUntouched(t *testing.T, canv *canvas.Canvas) {
	t.Helper()

	bounds := canv.Bounds()

	for pixelY := bounds.Min.Y; pixelY < bounds.Max.Y; pixelY++ {
		for pixelX := bounds.Min.X; pixelX < bounds.Max.X; pixelX++ {
			assertPixel(t, canv, pixelX, pixelY, color.RGBA{})
		}
	}
}

func TestMeasureNilFont(t *testing.T) {
	t.Parallel()

	width, height := text.Measure(nil, nilFontProbe)
	if width != 0 || height != 0 {
		t.Errorf("Measure(nil, ...) = (%d, %d), want (0, 0)", width, height)
	}
}

func TestMeasureEmptyString(t *testing.T) {
	t.Parallel()

	font := basicFont(t)

	width, height := text.Measure(font, "")
	if width != 0 || height != 0 {
		t.Errorf("Measure(font, \"\") = (%d, %d), want (0, 0)", width, height)
	}
}

// TestMeasureRuneCount checks that a run of n runes measures n*width by
// height at the default scale, for one, two and three runes.
func TestMeasureRuneCount(t *testing.T) {
	t.Parallel()

	font := basicFont(t)

	for _, testCase := range []struct {
		name  string
		value string
		count int
	}{
		{name: "one rune", value: oneGlyphValue, count: 1},
		{name: "two runes", value: twoGlyphValue, count: 2},
		{name: "three runes", value: threeGlyphValue, count: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wantWidth := testCase.count * glyphWidth

			width, height := text.Measure(font, testCase.value)
			if width != wantWidth || height != glyphHeight {
				t.Errorf("Measure() = (%d, %d), want (%d, %d)", width, height, wantWidth, glyphHeight)
			}
		})
	}
}

func TestMeasureScale(t *testing.T) {
	t.Parallel()

	font := basicFont(t)

	const count = 2

	for _, testCase := range []struct {
		name  string
		scale int
	}{
		{name: "scale 2", scale: 2},
		{name: "scale 3", scale: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			width, height := text.Measure(font, twoGlyphValue, text.WithScale(testCase.scale))

			wantWidth := count * glyphWidth * testCase.scale
			wantHeight := glyphHeight * testCase.scale

			if width != wantWidth || height != wantHeight {
				t.Errorf("Measure() = (%d, %d), want (%d, %d)", width, height, wantWidth, wantHeight)
			}
		})
	}
}

// TestMeasureSpacing checks that the gap lands between glyphs and not after
// the last one: a two-rune string has one gap, a three-rune string has two.
func TestMeasureSpacing(t *testing.T) {
	t.Parallel()

	font := basicFont(t)

	const spacing = 3

	for _, testCase := range []struct {
		name  string
		value string
		count int
	}{
		{name: "two runes", value: twoGlyphValue, count: 2},
		{name: "three runes", value: threeGlyphValue, count: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			width, height := text.Measure(font, testCase.value, text.WithSpacing(spacing))

			wantWidth := testCase.count*glyphWidth + (testCase.count-1)*spacing
			if width != wantWidth || height != glyphHeight {
				t.Errorf("Measure() = (%d, %d), want (%d, %d)", width, height, wantWidth, glyphHeight)
			}
		})
	}
}

// TestWithScaleClamps checks the effect through Measure, which is the only
// observable trace of the clamp: 0 and any negative value settle on
// MinScale, and anything from 9 upward settles on MaxScale.
func TestWithScaleClamps(t *testing.T) {
	t.Parallel()

	font := basicFont(t)

	for _, testCase := range []struct {
		name      string
		scale     int
		wantScale int
	}{
		{name: "zero clamps to MinScale", scale: 0, wantScale: text.MinScale},
		{name: "negative clamps to MinScale", scale: -5, wantScale: text.MinScale},
		{name: "nine clamps to MaxScale", scale: 9, wantScale: text.MaxScale},
		{name: "one thousand clamps to MaxScale", scale: 1000, wantScale: text.MaxScale},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			width, _ := text.Measure(font, oneGlyphValue, text.WithScale(testCase.scale))

			wantWidth := glyphWidth * testCase.wantScale
			if width != wantWidth {
				t.Errorf("Measure() width with scale %d = %d, want %d", testCase.scale, width, wantWidth)
			}
		})
	}
}

// TestWithSpacingClamps checks the effect through Measure: a negative
// spacing settles on MinSpacing, and 65 or above settles on MaxSpacing.
func TestWithSpacingClamps(t *testing.T) {
	t.Parallel()

	font := basicFont(t)

	const count = 2 // two runes, so there is exactly one gap to observe

	for _, testCase := range []struct {
		name        string
		spacing     int
		wantSpacing int
	}{
		{name: "negative clamps to MinSpacing", spacing: -1, wantSpacing: text.MinSpacing},
		{name: "sixty-five clamps to MaxSpacing", spacing: 65, wantSpacing: text.MaxSpacing},
		{name: "far above range clamps to MaxSpacing", spacing: 1000, wantSpacing: text.MaxSpacing},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			width, _ := text.Measure(font, twoGlyphValue, text.WithSpacing(testCase.spacing))

			wantWidth := count*glyphWidth + testCase.wantSpacing
			if width != wantWidth {
				t.Errorf("Measure() width with spacing %d = %d, want %d", testCase.spacing, width, wantWidth)
			}
		})
	}
}

// TestDrawReturnValueMatchesMeasure checks the contract callers chain runs
// on: Draw always returns x plus whatever Measure reports for the same
// arguments.
func TestDrawReturnValueMatchesMeasure(t *testing.T) {
	t.Parallel()

	const (
		originX = 0
		originY = 0
		side    = 40
	)

	font := basicFont(t)

	for _, testCase := range []struct {
		name  string
		value string
		opts  []text.Option
	}{
		{name: "no options", value: twoGlyphValue, opts: nil},
		{name: "with scale", value: twoGlyphValue, opts: []text.Option{text.WithScale(2)}},
		{name: "with spacing", value: threeGlyphValue, opts: []text.Option{text.WithSpacing(3)}},
		{
			name:  "with scale and spacing",
			value: threeGlyphValue,
			opts:  []text.Option{text.WithScale(2), text.WithSpacing(3)},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(side, side)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			measuredWidth, _ := text.Measure(font, testCase.value, testCase.opts...)
			want := originX + measuredWidth

			got := text.Draw(canv, font, originX, originY, testCase.value, canvas.Red, testCase.opts...)
			if got != want {
				t.Errorf("Draw() = %d, want %d (x + Measure width)", got, want)
			}
		})
	}
}

func TestDrawNilFont(t *testing.T) {
	t.Parallel()

	const (
		originX    = 2
		originY    = 0
		canvasSide = 4
	)

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	got := text.Draw(canv, nil, originX, originY, nilFontProbe, canvas.Red)
	if got != originX {
		t.Errorf("Draw() with nil font = %d, want %d (x unchanged)", got, originX)
	}

	assertCanvasUntouched(t, canv)
}

func TestDrawEmptyString(t *testing.T) {
	t.Parallel()

	const (
		originX    = 2
		originY    = 0
		canvasSide = 4
	)

	font := basicFont(t)

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	got := text.Draw(canv, font, originX, originY, "", canvas.Red)
	if got != originX {
		t.Errorf("Draw() with empty string = %d, want %d (x unchanged)", got, originX)
	}

	assertCanvasUntouched(t, canv)
}

// TestDrawScale1PixelExact checks the full pixel grid of a small canvas
// against an expected picture: only glyph 0's own ink pixels change, and
// every other pixel, including the glyph's own blank row, stays untouched.
func TestDrawScale1PixelExact(t *testing.T) {
	t.Parallel()

	const (
		canvasSide = 4
		originX    = 1
		originY    = 1
	)

	font := basicFont(t)

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	text.Draw(canv, font, originX, originY, oneGlyphValue, canvas.Red)

	inked := map[[2]int]bool{
		{originX, originY}:     true,
		{originX + 1, originY}: true,
	}

	bounds := canv.Bounds()

	for pixelY := bounds.Min.Y; pixelY < bounds.Max.Y; pixelY++ {
		for pixelX := bounds.Min.X; pixelX < bounds.Max.X; pixelX++ {
			want := color.RGBA{}
			if inked[[2]int{pixelX, pixelY}] {
				want = canvas.Red
			}

			assertPixel(t, canv, pixelX, pixelY, want)
		}
	}
}

// TestDrawScaleExpandsPixels checks that scale 2 and scale 3 both expand
// each ink pixel of glyph 0 into a scale by scale block at the right
// offset, verified against every pixel on the canvas.
func TestDrawScaleExpandsPixels(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		scale int
	}{
		{name: "scale 2", scale: 2},
		{name: "scale 3", scale: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const (
				originX = 1
				originY = 1
			)

			font := basicFont(t)
			side := originX + glyphWidth*testCase.scale + originX

			canv, err := canvas.New(side, side)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			text.Draw(canv, font, originX, originY, oneGlyphValue, canvas.Green, text.WithScale(testCase.scale))

			bounds := canv.Bounds()

			for pixelY := bounds.Min.Y; pixelY < bounds.Max.Y; pixelY++ {
				for pixelX := bounds.Min.X; pixelX < bounds.Max.X; pixelX++ {
					// Glyph 0's top source row is fully inked and its bottom
					// row is blank, so only the top scale rows of the block
					// should be painted.
					inCol := pixelX-originX >= 0 && pixelX-originX < glyphWidth*testCase.scale
					inRow := pixelY-originY >= 0 && pixelY-originY < testCase.scale

					want := color.RGBA{}
					if inCol && inRow {
						want = canvas.Green
					}

					assertPixel(t, canv, pixelX, pixelY, want)
				}
			}
		})
	}
}

// TestDrawWithBackground checks that the background fills the whole
// measured run box, including the gap a spacing option opens between
// glyphs, before any ink goes down.
func TestDrawWithBackground(t *testing.T) {
	t.Parallel()

	const (
		originX = 1
		originY = 1
		spacing = 2
	)

	font := basicFont(t)

	width, height := text.Measure(font, twoGlyphValue, text.WithSpacing(spacing))

	canv, err := canvas.New(originX+width+originX, originY+height+originY)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	text.Draw(canv, font, originX, originY, twoGlyphValue, canvas.White,
		text.WithBackground(canvas.Blue), text.WithSpacing(spacing))

	gapX := originX + glyphWidth // first column of the spacing gap, painted but unlit by either glyph
	assertPixel(t, canv, gapX, originY, canvas.Blue)
}

// TestDrawFallbackGlyph checks that a rune the font has no glyph for is
// drawn as the font's Fallback, using mappedFont so the fallback glyph is
// known ahead of time.
func TestDrawFallbackGlyph(t *testing.T) {
	t.Parallel()

	const (
		originX = 1
		originY = 1
	)

	font := mappedFont(t)

	canv, err := canvas.New(originX+glyphWidth+originX, originY+glyphHeight+originY)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	// 'Z' is not in the font's unicode table, so it draws as Fallback, which
	// for this font is glyph 2 (mapped explicitly to U+FFFD): bottom row
	// inked, top row blank.
	text.Draw(canv, font, originX, originY, "Z", canvas.Yellow)

	assertPixel(t, canv, originX, originY, color.RGBA{})
	assertPixel(t, canv, originX+1, originY, color.RGBA{})
	assertPixel(t, canv, originX, originY+1, canvas.Yellow)
	assertPixel(t, canv, originX+1, originY+1, canvas.Yellow)
}

// TestDrawInvalidUTF8 checks that a bare continuation byte advances by
// exactly one glyph width and paints the fallback, since ranging over the
// string yields one U+FFFD for the bad byte.
func TestDrawInvalidUTF8(t *testing.T) {
	t.Parallel()

	const (
		originX = 1
		originY = 1
	)

	font := basicFont(t)
	value := string([]byte{0x80})

	canv, err := canvas.New(originX+glyphWidth+originX, originY+glyphHeight+originY)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	got := text.Draw(canv, font, originX, originY, value, canvas.Magenta)

	want := originX + glyphWidth
	if got != want {
		t.Errorf("Draw() = %d, want %d", got, want)
	}

	// basicFont has only 3 glyphs and no unicode table, so neither U+FFFD
	// nor '?' fits and Fallback settles on glyph 0: top row inked, bottom
	// row blank.
	assertPixel(t, canv, originX, originY, canvas.Magenta)
	assertPixel(t, canv, originX+1, originY, canvas.Magenta)
	assertPixel(t, canv, originX, originY+1, color.RGBA{})
	assertPixel(t, canv, originX+1, originY+1, color.RGBA{})
}

// TestDrawClipping checks that starting a run off any of the four edges
// neither panics nor paints anything, since the whole glyph falls outside
// canvas.Set's bounds check in every case.
func TestDrawClipping(t *testing.T) {
	t.Parallel()

	const (
		canvasSide = 8
		farOffset  = 20
	)

	font := basicFont(t)

	for _, testCase := range []struct {
		name    string
		originX int
		originY int
	}{
		{name: "x far negative", originX: -farOffset, originY: 0},
		{name: "x past right edge", originX: canvasSide + farOffset, originY: 0},
		{name: "y far negative", originX: 0, originY: -farOffset},
		{name: "y past bottom edge", originX: 0, originY: canvasSide + farOffset},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(canvasSide, canvasSide)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			text.Draw(canv, font, testCase.originX, testCase.originY, oneGlyphValue, canvas.Red)

			assertCanvasUntouched(t, canv)
		})
	}
}

// TestDrawRight checks that a run ends at rightX and that its leftmost ink
// pixel lands at rightX minus the measured width, since glyph 0's own
// leftmost ink column is column 0 of its cell.
func TestDrawRight(t *testing.T) {
	t.Parallel()

	const (
		rightX     = 6
		originY    = 1
		canvasSide = 8
	)

	font := basicFont(t)

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	got := text.DrawRight(canv, font, rightX, originY, oneGlyphValue, canvas.Cyan)
	if got != rightX {
		t.Errorf("DrawRight() = %d, want %d", got, rightX)
	}

	leftInk := rightX - glyphWidth
	assertPixel(t, canv, leftInk, originY, canvas.Cyan)
	assertPixel(t, canv, leftInk+1, originY, canvas.Cyan)
}

// TestDrawCentered checks that a run centres on centerX and returns the x
// just past its last glyph.
func TestDrawCentered(t *testing.T) {
	t.Parallel()

	const (
		centerX    = 4
		originY    = 1
		canvasSide = 8
	)

	font := basicFont(t)

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	got := text.DrawCentered(canv, font, centerX, originY, oneGlyphValue, canvas.Green)

	width, _ := text.Measure(font, oneGlyphValue)
	want := centerX - width/2 + width

	if got != want {
		t.Errorf("DrawCentered() = %d, want %d", got, want)
	}

	leftInk := centerX - width/2
	assertPixel(t, canv, leftInk, originY, canvas.Green)
	assertPixel(t, canv, leftInk+1, originY, canvas.Green)
}

// TestDrawRightAndCenteredNilFont checks that a nil font takes the path
// where Measure reports zero width, so both functions collapse to their
// anchor coordinate and paint nothing.
func TestDrawRightAndCenteredNilFont(t *testing.T) {
	t.Parallel()

	const (
		anchor     = 5
		originY    = 1
		canvasSide = 8
	)

	for _, testCase := range []struct {
		name string
		draw func(*canvas.Canvas, *psf.Font, int, int, string, color.RGBA, ...text.Option) int
	}{
		{name: "DrawRight", draw: text.DrawRight},
		{name: "DrawCentered", draw: text.DrawCentered},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(canvasSide, canvasSide)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			got := testCase.draw(canv, nil, anchor, originY, nilFontProbe, canvas.Red)
			if got != anchor {
				t.Errorf("%s() with nil font = %d, want %d", testCase.name, got, anchor)
			}

			assertCanvasUntouched(t, canv)
		})
	}
}
