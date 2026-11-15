package radar_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// coastSettings is the bare flat view at a pinned range, where nothing but
// the traffic and the receiver's marker is drawn, so a pixel count is a count
// of aircraft.
func coastSettings() radar.Settings {
	return radar.Settings{View: radar.ViewMinimal, RangeNm: sceneRangeNm}
}

// highPlane is an aircraft in the high band, which the night palette paints
// in AltHigh. Nothing else on the bare field uses it: AltLow repeats OK, which
// is the receiver marker's colour.
func highPlane() airplane.Snapshot {
	return scenePlane("484AC1", "KLM123", 45, 12, 36000, 41)
}

// lostTrail is an aircraft as the ghost ring hands it over, lost ago before
// the scene's clock.
func lostTrail(plane airplane.Snapshot, ago time.Duration) source.Trail {
	return source.Trail{
		ICAO:      plane.ICAO,
		Callsign:  plane.Callsign,
		Altitude:  plane.Altitude,
		Points:    plane.PositionHistory,
		LostAt:    sceneClock.Add(-ago),
		Latitude:  plane.Latitude,
		Longitude: plane.Longitude,
		Heading:   plane.Heading,
		VertRate:  plane.VertRate,
	}
}

// How many presses of f it takes to reach each band filter from ALL, in
// altitude mode.
const (
	pressFilterLow  = 1
	pressFilterMid  = 2
	pressFilterHigh = 3
)

// pressFilter moves the filter on times presses of f.
func pressFilter(tb testing.TB, scene *radar.Scene, times int) {
	tb.Helper()

	for step := range times {
		if !press(scene, 'f') {
			tb.Fatalf("press %d of f was not handled, want the filter key to take it", step+1)
		}
	}
}

// coastFrame is a frame with nothing heard and the given trails lost.
func coastFrame(trails ...source.Trail) source.Frame {
	frame := sceneFrame()
	frame.Ghosts = trails

	return frame
}

// drawOnce draws one frame of a scene built over frame and set, with the
// trail mode moved on presses times.
func drawOnce(t *testing.T, frame source.Frame, set radar.Settings, presses int) *canvas.Canvas {
	t.Helper()

	return drawFiltered(t, frame, set, presses, 0)
}

// drawFiltered is drawOnce with the filter moved on filters presses of f as
// well.
func drawFiltered(t *testing.T, frame source.Frame, set radar.Settings, presses, filters int) *canvas.Canvas {
	t.Helper()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(set)
	pressTrails(t, scene, presses)
	pressFilter(t, scene, filters)
	scene.Draw(canv, 0)

	return canv
}

// TestCoastingAircraftStaysOnTheField checks that an aircraft lost under a
// minute ago is still drawn in every trail mode, and that one lost longer ago
// than that is a ghost again: drawn by the mode that draws ghosts and by no
// other.
func TestCoastingAircraftStaysOnTheField(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		ago     time.Duration
		presses int
		drawn   bool
	}{
		{name: "just lost, long trails", ago: 0, drawn: true},
		{name: "half a minute, trails off", ago: 30 * time.Second, presses: pressOff, drawn: true},
		{name: "half a minute, short trails", ago: 30 * time.Second, presses: pressShort, drawn: true},
		{name: "past the coast, long trails", ago: time.Minute},
		{name: "past the coast, all trails", ago: time.Minute, presses: pressAll, drawn: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			empty := drawOnce(t, sceneFrame(), coastSettings(), testCase.presses)
			got := drawOnce(t, coastFrame(lostTrail(highPlane(), testCase.ago)), coastSettings(), testCase.presses)

			if drawn := !identical(got, empty); drawn != testCase.drawn {
				t.Errorf("lost %s ago, something drawn = %v, want %v", testCase.ago, drawn, testCase.drawn)
			}
		})
	}
}

