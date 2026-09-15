package specimen

import (
	"encoding/binary"
	"image/color"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// Dimensions of the synthetic faces this file builds, chosen small so the
// blocks they drive fit inside modest canvases and the arithmetic below stays
// readable. Body and BodyBold share a size, the same way the real Terminus
// faces do.
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

// internalCanvasSide is a canvas big enough to hold any single block built
// from the small synthetic faces above, with room to spare. Tests that only
// care about the layout bookkeeping (lay.y, lay.bottom) rather than exact
// pixels use it so the canvas size never has to be worked out by hand.
const internalCanvasSide = 160

// A fixed clock, reused by every test that needs one but does not care what
// time it shows.
const (
	fixedYear   = 2024
	fixedHour   = 9
	fixedMinute = 5
)

// buildFace assembles a tiny synthetic PSF2 font: syntheticGlyphs glyphs of
// width by height, every pixel set to ink, and no unicode table, so a rune
// maps straight onto the glyph of the same code point. Every glyph carries
// the same bit pattern, which is enough to prove a face was used without
// pulling in the real embedded fonts, whose geometry would make the
// arithmetic in these tests unpredictable.
func buildFace(t *testing.T, width, height int) *psf.Font {
	t.Helper()

	stride := (width + bitsPerByte - 1) / bitsPerByte
	charsize := stride * height

	data := make([]byte, psf2HeaderSize+syntheticGlyphs*charsize)
	// width, height and charsize are the test's own small constants, nowhere
	// near uint32's range.
	charsize32 := uint32(charsize) //nolint:gosec // see comment above
	height32 := uint32(height)     //nolint:gosec // see comment above
	width32 := uint32(width)       //nolint:gosec // see comment above

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
	// code points render as the same pixels. Marking each glyph's last byte
	// with its own index gives different characters different bit patterns.
	// The first row of every glyph is untouched, so a test that checks a
	// glyph's top-left pixel still sees plain ink.
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

// assertPixel fails the test if the pixel at (x, y) is not want.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout this file.
func assertPixel(t *testing.T, canv *canvas.Canvas, x, y int, want color.RGBA, what string) {
	t.Helper()

	if got := canv.Image().RGBAAt(x, y); got != want {
		t.Errorf("%s: pixel (%d, %d) = %v, want %v", what, x, y, got, want)
	}
}

func TestLineHeight(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		face *psf.Font
		want int
	}{
		{name: "nil font has no lines", face: nil, want: 0},
		{name: "real font reports its height", face: buildFace(t, bodyWidth, bodyHeight), want: bodyHeight},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := lineHeight(testCase.face); got != testCase.want {
				t.Errorf("lineHeight() = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestLayoutFits(t *testing.T) {
	t.Parallel()

	const (
		bottom = internalCanvasSide
		startY = smallHeight
		room   = bottom - startY
	)

	for _, testCase := range []struct {
		name   string
		height int
		want   bool
	}{
		{name: "height exactly fills the remaining room", height: room, want: true},
		{name: "height overshoots by one pixel", height: room + 1, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			built := layout{y: startY, bottom: bottom}
			if got := built.fits(testCase.height); got != testCase.want {
				t.Errorf("fits(%d) = %v, want %v", testCase.height, got, testCase.want)
			}
		})
	}
}

func TestLayoutAdvance(t *testing.T) {
	t.Parallel()

	const (
		startY = smallHeight
		delta  = bodyHeight
	)

	built := layout{y: startY}
	built.advance(delta)

	if got, want := built.y, startY+delta; got != want {
		t.Errorf("after advance(%d), y = %d, want %d", delta, got, want)
	}
}

func TestCellInk(t *testing.T) {
	t.Parallel()

	scene := &Scene{pal: theme.Night}

	for column := range columns {
		want := scene.pal.Muted
		if column == callsignColumn {
			want = scene.pal.Ink
		}

		t.Run(columnName(column), func(t *testing.T) {
			t.Parallel()

			if got := scene.cellInk(column); got != want {
				t.Errorf("cellInk(%d) = %v, want %v", column, got, want)
			}
		})
	}
}

// columnName gives a compact-row column index a subtest name.
func columnName(column int) string {
	if column == callsignColumn {
		return "callsign column is ink"
	}

	return "column is muted"
}

func TestColumnWidths(t *testing.T) {
	t.Parallel()

	t.Run("nil body face returns all zeros", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{}

		want := [columns]int{}
		if got := scene.columnWidths(); got != want {
			t.Errorf("columnWidths() = %v, want %v", got, want)
		}
	})

	t.Run("widest measured cell per column", func(t *testing.T) {
		t.Parallel()

		body := buildFace(t, bodyWidth, bodyHeight)
		scene := &Scene{faces: Faces{Body: body}}

		var want [columns]int

		for _, row := range flightRows {
			for column, cell := range row {
				width, _ := text.Measure(body, cell)
				want[column] = max(want[column], width)
			}
		}

		if got := scene.columnWidths(); got != want {
			t.Errorf("columnWidths() = %v, want %v", got, want)
		}
	})
}

func TestFigureColumn(t *testing.T) {
	t.Parallel()

	t.Run("nil faces still return a sane width", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{}

		// Measure returns 0 width for a nil font, but drawFigures still
		// leaves unitGap between the value and the unit, so the floor is
		// unitGap plus the gap between figures, not figureGap alone.
		want := unitGap + figureGap
		if got := scene.figureColumn(); got != want {
			t.Errorf("figureColumn() = %d, want %d (nothing measured, just the gaps)", got, want)
		}
	})

	t.Run("real faces return a positive width", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{faces: Faces{
			Small: buildFace(t, smallWidth, smallHeight),
			Large: buildFace(t, largeWidth, largeHeight),
		}}

		if got := scene.figureColumn(); got <= 0 {
			t.Errorf("figureColumn() = %d, want a positive width", got)
		}
	})
}

