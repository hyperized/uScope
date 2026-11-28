package radar

import (
	"image"
	"math"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The aircraft the caption tests place east of boardCorridors' field, in
// nautical miles, and how they fly there.
const (
	windowArrivalNm      = 12.0 // inside the arrival reach
	windowNearArrivalNm  = 8.0
	windowFarArrivalNm   = 16.0 // outside it
	windowDepartureNm    = 5.0  // inside the departure reach
	windowFarDepartureNm = 10.0 // outside it

	windowDescentFpm = -800
	windowClimbFpm   = 1500
	windowLowFt      = 3000
	windowWestDeg    = 270
	windowNorthDeg   = 0
)

// windowPlane is an aircraft nmEast of the field, heading west towards it.
// A descent below 10,000 ft inside 30 nm reads as an arrival and a climb as a
// departure, whatever the heading; level flight across the bearing is
// transit.
func windowPlane(icao, callsign string, nmEast, vertRate float64) airplane.Snapshot {
	lon := boardFieldLon + nmEast/(nmPerDegree*math.Cos(boardFieldLat*math.Pi/halfCircle))

	return airplane.Snapshot{
		ICAO:     icao,
		Callsign: callsign,
		Altitude: windowLowFt,
		Heading:  windowWestDeg,
		VertRate: vertRate,
		Latitude: boardFieldLat, Longitude: lon,
		PositionHistory: []airplane.PositionEntry{{Latitude: boardFieldLat, Longitude: lon, Altitude: windowLowFt}},
	}
}

// windowFrame is a frame over the field carrying these aircraft.
func windowFrame(planes ...airplane.Snapshot) source.Frame {
	return source.Frame{
		Planes: planes,
		Receiver: source.Receiver{
			Latitude: boardFieldLat, Longitude: boardFieldLon, HasFix: true, Mode: source.FixManual,
		},
		Corridors: boardCorridors(),
		Now:       time.Date(2026, time.November, 28, 14, 30, 0, 0, time.UTC),
	}
}

// windowScene is a scene in the bare 3D view with the overlay on, over a
// source whose frame a test can change between draws.
func windowScene(tb testing.TB, frame source.Frame) (*Scene, *stubSource) {
	tb.Helper()

	src := &stubSource{frame: frame}
	scene := New(layerTestFaces(tb), src, scope.New(scope.WithCurrent(waterProbeRangeNm)))
	scene.Apply(Settings{View: ViewMinimal3D, Window: true, Colour: ColourCorridor, RangeNm: waterProbeRangeNm})

	return scene, src
}

func TestApplyCarriesTheWindow(t *testing.T) {
	t.Parallel()

	scene, _ := windowScene(t, windowFrame())
	if !scene.overlay || !scene.windowShown() {
		t.Fatal("Apply(Window: true) did not put the overlay on the bare 3D view")
	}

	scene.Apply(Settings{View: View3D, Window: true})

	if scene.windowShown() {
		t.Error("the overlay is shown in the full 3D view")
	}

	scene.Apply(Settings{View: ViewMinimal3D})

	if scene.overlay {
		t.Error("Apply with Window false left the overlay on")
	}
}

func TestNewWindowLayoutScalesWithTheHeight(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		width, height int
		wantScale     int
		wantShadow    int
	}{
		{width: 400, height: 300, wantScale: 1, wantShadow: 1},
		{width: 1280, height: 720, wantScale: 2, wantShadow: 1},
		{width: 1920, height: 1080, wantScale: 3, wantShadow: 2},
		{width: 2560, height: 1440, wantScale: 4, wantShadow: 2},
		{width: 3840, height: 2160, wantScale: 6, wantShadow: 3},
	} {
		canv, err := canvas.New(testCase.width, testCase.height)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		lay := layout{dst: canv, scope: canv.Bounds()}
		win := newWindowLayout(&lay)
		margin := testCase.width / windowMarginDivisor

		if win.scale != testCase.wantScale || win.shadow != testCase.wantShadow {
			t.Errorf("%dx%d: scale %d shadow %d, want %d and %d", testCase.width, testCase.height,
				win.scale, win.shadow, testCase.wantScale, testCase.wantShadow)
		}

		if win.box != canv.Bounds().Inset(margin) {
			t.Errorf("%dx%d: box %v, want the canvas less %d", testCase.width, testCase.height, win.box, margin)
		}
	}
}

