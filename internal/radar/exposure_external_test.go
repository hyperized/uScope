package radar_test

import (
	"math"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// exposureKind is which corridor exposure a fixture frame carries.
type exposureKind int

const (
	// exposureNone carries none.
	exposureNone exposureKind = iota

	// exposureDemo carries the demo's invented history around Schiphol, the
	// forty tracks the docs pictures show.
	exposureDemo

	// exposureRing carries the bench ghosts as a full exposure: five hundred
	// tracks of two hundred fixes, lost over the last hour and a half.
	exposureRing

	// exposureRingMoving is exposureRing from a source that loses a track on
	// every frame, so the flat views rebuild their layer on each.
	exposureRingMoving

	// exposureTubeCap is exposureRing with every track in a route and the
	// routes at the source's caps: sixty-four of them, a hundred and sixty
	// stations each, fanned out from the receiver. No real feed reaches it;
	// it is the most tube a frame can be handed.
	exposureTubeCap
)

// The tube cap's shape, matching the source's routeMaxRoutes and
// routeMaxStations, which are unexported.
const (
	tubeCapRoutes   = 64
	tubeCapStations = 160
)

// tubeCap is exposureTubeCap's routes around the fixture receiver: each one
// straight out on its own bearing, a station every two miles, climbing and
// widening as it goes.
func tubeCap() []source.Route {
	routes := make([]source.Route, 0, tubeCapRoutes)
	cosLat := math.Cos(receiverLat * math.Pi / 180)

	for route := range tubeCapRoutes {
		sin, cos := math.Sincos(float64(route) * 2 * math.Pi / tubeCapRoutes)
		stations := make([]source.Station, 0, tubeCapStations)

		for station := range tubeCapStations {
			distanceNm := 2 * float64(station)
			stations = append(stations, source.Station{
				Latitude:  receiverLat + distanceNm*cos/60,
				Longitude: receiverLon + distanceNm*sin/(60*cosLat),
				Altitude:  300 + 200*float64(station),
				SpreadNm:  0.3 + 0.02*float64(station),
				SpreadFt:  100 + 10*float64(station),
				Members:   2,
			})
		}

		routes = append(routes, source.Route{
			Class: source.TrackClass(route % 3), Strength: 1, Stations: stations,
		})
	}

	return routes
}

// exposeFrame puts the exposure kind asks for on a frame.
func exposeFrame(tb testing.TB, frame *source.Frame, kind exposureKind) {
	tb.Helper()

	switch kind {
	case exposureDemo:
		demo := demoExposure(tb, frame.Now)
		frame.Ghosts, frame.GhostGeneration, frame.Corridors = demo.Ghosts, demo.GhostGeneration, demo.Corridors
		frame.Routes, frame.RouteGeneration = demo.Routes, demo.RouteGeneration
	case exposureRing, exposureRingMoving, exposureTubeCap:
		ring := ghostFleet(benchGhosts, benchGhostHistory)
		for index := range ring {
			ring[index].LostAt = frame.Now.Add(-time.Duration(index) * 10 * time.Second)
			ring[index].Class = source.TrackClass(index % 3)
		}

		frame.Ghosts, frame.GhostGeneration = ring, 1

		if kind == exposureTubeCap {
			for index := range ring {
				ring[index].Tubed, ring[index].TubeTo = true, len(ring[index].Points)-1
			}

			frame.Routes, frame.RouteGeneration = tubeCap(), 1
		}

		frame.Corridors = source.Corridors{
			Field:    source.Airfield{ICAO: "EHAM", Latitude: receiverLat, Longitude: receiverLon},
			HalfLife: source.DefaultCorridorHalfLife,
		}
	case exposureNone:
		fallthrough
	default:
		return
	}
}

// demoExposure is the demo's first frame on a clock stopped at now, which
// carries its invented history. The demo receiver sits beside Schiphol, as
// every fixture receiver in this package does.
func demoExposure(tb testing.TB, now time.Time) source.Frame {
	tb.Helper()

	demo, err := source.NewDemo(source.WithDemoClock(func() time.Time { return now }))
	if err != nil {
		tb.Fatalf("NewDemo: %v", err)
	}

	frame := demo.Frame()
	if !frame.Corridors.Placed() || len(frame.Ghosts) == 0 {
		tb.Fatal("the demo carries no exposure")
	}

	return frame
}

// exposureFrame is benchFrame with the demo's exposure on it.
func exposureFrame(tb testing.TB) source.Frame {
	tb.Helper()

	frame := benchFrame()
	exposeFrame(tb, &frame, exposureDemo)

	return frame
}

// drawExposureCase draws one frame of frame with set applied, on pal, and
// returns the canvas.
func drawExposureCase(t *testing.T, frame source.Frame, set radar.Settings, pal theme.Palette) *canvas.Canvas {
	t.Helper()

	return drawExposureFiltered(t, frame, set, pal, 0)
}

// drawExposureFiltered is drawExposureCase with the filter moved on filters
// presses of f first.
func drawExposureFiltered(
	t *testing.T, frame source.Frame, set radar.Settings, pal theme.Palette, filters int,
) *canvas.Canvas {
	t.Helper()

	scene, canv := benchSceneWith(t, frame)
	scene.Apply(set)
	scene.SetPalette(pal)
	pressFilter(t, scene, filters)
	scene.Draw(canv, 0)

	return canv
}

// differing counts the pixels two canvases of the same size disagree on.
func differing(one, other *canvas.Canvas) int {
	count := 0
	first, second := one.Image(), other.Image()
	bounds := first.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if first.RGBAAt(x, y) != second.RGBAAt(x, y) {
				count++
			}
		}
	}

	return count
}