func TestFaceList(t *testing.T) {
	t.Parallel()

	small := buildFace(t, smallWidth, smallHeight)
	body := buildFace(t, bodyWidth, bodyHeight)
	bodyBold := buildFace(t, bodyBoldWidth, bodyBoldHeight)
	large := buildFace(t, largeWidth, largeHeight)

	scene := &Scene{faces: Faces{Small: small, Body: body, BodyBold: bodyBold, Large: large}}

	want := [4]namedFace{
		{name: "TERMINUS 6x12", face: small},
		{name: "TERMINUS 8x16", face: body},
		{name: "TERMINUS BOLD 8x16", face: bodyBold},
		{name: "TERMINUS BOLD 16x32", face: large},
	}

	if got := scene.faceList(); got != want {
		t.Errorf("faceList() = %+v, want %+v", got, want)
	}
}

func TestDrawKeyBar(t *testing.T) {
	t.Parallel()

	t.Run("nil small face draws nothing", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{pal: theme.Night}
		lay := &layout{bottom: internalCanvasSide}

		scene.drawKeyBar(lay)

		if lay.bottom != internalCanvasSide {
			t.Errorf("bottom = %d, want %d (unchanged, no small face)", lay.bottom, internalCanvasSide)
		}
	})

	height := smallHeight + 2*capPadY

	t.Run("no room leaves the bar undrawn", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{pal: theme.Night, faces: Faces{Small: buildFace(t, smallWidth, smallHeight)}}
		lay := &layout{bottom: height - 1}

		scene.drawKeyBar(lay)

		if want := height - 1; lay.bottom != want {
			t.Errorf("bottom = %d, want %d (unchanged, the bar did not fit)", lay.bottom, want)
		}
	})

	t.Run("draws the bar and takes its room off the bottom", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(internalCanvasSide, internalCanvasSide)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		scene := &Scene{pal: theme.Night, faces: Faces{Small: buildFace(t, smallWidth, smallHeight)}}
		lay := &layout{dst: canv, left: 0, right: internalCanvasSide, bottom: internalCanvasSide}

		scene.drawKeyBar(lay)

		want := internalCanvasSide - (height + blockGap)
		if lay.bottom != want {
			t.Errorf("bottom = %d, want %d", lay.bottom, want)
		}

		assertPixel(t, canv, 0, lay.bottom+blockGap, theme.Night.Ink, "top-left corner of the first key cap")
	})
}

