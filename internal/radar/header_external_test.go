package radar_test

import (
	"image"
	"testing"

	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/fonts"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// The header's own spacing, repeated here because the scene keeps it
// unexported and these cases have to measure against the same numbers.
const (
	headerLeft     = 16 // radar's own baseMargin.
	headerRowLead  = 4  // radar's own rowLead.
	headerTracking = 2  // radar's own headerTracking.
)

// The two column bands the cases below read ink out of: the left one holds the
// wordmark and the start of the receiver line under it, and the right one the
// two clocks. Neither reaches the other, and nothing else in the frame is set
// between them and the hairline.
const (
	headerLeftEdge  = headerLeft
	headerLeftStop  = 80
	headerRightEdge = 1000
	headerRightStop = 1264
)

// headerColumnBand is one of those two bands, cut off at the hairline so the
// ink below it is not counted as part of the header.
func headerColumnBand(left, right, rule int) image.Rectangle {
	return image.Rect(left, 0, right, rule)
}

// headerScene draws one frame at the panel's resolution with a receiver whose
// position is known, so the band carries both its lines.
func headerScene(tb testing.TB) *canvas.Canvas {
	tb.Helper()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv, _ := sceneOn(tb, panelWidth, panelHeight, frame)
	scene.Apply(radar.Settings{RangeNm: sceneRangeNm})
	scene.Draw(canv, 0)

	return canv
}

// hairlineRow is the first full-width row of the rule colour, which is the
// hairline the header band ends at.
//
// It is found rather than written down, so these cases keep working if the
// band's height ever changes. Nothing else in the frame runs the full width in
// that colour.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func hairlineRow(tb testing.TB, canv *canvas.Canvas) int {
	tb.Helper()

	bounds := canv.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		full := true

		for x := bounds.Min.X; x < bounds.Max.X && full; x++ {
			full = canv.Image().RGBAAt(x, y) == theme.Night.Rule
		}

		if full {
			return y
		}
	}

	tb.Fatal("no full-width hairline was drawn, so there is no band to measure")

	return 0
}

// inkRows is the topmost and bottommost rows of box holding anything that is
// not the field.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func inkRows(tb testing.TB, canv *canvas.Canvas, box image.Rectangle) (int, int) {
	tb.Helper()

	area := box.Intersect(canv.Bounds())
	first, last := -1, -1

	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) == theme.Night.Field {
				continue
			}

			if first < 0 {
				first = y
			}

			last = y

			break
		}
	}

	if first < 0 {
		tb.Fatalf("nothing was drawn in %v, so there is no ink to measure", box)
	}

	return first, last
}

// TestHeaderTypeIsCentredInTheBand checks that the band's type sits in the
// middle of the fill rather than in the middle of the room the layout
// reserved.
//
// The two are a whole margin apart, because the fill bleeds up to the canvas
// edge and the reserved band starts below the margin. Measured against the
// second, the type used to sit with twenty-two pixels of air over the wordmark
// and six under the receiver line, which on a band that reads as a masthead is
// the one thing about it anybody notices.
//
// It measures the ink and not the line boxes. Centring the boxes is what the
// scene does, and that only centres the type as well because both faces leave
// the same two blank rows above a capital and below a baseline; this is what
// says so.
func TestHeaderTypeIsCentredInTheBand(t *testing.T) {
	t.Parallel()

	canv := headerScene(t)
	rule := hairlineRow(t, canv)

	top, bottom := inkRows(t, canv, headerColumnBand(headerLeftEdge, headerLeftStop, rule))

	above := top - canv.Bounds().Min.Y
	below := rule - 1 - bottom

	if abs(above-below) > 1 {
		t.Errorf("the type sits %d pixels under the band's top edge and %d over its bottom, want them equal",
			above, below)
	}
}

// TestHeaderClocksShareTheBandCentre checks the other half of the same move:
// the clocks and the battery are centred on the middle of the fill, so the
// band reads level across its whole width.
func TestHeaderClocksShareTheBandCentre(t *testing.T) {
	t.Parallel()

	canv := headerScene(t)
	rule := hairlineRow(t, canv)

	top, bottom := inkRows(t, canv, headerColumnBand(headerRightEdge, headerRightStop, rule))

	above := top - canv.Bounds().Min.Y
	below := rule - 1 - bottom

	if abs(above-below) > 1 {
		t.Errorf("the clocks sit %d pixels under the band's top edge and %d over its bottom, want them equal",
			above, below)
	}
}

// TestHeaderWordmarkIsTheProjectSpelling checks that the band says uScope.
//
// The wordmark used to be set USCOPE, in the all-caps every label in the scene
// uses. A wordmark is a name rather than a label, and setting it in caps made
// the band disagree with the binary, the repository and the README about what
// the thing is called.
//
// It is checked by drawing both spellings through the same face and comparing
// the pixels, which is the only way from outside the package to read what the
// band actually says. Matching one and not the other is the whole claim: a
// face with no lower case would draw them alike and pass a test that only
// looked at the first.
func TestHeaderWordmarkIsTheProjectSpelling(t *testing.T) {
	t.Parallel()

	canv := headerScene(t)
	rule := hairlineRow(t, canv)

	bold, small := headerFace(t, fonts.BodyBold), headerFace(t, fonts.Small)

	// The scene centres the two line boxes in the fill, so the wordmark's own
	// box starts half the leftover above the stack of the two.
	stack := bold.Height() + headerRowLead + small.Height()
	top := canv.Bounds().Min.Y + (rule-canv.Bounds().Min.Y-stack)/2

	for _, testCase := range []struct {
		name string
		text string
		want bool
	}{
		{name: "the project's own spelling", text: "uScope", want: true},
		{name: "the all-caps it replaced", text: "USCOPE"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			width, height := text.Measure(bold, testCase.text, text.WithSpacing(headerTracking))
			box := image.Rect(headerLeft, top, headerLeft+width, top+height)

			if got := identicalIn(canv, wordmarkOn(t, testCase.text, top), box); got != testCase.want {
				t.Errorf("the band matched %q = %v, want %v", testCase.text, got, testCase.want)
			}
		})
	}
}

// wordmarkOn draws one spelling of the wordmark alone, on a canvas the size of
// the frame and in the colours the band uses, for the comparison above.
func wordmarkOn(tb testing.TB, word string, top int) *canvas.Canvas {
	tb.Helper()

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(theme.Night.Band)
	text.Draw(canv, headerFace(tb, fonts.BodyBold), headerLeft, top, word, theme.Night.BandInk,
		text.WithSpacing(headerTracking))

	return canv
}

// headerFace loads one of the embedded faces, failing the test rather than
// returning an error nobody here could act on.
func headerFace(tb testing.TB, loader func() (*psf.Font, error)) *psf.Font {
	tb.Helper()

	face, err := loader()
	if err != nil {
		tb.Fatalf("loading a face: %v", err)
	}

	return face
}