func TestCaptionCandidate(t *testing.T) {
	t.Parallel()

	arrival := windowPlane(lightICAO, "KLM1452", windowArrivalNm, windowDescentFpm)
	nearArrival := windowPlane("484AC2", "TRA6721", windowNearArrivalNm, windowDescentFpm)
	farArrival := windowPlane("484AC3", "KLM88", windowFarArrivalNm, windowDescentFpm)
	departure := windowPlane("484AC4", "EZY12", windowDepartureNm, windowClimbFpm)
	farDeparture := windowPlane("484AC5", "EZY13", windowFarDepartureNm, windowClimbFpm)
	transit := windowPlane("484AC6", "DLH4", windowDepartureNm, 0)
	transit.Heading = windowNorthDeg
	nowhere := windowPlane("484AC7", "DLH5", windowDepartureNm, windowClimbFpm)
	nowhere.Latitude, nowhere.Longitude = 0, 0

	for _, testCase := range []struct {
		name      string
		planes    []airplane.Snapshot
		wantICAO  string
		wantClass source.TrackClass
	}{
		{name: "nothing near", planes: []airplane.Snapshot{farArrival, farDeparture, transit, nowhere}},
		{
			name:     "the nearest arrival beats a nearer departure",
			planes:   []airplane.Snapshot{departure, arrival, nearArrival},
			wantICAO: nearArrival.ICAO, wantClass: source.TrackInbound,
		},
		{
			name:     "a departure when no arrival is in reach",
			planes:   []airplane.Snapshot{farArrival, departure, farDeparture},
			wantICAO: departure.ICAO, wantClass: source.TrackOutbound,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := windowFrame(testCase.planes...)
			scene, _ := windowScene(t, frame)

			pick := scene.captionCandidate(frame)
			if pick.found != (testCase.wantICAO != "") || pick.plane.ICAO != testCase.wantICAO ||
				pick.class != testCase.wantClass {
				t.Errorf("captionCandidate() = %q, %v, %v; want %q, %v", pick.plane.ICAO, pick.class, pick.found,
					testCase.wantICAO, testCase.wantClass)
			}
		})
	}
}

// TestCaptionHoldsAndFades walks one caption through its life: up whole on
// the first frame, held for captionHold after its aircraft has gone out of
// reach, faded out over captionFade, then replaced by the next aircraft,
// which fades in.
func TestCaptionHoldsAndFades(t *testing.T) {
	t.Parallel()

	first := windowPlane(lightICAO, "KLM1452", windowArrivalNm, windowDescentFpm)
	second := windowPlane("484AC2", "TRA6721", windowNearArrivalNm, windowDescentFpm)
	scene, src := windowScene(t, windowFrame(first))

	step := func(at time.Duration) {
		scene.followCaption(src.frame, at)
	}

	step(0)
	checkCaption(t, scene, "first frame", first.ICAO, 0, 1)

	away := first
	away.Longitude = windowPlane("", "", windowFarArrivalNm, 0).Longitude
	src.frame = windowFrame(away, second)

	step(captionHold / 2)
	checkCaption(t, scene, "inside the hold", first.ICAO, captionHold/2, 1)

	step(captionHold)
	checkCaption(t, scene, "the hold over", first.ICAO, captionHold, 1)

	step(captionHold + captionFade/2)
	checkCaption(t, scene, "half faded", first.ICAO, captionHold+captionFade/2, 0.5)

	step(captionHold + captionFade)
	checkCaption(t, scene, "the next one", second.ICAO, captionHold+captionFade, 0)

	step(captionHold + captionFade*3/2)
	checkCaption(t, scene, "the next one half up", second.ICAO, captionHold+captionFade*3/2, 0.5)
}

