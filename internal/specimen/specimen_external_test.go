package specimen_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/specimen"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
)

// Dimensions of the synthetic faces this file builds. Small enough that the
// scene's blocks fit comfortably inside a 640x480 canvas, the logical size of
// the device panel. Body and BodyBold share a size, the same way the real
// Terminus faces do.
const (
	smallWidth     = 2
	smallHeight    = 3
	bodyWidth      = 4
	bodyHeight     = 6
	bodyBoldWidth  = 4
	bodyBoldHeight = 6
	largeWidth     = 8
	largeHeight    = 12
)

// PSF2 header layout: byte offsets and the magic that opens the file. See
// pkg/psf/psf2.go, which this file builds a minimal instance of rather than
// importing anything unexported from it.
const (
	psf2HeaderSize = 32
	offHeaderSize  = 8
	offGlyphCount  = 16
	offCharsize    = 20
	offFontHeight  = 24
	offFontWidth   = 28

	psf2Magic0 = 0x72
	psf2Magic1 = 0xB5
	psf2Magic2 = 0x4A
	psf2Magic3 = 0x86

	syntheticGlyphs = 256
	inkByte         = 0xFF
	bitsPerByte     = 8
)

// canvasWidth and canvasHeight are the panel's logical resolution, and the
// size every Draw call in this file uses unless a test is specifically about
// a different canvas size.
const (
	canvasWidth  = 640
	canvasHeight = 480
)

// The horizontal bands a full 640x480 Draw call with the faces above paints
// into, worked out empirically by scanning a rendered frame row by row rather
// than by re-deriving the margin and block-height arithmetic here. Each band
// is wider than the block it names, so a test does not have to track the
// scene's layout constants to know where to look.
const (
	headerBandTop    = 16
	headerBandBottom = 50
	cardBandTop      = 50
	cardBandBottom   = 155
	rowsBandTop      = 155
	rowsBandBottom   = 200
	fontBandTop      = 200
	fontBandBottom   = 330
	keyBarBandTop    = 450
	keyBarBandBottom = 465
)

// A fixed clock, and two fixed times whose "15:04" formatting differs.
const (
	fixedYear   = 2024
	earlyHour   = 9
	earlyMinute = 5
	lateHour    = 21
	lateMinute  = 47
)

// Canvas sizes for the degradation table: the half-block terminal case at the
// small end, up to the panel's own resolution.
const (
	tinyWidth          = 8
	tinyHeight         = 8
	smallCanvasWidth   = 40
	smallCanvasHeight  = 30
	mediumCanvasWidth  = 120
	mediumCanvasHeight = 90
	largeCanvasWidth   = 200
	largeCanvasHeight  = 150
)

// buildFace assembles a tiny synthetic PSF2 font: syntheticGlyphs glyphs of
// width by height, every pixel set to ink, and no unicode table, so a rune
// maps straight onto the glyph of the same code point. Every glyph carries
// the same bit pattern, which is enough to prove a face was used without
// pulling in the real embedded fonts, whose geometry would make the bands
// above unpredictable.
func buildFace(t *testing.T, width, height int) *psf.Font {
	t.Helper()

	stride := (width + bitsPerByte - 1) / bitsPerByte
	charsize := stride * height

	// width, height and charsize are the test's own small constants, nowhere
	// near uint32's range.
	charsize32 := uint32(charsize) //nolint:gosec // see comment above
	height32 := uint32(height)     //nolint:gosec // see comment above
	width32 := uint32(width)       //nolint:gosec // see comment above

	data := make([]byte, psf2HeaderSize+syntheticGlyphs*charsize)
	data[0], data[1], data[2], data[3] = psf2Magic0, psf2Magic1, psf2Magic2, psf2Magic3
	binary.LittleEndian.PutUint32(data[offHeaderSize:], psf2HeaderSize)
	binary.LittleEndian.PutUint32(data[offGlyphCount:], syntheticGlyphs)
	binary.LittleEndian.PutUint32(data[offCharsize:], charsize32)
	binary.LittleEndian.PutUint32(data[offFontHeight:], height32)
	binary.LittleEndian.PutUint32(data[offFontWidth:], width32)

	for index := psf2HeaderSize; index < len(data); index++ {
		data[index] = inkByte
	}

	// Every glyph is otherwise identical ink, which would make two different
	// digits render as the same pixels. Marking each glyph's last byte with
	// its own index gives different code points different bit patterns, so a
	// clock reading "09:05" and one reading "21:47" actually draw different
	// pixels. The first row of every glyph is untouched, so a test that
	// checks a glyph's top-left pixel still sees plain ink.
	for glyph := range syntheticGlyphs {
		last := psf2HeaderSize + (glyph+1)*charsize - 1
		data[last] = byte(glyph) //nolint:gosec // glyph is bounded by syntheticGlyphs, well under 256.
	}

	font, err := psf.Parse(data)
	if err != nil {
		t.Fatalf("psf.Parse: %v", err)
	}

	return font
}