// TestCoastingAircraftFades checks the fade itself: at the moment of loss the
// aircraft is drawn in its own colour, half a minute later at half of it, and
// the colour is mixed once rather than blended per pixel, so the faded ink is
// one exact value on the canvas.
func TestCoastingAircraftFades(t *testing.T) {
	t.Parallel()

	fresh := drawOnce(t, coastFrame(lostTrail(highPlane(), 0)), coastSettings(), pressOff)
	if countColour(fresh, fresh.Bounds(), theme.Night.AltHigh) == 0 {
		t.Error("an aircraft lost this instant drew no full-strength pixels, want its own colour")
	}

	half := drawOnce(t, coastFrame(lostTrail(highPlane(), 30*time.Second)), coastSettings(), pressOff)

	if got := countColour(half, half.Bounds(), theme.Night.AltHigh); got != 0 {
		t.Errorf("half a minute after the loss drew %d full-strength pixels, want none", got)
	}

	faded := fadeInto(theme.Night.Field, theme.Night.AltHigh, 0.5)
	if countColour(half, half.Bounds(), faded) == 0 {
		t.Error("half a minute after the loss drew nothing at half strength")
	}
}

// TestCoastingAircraftKeepsTheFilter checks that a coasting aircraft is
// filtered on what it was last heard with, the way it was while it was heard.
func TestCoastingAircraftKeepsTheFilter(t *testing.T) {
	t.Parallel()

	set := coastSettings()

	empty := drawFiltered(t, sceneFrame(), set, 0, pressFilterLow)
	got := drawFiltered(t, coastFrame(lostTrail(highPlane(), 0)), set, 0, pressFilterLow)

	if !identical(got, empty) {
		t.Error("a high aircraft coasting under the low-band filter was drawn, want it hidden")
	}
}

// TestCoastingAircraftCarriesNoLights checks that the lamps go out with the
// radio: the live aircraft carries its red port light, and the same aircraft
// coasting at full strength does not.
func TestCoastingAircraftCarriesNoLights(t *testing.T) {
	t.Parallel()

	set := coastSettings()
	set.Lights = true

	live := drawOnce(t, sceneFrame(highPlane()), set, pressOff)
	if countColour(live, live.Bounds(), theme.Night.Warn) == 0 {
		t.Fatal("the live aircraft drew no port light, so the test proves nothing")
	}

	lost := drawOnce(t, coastFrame(lostTrail(highPlane(), 0)), set, pressOff)
	if got := countColour(lost, lost.Bounds(), theme.Night.Warn); got != 0 {
		t.Errorf("the coasting aircraft drew %d light pixels, want none", got)
	}
}

// TestCoastingAircraftInThe3DView checks the perspective path: the model and
// its trail are held for the minute, in both 3D views.
func TestCoastingAircraftInThe3DView(t *testing.T) {
	t.Parallel()

	for _, view := range []radar.View{radar.View3D, radar.ViewMinimal3D} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			set := radar.Settings{View: view, RangeNm: sceneRangeNm, Orbit: radar.ToggleOff}

			empty := drawOnce(t, sceneFrame(), set, 0)
			got := drawOnce(t, coastFrame(lostTrail(highPlane(), 10*time.Second)), set, 0)

			if identical(got, empty) {
				t.Error("a coasting aircraft drew nothing in the 3D view")
			}

			gone := drawOnce(t, coastFrame(lostTrail(highPlane(), time.Minute)), set, 0)
			if !identical(gone, empty) {
				t.Error("an aircraft past its coast was drawn in long trail mode")
			}
		})
	}
}

// TestNewAircraftFadeIn checks the fade-in: an aircraft that turns up after
// the first frame comes up from the field over two seconds, and the first
// frame itself draws everything at full strength whatever the clock says.
func TestNewAircraftFadeIn(t *testing.T) {
	t.Parallel()

	src := &fakeSource{frame: sceneFrame()}
	scene := radar.New(testFaces(t), src, scope.New(scope.WithCurrent(sceneRangeNm)))
	scene.Apply(coastSettings())
	pressTrails(t, scene, pressOff)

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene.Draw(canv, 0)

	src.frame = sceneFrame(highPlane())

	scene.Draw(canv, time.Second)
	scene.Draw(canv, 2*time.Second)

	if got := countColour(canv, canv.Bounds(), theme.Night.AltHigh); got != 0 {
		t.Errorf("a second after it arrived the aircraft drew %d full-strength pixels, want none", got)
	}

	if countColour(canv, canv.Bounds(), fadeInto(theme.Night.Field, theme.Night.AltHigh, 0.5)) == 0 {
		t.Error("a second after it arrived the aircraft was not at half strength")
	}

	scene.Draw(canv, 4*time.Second)

	if countColour(canv, canv.Bounds(), theme.Night.AltHigh) == 0 {
		t.Error("three seconds after it arrived the aircraft was still fading in")
	}

	// A scene whose first frame is drawn late still draws that frame whole.
	late, lateCanv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(highPlane()))
	late.Apply(coastSettings())
	pressTrails(t, late, pressOff)
	late.Draw(lateCanv, time.Hour)

	if countColour(lateCanv, lateCanv.Bounds(), theme.Night.AltHigh) == 0 {
		t.Error("the first frame drew a fading aircraft, want every aircraft at full strength")
	}
}

