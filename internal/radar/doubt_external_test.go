package radar_test

import (
	"image"
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
)

// homeRangeRadius is the outer range ring's radius in pixels at the panel's
// resolution, with the scope box where scopeBox puts it. The estimate ring is
// measured in the same pixels per nautical mile as that ring.
const homeRangeRadius = 270

// smallRingRadius is the fixed little ring every fix mode but an estimate
// draws, which is how far homeRingPixel sits from the receiver's own pixel.
const smallRingRadius = 4

// estimateFrame is sceneFrame's frame with the receiver's position downgraded
// to a self-locate estimate of the given confidence.
func estimateFrame(confidenceNm float64) source.Frame {
	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	frame.Receiver.HasFix = false
	frame.Receiver.Label = source.LabelEstimate
	frame.Receiver.Mode = source.FixEstimated
	frame.Receiver.ConfidenceNm = confidenceNm

	return frame
}

// TestEstimateRingIsTheConfidenceRadius checks that a self-locate estimate's
// home marker is a caution ring at the confidence radius rather than the
// fixed little ring every other fix mode draws: the ring lands where the
// scope's own scale puts that many nautical miles, and the small ring's pixel
// is left alone.
func TestEstimateRingIsTheConfidenceRadius(t *testing.T) {
	t.Parallel()

	const confidenceNm = 5

	scene, canv, ranges := sceneOn(t, panelWidth, panelHeight, estimateFrame(confidenceNm))
	scene.Draw(canv, 0)

	radius := int(math.Round(confidenceNm * homeRangeRadius / ranges.GetCurrent()))
	if radius <= 2*smallRingRadius {
		t.Fatalf("radius %d px is too close to the small ring for the test to tell them apart", radius)
	}

	// The first sample of a dashed circle is always drawn and sits at three
	// o'clock, which is why the probe is on the centre's own row.
	centre := image.Pt(homeRingPixel.X-smallRingRadius, homeRingPixel.Y)

	if got := canv.Image().RGBAAt(centre.X+radius, centre.Y); got != theme.Night.Caution {
		t.Errorf("pixel %d px east of the receiver = %v, want the caution ring", radius, got)
	}

	if got := canv.Image().RGBAAt(homeRingPixel.X, homeRingPixel.Y); got == theme.Night.Caution {
		t.Error("the small ring's pixel is still caution, want the ring moved out to the estimate's radius")
	}

	// One pixel up rather than the centre itself: the dot is a filled circle
	// of radius one, and the range rings' cross-hair owns the middle pixel.
	if got := canv.Image().RGBAAt(centre.X, centre.Y-1); got != theme.Night.Ink {
		t.Errorf("centre dot = %v, want ink at the best estimate", got)
	}
}

// TestEstimateRingStopsAtTheRange checks the ceiling: an estimate wider than
// the scope draws its ring on the outer range ring rather than across the
// column, and says the antenna could be anywhere in view.
func TestEstimateRingStopsAtTheRange(t *testing.T) {
	t.Parallel()

	scene, canv, ranges := sceneOn(t, panelWidth, panelHeight, estimateFrame(1000))
	scene.Draw(canv, 0)

	if ranges.GetCurrent() >= 1000 {
		t.Fatalf("range %v nm holds the estimate, the test needs one that does not", ranges.GetCurrent())
	}

	centreX := homeRingPixel.X - smallRingRadius

	if got := canv.Image().RGBAAt(centreX+homeRangeRadius, homeRingPixel.Y); got != theme.Night.Caution {
		t.Errorf("pixel on the outer ring = %v, want the caution ring capped there", got)
	}

	outside := image.Rect(scopeBox.Max.X, scopeBox.Min.Y, panelWidth, scopeBox.Max.Y)
	if got := countColour(canv, outside, theme.Night.Caution); got != 0 {
		t.Errorf("%d caution pixels beside the scope, want none: the ring must not cross the column", got)
	}
}

// TestEstimateRingIsDashed checks that the ring is broken rather than solid,
// which is what tells it apart from the small solid ring on a real fix at a
// glance: a solid ring at the same radius would paint every pixel of the
// circumference and a dashed one paints under half of them.
func TestEstimateRingIsDashed(t *testing.T) {
	t.Parallel()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, estimateFrame(1000))
	scene.Draw(canv, 0)

	got := countColour(canv, scopeBox, theme.Night.Caution)
	circumference := int(math.Round(2 * math.Pi * homeRangeRadius))

	if got == 0 || got > circumference/2 {
		t.Errorf("%d caution pixels on a ring of %d, want some and under half", got, circumference)
	}
}

// TestEstimateRingInEveryView checks that the other three views draw the
// estimate too. Each is compared against the same estimate with no confidence
// radius on it, which draws the same header word and the same marker and
// nothing for the area, so whatever caution ink is added is the ring.
func TestEstimateRingInEveryView(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		view radar.View
	}{
		{name: "the flat bare view", view: radar.ViewMinimal},
		{name: "the tilted view", view: radar.View3D},
		{name: "the tilted bare view", view: radar.ViewMinimal3D},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			field := image.Rect(0, 0, panelWidth, panelHeight)
			set := radar.Settings{View: testCase.view, RangeNm: sceneRangeNm}

			plain, plainCanvas, _ := sceneOn(t, panelWidth, panelHeight, estimateFrame(0))
			plain.Apply(set)
			plain.Draw(plainCanvas, 0)

			doubted, doubtedCanvas, _ := sceneOn(t, panelWidth, panelHeight, estimateFrame(10))
			doubted.Apply(set)
			doubted.Draw(doubtedCanvas, 0)

			before := countColour(plainCanvas, field, theme.Night.Caution)
			after := countColour(doubtedCanvas, field, theme.Night.Caution)

			if after <= before {
				t.Errorf("caution pixels: %d with no radius, %d with one, want the ring on top", before, after)
			}
		})
	}
}