// newFaces builds the four synthetic faces the layout tests draw with.
func newFaces(t *testing.T) specimen.Faces {
	t.Helper()

	return specimen.Faces{
		Small:    buildFace(t, smallWidth, smallHeight),
		Body:     buildFace(t, bodyWidth, bodyHeight),
		BodyBold: buildFace(t, bodyBoldWidth, bodyBoldHeight),
		Large:    buildFace(t, largeWidth, largeHeight),
	}
}

// assertPixel fails the test if the pixel at (x, y) is not want.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout this file.
func assertPixel(t *testing.T, canv *canvas.Canvas, x, y int, want color.RGBA, what string) {
	t.Helper()

	if got := canv.Image().RGBAAt(x, y); got != want {
		t.Errorf("%s: pixel (%d, %d) = %v, want %v", what, x, y, got, want)
	}
}

// bandHasNonField reports whether any pixel in the row range [top, bottom)
// differs from field.
func bandHasNonField(img *image.RGBA, top, bottom int, field color.RGBA) bool {
	bounds := img.Bounds()

	for y := top; y < bottom; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if img.RGBAAt(x, y) != field {
				return true
			}
		}
	}

	return false
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("returns a scene", func(t *testing.T) {
		t.Parallel()

		if scene := specimen.New(specimen.Faces{}); scene == nil {
			t.Fatal("New() = nil, want a scene")
		}
	})

	t.Run("two calls are independent", func(t *testing.T) {
		t.Parallel()

		first := specimen.New(specimen.Faces{})
		second := specimen.New(specimen.Faces{})

		if first == second {
			t.Fatal("New() returned the same instance twice, want distinct scenes")
		}
	})
}

// TestDrawPaintsEveryRegion checks that a full-sized Draw leaves ink somewhere
// in each of the scene's regions. Bands are asserted rather than exact
// coordinates, worked out empirically rather than by re-deriving the layout
// arithmetic in the test.
func TestDrawPaintsEveryRegion(t *testing.T) {
	t.Parallel()

	faces := newFaces(t)

	canv, err := canvas.New(canvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	specimen.New(faces).Draw(canv, 0)

	img := canv.Image()
	field := img.RGBAAt(0, 0)

	for _, testCase := range []struct {
		name   string
		top    int
		bottom int
	}{
		{name: "header band near the top", top: headerBandTop, bottom: headerBandBottom},
		{name: "card band", top: cardBandTop, bottom: cardBandBottom},
		{name: "compact rows band", top: rowsBandTop, bottom: rowsBandBottom},
		{name: "font sample band", top: fontBandTop, bottom: fontBandBottom},
		{name: "key bar band near the bottom edge", top: keyBarBandTop, bottom: keyBarBandBottom},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if !bandHasNonField(img, testCase.top, testCase.bottom, field) {
				t.Errorf("%s: no non-field pixel in rows [%d, %d)", testCase.name, testCase.top, testCase.bottom)
			}
		})
	}
}

