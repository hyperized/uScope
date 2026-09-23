package radar

import (
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/text"
)

// TestDoubtRadius checks the rule that sizes the home marker's ring: nothing
// for a position that is not an estimate, the floor for a confidence the
// self-locator never filled in, the scaled radius in between, and the ceiling
// past it.
func TestDoubtRadius(t *testing.T) {
	t.Parallel()

	const (
		floor   = 4
		ceiling = 270
		scale   = 4.5
	)

	estimate := func(confidenceNm float64) source.Receiver {
		return source.Receiver{Mode: source.FixEstimated, ConfidenceNm: confidenceNm}
	}

	for _, testCase := range []struct {
		name     string
		receiver source.Receiver
		want     int
	}{
		{name: "a manual position has no doubt", receiver: source.Receiver{Mode: source.FixManual, ConfidenceNm: 22}},
		{name: "a GPS fix has no doubt", receiver: source.Receiver{Mode: source.FixGPS3D, ConfidenceNm: 22}},
		{name: "no confidence lands on the floor", receiver: estimate(0), want: floor},
		{name: "a negative confidence lands on the floor", receiver: estimate(-3), want: floor},
		{name: "NaN lands on the floor", receiver: estimate(math.NaN()), want: floor},
		{name: "a confidence under the floor is lifted to it", receiver: estimate(0.2), want: floor},
		{name: "an ordinary confidence scales", receiver: estimate(22), want: 99},
		{
			name:     "a measured spread is drawn over the bound",
			receiver: source.Receiver{Mode: source.FixEstimated, ConfidenceNm: 97, SpreadNm: 4},
			want:     18,
		},
		{name: "half a pixel rounds up", receiver: estimate(1), want: 5},
		{name: "a confidence past the ceiling sits on it", receiver: estimate(100), want: ceiling},
		{name: "infinity sits on the ceiling", receiver: estimate(math.Inf(1)), want: ceiling},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := doubtRadius(testCase.receiver, scale, floor, ceiling); got != testCase.want {
				t.Errorf("doubtRadius(%+v) = %d, want %d", testCase.receiver, got, testCase.want)
			}
		})
	}
}

// TestDoubtRadiusFloorWinsOverCeiling checks the one order the clamp has to
// keep: a ceiling under the floor, which a scope too small for a ring could
// hand over, still answers the floor so the marker keeps its ordinary size.
func TestDoubtRadiusFloorWinsOverCeiling(t *testing.T) {
	t.Parallel()

	receiver := source.Receiver{Mode: source.FixEstimated, ConfidenceNm: 100}

	if got := doubtRadius(receiver, 1, homeRadius, 2); got != homeRadius {
		t.Errorf("doubtRadius with the ceiling under the floor = %d, want %d", got, homeRadius)
	}
}

// TestDoubtNm checks what the layer key carries: the estimate's own radius,
// and zero for a radius riding on any other fix mode.
func TestDoubtNm(t *testing.T) {
	t.Parallel()

	if got := doubtNm(source.Receiver{Mode: source.FixEstimated, ConfidenceNm: 22}); got != 22 {
		t.Errorf("doubtNm of an estimate = %v, want 22", got)
	}

	if got := doubtNm(source.Receiver{Mode: source.FixGPS3D, ConfidenceNm: 22}); got != 0 {
		t.Errorf("doubtNm of a GPS fix = %v, want 0", got)
	}

	if got := doubtNm(source.Receiver{Mode: source.FixEstimated, ConfidenceNm: 97, SpreadNm: 4}); got != 4 {
		t.Errorf("doubtNm of an estimate with a spread = %v, want the spread, 4", got)
	}
}