// TestCaptionLetsGo covers the other ways a caption ends: its aircraft
// leaving the frame, a departure giving way to an arrival, a fade out that
// starts before the fade in has finished, and a render clock that runs
// backwards.
func TestCaptionLetsGo(t *testing.T) {
	t.Parallel()

	arrival := windowPlane(lightICAO, "KLM1452", windowArrivalNm, windowDescentFpm)
	departure := windowPlane("484AC4", "EZY12", windowDepartureNm, windowClimbFpm)

	t.Run("the aircraft leaves the frame", func(t *testing.T) {
		t.Parallel()

		scene, src := windowScene(t, windowFrame(arrival))
		scene.followCaption(src.frame, 0)

		src.frame = windowFrame()
		scene.followCaption(src.frame, time.Second)
		checkCaption(t, scene, "gone", arrival.ICAO, time.Second, 1)

		if !scene.caption.leaving {
			t.Error("the caption is not leaving after its aircraft left the frame")
		}

		scene.followCaption(src.frame, 2*time.Second)

		if scene.caption.icao != "" {
			t.Errorf("caption still on %q after the fade", scene.caption.icao)
		}
	})

	t.Run("a departure gives way to an arrival after the hold", func(t *testing.T) {
		t.Parallel()

		scene, src := windowScene(t, windowFrame(departure))
		scene.followCaption(src.frame, 0)

		src.frame = windowFrame(departure, arrival)
		scene.followCaption(src.frame, time.Second)
		checkCaption(t, scene, "inside the hold", departure.ICAO, time.Second, 1)

		scene.followCaption(src.frame, captionHold)

		if !scene.caption.leaving {
			t.Error("the departure kept the caption with an arrival in reach")
		}
	})

	t.Run("an arrival keeps it while it still qualifies", func(t *testing.T) {
		t.Parallel()

		scene, src := windowScene(t, windowFrame(arrival))
		scene.followCaption(src.frame, 0)

		src.frame = windowFrame(arrival, departure)
		scene.followCaption(src.frame, 2*captionHold)
		checkCaption(t, scene, "still qualifying", arrival.ICAO, 2*captionHold, 1)
	})

	t.Run("going away during the fade in", func(t *testing.T) {
		t.Parallel()

		scene, src := windowScene(t, windowFrame())
		scene.followCaption(src.frame, 0)

		src.frame = windowFrame(arrival)
		scene.followCaption(src.frame, time.Second)

		src.frame = windowFrame()
		scene.followCaption(src.frame, time.Second+captionFade/4)
		checkCaption(t, scene, "leaving a quarter up", arrival.ICAO, time.Second+captionFade/4, 0.25)
	})

	t.Run("a clock that runs backwards", func(t *testing.T) {
		t.Parallel()

		scene, src := windowScene(t, windowFrame(arrival))
		scene.followCaption(src.frame, time.Minute)
		scene.followCaption(src.frame, time.Second)
		checkCaption(t, scene, "rewound", arrival.ICAO, time.Second, 1)
	})
}

// checkCaption fails unless the caption is on icao at the given alpha.
func checkCaption(t *testing.T, scene *Scene, step, icao string, moment time.Duration, alpha float64) {
	t.Helper()

	const tolerance = 1e-9

	if scene.caption.icao != icao {
		t.Fatalf("%s: caption on %q, want %q", step, scene.caption.icao, icao)
	}

	if got := scene.caption.alpha(moment); math.Abs(got-alpha) > tolerance {
		t.Errorf("%s: alpha %v, want %v", step, got, alpha)
	}
}