func TestDrawHeader(t *testing.T) {
	t.Parallel()

	bodyBold := buildFace(t, bodyBoldWidth, bodyBoldHeight)
	large := buildFace(t, largeWidth, largeHeight)

	band := max(bodyBoldHeight, largeHeight) + 2*headerPadY
	total := band + ruleHeight + blockGap

	for _, testCase := range []struct {
		name  string
		faces Faces
	}{
		{name: "no bold body face", faces: Faces{Large: large}},
		{name: "no large face", faces: Faces{BodyBold: bodyBold}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{pal: theme.Night, faces: testCase.faces, now: time.Now}
			lay := &layout{bottom: total}

			scene.drawHeader(lay)

			if lay.y != 0 {
				t.Errorf("y = %d, want 0 (unchanged, a required face is missing)", lay.y)
			}
		})
	}

	t.Run("no room leaves it undrawn", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{pal: theme.Night, faces: Faces{BodyBold: bodyBold, Large: large}, now: time.Now}
		lay := &layout{bottom: total - 1}

		scene.drawHeader(lay)

		if lay.y != 0 {
			t.Errorf("y = %d, want 0 (unchanged, the header did not fit)", lay.y)
		}
	})

	t.Run("draws and advances when it fits", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(internalCanvasSide, internalCanvasSide)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		fixed := time.Date(fixedYear, time.January, 1, fixedHour, fixedMinute, 0, 0, time.UTC)
		scene := &Scene{
			pal:   theme.Night,
			faces: Faces{BodyBold: bodyBold, Large: large},
			now:   func() time.Time { return fixed },
		}
		lay := &layout{dst: canv, left: 0, right: internalCanvasSide, bottom: total}

		scene.drawHeader(lay)

		if lay.y != total {
			t.Errorf("y = %d, want %d", lay.y, total)
		}

		assertPixel(t, canv, 0, band, theme.Night.Muted, "hairline under the header")
	})
}

func TestDrawCard(t *testing.T) {
	t.Parallel()

	small := buildFace(t, smallWidth, smallHeight)
	body := buildFace(t, bodyWidth, bodyHeight)
	large := buildFace(t, largeWidth, largeHeight)

	titleHeight := largeHeight * cardTitleScale
	middle := max(titleHeight, 2*bodyHeight)
	inner := smallHeight + rowGap + middle + rowGap + largeHeight
	total := 2*cardPadY + inner

	for _, testCase := range []struct {
		name  string
		faces Faces
	}{
		{name: "no small face", faces: Faces{Body: body, Large: large}},
		{name: "no body face", faces: Faces{Small: small, Large: large}},
		{name: "no large face", faces: Faces{Small: small, Body: body}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{pal: theme.Night, faces: testCase.faces}
			lay := &layout{bottom: total + blockGap}

			scene.drawCard(lay)

			if lay.y != 0 {
				t.Errorf("y = %d, want 0 (unchanged, a required face is missing)", lay.y)
			}
		})
	}

	t.Run("no room leaves it undrawn", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{pal: theme.Night, faces: Faces{Small: small, Body: body, Large: large}}
		lay := &layout{bottom: total + blockGap - 1}

		scene.drawCard(lay)

		if lay.y != 0 {
			t.Errorf("y = %d, want 0 (unchanged, the card did not fit)", lay.y)
		}
	})

	t.Run("draws and advances when it fits", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(internalCanvasSide, internalCanvasSide)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		scene := &Scene{pal: theme.Night, faces: Faces{Small: small, Body: body, Large: large}}
		lay := &layout{dst: canv, left: 0, right: internalCanvasSide, bottom: total + blockGap}

		scene.drawCard(lay)

		if want := total + blockGap; lay.y != want {
			t.Errorf("y = %d, want %d", lay.y, want)
		}

		assertPixel(t, canv, 0, 0, theme.Night.Accent, "accent bar at the card's top-left corner")
		assertPixel(t, canv, accentWidth, 0, theme.Night.Rule, "top border just past the accent bar")
	})
}

