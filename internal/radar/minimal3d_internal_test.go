package radar

import (
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/source"
)

// The four views' case names, written once because several of the tables below
// walk the same list and a repeated literal is a repeated literal.
const (
	caseScope   = "the scope"
	case3D      = "the 3D view"
	caseMinimal = "minimal mode"
	caseBare3D  = "the bare 3D view"
)

// TestViewPredicates checks the two questions view.go asks about a view, on
// all four of them.
//
// They are deliberately not one question. Whether the picture is tilted and
// whether it carries furniture are independent, and the four views are the
// grid: reading one off the other is what a pair of bools would let a caller
// do by accident.
func TestViewPredicates(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		view        View
		minimal     bool
		perspective bool
		bare        bool
	}{
		{name: caseScope},
		{name: case3D, view: View3D, perspective: true},
		{name: caseMinimal, view: ViewMinimal, minimal: true, bare: true},
		{name: caseBare3D, view: ViewMinimal3D, perspective: true, bare: true},
		{name: "the zero value reads as the scope", view: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{shown: testCase.view}

			if got := scene.minimal(); got != testCase.minimal {
				t.Errorf("minimal() = %v, want %v", got, testCase.minimal)
			}

			if got := scene.perspective(); got != testCase.perspective {
				t.Errorf("perspective() = %v, want %v", got, testCase.perspective)
			}

			if got := scene.bare(); got != testCase.bare {
				t.Errorf("bare() = %v, want %v", got, testCase.bare)
			}
		})
	}
}

// TestBareViewsShareTheOverlayPair checks that m and a reach the same pair of
// toggles from both bare views, and the scope's pair from both views with
// chrome.
//
// One pair rather than four is the point. The two bare views are a single
// press apart and the choice is about how much furniture a bare picture
// carries, not about which of the two you happen to be looking at.
func TestBareViewsShareTheOverlayPair(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		view View
		bare bool
	}{
		{name: caseScope},
		{name: case3D, view: View3D},
		{name: caseMinimal, view: ViewMinimal, bare: true},
		{name: caseBare3D, view: ViewMinimal3D, bare: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// The scope's pair starts on and the bare pair starts off, which
			// is what New does and what makes the two tell each other apart
			// here: whichever pair the view reads is the one drawn.
			scene := &Scene{shown: testCase.view, shoreOn: true, airports: true}

			if scene.shoreDrawn() == testCase.bare || scene.airportsDrawn() == testCase.bare {
				t.Fatalf("before the keys: shoreDrawn %v airportsDrawn %v, want both %v",
					scene.shoreDrawn(), scene.airportsDrawn(), !testCase.bare)
			}

			scene.toggleShore()
			scene.toggleAirports()

			if scene.shoreDrawn() != testCase.bare || scene.airportsDrawn() != testCase.bare {
				t.Errorf("after the keys: shoreDrawn %v airportsDrawn %v, want both %v",
					scene.shoreDrawn(), scene.airportsDrawn(), testCase.bare)
			}

			// The other pair is untouched, which is the half that says the two
			// cannot be mixed up: a bare view's m is not a change to the scope
			// you get back when you press v.
			if scene.shoreOn != testCase.bare || scene.airports != testCase.bare {
				t.Errorf("the scope pair is shore %v airports %v, want both left at %v",
					scene.shoreOn, scene.airports, testCase.bare)
			}

			if scene.minimalShore != testCase.bare || scene.minimalAirports != testCase.bare {
				t.Errorf("the bare pair is shore %v airports %v, want both %v",
					scene.minimalShore, scene.minimalAirports, testCase.bare)
			}
		})
	}
}

// TestToggleEnvelope checks that e is taken in the 3D view and refused in the
// bare one, where the envelope is never drawn whatever the flag says.
//
// The refusal is the interesting half. The envelope is the largest piece of
// furniture in the picture and the bare view exists to have none, so a press
// that flipped the flag would be one whose effect turned up when v came back
// round to the full view.
func TestToggleEnvelope(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		view  View
		start bool
		want  bool
		taken bool
	}{
		{name: case3D + " turns it off", view: View3D, start: true, taken: true},
		{name: case3D + " turns it back on", view: View3D, want: true, taken: true},
		{name: caseBare3D + " refuses it", view: ViewMinimal3D, start: true, want: true},
		{name: "the flat views never see it", view: ViewScope, start: true, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{shown: testCase.view, envelope: testCase.start}

			if got := scene.handleRune('e'); got != testCase.taken {
				t.Errorf("handleRune('e') = %v, want %v", got, testCase.taken)
			}

			if scene.envelope != testCase.want {
				t.Errorf("envelope = %v, want %v", scene.envelope, testCase.want)
			}

			if got := scene.envelopeDrawn(); got && scene.bare() {
				t.Error("envelopeDrawn() = true in a bare view, want the envelope kept off it")
			}
		})
	}
}