// TestDrawClearsBackground pre-fills the canvas with a colour no block draws,
// so a corner Draw never reaches proves Clear ran first.
func TestDrawClearsBackground(t *testing.T) {
	t.Parallel()

	faces := newFaces(t)

	canv, err := canvas.New(canvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	canv.Clear(canvas.Red)

	specimen.New(faces).Draw(canv, 0)

	assertPixel(t, canv, 0, 0, theme.Night.Field, "top-left corner, past every block's margin")
}

func TestWithPaletteChangesColours(t *testing.T) {
	t.Parallel()

	faces := newFaces(t)

	nightCanvas, err := canvas.New(canvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	specimen.New(faces).Draw(nightCanvas, 0)

	custom := theme.Palette{
		Field:   canvas.Blue,
		Ink:     canvas.White,
		Muted:   canvas.Cyan,
		Accent:  canvas.Yellow,
		Rule:    canvas.Magenta,
		AltLow:  canvas.Green,
		AltMid:  canvas.Yellow,
		AltHigh: canvas.Red,
	}

	customCanvas, err := canvas.New(canvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	specimen.New(faces, specimen.WithPalette(custom)).Draw(customCanvas, 0)

	assertPixel(t, nightCanvas, 0, 0, theme.Night.Field, "background under the default palette")
	assertPixel(t, customCanvas, 0, 0, custom.Field, "background under the custom palette")

	if nightCanvas.Image().RGBAAt(0, 0) == customCanvas.Image().RGBAAt(0, 0) {
		t.Error("background pixel is the same under both palettes, want WithPalette to change it")
	}
}

func TestWithClock(t *testing.T) {
	t.Parallel()

	t.Run("fixed time renders", func(t *testing.T) {
		t.Parallel()

		faces := newFaces(t)
		fixed := time.Date(fixedYear, time.January, 1, earlyHour, earlyMinute, 0, 0, time.UTC)

		canv, err := canvas.New(canvasWidth, canvasHeight)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		specimen.New(faces, specimen.WithClock(func() time.Time { return fixed })).Draw(canv, 0)

		img := canv.Image()
		if !bandHasNonField(img, headerBandTop, headerBandBottom, img.RGBAAt(0, 0)) {
			t.Error("header band has no drawn pixels with a fixed clock")
		}
	})

	t.Run("different times draw different pixels", func(t *testing.T) {
		t.Parallel()

		faces := newFaces(t)
		early := time.Date(fixedYear, time.January, 1, earlyHour, earlyMinute, 0, 0, time.UTC)
		late := time.Date(fixedYear, time.January, 1, lateHour, lateMinute, 0, 0, time.UTC)

		earlyCanvas, err := canvas.New(canvasWidth, canvasHeight)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		specimen.New(faces, specimen.WithClock(func() time.Time { return early })).Draw(earlyCanvas, 0)

		lateCanvas, err := canvas.New(canvasWidth, canvasHeight)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		specimen.New(faces, specimen.WithClock(func() time.Time { return late })).Draw(lateCanvas, 0)

		if bytes.Equal(earlyCanvas.Image().Pix, lateCanvas.Image().Pix) {
			t.Error(`two clocks with different "15:04" text drew identical frames, want the clock to reach the canvas`)
		}
	})

	t.Run("nil clock leaves time.Now in place", func(t *testing.T) {
		t.Parallel()

		faces := newFaces(t)

		canv, err := canvas.New(canvasWidth, canvasHeight)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		specimen.New(faces, specimen.WithClock(nil)).Draw(canv, 0)
	})
}

// TestDrawWithNoFacesLeavesCanvasField checks that every block skips itself
// when it has no face to draw with, leaving the canvas exactly as Clear left
// it.
func TestDrawWithNoFacesLeavesCanvasField(t *testing.T) {
	t.Parallel()

	canv, err := canvas.New(canvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	specimen.New(specimen.Faces{}).Draw(canv, 0)

	want, err := canvas.New(canvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	want.Clear(theme.Night.Field)

	if !bytes.Equal(canv.Image().Pix, want.Image().Pix) {
		t.Error("Draw with no faces changed the canvas, want it to stay entirely field coloured")
	}
}

// TestDrawWithPartialFaces checks that Draw survives every face missing on
// its own, each shape dropping a different set of blocks.
func TestDrawWithPartialFaces(t *testing.T) {
	t.Parallel()

	faces := newFaces(t)

	for _, testCase := range []struct {
		name  string
		faces specimen.Faces
	}{
		{name: "small only", faces: specimen.Faces{Small: faces.Small}},
		{name: "small and body", faces: specimen.Faces{Small: faces.Small, Body: faces.Body}},
		{
			name:  "everything but large",
			faces: specimen.Faces{Small: faces.Small, Body: faces.Body, BodyBold: faces.BodyBold},
		},
		{
			name:  "everything but small",
			faces: specimen.Faces{Body: faces.Body, BodyBold: faces.BodyBold, Large: faces.Large},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(canvasWidth, canvasHeight)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			want := canv.Bounds()

			specimen.New(testCase.faces).Draw(canv, 0)

			if got := canv.Bounds(); got != want {
				t.Errorf("Bounds() after Draw = %v, want %v (unchanged)", got, want)
			}
		})
	}
}

// TestDrawCanvasSizeDegradation checks Draw across sizes from the half-block
// terminal's smallest common canvas up to the panel's own resolution: at
// every size it must not panic, and the canvas bounds must not move.
func TestDrawCanvasSizeDegradation(t *testing.T) {
	t.Parallel()

	faces := newFaces(t)
	scene := specimen.New(faces)

	for _, testCase := range []struct {
		name   string
		width  int
		height int
	}{
		{name: "1x1, nothing fits", width: 1, height: 1},
		{name: "8x8, almost nothing fits", width: tinyWidth, height: tinyHeight},
		{name: "40x30", width: smallCanvasWidth, height: smallCanvasHeight},
		{name: "120x90", width: mediumCanvasWidth, height: mediumCanvasHeight},
		{name: "200x150", width: largeCanvasWidth, height: largeCanvasHeight},
		{name: "640x480, the panel-sized canvas", width: canvasWidth, height: canvasHeight},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(testCase.width, testCase.height)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			want := canv.Bounds()

			scene.Draw(canv, 0)

			if got := canv.Bounds(); got != want {
				t.Errorf("Bounds() after Draw = %v, want %v (unchanged)", got, want)
			}
		})
	}
}

func TestDrawDeterministic(t *testing.T) {
	t.Parallel()

	faces := newFaces(t)
	fixed := time.Date(fixedYear, time.January, 1, earlyHour, earlyMinute, 0, 0, time.UTC)
	clock := func() time.Time { return fixed }

	first := specimen.New(faces, specimen.WithClock(clock))
	second := specimen.New(faces, specimen.WithClock(clock))

	firstCanvas, err := canvas.New(canvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	secondCanvas, err := canvas.New(canvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	first.Draw(firstCanvas, 0)
	second.Draw(secondCanvas, 0)

	if !bytes.Equal(firstCanvas.Image().Pix, secondCanvas.Image().Pix) {
		t.Error("two scenes built with the same faces and clock drew different frames")
	}
}

func TestDrawTwiceIsIdempotent(t *testing.T) {
	t.Parallel()

	faces := newFaces(t)
	scene := specimen.New(faces)

	canv, err := canvas.New(canvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	scene.Draw(canv, 0)
	first := append([]byte(nil), canv.Image().Pix...)

	scene.Draw(canv, 0)

	if !bytes.Equal(first, canv.Image().Pix) {
		t.Error("drawing the same scene twice onto the same canvas changed the result")
	}
}

func TestDrawElapsedIsIgnored(t *testing.T) {
	t.Parallel()

	faces := newFaces(t)

	zeroCanvas, err := canvas.New(canvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	specimen.New(faces).Draw(zeroCanvas, 0)

	hourCanvas, err := canvas.New(canvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	specimen.New(faces).Draw(hourCanvas, time.Hour)

	if !bytes.Equal(zeroCanvas.Image().Pix, hourCanvas.Image().Pix) {
		t.Error("Draw with elapsed 0 and elapsed one hour produced different frames, want elapsed to be ignored")
	}
}