// TestReceiverLineWritesTheBoundBesideTheSpread checks the header's estimate
// line when the self-locator has both figures: the spread after EST in the
// estimate's colour, then the bound in the band's quiet ink, and nothing
// after the spread while the two are the same number.
func TestReceiverLineWritesTheBoundBesideTheSpread(t *testing.T) {
	t.Parallel()

	const (
		top  = 8
		left = 4
	)

	faces := Faces{Small: rowPlanSmall(t)}
	scene := &Scene{faces: faces}
	scene.SetPalette(theme.Night)

	measured := source.Receiver{
		Latitude: 52.31, Longitude: 4.77, ConfidenceNm: 97, SpreadNm: 4,
		Label: source.LabelEstimate, Mode: source.FixEstimated,
	}
	unmeasured := measured
	unmeasured.SpreadNm = 97

	got := lineCanvas(t)
	scene.drawReceiverLine(&layout{dst: got, left: left}, top, measured)

	plain := lineCanvas(t)
	scene.drawReceiverLine(&layout{dst: plain, left: left}, top, unmeasured)

	// The reference is built from the same calls drawReceiverLine makes, so
	// the test follows the layout rather than a pixel offset worked out by
	// hand.
	reference := lineCanvas(t)
	ink := scene.fixColour(source.FixEstimated, theme.Night.BandInk)
	quiet := scene.bandMuted()
	pen := text.Draw(reference, faces.Small, left, top, locPrefix, theme.Night.BandInk)
	pen = text.Draw(reference, faces.Small, pen, top, estimatePrefix, ink)
	pen = drawBytes(reference, faces.Small, pen, top, scene.whole(measured.SpreadNm), ink)
	pen = text.Draw(reference, faces.Small, pen, top, rangeUnit, ink)
	pen = text.Draw(reference, faces.Small, pen, top, boundPrefix, quiet)
	pen = drawBytes(reference, faces.Small, pen, top, scene.whole(measured.ConfidenceNm), quiet)
	text.Draw(reference, faces.Small, pen, top, rangeUnit, quiet)

	if !samePixels(got, reference, reference.Bounds()) {
		t.Error("the estimate line with a spread is not the spread, the unit and the bound in quiet ink")
	}

	plainReference := lineCanvas(t)
	pen = text.Draw(plainReference, faces.Small, left, top, locPrefix, theme.Night.BandInk)
	pen = text.Draw(plainReference, faces.Small, pen, top, estimatePrefix, ink)
	pen = drawBytes(plainReference, faces.Small, pen, top, scene.whole(unmeasured.ConfidenceNm), ink)
	text.Draw(plainReference, faces.Small, pen, top, rangeUnit, ink)

	if !samePixels(plain, plainReference, plainReference.Bounds()) {
		t.Error("an estimate whose spread is the bound wrote something after the one figure")
	}
}

// lineCanvasSide is the square the header line tests draw on, wide enough
// for LOC EST ±4 NM / MAX 97 NM in the small face.
const lineCanvasSide = 320

// lineCanvas is a cleared square canvas for the header line tests.
func lineCanvas(tb testing.TB) *canvas.Canvas {
	tb.Helper()

	canv, err := canvas.New(lineCanvasSide, lineCanvasSide)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(theme.Night.Field)

	return canv
}

// TestBackgroundLayerFollowsTheEstimateRadius checks that the layer, which
// the home marker is drawn on, is rebuilt when the self-locator tightens its
// answer without the position moving, and not when the same radius rides on
// a fix that never draws it.
func TestBackgroundLayerFollowsTheEstimateRadius(t *testing.T) {
	t.Parallel()

	t.Run("a tightened estimate redraws", func(t *testing.T) {
		t.Parallel()

		scene, canv, src := layerScene(t)
		src.frame.Receiver.Mode = source.FixEstimated
		src.frame.Receiver.ConfidenceNm = 22

		scene.Draw(canv, 0)

		src.frame.Receiver.ConfidenceNm = 9

		scene.Draw(canv, 0)

		if scene.layerRuns != 2 {
			t.Errorf("layerRuns after the estimate tightened = %d, want 2", scene.layerRuns)
		}
	})

	t.Run("a radius on a GPS fix does not", func(t *testing.T) {
		t.Parallel()

		scene, canv, src := layerScene(t)
		src.frame.Receiver.Mode = source.FixGPS3D
		src.frame.Receiver.ConfidenceNm = 22

		scene.Draw(canv, 0)

		src.frame.Receiver.ConfidenceNm = 9

		scene.Draw(canv, 0)

		if scene.layerRuns != 1 {
			t.Errorf("layerRuns after a radius changed under a GPS fix = %d, want 1", scene.layerRuns)
		}
	})
}