// TestOrigin3FollowsTheTraffic checks which point each 3D view projects from.
//
// The full view is pinned to the receiver because its rings are measured from
// there and its envelope is drawn around the antenna. The bare view follows
// the traffic, which is the whole reason it and minimal share a cadence.
func TestOrigin3FollowsTheTraffic(t *testing.T) {
	t.Parallel()

	receiver := source.Receiver{Latitude: layerBaseLat, Longitude: layerBaseLon, HasFix: true}
	centre := geo{lat: layerBaseLat + 1, lon: layerBaseLon + 1}

	for _, testCase := range []struct {
		name string
		view View
		want geo
	}{
		{name: case3D + " stays on the receiver", view: View3D, want: geo{lat: layerBaseLat, lon: layerBaseLon}},
		{name: caseBare3D + " takes the centre", view: ViewMinimal3D, want: centre},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{
				shown:      testCase.view,
				recentre:   DefaultRecentre,
				centre:     centre,
				haveCentre: true,
			}

			if got := scene.origin3(receiver); got != testCase.want {
				t.Errorf("origin3 = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

// TestReach3 checks how far each 3D view draws.
//
// The full view stops at the range, where its outer ring is. The bare one has
// no ring to be outside of and reaches further, which is minimal's rule about
// the corners applied to a picture that has no corners to measure.
func TestReach3(t *testing.T) {
	t.Parallel()

	const scopeNm = 40.0

	for _, testCase := range []struct {
		name string
		view View
		want float64
	}{
		{name: case3D + " stops at the range", view: View3D, want: scopeNm},
		{name: caseBare3D + " reaches past it", view: ViewMinimal3D, want: scopeNm * minimalReach3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{shown: testCase.view}
			if got := scene.reach3(scopeNm); math.Abs(got-testCase.want) > 1e-9 {
				t.Errorf("reach3(%g) = %g, want %g", scopeNm, got, testCase.want)
			}
		})
	}
}

// TestBareViewsFollowTheCadence checks that following answers for both bare
// views and for neither of the two with chrome.
//
// The scope has a home marker, rings measured from it and a table of bearings
// off it; the 3D view draws the antenna's own envelope around the same point.
// A centre that moved would make all of it lie, which is why the cadence
// belongs to the bare pair alone.
func TestBareViewsFollowTheCadence(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		view View
		want bool
	}{
		{name: caseScope, view: ViewScope},
		{name: case3D, view: View3D},
		{name: caseMinimal, view: ViewMinimal, want: true},
		{name: caseBare3D, view: ViewMinimal3D, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{shown: testCase.view, recentre: DefaultRecentre}
			if got := scene.following(); got != testCase.want {
				t.Errorf("following() = %v, want %v", got, testCase.want)
			}

			off := &Scene{shown: testCase.view}
			if off.following() {
				t.Error("following() = true with the cadence off, want it to need both halves")
			}
		})
	}
}

// TestDrawReceiver3 checks the three answers the bare 3D view's receiver
// marker has: drawn where the camera can see it, and nothing at all for a
// position nobody has decoded or one the camera is not pointing at.
//
// The last of those is why the marker has no bounding test of its own. The
// camera already refuses a point behind the lens or far outside the box, which
// is the check the flat version has to do by hand against the canvas bounds.
func TestDrawReceiver3(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		receiver source.Receiver
		want     bool
	}{
		{
			name:     "at the origin, where the camera is looking",
			receiver: source.Receiver{Latitude: layerBaseLat, Longitude: layerBaseLon, Mode: source.FixManual},
			want:     true,
		},
		{
			name:     "no position decoded",
			receiver: source.Receiver{Mode: source.FixNone},
		},
		{
			// Azimuth zero puts the camera due south of the origin looking
			// north, so a point ninety degrees south of it is behind the lens
			// rather than merely far away, and the camera refuses it on depth.
			name:     "behind the camera",
			receiver: source.Receiver{Latitude: layerBaseLat - 90, Longitude: layerBaseLon, Mode: source.FixManual},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, view := envelopeFixture()
			scene.shown = ViewMinimal3D

			canv := blankCanvas(t)
			scene.drawReceiver3(canv, view, testCase.receiver)

			if drawn := filterPainted(canv) > 0; drawn != testCase.want {
				t.Errorf("the receiver marker was drawn = %v, want %v", drawn, testCase.want)
			}
		})
	}
}