// TestExposureToggleTakesOnlyTheTracks checks the setting in each of the four
// views: off, the picture is byte for byte the one a frame with no airfield
// draws, and on, it differs, which is the exposure and nothing else.
//
// The shore-off cases take the paths where the exposure is drawn on its own,
// and the narrow range the ones where tracks run off the ring, the reach, or
// behind the camera.
func TestExposureToggleTakesOnlyTheTracks(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		set     radar.Settings
		filters int
	}{
		{name: "full scope", set: radar.Settings{View: radar.ViewScope}},
		{name: "full scope without the shore", set: radar.Settings{View: radar.ViewScope, Shore: radar.ToggleOff}},
		{name: "full scope at ten miles", set: radar.Settings{View: radar.ViewScope, RangeNm: 10}},
		{name: "flat bare view", set: radar.Settings{View: radar.ViewMinimal}},
		{name: "flat bare view over its shore", set: radar.Settings{View: radar.ViewMinimal, MinimalShore: true}},
		{name: "full 3D", set: radar.Settings{View: radar.View3D}},
		{name: "full 3D without the shore", set: radar.Settings{View: radar.View3D, Shore: radar.ToggleOff}},
		{name: "full 3D at ten miles", set: radar.Settings{View: radar.View3D, RangeNm: 10}},
		{name: "bare 3D", set: radar.Settings{View: radar.ViewMinimal3D}},
		{name: "bare 3D at ten miles", set: radar.Settings{View: radar.ViewMinimal3D, RangeNm: 10}},
		{name: "full scope in the low band", set: radar.Settings{View: radar.ViewScope}, filters: pressFilterLow},
		{
			name: "bare 3D over the high band", set: radar.Settings{View: radar.ViewMinimal3D},
			filters: pressFilterHigh,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := exposureFrame(t)
			unplaced := frame
			unplaced.Corridors = source.Corridors{}

			off := testCase.set
			off.Corridors = radar.ToggleOff

			// Both sides of each comparison carry the same setting, because
			// the flat views' U cap shows it and would otherwise be the
			// difference counted.
			without := drawExposureFiltered(t, unplaced, testCase.set, theme.Night, testCase.filters)
			withoutOff := drawExposureFiltered(t, unplaced, off, theme.Night, testCase.filters)
			hidden := drawExposureFiltered(t, frame, off, theme.Night, testCase.filters)
			exposed := drawExposureFiltered(t, frame, testCase.set, theme.Night, testCase.filters)

			if got := differing(withoutOff, hidden); got != 0 {
				t.Errorf("with the exposure off, %d pixels differ from a frame with no airfield, want 0", got)
			}

			if differing(without, exposed) == 0 {
				t.Error("with the exposure on, the picture is the one a frame with no airfield draws")
			}
		})
	}
}