// TestBandHoldsInsideTheDeadBand walks an aircraft down and up through the
// high band's 25,000 ft floor and checks it switches only once it is 500 ft
// past the edge, in whichever direction it is going.
func TestBandHoldsInsideTheDeadBand(t *testing.T) {
	t.Parallel()

	src := &fakeSource{frame: sceneFrame()}
	scene := radar.New(testFaces(t), src, scope.New(scope.WithCurrent(sceneRangeNm)))
	scene.Apply(coastSettings())
	pressFilter(t, scene, pressFilterHigh)

	empty, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	// Past the hold the f presses opened the bare view's key bar for, so the
	// empty frame and the measured ones agree about the bar.
	scene.Draw(empty, 5*time.Second)

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	for step, testCase := range []struct {
		altitude float64
		shown    bool
	}{
		{altitude: 25100, shown: true},
		{altitude: 24700, shown: true},
		{altitude: 24400},
		{altitude: 25300},
		{altitude: 25600, shown: true},
	} {
		plane := scenePlane("484AC1", "KLM123", 45, 12, testCase.altitude, 41)
		src.frame = sceneFrame(plane)
		// Twice, the second past the fade-in a newly shown aircraft starts, so
		// a shown aircraft is not mistaken for a faint one.
		at := time.Duration(step+1) * 10 * time.Second
		scene.Draw(canv, at)
		scene.Draw(canv, at+3*time.Second)

		if shown := !identical(canv, empty); shown != testCase.shown {
			t.Errorf("step %d at %.0f ft: shown = %v, want %v", step, testCase.altitude, shown, testCase.shown)
		}
	}
}

// coastAllocKind is which of the three moving parts a TestDrawAllocations
// case prices.
type coastAllocKind int

const (
	coastAllocNone coastAllocKind = iota
	coastAllocCoasting
	coastAllocFadingIn
	coastAllocDeadBand
)

// hexFleet is the bench fleet with addresses the scene's table can key, which
// fleet's letters past F are not.
func hexFleet(altitude float64) airplanes.List {
	list := fleet(benchPlanes, benchHistory)
	for index := range list {
		list[index].ICAO = fmt.Sprintf("%06X", 0x400000+index)
		if altitude > 0 {
			list[index].Altitude = altitude
		}
	}

	return list
}

// coastAllocScene builds the scene for one of the coast cases, already moved
// into the state the case prices.
func coastAllocScene(tb testing.TB, kind coastAllocKind, set radar.Settings) (*radar.Scene, *canvas.Canvas) {
	tb.Helper()

	frame := benchFrame()
	frame.Planes = hexFleet(0)

	switch kind {
	case coastAllocCoasting:
		for _, plane := range hexFleet(0) {
			trail := lostTrail(plane, 20*time.Second)
			trail.LostAt = frame.Now.Add(-20 * time.Second)
			frame.Ghosts = append(frame.Ghosts, trail)
		}

		frame.Planes = nil
	case coastAllocFadingIn:
		frame.Planes = frame.Planes[:benchPlanes/2]
	case coastAllocDeadBand:
		frame.Planes = hexFleet(25100)
	case coastAllocNone:
		fallthrough
	default:
	}

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	src := &fakeSource{frame: frame}
	scene := radar.New(testFaces(tb), src, scope.New(scope.WithCurrent(60)))

	scene.Apply(set)

	if kind == coastAllocDeadBand {
		pressFilter(tb, scene, pressFilterHigh)
	}

	scene.Draw(canv, 0)

	switch kind {
	case coastAllocFadingIn:
		src.frame.Planes = hexFleet(0)

		scene.Draw(canv, coastAllocAt/2)
	case coastAllocDeadBand:
		src.frame.Planes = hexFleet(24800)
	case coastAllocNone, coastAllocCoasting:
		fallthrough
	default:
	}

	return scene, canv
}

// coastAllocAt is the render clock the fade-in case is measured at: a second
// after the newcomers' fade started, so every one of them is half drawn.
const coastAllocAt = 1500 * time.Millisecond