func TestDrawRows(t *testing.T) {
	t.Parallel()

	body := buildFace(t, bodyWidth, bodyHeight)
	total := len(flightRows) * (bodyHeight + rowLead)

	t.Run("nil body face draws nothing", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{pal: theme.Night}
		lay := &layout{bottom: total + blockGap}

		scene.drawRows(lay)

		if lay.y != 0 {
			t.Errorf("y = %d, want 0 (unchanged, no body face)", lay.y)
		}
	})

	t.Run("no room leaves it undrawn", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{pal: theme.Night, faces: Faces{Body: body}}
		lay := &layout{bottom: total + blockGap - 1}

		scene.drawRows(lay)

		if lay.y != 0 {
			t.Errorf("y = %d, want 0 (unchanged, the rows did not fit)", lay.y)
		}
	})

	t.Run("draws and advances when it fits", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(internalCanvasSide, internalCanvasSide)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		scene := &Scene{pal: theme.Night, faces: Faces{Body: body}}
		lay := &layout{dst: canv, left: 0, right: internalCanvasSide, bottom: total + blockGap}

		scene.drawRows(lay)

		if want := total + blockGap; lay.y != want {
			t.Errorf("y = %d, want %d", lay.y, want)
		}

		widths := scene.columnWidths()
		callsignX := widths[0] + columnGap

		assertPixel(t, canv, callsignX, 0, theme.Night.Ink, "callsign cell of the first row")
	})
}

// bandHasInk reports whether any pixel in the x range [left, right) differs
// from field, checked across the whole canvas height. Figures sit side by
// side, so a vertical band is the natural unit to check rather than an exact
// coordinate.
func bandHasInk(canv *canvas.Canvas, left, right int, field color.RGBA) bool {
	img := canv.Image()
	bounds := img.Bounds()

	for x := left; x < right; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			if img.RGBAAt(x, y) != field {
				return true
			}
		}
	}

	return false
}

func TestDrawFigures(t *testing.T) {
	t.Parallel()

	const slack = largeWidth

	small := buildFace(t, smallWidth, smallHeight)
	large := buildFace(t, largeWidth, largeHeight)
	scene := &Scene{pal: theme.Night, faces: Faces{Small: small, Large: large}}
	column := scene.figureColumn()

	t.Run("stops before a figure that would start past the right edge", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(column+slack, largeHeight)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		field := canv.Image().RGBAAt(0, 0)
		lay := &layout{dst: canv}
		scene.drawFigures(lay, 0, column, 0)

		if !bandHasInk(canv, 0, column, field) {
			t.Error("no ink drawn for the first figure, want it to draw before drawFigures stops")
		}

		if bandHasInk(canv, column, column+slack, field) {
			t.Error("ink found past the card's right edge, want drawFigures to have stopped before it")
		}
	})

	t.Run("draws every figure when the card is wide enough", func(t *testing.T) {
		t.Parallel()

		width := column*len(figures) + slack

		canv, err := canvas.New(width, largeHeight)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		field := canv.Image().RGBAAt(0, 0)
		lay := &layout{dst: canv}
		scene.drawFigures(lay, 0, width, 0)

		lastStart := column * (len(figures) - 1)
		if !bandHasInk(canv, lastStart, width, field) {
			t.Error("no ink drawn for the last figure, want all three figures to be drawn")
		}
	})
}

// TestDrawFacesNilSmallFaceDrawsNothing covers drawFaces' own guard: without
// a label face there is nothing to caption any sample with, so the whole
// block is skipped regardless of what Body, BodyBold or Large hold.
func TestDrawFacesNilSmallFaceDrawsNothing(t *testing.T) {
	t.Parallel()

	scene := &Scene{pal: theme.Night, faces: Faces{Large: buildFace(t, largeWidth, largeHeight)}}
	lay := &layout{bottom: internalCanvasSide}

	scene.drawFaces(lay)

	if lay.y != 0 {
		t.Errorf("y = %d, want 0 (unchanged, no small face to label with)", lay.y)
	}
}