// TestExposureOnTheDayGround checks the rule works on a light field: the
// tracks fade towards the day palette's own paper, so they still show.
func TestExposureOnTheDayGround(t *testing.T) {
	t.Parallel()

	frame := exposureFrame(t)
	unplaced := frame
	unplaced.Corridors = source.Corridors{}

	set := radar.Settings{View: radar.ViewScope}
	if differing(drawExposureCase(t, unplaced, set, theme.Day), drawExposureCase(t, frame, set, theme.Day)) == 0 {
		t.Error("the exposure drew nothing on the day palette")
	}
}

// TestExposureFollowsTheFilter checks the filter reaches the exposure in each
// of the four views: the tracks in the low band are some exposure, and not the
// exposure every band makes.
//
// Each exposure is measured against the same scene with it off, so the fleet,
// which the filter thins as well, is drawn alike on both sides of each
// comparison and what is counted is the tracks.
func TestExposureFollowsTheFilter(t *testing.T) {
	t.Parallel()

	for _, view := range []radar.View{radar.ViewScope, radar.ViewMinimal, radar.View3D, radar.ViewMinimal3D} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			frame := exposureFrame(t)
			exposed := func(filters int) int {
				set := radar.Settings{View: view}
				off := set
				off.Corridors = radar.ToggleOff

				return differing(
					drawExposureFiltered(t, frame, off, theme.Night, filters),
					drawExposureFiltered(t, frame, set, theme.Night, filters))
			}

			every := exposed(0)
			low := exposed(pressFilterLow)

			if low == 0 {
				t.Error("in the low band no exposure was drawn at all")
			}

			if low == every {
				t.Errorf("in the low band the exposure covered %d pixels, the same as every band", low)
			}
		})
	}
}

// TestTiltedViewsDrawNoHairlines checks a lost track outside every tube adds
// nothing to either tilted view, where the flat scope draws it as a hairline.
func TestTiltedViewsDrawNoHairlines(t *testing.T) {
	t.Parallel()

	frame := exposureFrame(t)
	frame.Routes = nil

	loose := make([]source.Trail, len(frame.Ghosts))
	copy(loose, frame.Ghosts)

	for index := range loose {
		loose[index].Tubed = false
	}

	frame.Ghosts = loose

	for _, testCase := range []struct {
		view  radar.View
		drawn bool
	}{
		{view: radar.ViewScope, drawn: true},
		{view: radar.View3D},
		{view: radar.ViewMinimal3D},
	} {
		t.Run(string(testCase.view), func(t *testing.T) {
			t.Parallel()

			set := radar.Settings{View: testCase.view, Shore: radar.ToggleOff}
			off := set
			off.Corridors = radar.ToggleOff

			// The flat scope's U cap shows the setting, so its comparison is
			// against the same setting on a frame with no airfield.
			unplaced := frame
			unplaced.Corridors = source.Corridors{}
			bare := drawExposureCase(t, unplaced, set, theme.Night)

			if !testCase.drawn {
				bare = drawExposureCase(t, frame, off, theme.Night)
			}

			got := differing(bare, drawExposureCase(t, frame, set, theme.Night))
			if (got > 0) != testCase.drawn {
				t.Errorf("the lost tracks changed %d pixels, want drawn %v", got, testCase.drawn)
			}
		})
	}
}