func TestCaptionDescribe(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, icao, callsign string
		altitudeFt           float64
		wantName, wantNumber string
		wantCallsign         string
		wantFeet             float64
	}{
		{
			name: "a known operator", icao: lightICAO, callsign: "KLM1452 ", altitudeFt: 2100,
			wantName: sampleICAO, wantNumber: "1452", wantCallsign: "KLM1452", wantFeet: 2100,
		},
		{
			name: "an unknown prefix", icao: lightICAO, callsign: windowUnknownCallsign,
			wantName: windowUnknownCallsign, wantCallsign: windowUnknownCallsign,
		},
		{
			name: "a bare designator", icao: lightICAO, callsign: sampleICAO,
			wantName: sampleICAO, wantCallsign: sampleICAO,
		},
		{name: "no callsign", icao: lightICAO, wantName: lightICAO, wantCallsign: lightICAO},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			plane := windowPlane(testCase.icao, testCase.callsign, windowDepartureNm, 0)
			plane.Altitude = testCase.altitudeFt

			var held caption

			held.describe(plane, boardCorridors().Field)

			if held.callsign != testCase.wantCallsign {
				t.Errorf("describe() callsign = %q, want %q", held.callsign, testCase.wantCallsign)
			}

			if held.name != testCase.wantName || held.number != testCase.wantNumber {
				t.Errorf("describe() = %q %q, want %q %q",
					held.name, held.number, testCase.wantName, testCase.wantNumber)
			}

			if held.altitudeFt != testCase.wantFeet {
				t.Errorf("describe() altitude = %v ft, want %v", held.altitudeFt, testCase.wantFeet)
			}

			if math.Abs(held.nm-windowDepartureNm) > 1e-6 {
				t.Errorf("describe() distance = %v NM, want %v", held.nm, windowDepartureNm)
			}
		})
	}
}