func TestDrawFacesSkipsNilFace(t *testing.T) {
	t.Parallel()

	small := buildFace(t, smallWidth, smallHeight)
	bodyBold := buildFace(t, bodyBoldWidth, bodyBoldHeight)
	large := buildFace(t, largeWidth, largeHeight)

	scene := &Scene{
		pal: theme.Night,
		// Body left nil on purpose: this is the entry drawFaces must skip.
		faces: Faces{Small: small, BodyBold: bodyBold, Large: large},
	}

	smallBlock := smallHeight + labelLead + smallHeight + blockGap
	bodyBoldBlock := smallHeight + labelLead + bodyBoldHeight + blockGap
	largeBlock := smallHeight + labelLead + largeHeight + blockGap
	samples := smallBlock + bodyBoldBlock + largeBlock
	credit := smallHeight + blockGap

	canv, err := canvas.New(internalCanvasSide, internalCanvasSide)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	lay := &layout{dst: canv, left: 0, right: internalCanvasSide, bottom: samples + credit}

	scene.drawFaces(lay)

	if want := samples + credit; lay.y != want {
		t.Errorf("y = %d, want %d (three blocks plus the credit line, Body skipped)", lay.y, want)
	}
}

// TestDrawFacesCreditSkippedWhenNoRoom sizes bottom to the exact pixel that
// fits all four face samples and nothing more, so drawCredit's own fit check
// is what turns the credit line away, not a shortage the samples already hit.
func TestDrawFacesCreditSkippedWhenNoRoom(t *testing.T) {
	t.Parallel()

	small := buildFace(t, smallWidth, smallHeight)
	body := buildFace(t, bodyWidth, bodyHeight)
	bodyBold := buildFace(t, bodyBoldWidth, bodyBoldHeight)
	large := buildFace(t, largeWidth, largeHeight)

	scene := &Scene{pal: theme.Night, faces: Faces{Small: small, Body: body, BodyBold: bodyBold, Large: large}}

	smallBlock := smallHeight + labelLead + smallHeight + blockGap
	bodyBlock := smallHeight + labelLead + bodyHeight + blockGap
	bodyBoldBlock := smallHeight + labelLead + bodyBoldHeight + blockGap
	largeBlock := smallHeight + labelLead + largeHeight + blockGap
	samples := smallBlock + bodyBlock + bodyBoldBlock + largeBlock

	canv, err := canvas.New(internalCanvasSide, internalCanvasSide)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	lay := &layout{dst: canv, left: 0, right: internalCanvasSide, bottom: samples}

	scene.drawFaces(lay)

	if lay.y != samples {
		t.Errorf("y = %d, want %d (all four samples fit, but no room is left for the credit line)", lay.y, samples)
	}
}

func TestDrawCredit(t *testing.T) {
	t.Parallel()

	const labelHeight = smallHeight

	t.Run("skipped when there is no room", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{pal: theme.Night, faces: Faces{Small: buildFace(t, smallWidth, smallHeight)}}
		lay := &layout{bottom: labelHeight + blockGap - 1}

		scene.drawCredit(lay, labelHeight)

		if lay.y != 0 {
			t.Errorf("y = %d, want 0 (unchanged, credit did not fit)", lay.y)
		}
	})

	t.Run("drawn and advances when it fits", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(internalCanvasSide, internalCanvasSide)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		scene := &Scene{pal: theme.Night, faces: Faces{Small: buildFace(t, smallWidth, smallHeight)}}
		lay := &layout{dst: canv, left: 0, bottom: labelHeight + blockGap}

		scene.drawCredit(lay, labelHeight)

		if want := labelHeight + blockGap; lay.y != want {
			t.Errorf("y = %d, want %d", lay.y, want)
		}

		assertPixel(t, canv, 0, 0, theme.Night.Muted, "credit text top-left pixel")
	})
}