func TestCaptionFigures(t *testing.T) {
	t.Parallel()

	scene, _ := windowScene(t, windowFrame())

	for _, testCase := range []struct {
		name string
		got  func() []byte
		want string
	}{
		{name: "feet down to the nearest fifty", got: func() []byte { return scene.feetFigure(2424) }, want: "2,400"},
		{name: "feet up to the nearest fifty", got: func() []byte { return scene.feetFigure(675) }, want: "700"},
		{name: "tenths close in", got: func() []byte { return scene.nmFigure(4.24) }, want: "4.2"},
		{name: "whole miles further out", got: func() []byte { return scene.nmFigure(13.4) }, want: "13"},
	} {
		if got := string(testCase.got()); got != testCase.want {
			t.Errorf("%s: %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

// TestCaptionHeight checks the caption's height is the altitude in feet,
// and that a zero or negative altitude gives none.
func TestCaptionHeight(t *testing.T) {
	t.Parallel()

	var scene Scene

	for _, testCase := range []struct {
		altitudeFt float64
		known      bool
	}{
		{altitudeFt: 2400, known: true},
		{altitudeFt: 0},
		{altitudeFt: -100},
	} {
		got, known := scene.captionHeight(testCase.altitudeFt)
		if known != testCase.known || (known && got != testCase.altitudeFt) {
			t.Errorf("captionHeight(%v) = %v, %v, want the same feet, %v",
				testCase.altitudeFt, got, known, testCase.known)
		}
	}
}

func TestWindowPenWithNoFace(t *testing.T) {
	t.Parallel()

	pen := windowPen{scale: 2}
	if got := pen.height(); got != 0 {
		t.Errorf("height() with no face = %d, want 0", got)
	}
}

// TestDrawWindowWithNoField checks that a frame with no corridor airfield
// gets the clock and nothing else, and that it drops any caption held from an
// earlier frame.
func TestDrawWindowWithNoField(t *testing.T) {
	t.Parallel()

	const width, height = 1920, 1080

	scene, src := windowScene(t, windowFrame(windowPlane(lightICAO, "KLM1452", windowArrivalNm, windowDescentFpm)))
	canv := boardCanvas(t, width, height)
	scene.Draw(canv, 0)

	if scene.caption.icao == "" {
		t.Fatal("no caption with an arrival in reach")
	}

	src.frame.Corridors = source.Corridors{}

	canv.Clear(theme.Night.Field)

	lay := scene.newLayout(canv)
	lay.scope = canv.Bounds()
	scene.drawWindow(&lay, src.frame, time.Second)

	if scene.caption.icao != "" {
		t.Errorf("caption still on %q with no field", scene.caption.icao)
	}

	bottom := image.Rect(0, height/2, width, height)
	if got := inkIn(canv, bottom, theme.Night.Ink) + inkIn(canv, bottom, theme.Night.Data); got != 0 {
		t.Errorf("%d pixels of caption or count with no field", got)
	}

	if got := inkIn(canv, image.Rect(width/2, 0, width, height/4), theme.Night.Ink); got == 0 {
		t.Error("no clock with no field")
	}
}

// TestCaptionLeavesTheCountsRoom checks that the count stays on the frame
// whatever the caption says, a real operator's long name and a callsign too
// long to fit in any form alike.
func TestCaptionLeavesTheCountsRoom(t *testing.T) {
	t.Parallel()

	const width, height = 1920, 1080

	short := windowPlane("484AC4", "EZY12", windowDepartureNm, windowClimbFpm)
	named := short
	named.Callsign = windowLongCallsign
	long := short
	long.Callsign = "QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ"

	for _, testCase := range []struct {
		name  string
		plane airplane.Snapshot
	}{
		{name: "room for both", plane: short},
		{name: "a long operator name", plane: named},
		{name: "too long in any form", plane: long},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, _ := windowScene(t, windowFrame(testCase.plane))
			canv := boardCanvas(t, width, height)
			scene.Draw(canv, 0)

			// The arrivals' column is the only thing in the data colour on
			// the right of the bottom band: the caption is a departure.
			if inkIn(canv, image.Rect(width*2/3, height*3/4, width, height), theme.Night.Data) == 0 {
				t.Error("no count beside the caption")
			}
		})
	}
}

// windowUnknownCallsign has a prefix pkg/airlines does not know.
const windowUnknownCallsign = "ZZZ99"

// windowLongCallsign is a real operator whose name makes the caption's
// full form too long to sit beside the count at 1080 rows.
const windowLongCallsign = "SWR52CL"

// TestCaptionFit checks the caption's forms are tried longest first and the
// first that fits is kept: the full form, the callsign in place of the name,
// then the height without the distance, which is kept even when nothing fits.
func TestCaptionFit(t *testing.T) {
	t.Parallel()

	plane := windowPlane(lightICAO, windowLongCallsign, windowArrivalNm, windowDescentFpm)
	scene, _ := windowScene(t, windowFrame(plane))
	canv := boardCanvas(t, 1920, 1080)
	scene.Draw(canv, 0)

	if scene.caption.name != "Swiss International Air Lines" || scene.caption.callsign != windowLongCallsign {
		t.Fatalf("caption on %q %q, want the operator and the callsign", scene.caption.name, scene.caption.callsign)
	}

	lay := scene.newLayout(canv)
	lay.scope = canv.Bounds()
	win := newWindowLayout(&lay)
	line := scene.windowPen(&lay, &win, scene.faces.BodyBold, 0)
	line.x = win.box.Min.X

	widths := [3]int{}
	for form := formFull; form <= formNoDistance; form++ {
		widths[form] = scene.captionLine(&line, 1, form, false)
	}

	if widths[formFull] <= widths[formCallsign] || widths[formCallsign] <= widths[formNoDistance] {
		t.Fatalf("forms end at %v, want each shorter than the one before", widths)
	}

	for _, testCase := range []struct {
		name string
		room int
		want captionForm
	}{
		{name: "room for the full form", room: widths[formFull], want: formFull},
		{name: "a pixel short of it", room: widths[formFull] - 1, want: formCallsign},
		{name: "room for the callsign", room: widths[formCallsign], want: formCallsign},
		{name: "a pixel short of that", room: widths[formCallsign] - 1, want: formNoDistance},
		{name: "no room at all", room: 0, want: formNoDistance},
	} {
		if got := scene.captionFit(&line, testCase.room); got != testCase.want {
			t.Errorf("%s: form %d, want %d", testCase.name, got, testCase.want)
		}
	}

	if line.x != win.box.Min.X || !line.skip {
		t.Errorf("captionFit moved the pen to %d or set skip %v", line.x, line.skip)
	}
}

// TestCaptionRing checks the ring around the captioned aircraft: drawn in
// the accent ink centred on the model while the caption is up, blended while
// it fades, and absent with no caption, no 3D view to draw through, or no
// position to put it at.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func TestCaptionRing(t *testing.T) {
	t.Parallel()

	const width, height = 1920, 1080

	plane := windowPlane(lightICAO, "KLM1452", windowArrivalNm, windowDescentFpm)
	scene, _ := windowScene(t, windowFrame(plane))
	canv := boardCanvas(t, width, height)
	scene.Draw(canv, 0)

	shot := scene.captionView
	if !shot.ok {
		t.Fatal("the 3D view kept nothing to ring through")
	}

	x, y, ok := shot.view.project(plane.Latitude, plane.Longitude, plane.Altitude)
	if !ok {
		t.Fatal("the arrival is not on the picture")
	}

	radius := int(shot.view.cam.modelSpan * captionRingShare)
	around := image.Rect(x-radius-2, y-radius-2, x+radius+3, y+radius+3)

	if got := inkIn(canv, around, theme.Night.Accent); got < radius {
		t.Errorf("%d accent pixels around the arrival, want a ring of radius %d", got, radius)
	}

	if got := inkIn(canv, canv.Bounds(), theme.Night.Accent) - inkIn(canv, around, theme.Night.Accent); got != 0 {
		t.Errorf("%d accent pixels away from the arrival", got)
	}

	for _, testCase := range []struct {
		name   string
		change func(held *caption, view *captionView)
		want   bool
	}{
		{name: "held", change: func(*caption, *captionView) {}, want: true},
		{name: "no caption", change: func(held *caption, _ *captionView) { held.icao = "" }},
		{name: "no view", change: func(_ *caption, view *captionView) { view.ok = false }},
		{name: "no position", change: func(held *caption, _ *captionView) { held.latitude, held.longitude = 0, 0 }},
		{name: "faded out", change: func(held *caption, _ *captionView) { held.shownAt = time.Hour }},
	} {
		clean := boardCanvas(t, width, height)
		held, view := scene.caption, shot
		view.dst = clean

		testCase.change(&held, &view)

		probe := &Scene{caption: held, captionView: view, pal: scene.pal}
		probe.drawCaptionRing(0)

		if got := inkIn(clean, around, theme.Night.Field) < around.Dx()*around.Dy(); got != testCase.want {
			t.Errorf("%s: ring drawn %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// TestCaptionRingFades checks the ring follows the caption's alpha: at half
// way up it is drawn, but blended, so no pixel is the full accent ink.
func TestCaptionRingFades(t *testing.T) {
	t.Parallel()

	plane := windowPlane(lightICAO, "KLM1452", windowArrivalNm, windowDescentFpm)
	scene, _ := windowScene(t, windowFrame(plane))
	canv := boardCanvas(t, 1920, 1080)
	scene.Draw(canv, 0)

	clean := boardCanvas(t, 1920, 1080)
	scene.captionView.dst = clean
	scene.caption.shownAt = 0
	scene.drawCaptionRing(captionFade / 2)

	if got := inkIn(clean, clean.Bounds(), theme.Night.Accent); got != 0 {
		t.Errorf("%d pixels of full accent half way through the fade", got)
	}

	if got := inkIn(clean, clean.Bounds(), theme.Night.Field); got == 1920*1080 {
		t.Error("no ring half way through the fade")
	}
}

// TestCaptionRingClipped checks a ring on an aircraft at the edge of the
// picture is cut off by the 3D view's window rather than drawn past it.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func TestCaptionRingClipped(t *testing.T) {
	t.Parallel()

	plane := windowPlane(lightICAO, "KLM1452", windowArrivalNm, windowDescentFpm)
	scene, _ := windowScene(t, windowFrame(plane))
	canv := boardCanvas(t, 1920, 1080)
	scene.Draw(canv, 0)

	x, y, _ := scene.captionView.view.project(plane.Latitude, plane.Longitude, plane.Altitude)

	whole := boardCanvas(t, 1920, 1080)

	window, ok := whole.Sub(image.Rect(0, 0, x, 1080))
	if !ok {
		t.Fatal("no room left of the arrival")
	}

	scene.captionView.dst = window
	scene.drawCaptionRing(time.Hour)

	if got := inkIn(whole, image.Rect(x, 0, 1920, 1080), theme.Night.Accent); got != 0 {
		t.Errorf("%d ring pixels past the window's edge", got)
	}

	if got := inkIn(whole, image.Rect(0, y-30, x, y+30), theme.Night.Accent); got == 0 {
		t.Error("no ring inside the window")
	}
}
