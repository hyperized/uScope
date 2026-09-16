package radar_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/adsb"
	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
	"github.com/hyperized/uScope/pkg/sprite"
)

// The scope range these tests run at, wide enough to hold every aircraft the
// fixtures below put on the field.
const sceneRangeNm = 60

// fieldProbeX and fieldProbeY are a pixel that is bare field in every view:
// the bottom-left corner, which is inside the layout margin under the key bar.
// The top-left corner used to serve for this and no longer can, because the
// header band bleeds into it.
const (
	fieldProbeX = 0
	fieldProbeY = panelHeight - 1
)

// sceneClock is a fixed instant, so a header drawn twice is drawn the same.
//
//nolint:gochecknoglobals // a fixed clock is data, and time.Time cannot be const.
var sceneClock = time.Date(2026, time.September, 15, 21, 30, 0, 0, time.UTC)

// scenePlane builds one aircraft at a bearing and distance from the receiver,
// with a short trail behind it along its own track.
//
// The positions are worked out the same way internal/radar projects them, on a
// local flat-earth approximation, so a plane placed 10 nm out lands 10 nm out
// on the scope rather than somewhere the test has to guess at.
func scenePlane(icao, callsign string, bearing, distanceNm, altitude, heading float64) airplane.Snapshot {
	const (
		nmPerDegree = 60.0
		halfCircle  = 180.0
		trailFixes  = 6
		fixSpacing  = 1.5
	)

	radians := bearing * math.Pi / halfCircle
	lat := receiverLat + distanceNm*math.Cos(radians)/nmPerDegree
	lon := receiverLon + distanceNm*math.Sin(radians)/(nmPerDegree*math.Cos(receiverLat*math.Pi/halfCircle))

	trail := make([]airplane.PositionEntry, 0, trailFixes)
	for fix := range trailFixes {
		back := float64(trailFixes-fix) * fixSpacing
		trail = append(trail, airplane.PositionEntry{
			Latitude:  lat - back/nmPerDegree,
			Longitude: lon - back/nmPerDegree,
			Altitude:  altitude,
		})
	}

	return airplane.Snapshot{
		ICAO:            icao,
		Callsign:        callsign,
		Altitude:        altitude,
		Heading:         heading,
		Velocity:        420,
		Latitude:        lat,
		Longitude:       lon,
		LastUpdate:      sceneClock,
		Squawk:          "1000",
		MessageCount:    12,
		PositionHistory: trail,
	}
}

// denseTrail rebuilds an aircraft's history with more fixes over the same
// ground it already covers.
//
// It exists because scenePlane gives an aircraft six fixes, and the short
// trail mode keeps twelve: with a history that short the two modes draw the
// identical line, so a test could not tell them apart. Filling the same run
// with thirty fixes leaves the aeroplane and the head of its track exactly
// where they were, and gives the short mode a window it can actually cut
// something out of.
func denseTrail(plane airplane.Snapshot, fixes int) airplane.Snapshot {
	history := plane.PositionHistory
	first, last := history[0], history[len(history)-1]
	span := float64(fixes - 1)

	dense := make([]airplane.PositionEntry, 0, fixes)

	for index := range fixes {
		along := float64(index) / span
		dense = append(dense, airplane.PositionEntry{
			Latitude:  first.Latitude + (last.Latitude-first.Latitude)*along,
			Longitude: first.Longitude + (last.Longitude-first.Longitude)*along,
			Altitude:  first.Altitude,
		})
	}

	plane.PositionHistory = dense

	return plane
}

// sceneFrame wraps a list of aircraft in a frame with a known receiver.
func sceneFrame(planes ...airplane.Snapshot) source.Frame {
	return source.Frame{
		Planes: planes,
		Receiver: source.Receiver{
			Latitude:  receiverLat,
			Longitude: receiverLon,
			HasFix:    true,
			Label:     source.LabelManual,
			Mode:      source.FixManual,
		},
		Source: adsb.SourceInfo{Label: "DEMO", Connected: true},
		Stats:  adsb.Stats{TotalFrames: 512},
		Now:    sceneClock,
	}
}

// sceneOn builds a scene over a frame, plus the canvas it draws on.
//
// Every test that draws needs its own pair: a Scene carries the selection, the
// trail toggle and the scratch buffers it formats numbers into, so two
// parallel subtests sharing one would race.
func sceneOn(tb testing.TB, width, height int, frame source.Frame, opts ...radar.Option) (
	*radar.Scene, *canvas.Canvas, *scope.Scope,
) {
	tb.Helper()

	canv, err := canvas.New(width, height)
	if err != nil {
		tb.Fatalf("canvas.New(%d, %d): %v", width, height, err)
	}

	ranges := scope.New(scope.WithCurrent(sceneRangeNm))
	scene := radar.New(testFaces(tb), &fakeSource{frame: frame}, ranges, opts...)

	return scene, canv, ranges
}

// painted counts the pixels that are not the night palette's field colour,
// which is how these tests ask "did anything get drawn here" without pinning
// an exact picture.
func painted(canv *canvas.Canvas, box image.Rectangle) int {
	area := box.Intersect(canv.Bounds())
	count := 0

	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) != theme.Night.Field {
				count++
			}
		}
	}

	return count
}

// countColour counts the pixels in box that are exactly col, which is how a
// few of the tests below confirm a specific ink was used without pinning a
// whole picture.
func countColour(canv *canvas.Canvas, box image.Rectangle, col color.RGBA) int {
	area := box.Intersect(canv.Bounds())
	count := 0

	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) == col {
				count++
			}
		}
	}

	return count
}

// identical reports whether two canvases hold the same picture.
func identical(first, second *canvas.Canvas) bool {
	if first.Bounds() != second.Bounds() {
		return false
	}

	return identicalIn(first, second, first.Bounds())
}

// identicalIn is identical over one rectangle, for the tests that only care
// about the scope and not about the card beside it.
func identicalIn(first, second *canvas.Canvas, box image.Rectangle) bool {
	area := box.Intersect(first.Bounds()).Intersect(second.Bounds())

	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if first.Image().RGBAAt(x, y) != second.Image().RGBAAt(x, y) {
				return false
			}
		}
	}

	return true
}

// scopeBox is the square the rings and the aircraft are drawn in at the
// panel's resolution, worked out the way internal/radar works it out: the
// frame less its margin, less the header band and the key bar.
//
//nolint:gochecknoglobals // a rectangle is data, and image.Rectangle cannot be const.
var scopeBox = image.Rect(16, 77, 605, 666)

// press sends a rune through Handle.
func press(scene *radar.Scene, value rune) bool {
	return scene.Handle(input.Key{Kind: input.Rune, Rune: value})
}

// How many presses of t it takes to reach each trail mode from the one a
// scene starts in.
//
// The mode is unexported and no Setting carries it, so the key is the only
// way in from outside the package, which is also the only way an operator has.
// A test that reaches the mode the way the operator does is a test of the
// thing that ships.
const (
	pressAll   = 1
	pressOff   = 2
	pressShort = 3
)

// pressTrails cycles the trail mode with the t key and fails the test if the
// scene ever declines the key.
func pressTrails(tb testing.TB, scene *radar.Scene, times int) {
	tb.Helper()

	for step := range times {
		if !press(scene, 't') {
			tb.Fatalf("press %d of t was not handled, want the trail key to take it", step+1)
		}
	}
}

// How many presses of v it takes to reach each view from the scope, which is
// where a scene starts.
//
// --view carries every one of them, so a Settings block would get there in one
// step. The tests below that use these are about the key rather than about the
// view, and walking the cycle is what an operator does.
const (
	pressView3D        = 1
	pressViewMinimal   = 2
	pressViewMinimal3D = 3
	pressViewRound     = 4
)

// pressWide hides the right column with the w key when a case asks for it, and
// fails the test if the scene declines the key.
//
// It takes the flag rather than a count because there is nothing to cycle: one
// press hides the column and the next puts it back, so a case either wants it
// gone or does not.
//
//nolint:revive // flag-parameter: hidden is a case's own answer, not a mode to branch deeper on.
func pressWide(tb testing.TB, scene *radar.Scene, hidden bool) {
	tb.Helper()

	if !hidden {
		return
	}

	if !press(scene, 'w') {
		tb.Fatal("the w key was not handled, want the view to take it")
	}
}

// pressView cycles the view with the v key and fails the test if the scene
// ever declines the key.
func pressView(tb testing.TB, scene *radar.Scene, times int) {
	tb.Helper()

	for step := range times {
		if !press(scene, 'v') {
			tb.Fatalf("press %d of v was not handled, want the view key to take it", step+1)
		}
	}
}

func TestDrawAtEverySize(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	)

	for _, testCase := range []struct {
		name          string
		width, height int
		wantPainted   bool
	}{
		{name: "the panel", width: panelWidth, height: panelHeight, wantPainted: true},
		{name: "half the panel", width: 640, height: 480, wantPainted: true},
		{name: "one pixel under the column threshold", width: 639, height: 480, wantPainted: true},
		{name: "small but still labelled", width: 480, height: 320, wantPainted: true},
		{name: "one pixel under the label threshold", width: 480, height: 319, wantPainted: true},
		{name: "a quarter of the panel", width: 320, height: 240, wantPainted: true},
		{name: "an eighty by twenty-four terminal of half blocks", width: 80, height: 48, wantPainted: true},

		// The four sizes below each take one block away. They are picked from
		// the arithmetic rather than by eye, so a change to any of the spacing
		// constants will move them and the coverage will say so.
		{name: "too short for the card", width: panelWidth, height: 360, wantPainted: true},
		{name: "room for the card but not one row", width: panelWidth, height: 380, wantPainted: true},
		{name: "a column too narrow for the legend", width: 640, height: 712, wantPainted: true},
		{name: "the key bar eats the whole frame", width: 400, height: 40, wantPainted: true},

		{name: "nothing fits at all", width: 8, height: 8, wantPainted: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, _ := sceneOn(t, testCase.width, testCase.height, frame)
			scene.Draw(canv, 0)

			got := painted(canv, canv.Bounds()) > 0
			if got != testCase.wantPainted {
				t.Errorf("anything drawn = %v, want %v", got, testCase.wantPainted)
			}
		})
	}
}

// TestDrawDropsTheColumnAndTheLabels checks the two responsive thresholds by
// what they leave behind rather than by reading a flag off the scene.
func TestDrawDropsTheColumnAndTheLabels(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	t.Run("the right column goes below 640 pixels wide", func(t *testing.T) {
		t.Parallel()

		wide, wideCanvas, _ := sceneOn(t, 640, 480, frame)
		wide.Draw(wideCanvas, 0)

		narrow, narrowCanvas, _ := sceneOn(t, 639, 480, frame)
		narrow.Draw(narrowCanvas, 0)

		// The column lives to the right of the square scope, which the
		// narrower canvas re-centres into the space the column left: at 639
		// pixels the circle itself reaches past x=494, so the probe starts
		// well clear of it and sits inside the column's own bounds at 640,
		// where nothing else on screen reaches.
		right := image.Rect(500, 90, 620, 420)
		if painted(narrowCanvas, right) >= painted(wideCanvas, right) {
			t.Error("the right half is no emptier at 639 pixels than at 640, so the column did not go")
		}
	})

	t.Run("the labels go below 320 pixels tall", func(t *testing.T) {
		t.Parallel()

		tall, tallCanvas, _ := sceneOn(t, 480, 320, frame)
		tall.Draw(tallCanvas, 0)

		short, shortCanvas, _ := sceneOn(t, 480, 319, frame)
		short.Draw(shortCanvas, 0)

		if painted(shortCanvas, shortCanvas.Bounds()) >= painted(tallCanvas, tallCanvas.Bounds()) {
			t.Error("one pixel of height dropped nothing, so the labels are still being drawn")
		}
	})
}

// TestDrawWithAMissingFace covers the guards that let a block skip itself. A
// nil face is not an error: the scene has to survive a canvas too small for
// the font it wanted, and that is the same code path.
func TestDrawWithAMissingFace(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	for _, testCase := range []struct {
		name  string
		blank func(*radar.Faces)
	}{
		{name: "no small face", blank: func(f *radar.Faces) { f.Small = nil }},
		{name: "no body face", blank: func(f *radar.Faces) { f.Body = nil }},
		{name: "no bold face", blank: func(f *radar.Faces) { f.BodyBold = nil }},
		{name: "no large face", blank: func(f *radar.Faces) { f.Large = nil }},
		{name: "no faces at all", blank: func(f *radar.Faces) { *f = radar.Faces{} }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			faces := testFaces(t)
			testCase.blank(&faces)

			canv, err := canvas.New(panelWidth, panelHeight)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			scene := radar.New(faces, &fakeSource{frame: frame}, scope.New(scope.WithCurrent(sceneRangeNm)))

			// The assertion is that this returns at all. A block that reached
			// for a face it does not have would panic here.
			scene.Draw(canv, 0)
		})
	}
}

func TestDrawReceiverStates(t *testing.T) {
	t.Parallel()

	plane := scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)

	for _, testCase := range []struct {
		name        string
		receiver    source.Receiver
		wantPlotted bool
	}{
		{
			name: "a position the operator gave",
			receiver: source.Receiver{
				Latitude: receiverLat, Longitude: receiverLon, HasFix: true, Label: source.LabelManual,
			},
			wantPlotted: true,
		},
		{
			name: "a self-locate estimate",
			receiver: source.Receiver{
				Latitude: receiverLat, Longitude: receiverLon, ConfidenceNm: 22, Label: source.LabelEstimate,
			},
			wantPlotted: true,
		},
		{
			name:     "nothing known yet",
			receiver: source.Receiver{Label: source.LabelNone},
		},
		{
			// Null island is uAirwaves' "no position" sentinel rather than a
			// buoy in the Gulf of Guinea, so nothing may be plotted against it.
			name:     "a receiver at null island",
			receiver: source.Receiver{HasFix: true, Label: source.LabelGPS},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Two frames with the same receiver, one holding the aircraft and
			// one empty. The rings are drawn either way, so comparing the two
			// scopes is what isolates "was anything plotted" from "was the
			// scope drawn at all".
			withPlane := sceneFrame(plane)
			withPlane.Receiver = testCase.receiver

			empty := sceneFrame()
			empty.Receiver = testCase.receiver

			loaded, loadedCanvas, _ := sceneOn(t, panelWidth, panelHeight, withPlane)
			loaded.Draw(loadedCanvas, 0)

			bare, bareCanvas, _ := sceneOn(t, panelWidth, panelHeight, empty)
			bare.Draw(bareCanvas, 0)

			plotted := !identicalIn(loadedCanvas, bareCanvas, scopeBox)
			if plotted != testCase.wantPlotted {
				t.Errorf("aircraft plotted = %v, want %v", plotted, testCase.wantPlotted)
			}
		})
	}
}

// TestDrawAwkwardAircraft feeds the scene every shape of incomplete decode it
// has to survive, all in one frame, because that is how they arrive.
func TestDrawAwkwardAircraft(t *testing.T) {
	t.Parallel()

	noTrail := scenePlane("111111", "SHORT1", 10, 8, 5000, 90)
	noTrail.PositionHistory = nil

	oneFix := scenePlane("222222", "SHORT2", 20, 9, 5000, 100)
	oneFix.PositionHistory = oneFix.PositionHistory[:1]

	noPosition := scenePlane("333333", "NOWHERE", 30, 10, 5000, 110)
	noPosition.Latitude, noPosition.Longitude = 0, 0

	overlong := scenePlane("444444", "CALLSIGNTOOLONG", 40, 11, 5000, 120)
	overlong.Squawk = "76001234"

	frame := sceneFrame(
		scenePlane("484AC1", "", 45, 12, 0, 0),          // no callsign, no altitude, no heading
		scenePlane("3C6745", "LOWONE", 90, 14, 4000, 5), // low band
		scenePlane("4CA2D3", "MIDONE", 135, 20, 18000, 95),
		scenePlane("4CA8B7", "HIGHONE", 180, 30, 39000, 185),
		scenePlane("555555", "FARAWAY", 225, 500, 20000, 275), // outside the range
		noTrail, oneFix, noPosition, overlong,
	)

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	if painted(canv, canv.Bounds()) == 0 {
		t.Fatal("nothing was drawn at all")
	}

	// Drawing the same frame again has to produce the same picture. A scene
	// that only worked on its first frame would show up here.
	second, secondCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	second.Draw(secondCanvas, 0)
	second.Draw(secondCanvas, 0)

	scene.Draw(canv, 0)

	if !identical(canv, secondCanvas) {
		t.Error("the second frame differs from the first, so something is carrying state it should not")
	}
}

func TestDrawWithNoAircraft(t *testing.T) {
	t.Parallel()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame())
	scene.Draw(canv, 0)

	// The card still draws its frame and its NO CONTACT line, so the column
	// is not blank even with an empty sky.
	column := image.Rect(620, 80, 1264, 300)
	if painted(canv, column) == 0 {
		t.Error("the card drew nothing with an empty aircraft list, want the NO CONTACT state")
	}
}

// TestTrailCycle walks the t key all the way round the four modes and back to
// the one the scene started in.
//
// The picture is read at each stop rather than only at the ends, because the
// modes are ordered by how much track they draw: off is nothing, short is the
// last twelve fixes, and long and all are the whole history. Anything that
// broke the order, or left a mode drawing the mode before it, would show up
// as two stops painting the same number of pixels.
//
// The last press has to restore the opening picture exactly. A cycle that came
// back to a different frame would be one that lost state on the way round.
func TestTrailCycle(t *testing.T) {
	t.Parallel()

	const denseFixes = 30

	frame := sceneFrame(
		denseTrail(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41), denseFixes),
		denseTrail(scenePlane("3C6745", "DLH4EA", 200, 30, 36000, 268), denseFixes),
	)

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(bareScope())
	scene.Draw(canv, 0)

	long := painted(canv, scopeBox)

	stop := func(tb testing.TB, key rune) int {
		tb.Helper()

		if !press(scene, key) {
			tb.Fatalf("Handle(%q) = false, want the scene to take it", key)
		}

		scene.Draw(canv, 0)

		return painted(canv, scopeBox)
	}

	// Both letter cases are bound, so the walk uses one of each.
	all := stop(t, 't')
	off := stop(t, 'T')
	short := stop(t, 't')
	back := stop(t, 'T')

	// The all mode covers the same ground as long and differs only in that
	// nothing fades, so the count can only hold or rise. What the fade
	// actually does to the colours is TestTrailAllDrawsTheWholeTrailInOneColour.
	if all < long {
		t.Errorf("the all mode painted %d pixels against long's %d, want at least as many", all, long)
	}

	if off >= short {
		t.Errorf("the off mode painted %d pixels against short's %d, want fewer", off, short)
	}

	if short >= long {
		t.Errorf("the short mode painted %d pixels against long's %d, want fewer", short, long)
	}

	if back != long {
		t.Errorf("four presses of t painted %d pixels, want the %d it started with", back, long)
	}
}

func TestHandleTakesItsOwnKeys(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		key  input.Key
		want bool
	}{
		{name: "n selects the next", key: input.Key{Kind: input.Rune, Rune: 'n'}, want: true},
		{name: "N selects the next", key: input.Key{Kind: input.Rune, Rune: 'N'}, want: true},
		{name: "p selects the previous", key: input.Key{Kind: input.Rune, Rune: 'p'}, want: true},
		{name: "P selects the previous", key: input.Key{Kind: input.Rune, Rune: 'P'}, want: true},
		{name: "plus widens", key: input.Key{Kind: input.Rune, Rune: '+'}, want: true},
		{name: "equals widens", key: input.Key{Kind: input.Rune, Rune: '='}, want: true},
		{name: "minus narrows", key: input.Key{Kind: input.Rune, Rune: '-'}, want: true},
		{name: "underscore narrows", key: input.Key{Kind: input.Rune, Rune: '_'}, want: true},
		{name: "r toggles auto", key: input.Key{Kind: input.Rune, Rune: 'r'}, want: true},
		{name: "R toggles auto", key: input.Key{Kind: input.Rune, Rune: 'R'}, want: true},
		{name: "t toggles trails", key: input.Key{Kind: input.Rune, Rune: 't'}, want: true},
		{name: "T toggles trails", key: input.Key{Kind: input.Rune, Rune: 'T'}, want: true},
		{name: "a toggles airports", key: input.Key{Kind: input.Rune, Rune: 'a'}, want: true},
		{name: "A toggles airports", key: input.Key{Kind: input.Rune, Rune: 'A'}, want: true},
		{name: "m toggles the shore", key: input.Key{Kind: input.Rune, Rune: 'm'}, want: true},
		{name: "M toggles the shore", key: input.Key{Kind: input.Rune, Rune: 'M'}, want: true},
		{name: "v flips minimal", key: input.Key{Kind: input.Rune, Rune: 'v'}, want: true},
		{name: "V flips minimal", key: input.Key{Kind: input.Rune, Rune: 'V'}, want: true},
		{name: "c cycles the colour mode", key: input.Key{Kind: input.Rune, Rune: 'c'}, want: true},
		{name: "C cycles the colour mode", key: input.Key{Kind: input.Rune, Rune: 'C'}, want: true},
		{name: "down selects the next", key: input.Key{Kind: input.Down}, want: true},
		{name: "up selects the previous", key: input.Key{Kind: input.Up}, want: true},

		// The false cases are the ones that matter. Anything the scene takes
		// here is a key the run loop never sees, and q is how you get out. s
		// used to cycle scenes and falls through now as an ordinary unbound
		// letter, the same as it always has.
		{name: "q falls through", key: input.Key{Kind: input.Rune, Rune: 'q'}},
		{name: "Q falls through", key: input.Key{Kind: input.Rune, Rune: 'Q'}},
		{name: "s falls through", key: input.Key{Kind: input.Rune, Rune: 's'}},
		{name: "S falls through", key: input.Key{Kind: input.Rune, Rune: 'S'}},
		{name: "esc is taken, it unpins rather than quitting", key: input.Key{Kind: input.Esc}, want: true},
		{name: "ctrl-c falls through", key: input.Key{Kind: input.CtrlC}},
		{name: "left falls through", key: input.Key{Kind: input.Left}},
		{name: "right falls through", key: input.Key{Kind: input.Right}},
		{name: "enter falls through", key: input.Key{Kind: input.Enter}},
		{name: "an unbound rune falls through", key: input.Key{Kind: input.Rune, Rune: 'x'}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
			scene.Draw(canv, 0)

			if got := scene.Handle(testCase.key); got != testCase.want {
				t.Errorf("Handle(%+v) = %v, want %v", testCase.key, got, testCase.want)
			}
		})
	}
}

// selectionFleet is three aircraft at increasing distance, so the sorted
// order the scene works on is the order they are listed in.
func selectionFleet() []airplane.Snapshot {
	return []airplane.Snapshot{
		scenePlane("AAA111", "NEAREST", 0, 5, 3000, 10),
		scenePlane("BBB222", "MIDDLE", 90, 15, 15000, 100),
		scenePlane("CCC333", "FARTHEST", 180, 30, 30000, 190),
	}
}

func TestSelectionSteps(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(selectionFleet()...)

	// The card is where the selection shows, so four canvases of the same
	// scene at four selections should all differ from each other.
	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	first, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene.Draw(first, 0)

	press(scene, 'n')
	scene.Draw(canv, 0)

	if identical(canv, first) {
		t.Fatal("n did not change the picture, so the selection did not move")
	}

	press(scene, 'p')
	scene.Draw(canv, 0)

	if !identical(canv, first) {
		t.Error("p did not undo n, so the selection does not step back")
	}
}

func TestSelectionWraps(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(selectionFleet()...)

	forward, forwardCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	forward.Draw(forwardCanvas, 0)

	// Three aircraft, so three presses comes back to where it started.
	for range 3 {
		press(forward, 'n')
	}

	forward.Draw(forwardCanvas, 0)

	fresh, freshCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	fresh.Draw(freshCanvas, 0)

	if !identical(forwardCanvas, freshCanvas) {
		t.Error("stepping forward past the end did not wrap to the start")
	}

	backward, backwardCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	backward.Draw(backwardCanvas, 0)
	press(backward, 'p')
	backward.Draw(backwardCanvas, 0)

	last, lastCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	last.Draw(lastCanvas, 0)

	for range 2 {
		press(last, 'n')
	}

	last.Draw(lastCanvas, 0)

	if !identical(backwardCanvas, lastCanvas) {
		t.Error("stepping back from the first aircraft did not wrap to the last")
	}
}

// TestSelectionSurvivesReordering is why the selection is kept by ICAO. The
// list is sorted by distance, so one aircraft overtaking another would
// otherwise move the selection to a different aeroplane on its own.
func TestSelectionSurvivesReordering(t *testing.T) {
	t.Parallel()

	fleet := selectionFleet()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(fleet...))
	scene.Draw(canv, 0)
	press(scene, 'n')
	scene.Draw(canv, 0)

	// MIDDLE is now selected. Bring it to the front of the list, which is what
	// a re-sort looks like from here, and draw it again on a fresh scene that
	// was told to select the same aircraft.
	reordered := []airplane.Snapshot{fleet[1], fleet[0], fleet[2]}

	moved, movedCanvas, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(reordered...))
	moved.Draw(movedCanvas, 0)

	// A fresh scene selects the nearest, which in the reordered list is the
	// same MIDDLE aircraft. Its card should match the one the stepped scene
	// is showing, apart from the row numbers.
	card := image.Rect(620, 80, 1264, 260)
	if painted(movedCanvas, card) == 0 {
		t.Fatal("the card drew nothing")
	}
}

func TestSelectionFallsBackToTheNearest(t *testing.T) {
	t.Parallel()

	fleet := selectionFleet()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(fleet...))
	scene.Draw(canv, 0)

	// Select the farthest, then hand it a frame that aircraft has dropped out
	// of. The selection has to land on the nearest rather than on nothing.
	press(scene, 'p')
	scene.Draw(canv, 0)

	shrunk := &fakeSource{frame: sceneFrame(fleet[0], fleet[1])}
	gone := radar.New(testFaces(t), shrunk, scope.New(scope.WithCurrent(sceneRangeNm)))

	goneCanvas, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	gone.Draw(goneCanvas, 0)

	if painted(goneCanvas, image.Rect(620, 80, 1264, 260)) == 0 {
		t.Error("the card is empty after the selected aircraft left, want it on the nearest")
	}
}

func TestSelectionOnAnEmptyList(t *testing.T) {
	t.Parallel()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame())
	scene.Draw(canv, 0)

	// Both directions on an empty list have to be no-ops rather than an index
	// out of range.
	press(scene, 'n')
	press(scene, 'p')
	scene.Draw(canv, 0)
}

// TestRowWindowFollowsTheSelection covers the scrolling. With more aircraft
// than rows fit, selecting past the bottom has to bring the window with it, or
// the operator would be moving a selection nobody can see.
// rowFleet builds count aircraft that fan out around the receiver at
// increasing bearing and distance, distinct enough in ICAO and callsign to
// fill or overflow the compact row list.
func rowFleet(count int) []airplane.Snapshot {
	planes := make([]airplane.Snapshot, 0, count)
	for index := range count {
		planes = append(planes, scenePlane(
			string(rune('A'+index%26))+"00000"+string(rune('0'+index%10)),
			"FL"+string(rune('0'+index%10)),
			float64(index)*9, 3+float64(index), float64(index)*900, float64(index)*8+1,
		))
	}

	return planes
}

func TestRowWindowFollowsTheSelection(t *testing.T) {
	t.Parallel()

	const crowd = 40

	// A short canvas so far fewer than forty rows fit.
	scene, canv, _ := sceneOn(t, panelWidth, 400, sceneFrame(rowFleet(crowd)...))
	scene.Draw(canv, 0)

	top, err := canvas.New(panelWidth, 400)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene.Draw(top, 0)

	// Walk the selection well past the bottom of the window.
	for range crowd - 1 {
		press(scene, 'n')
	}

	scene.Draw(canv, 0)

	if identical(canv, top) {
		t.Error("selecting the last of forty aircraft did not scroll the rows")
	}
}

func TestAutoRangeFitsTheFarthestAircraft(t *testing.T) {
	t.Parallel()

	// 37 nm out, so auto range should round up to the next whole 20 nm step.
	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 37, 20000, 41))

	scene, canv, ranges := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	if got := ranges.GetCurrent(); got != 40 {
		t.Errorf("range after auto-fitting a 37 nm contact = %g, want 40", got)
	}
}

func TestAutoRangeLeavesAnEmptySkyAlone(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		planes []airplane.Snapshot
	}{
		{name: "no aircraft at all"},
		{
			name:   "one aircraft with no position",
			planes: []airplane.Snapshot{{ICAO: "484AC1", Callsign: "KLM123", Altitude: 3000}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, ranges := sceneOn(t, panelWidth, panelHeight, sceneFrame(testCase.planes...))
			before := ranges.GetCurrent()

			scene.Draw(canv, 0)

			if got := ranges.GetCurrent(); got != before {
				t.Errorf("range = %g after a frame with nothing to fit, want it left at %g", got, before)
			}
		})
	}
}

func TestManualRangeTurnsAutoOff(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 37, 20000, 41))

	scene, canv, ranges := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	press(scene, '+')

	widened := ranges.GetCurrent()
	if widened != 60 {
		t.Fatalf("range after one + = %g, want 60", widened)
	}

	// Auto range would pull it back to 40 on the next frame. Turning auto off
	// is the whole point of pressing the key.
	scene.Draw(canv, 0)

	if got := ranges.GetCurrent(); got != widened {
		t.Errorf("range = %g on the frame after +, want it left at %g", got, widened)
	}

	press(scene, '-')

	if got := ranges.GetCurrent(); got != 40 {
		t.Errorf("range after - = %g, want 40", got)
	}
}

func TestRangeClampsAtBothEnds(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv, ranges := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	// Far more presses than there are steps, so it has to stop somewhere.
	for range 40 {
		press(scene, '-')
	}

	if got := ranges.GetCurrent(); got != ranges.GetMin() {
		t.Errorf("range after narrowing forty times = %g, want the minimum %g", got, ranges.GetMin())
	}

	for range 40 {
		press(scene, '+')
	}

	if got := ranges.GetCurrent(); got != ranges.GetMax() {
		t.Errorf("range after widening forty times = %g, want the maximum %g", got, ranges.GetMax())
	}
}

func TestAutoToggleStopsTheRangeMoving(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 37, 20000, 41))

	scene, canv, ranges := sceneOn(t, panelWidth, panelHeight, frame)

	press(scene, 'r')
	scene.Draw(canv, 0)

	if got := ranges.GetCurrent(); got != sceneRangeNm {
		t.Errorf("range = %g with auto off, want it left at %g", got, float64(sceneRangeNm))
	}

	press(scene, 'R')
	scene.Draw(canv, 0)

	if got := ranges.GetCurrent(); got != 40 {
		t.Errorf("range = %g with auto back on, want it fitted to 40", got)
	}
}

// TestZeroValueScopeDrawsNothing covers the guard on a range control whose
// increment is zero. scope.New never produces one, but scope.Scope is an
// exported struct so any caller can hand the radar a zero value, and dividing
// by its increment would be a crash.
func TestZeroValueScopeDrawsNothing(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	empty := &scope.Scope{}
	scene := radar.New(testFaces(t), &fakeSource{frame: frame}, empty)

	scene.Draw(canv, 0)

	if got := empty.GetCurrent(); got != 0 {
		t.Errorf("range on a zero-value Scope = %g, want 0", got)
	}
}

func TestOptions(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	t.Run("WithPalette changes the colours", func(t *testing.T) {
		t.Parallel()

		inverted := theme.Night
		inverted.Field = color.RGBA{R: 0x40, G: 0x40, B: 0x40, A: 0xFF}

		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithPalette(inverted))
		scene.Draw(canv, 0)

		if got := canv.Image().RGBAAt(fieldProbeX, fieldProbeY); got != inverted.Field {
			t.Errorf("field pixel = %v, want the palette's %v", got, inverted.Field)
		}
	})

	t.Run("WithClock fills in a frame with no timestamp", func(t *testing.T) {
		t.Parallel()

		undated := frame
		undated.Now = time.Time{}

		fixed := time.Date(2026, time.September, 15, 3, 45, 0, 0, time.UTC)

		withClock, withCanvas, _ := sceneOn(t, panelWidth, panelHeight, undated, radar.WithClock(
			func() time.Time { return fixed },
		))
		withClock.Draw(withCanvas, 0)

		other, otherCanvas, _ := sceneOn(t, panelWidth, panelHeight, undated, radar.WithClock(
			func() time.Time { return fixed.Add(time.Hour) },
		))
		other.Draw(otherCanvas, 0)

		header := image.Rect(1000, 16, 1264, 64)
		if painted(withCanvas, header) == painted(otherCanvas, header) && identical(withCanvas, otherCanvas) {
			t.Error("two different clocks drew the same header, so WithClock is not being consulted")
		}
	})

	t.Run("a nil clock leaves the default", func(t *testing.T) {
		t.Parallel()

		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithClock(nil))
		scene.Draw(canv, 0)
	})

	t.Run("WithSprite changes the silhouette", func(t *testing.T) {
		t.Parallel()

		const solidRow = "###"

		blob, err := sprite.New([]string{solidRow, solidRow, solidRow})
		if err != nil {
			t.Fatalf("sprite.New: %v", err)
		}

		custom, customCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithSprite(blob))
		custom.Draw(customCanvas, 0)

		stock, stockCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
		stock.Draw(stockCanvas, 0)

		if identical(customCanvas, stockCanvas) {
			t.Error("a different sprite drew the same picture, so WithSprite is not being used")
		}
	})

	t.Run("a nil sprite leaves the default", func(t *testing.T) {
		t.Parallel()

		custom, customCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithSprite(nil))
		custom.Draw(customCanvas, 0)

		stock, stockCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
		stock.Draw(stockCanvas, 0)

		if !identical(customCanvas, stockCanvas) {
			t.Error("WithSprite(nil) changed the picture, want the built-in silhouette left alone")
		}
	})
}

// TestWithBiasTeeWiresTheKey checks the option on its own rather than as a
// subtest of TestOptions, only because the function it would otherwise join
// is already at the line budget. It is otherwise the same shape: build a
// scene with the option, press the key, and check the effect the option
// promised rather than reading a field back.
func TestWithBiasTeeWiresTheKey(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	frame.BiasTee = source.BiasTeeState{Supported: true}

	toggler := &fakeToggler{}
	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithBiasTee(toggler))
	scene.Draw(canv, 0)

	if !press(scene, 'b') {
		t.Fatal("Handle('b') = false, want the scene to take it once the source supports a bias-tee")
	}

	if toggler.calls != 1 {
		t.Errorf("Toggle() called %d times, want 1", toggler.calls)
	}
}

// TestFrameWithoutPlanesButWithAStatsLine keeps the stats line honest: it has
// to say zero aircraft rather than skip itself.
func TestFrameWithoutPlanesButWithAStatsLine(t *testing.T) {
	t.Parallel()

	frame := sceneFrame()
	frame.Source = adsb.SourceInfo{Label: "BEAST 192.168.1.10:30005", Connected: false}
	frame.Stats = adsb.Stats{TotalFrames: 0}

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	stats := image.Rect(620, 650, 1264, 680)
	if painted(canv, stats) == 0 {
		t.Error("the stats line drew nothing with an empty sky, want it to say zero aircraft")
	}
}

// TestSetPaletteChangesColours checks the runtime half of the palette
// contract: SetPalette on an already-built scene has to change what the next
// Draw paints, which is what the l key relies on to cycle the theme without
// rebuilding the scene set.
func TestSetPaletteChangesColours(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	if got := canv.Image().RGBAAt(fieldProbeX, fieldProbeY); got != theme.Night.Field {
		t.Fatalf("field pixel before SetPalette = %v, want %v", got, theme.Night.Field)
	}

	scene.SetPalette(theme.Day)
	scene.Draw(canv, 0)

	if got := canv.Image().RGBAAt(fieldProbeX, fieldProbeY); got != theme.Day.Field {
		t.Errorf("field pixel after SetPalette(Day) = %v, want %v", got, theme.Day.Field)
	}
}

// TestColourKeyCyclesMode checks that c and C are taken by Handle and cycle
// the colour mode, proved by the picture changing rather than by reading the
// mode back.
func TestColourKeyCyclesMode(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		key  rune
	}{
		{name: "lowercase c cycles the mode", key: 'c'},
		{name: "uppercase C cycles the mode", key: 'C'},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
			scene.Draw(canv, 0)

			before, err := canvas.New(panelWidth, panelHeight)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			scene.Draw(before, 0)

			if !press(scene, testCase.key) {
				t.Fatalf("Handle(%q) = false, want the scene to take it", testCase.key)
			}

			scene.Draw(canv, 0)

			if identical(canv, before) {
				t.Errorf("colour key %q did not change the picture", testCase.key)
			}
		})
	}
}

// TestColourSettingsChangeWhatIsDrawn checks the two ways airline mode can
// reach a scene (WithColour at construction, Apply on one already built) and
// the rule that an empty Settings reads as altitude mode.
func TestColourSettingsChangeWhatIsDrawn(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	t.Run("WithColour(ColourAirline) draws differently than the default", func(t *testing.T) {
		t.Parallel()

		altitude, altitudeCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
		altitude.Draw(altitudeCanvas, 0)

		airline, airlineCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithColour(radar.ColourAirline))
		airline.Draw(airlineCanvas, 0)

		if identical(altitudeCanvas, airlineCanvas) {
			t.Error("WithColour(ColourAirline) drew the same picture as the default altitude mode")
		}
	})

	t.Run("Apply changes what the next Draw paints", func(t *testing.T) {
		t.Parallel()

		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
		scene.Draw(canv, 0)

		before, err := canvas.New(panelWidth, panelHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		scene.Draw(before, 0)

		scene.Apply(radar.Settings{Colour: radar.ColourAirline})
		scene.Draw(canv, 0)

		if identical(canv, before) {
			t.Error("Apply(Settings{Colour: ColourAirline}) did not change the picture")
		}
	})

	t.Run("an empty Settings draws the same as ColourAltitude", func(t *testing.T) {
		t.Parallel()

		zero, zeroCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
		zero.Apply(radar.Settings{})
		zero.Draw(zeroCanvas, 0)

		altitude, altitudeCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame,
			radar.WithColour(radar.ColourAltitude))
		altitude.Draw(altitudeCanvas, 0)

		if !identical(zeroCanvas, altitudeCanvas) {
			t.Error("Apply(Settings{}) drew differently from ColourAltitude")
		}
	})
}

// TestAirportsToggle checks that a and A are taken by Handle, and that turning
// the airfield markers off actually removes them from the scope rather than
// merely accepting the key.
func TestAirportsToggle(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	withAirports := painted(canv, scopeBox)

	if !press(scene, 'a') {
		t.Fatal("Handle('a') = false, want the scene to take it")
	}

	scene.Draw(canv, 0)

	withoutAirports := painted(canv, scopeBox)
	if withoutAirports >= withAirports {
		t.Errorf("scope pixels with airports off = %d, with them on = %d, want fewer", withoutAirports, withAirports)
	}

	press(scene, 'A')
	scene.Draw(canv, 0)

	if painted(canv, scopeBox) != withAirports {
		t.Error("turning airports back on did not restore the picture")
	}
}

// homeRingPixel is a point on the home marker's ring at the panel's
// resolution, worked out the way scopeBox is: the scope's own centre plus its
// ring radius, landing just past the centre dot rather than on it.
//
//nolint:gochecknoglobals // a point is data, and image.Point cannot be const.
var homeRingPixel = image.Pt(314, 371)

// TestHomeMarkerRingColour checks that the ring around the receiver's own
// position takes the colour that says how much that position is worth, for
// every fix mode there is.
func TestHomeMarkerRingColour(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		mode source.FixMode
		want color.RGBA
	}{
		{name: "no fix reads as muted", mode: source.FixNone, want: theme.Night.Muted},
		{name: "a manual position takes the ink", mode: source.FixManual, want: theme.Night.Ink},
		{name: "an estimate is a caution", mode: source.FixEstimated, want: theme.Night.Caution},
		{name: "a GPS still searching is a caution too", mode: source.FixGPSNoFix, want: theme.Night.Caution},
		{name: "a 2D GPS fix is OK", mode: source.FixGPS2D, want: theme.Night.OK},
		{name: "a full 3D GPS fix is OK too", mode: source.FixGPS3D, want: theme.Night.OK},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
			frame.Receiver.Mode = testCase.mode

			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
			scene.Draw(canv, 0)

			if got := canv.Image().RGBAAt(homeRingPixel.X, homeRingPixel.Y); got != testCase.want {
				t.Errorf("ring pixel = %v, want %v", got, testCase.want)
			}
		})
	}
}

// cardBox is the selected-flight card's own area at the panel's resolution,
// used wherever a test needs to know a colour landed on the card rather than
// merely somewhere on the column.
var cardBox = image.Rect(620, 80, 1264, 260) //nolint:gochecknoglobals // a rectangle is data.

// TestAirlineModeColoursOperatorsAndMutesUnknowns checks that airline mode
// paints a fleet of known operators differently from altitude mode, and that
// an aircraft with no callsign or an unrecognised prefix falls back to the
// palette's muted colour, the same answer altitude mode gives an aircraft with
// no decoded altitude.
func TestAirlineModeColoursOperatorsAndMutesUnknowns(t *testing.T) {
	t.Parallel()

	t.Run("known operators paint differently than altitude mode", func(t *testing.T) {
		t.Parallel()

		frame := sceneFrame(
			scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
			scenePlane("3C6745", "EZY456", 90, 20, 8000, 100),
			scenePlane("4CA2D3", "DLH789", 135, 30, 15000, 190),
		)

		altitude, altitudeCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
		altitude.Draw(altitudeCanvas, 0)

		airline, airlineCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithColour(radar.ColourAirline))
		airline.Draw(airlineCanvas, 0)

		if identicalIn(altitudeCanvas, airlineCanvas, scopeBox) {
			t.Error("the scope painted the same picture in airline mode as in altitude mode")
		}
	})

	t.Run("no callsign is drawn muted", func(t *testing.T) {
		t.Parallel()

		frame := sceneFrame(scenePlane("484AC1", "", 45, 12, 2400, 41))

		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithColour(radar.ColourAirline))
		scene.Draw(canv, 0)

		if countColour(canv, cardBox, theme.Night.Muted) == 0 {
			t.Error("an aircraft with no callsign was not drawn in the muted colour")
		}
	})

	t.Run("an unrecognised prefix is drawn muted", func(t *testing.T) {
		t.Parallel()

		// LFV is a real ICAO code that is deliberately not in the airlines
		// database, which is what makes it the unknown-prefix case.
		frame := sceneFrame(scenePlane("484AC1", "LFV21", 45, 12, 2400, 41))

		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithColour(radar.ColourAirline))
		scene.Draw(canv, 0)

		if countColour(canv, cardBox, theme.Night.Muted) == 0 {
			t.Error("an aircraft with an unrecognised prefix was not drawn in the muted colour")
		}
	})
}

// fourOperatorFleet is four operators with strictly decreasing aircraft
// counts (KLM 5, EZY 4, DLH 3, BAW 2), so the legend's ranking is unambiguous
// and never depends on the order the frame lists them in.
func fourOperatorFleet() []airplane.Snapshot {
	return []airplane.Snapshot{
		scenePlane("AAA001", "KLM1", 10, 5, 3000, 10),
		scenePlane("AAA002", "KLM2", 10, 5, 3000, 10),
		scenePlane("AAA003", "KLM3", 10, 5, 3000, 10),
		scenePlane("AAA004", "KLM4", 10, 5, 3000, 10),
		scenePlane("AAA005", "KLM5", 10, 5, 3000, 10),
		scenePlane("BBB001", "EZY1", 20, 6, 3000, 10),
		scenePlane("BBB002", "EZY2", 20, 6, 3000, 10),
		scenePlane("BBB003", "EZY3", 20, 6, 3000, 10),
		scenePlane("BBB004", "EZY4", 20, 6, 3000, 10),
		scenePlane("CCC001", "DLH1", 30, 7, 3000, 10),
		scenePlane("CCC002", "DLH2", 30, 7, 3000, 10),
		scenePlane("CCC003", "DLH3", 30, 7, 3000, 10),
		scenePlane("DDD001", "BAW1", 40, 8, 3000, 10),
		scenePlane("DDD002", "BAW2", 40, 8, 3000, 10),
	}
}

// legendStrip is the row the altitude and airline legends draw into at the
// panel's resolution, worked out the way scopeBox is: the column, one line
// above the stats line already claimed off its bottom.
//
//nolint:gochecknoglobals // a rectangle is data, and image.Rectangle cannot be const.
var legendStrip = image.Rect(625, 630, 1264, 642)

// TestAirlineLegendNamesOnlyFourOperators checks that a fifth distinct
// operator outside the top four never changes what the legend shows, which is
// how these tests confirm "only four are named" without reading any text.
func TestAirlineLegendNamesOnlyFourOperators(t *testing.T) {
	t.Parallel()

	top4 := fourOperatorFleet()

	// A fifth operator with only one aircraft always ranks last behind the
	// four above, so its presence or absence must never move the legend.
	fifth := scenePlane("EEE001", "RYR1", 50, 9, 3000, 10)

	withFive, withFiveCanvas, _ := sceneOn(t, panelWidth, panelHeight,
		sceneFrame(append(append([]airplane.Snapshot{}, top4...), fifth)...), radar.WithColour(radar.ColourAirline))
	withFive.Draw(withFiveCanvas, 0)

	withFour, withFourCanvas, _ := sceneOn(t, panelWidth, panelHeight,
		sceneFrame(top4...), radar.WithColour(radar.ColourAirline))
	withFour.Draw(withFourCanvas, 0)

	if !identicalIn(withFiveCanvas, withFourCanvas, legendStrip) {
		t.Error("a fifth operator outside the top four changed the legend strip, want only four named")
	}
}

// TestAirlineLegendShowsOther checks that an aircraft with no colour of its
// own adds the OTHER entry: its swatch is a full, solid block, which a legend
// with nothing uncoloured on screen never paints there.
func TestAirlineLegendShowsOther(t *testing.T) {
	t.Parallel()

	top4 := fourOperatorFleet()
	unknown := scenePlane("EEE001", "LFV21", 50, 9, 3000, 10)

	// otherSwatchBox is where the fifth legend slot's swatch lands once OTHER
	// is showing, worked out the same way the legend itself divides its width
	// among however many slots are on screen.
	otherSwatchBox := image.Rect(1133, 654, 1143, 664)

	withOther, withOtherCanvas, _ := sceneOn(t, panelWidth, panelHeight,
		sceneFrame(append(append([]airplane.Snapshot{}, top4...), unknown)...), radar.WithColour(radar.ColourAirline))
	withOther.Draw(withOtherCanvas, 0)

	withoutOther, withoutOtherCanvas, _ := sceneOn(t, panelWidth, panelHeight,
		sceneFrame(top4...), radar.WithColour(radar.ColourAirline))
	withoutOther.Draw(withoutOtherCanvas, 0)

	const fullSwatch = 100 // the swatch is a solid 10x10 block when it is drawn.

	if got := painted(withOtherCanvas, otherSwatchBox); got != fullSwatch {
		t.Errorf("OTHER swatch painted pixels = %d, want the full %d-pixel block", got, fullSwatch)
	}

	if got := painted(withoutOtherCanvas, otherSwatchBox); got == fullSwatch {
		t.Error("that box is fully painted with no unrecognised aircraft on screen, want no swatch there")
	}
}

// panelBox is the selected-flight panel's own area at the panel's resolution,
// inside its border. Everything the old card and the old details block held is
// in here now.
var panelBox = image.Rect(626, 79, 1264, 278) //nolint:gochecknoglobals // a rectangle is data.

// TestPanelShowsNoTrafficWhenEmpty checks that the panel keeps its room and
// says NO TRAFFIC with an empty sky, rather than collapsing away.
func TestPanelShowsNoTrafficWhenEmpty(t *testing.T) {
	t.Parallel()

	withTraffic := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	empty := sceneFrame()

	loaded, loadedCanvas, _ := sceneOn(t, panelWidth, panelHeight, withTraffic)
	loaded.Draw(loadedCanvas, 0)

	if painted(loadedCanvas, panelBox) == 0 {
		t.Fatal("the panel drew nothing with an aircraft selected")
	}

	bare, bareCanvas, _ := sceneOn(t, panelWidth, panelHeight, empty)
	bare.Draw(bareCanvas, 0)

	if painted(bareCanvas, panelBox) == 0 {
		t.Fatal("the panel drew nothing with an empty sky, want the NO TRAFFIC state")
	}

	if identicalIn(loadedCanvas, bareCanvas, panelBox) {
		t.Error("the panel looks the same with and without traffic")
	}
}

// TestPanelVerticalRateTriangle checks that a climbing, a descending and a
// level aircraft each leave a different picture in the panel.
func TestPanelVerticalRateTriangle(t *testing.T) {
	t.Parallel()

	const (
		climbRate    = 2000.0
		descentRate  = -2000.0
		levelRateVal = 0.0
	)

	draw := func(tb testing.TB, rate float64) *canvas.Canvas {
		tb.Helper()

		plane := scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)
		plane.VertRate = rate

		scene, canv, _ := sceneOn(tb, panelWidth, panelHeight, sceneFrame(plane))
		scene.Draw(canv, 0)

		return canv
	}

	climbing := draw(t, climbRate)
	descending := draw(t, descentRate)
	level := draw(t, levelRateVal)

	if identicalIn(climbing, descending, panelBox) {
		t.Error("a climbing and a descending aircraft drew the same panel")
	}

	if identicalIn(climbing, level, panelBox) {
		t.Error("a climbing and a level aircraft drew the same panel")
	}

	if identicalIn(descending, level, panelBox) {
		t.Error("a descending and a level aircraft drew the same panel")
	}
}

// stripWindowCapacity is how many aircraft the board draws without needing
// the "+N MORE" line at the panel's resolution: the selected full strip plus
// sixteen half strips below it that all fit before the column runs out of
// room. A seventeenth aircraft still fits at that width (the window only
// gives up one line's worth of room once the board can no longer show
// everything), so it takes an eighteenth to force the tail.
const stripWindowCapacity = 17

// stripListRightBand sits inside the sixteenth half strip's line, in the
// field only a real aircraft strip fills (POS, SEEN and the rules between
// every field). The "+N MORE" line is short and left-aligned, so it never
// reaches this far right: painted pixels here mean a real strip, not the
// tail.
var stripListRightBand = image.Rect(1100, 607, 1264, 637) //nolint:gochecknoglobals // a rectangle is data.

// TestRowListMoreLine checks the window boundary the "+N MORE" line closes: a
// fleet exactly the size of the board's capacity leaves a real strip on the
// last line, one aircraft more replaces it with the tail, and both a light
// and a busy fleet still draw.
func TestRowListMoreLine(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		count       int
		wantRealRow bool
	}{
		{name: "fewer than capacity: no last line at all", count: stripWindowCapacity - 1, wantRealRow: false},
		{name: "exactly capacity: the last line is a real strip", count: stripWindowCapacity, wantRealRow: true},
		{
			name:  "one more than capacity: the last line is the more line",
			count: stripWindowCapacity + 1, wantRealRow: false,
		},
		{name: "a light fleet of twelve still draws", count: 12, wantRealRow: false},
		{name: "a busy fleet of forty still draws", count: benchPlanes, wantRealRow: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			frame := sceneFrame(rowFleet(testCase.count)...)
			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
			scene.Draw(canv, 0)

			if painted(canv, canv.Bounds()) == 0 {
				t.Fatal("nothing was drawn at all")
			}

			if got := painted(canv, stripListRightBand) > 0; got != testCase.wantRealRow {
				t.Errorf("last line is a real row = %v, want %v", got, testCase.wantRealRow)
			}
		})
	}
}

// fakeBattery is a fixed BatteryReader, standing in for uAirwaves' live
// poller.
type fakeBattery struct {
	percent  int8
	charging bool
}

func (f *fakeBattery) GetPercentage() int8 { return f.percent }

func (f *fakeBattery) IsCharging() bool { return f.charging }

// TestBatteryIndicator checks the header's battery glyph against a baseline
// with no battery wired up at all: every real reading has to change the
// header, and a negative reading (nothing read yet) has to draw nothing,
// matching the no-battery baseline exactly.
func TestBatteryIndicator(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	baseline, baselineCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	baseline.Draw(baselineCanvas, 0)

	for _, testCase := range []struct {
		name     string
		fake     fakeBattery
		wantSame bool
	}{
		{name: "84 percent, not charging", fake: fakeBattery{percent: 84}},
		{name: "8 percent, charging", fake: fakeBattery{percent: 8, charging: true}},
		{name: "a full battery", fake: fakeBattery{percent: 100}},
		{name: "an empty battery", fake: fakeBattery{percent: 0}},
		{name: "no reading yet draws nothing", fake: fakeBattery{percent: -1}, wantSame: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithBattery(&testCase.fake))
			scene.Draw(canv, 0)

			same := identical(canv, baselineCanvas)
			if same != testCase.wantSame {
				t.Errorf("battery drawn same as no battery = %v, want %v", same, testCase.wantSame)
			}
		})
	}
}

// headerRightBox covers both clocks and the battery slot on the right of the
// header band at the panel's resolution.
var headerRightBox = image.Rect(900, 16, 1264, 64) //nolint:gochecknoglobals // a rectangle is data.

// utcClockBox sits to the left of the local clock, in the region confirmed to
// hold only the UTC clock: the same instant in two different zones paints
// this area identically while the local clock beside it differs.
var utcClockBox = image.Rect(1000, 16, 1180, 64) //nolint:gochecknoglobals // a rectangle is data.

// TestHeaderClocks checks that a fixed instant draws both clocks, and that the
// same instant expressed in two different zones draws the same UTC half while
// the local clock differs.
func TestHeaderClocks(t *testing.T) {
	t.Parallel()

	t.Run("a fixed clock draws both clocks", func(t *testing.T) {
		t.Parallel()

		frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
		frame.Now = time.Date(2026, time.September, 15, 21, 30, 0, 0, time.FixedZone("CEST", 2*60*60))

		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
		scene.Draw(canv, 0)

		if painted(canv, headerRightBox) == 0 {
			t.Fatal("the header drew nothing, want both clocks")
		}
	})

	t.Run("the same instant in two zones draws the same UTC half", func(t *testing.T) {
		t.Parallel()

		zoneA := time.FixedZone("A", 60*60)
		zoneB := time.FixedZone("B", -5*60*60)
		instant := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)

		frameA := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
		frameA.Now = instant.In(zoneA)

		frameB := frameA
		frameB.Now = instant.In(zoneB)

		sceneA, canvasA, _ := sceneOn(t, panelWidth, panelHeight, frameA)
		sceneA.Draw(canvasA, 0)

		sceneB, canvasB, _ := sceneOn(t, panelWidth, panelHeight, frameB)
		sceneB.Draw(canvasB, 0)

		if identical(canvasA, canvasB) {
			t.Error("two different zones drew identical headers, want the local clock to differ")
		}

		if !identicalIn(canvasA, canvasB, utcClockBox) {
			t.Error("the UTC clock differed between two zones showing the same instant")
		}
	})
}

// TestRowTableRespectsItsRightEdge draws the compact rows at the panel width
// and at a canvas just over minColumnWidth, and checks the margin strip beside
// the first row is never painted there: the table never runs its own right
// column past the layout's own right edge.
//
// The check is scoped to the row band rather than the whole frame height,
// because the key bar spans the full width of the layout, not just the
// column, and can legitimately end a few pixels past its own break point; that
// is a property of the key bar, not of the row table this test is about.
func TestRowTableRespectsItsRightEdge(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(fleet(5, 10)...)

	// firstRowTop and firstRowHeight bound the first compact row's own line.
	// The card and the header above it are sized only from font metrics, so
	// this row starts at the same y on every canvas size tested here.
	const (
		firstRowTop    = 285
		firstRowHeight = 20
		marginWidth    = 16
	)

	for _, testCase := range []struct {
		name          string
		width, height int
	}{
		{name: "the panel width", width: panelWidth, height: panelHeight},
		{name: "just over the column threshold", width: 641, height: 480},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, _ := sceneOn(t, testCase.width, testCase.height, frame)
			scene.Draw(canv, 0)

			row := image.Rect(0, firstRowTop, testCase.width, firstRowTop+firstRowHeight)
			if painted(canv, row) == 0 {
				t.Fatal("nothing was drawn on the first row, want a real aircraft row there")
			}

			margin := image.Rect(testCase.width-marginWidth, firstRowTop, testCase.width, firstRowTop+firstRowHeight)
			if painted(canv, margin) != 0 {
				t.Error("the row table ran past its own right edge into the margin")
			}
		})
	}
}

// TestAirlineLegendEmptySky checks the guard that keeps the legend from
// drawing anything when there is nothing to show: an airline-mode frame with
// no aircraft has no operator to name and nothing uncoloured to call OTHER, so
// the strip stays blank rather than an empty swatch or a stray label.
func TestAirlineLegendEmptySky(t *testing.T) {
	t.Parallel()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(), radar.WithColour(radar.ColourAirline))
	scene.Draw(canv, 0)

	if painted(canv, legendStrip) != 0 {
		t.Error("the airline legend drew something with no aircraft on screen, want nothing")
	}
}

// TestAirlineColourAdaptsToThePalette checks that operator colours are
// adapted for the field they land on: WithSettings carries the colour mode at
// construction the same way WithColour does, and Paper's light field takes
// the OnLight() half of an airline's colour rather than OnDark()'s.
func TestAirlineColourAdaptsToThePalette(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	t.Run("WithSettings carries the colour mode at construction", func(t *testing.T) {
		t.Parallel()

		altitude, altitudeCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
		altitude.Draw(altitudeCanvas, 0)

		airline, airlineCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame,
			radar.WithSettings(radar.Settings{Colour: radar.ColourAirline}))
		airline.Draw(airlineCanvas, 0)

		if identical(altitudeCanvas, airlineCanvas) {
			t.Error("WithSettings(Settings{Colour: ColourAirline}) drew the same picture as the default")
		}
	})

	t.Run("an operator's colour is adapted differently on each palette", func(t *testing.T) {
		t.Parallel()

		night, nightCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithColour(radar.ColourAirline))
		night.Draw(nightCanvas, 0)

		paper, paperCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame,
			radar.WithColour(radar.ColourAirline), radar.WithPalette(theme.Day))
		paper.Draw(paperCanvas, 0)

		if painted(paperCanvas, scopeBox) == 0 {
			t.Fatal("nothing was drawn on the paper palette in airline mode")
		}

		if identicalIn(nightCanvas, paperCanvas, scopeBox) {
			t.Error("night and paper drew the same operator colour, want OnDark and OnLight to differ")
		}
	})
}

// TestPanelSquawkEmergency checks that an emergency squawk draws the filled
// warning box on the selected strip: red with white text, the one place a
// colour is fixed rather than themed.
func TestPanelSquawkEmergency(t *testing.T) {
	t.Parallel()

	plane := scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)
	plane.Squawk = "7700"
	plane.Emergency = true

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(plane))
	scene.Draw(canv, 0)

	if countColour(canv, panelBox, theme.Night.Warn) == 0 {
		t.Error("an emergency squawk did not paint the warning box in the panel")
	}
}

// syntheticShoreSet builds a *shore.Set with one polyline, by encoding it
// into the packed format and decoding it straight back. shore.Set has no
// exported constructor, so this round trip is the only way a test builds one
// by hand.
func syntheticShoreSet(tb testing.TB, line shore.Polyline) *shore.Set {
	tb.Helper()

	var buf bytes.Buffer

	if err := shore.Encode(&buf, []shore.Polyline{line}); err != nil {
		tb.Fatalf("shore.Encode: %v", err)
	}

	set, err := shore.Decode(&buf)
	if err != nil {
		tb.Fatalf("shore.Decode: %v", err)
	}

	return set
}

// shoreLineThroughReceiver is a synthetic coastline running north-south
// straight through the scene's receiver position, long enough to cross the
// scope at the range these tests draw at.
func shoreLineThroughReceiver() shore.Polyline {
	const shoreHalfSpanDeg = 1.0

	return shore.Polyline{
		{Lat: receiverLat - shoreHalfSpanDeg, Lon: receiverLon},
		{Lat: receiverLat, Lon: receiverLon},
		{Lat: receiverLat + shoreHalfSpanDeg, Lon: receiverLon},
	}
}

// TestShoreDrawsThroughTheReceiver checks the ordinary case: a synthetic
// coastline that runs through the receiver's own position paints the scope's
// shore colour.
func TestShoreDrawsThroughTheReceiver(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	set := syntheticShoreSet(t, shoreLineThroughReceiver())

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithShore(set))
	scene.Draw(canv, 0)

	if got := countColour(canv, scopeBox, theme.Night.Shore); got == 0 {
		t.Errorf("Draw with a coastline through the receiver = %d shore pixels, want more than 0", got)
	}
}

// TestShoreOffSettingRemovesIt checks that Settings.Shore actually stops the
// coastline being drawn, rather than only being accepted.
func TestShoreOffSettingRemovesIt(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	set := syntheticShoreSet(t, shoreLineThroughReceiver())

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithShore(set))
	scene.Apply(radar.Settings{Shore: radar.ToggleOff})
	scene.Draw(canv, 0)

	if got := countColour(canv, scopeBox, theme.Night.Shore); got != 0 {
		t.Errorf("Draw with the shore off = %d shore pixels, want 0", got)
	}
}

// TestShoreNilSetDrawsNothing checks the state a run never handed shore data
// is in: a nil set draws no coastline, and the rest of the frame is exactly
// what the shore being toggled off would have drawn.
func TestShoreNilSetDrawsNothing(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	noSet, noSetCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	noSet.Draw(noSetCanvas, 0)

	if got := countColour(noSetCanvas, scopeBox, theme.Night.Shore); got != 0 {
		t.Errorf("Draw with a nil shore set = %d shore pixels, want 0", got)
	}

	toggledOff, toggledOffCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame,
		radar.WithShore(syntheticShoreSet(t, shoreLineThroughReceiver())))
	toggledOff.Apply(radar.Settings{Shore: radar.ToggleOff})
	toggledOff.Draw(toggledOffCanvas, 0)

	// The scope only: the key bar's SHORE cap is hollow in the toggled-off
	// scene and filled in the other, which is the cap doing its job. What this
	// test is about is the field under it.
	if !identicalIn(noSetCanvas, toggledOffCanvas, scopeBox) {
		t.Error("a nil shore set drew differently from the shore being toggled off, want the same scope")
	}
}

// TestShoreKeyFlipsItLive checks that m and M flip the shore through Handle,
// proved by the shore pixel count going from many to none and back.
func TestShoreKeyFlipsItLive(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	set := syntheticShoreSet(t, shoreLineThroughReceiver())

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithShore(set))
	scene.Draw(canv, 0)

	withShore := countColour(canv, scopeBox, theme.Night.Shore)
	if withShore == 0 {
		t.Fatal("the fixture drew no shore pixels to begin with, so this comparison proves nothing")
	}

	if !press(scene, 'm') {
		t.Fatal("Handle('m') = false, want the scene to take it")
	}

	scene.Draw(canv, 0)

	if got := countColour(canv, scopeBox, theme.Night.Shore); got != 0 {
		t.Errorf("Draw after m = %d shore pixels, want 0", got)
	}

	press(scene, 'M')
	scene.Draw(canv, 0)

	if got := countColour(canv, scopeBox, theme.Night.Shore); got != withShore {
		t.Errorf("Draw after M = %d shore pixels, want it restored to %d", got, withShore)
	}
}

// TestMinimalModeStripsChrome checks that minimal mode draws none of the
// scope's furniture (the header band, the key caps, the range rings) while
// still drawing the aircraft on the field. Paper is used throughout rather
// than the default Night, because Night's header band repeats the field
// colour on purpose, which would make a band-colour count meaningless.
func TestMinimalModeStripsChrome(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	full, fullCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithPalette(theme.Day))
	full.Draw(fullCanvas, 0)

	if countColour(fullCanvas, fullCanvas.Bounds(), theme.Day.Band) == 0 {
		t.Fatal("the full scope drew no header-band pixels, so this comparison proves nothing")
	}

	minimal, minimalCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithPalette(theme.Day))
	minimal.Apply(radar.Settings{View: radar.ViewMinimal})
	minimal.Draw(minimalCanvas, 0)

	if got := countColour(minimalCanvas, minimalCanvas.Bounds(), theme.Day.Band); got != 0 {
		t.Errorf("minimal mode drew %d header-band pixels, want 0", got)
	}

	if got := countColour(minimalCanvas, minimalCanvas.Bounds(), theme.Day.Ink); got != 0 {
		t.Errorf("minimal mode drew %d key-cap pixels, want 0", got)
	}

	if got := countColour(minimalCanvas, minimalCanvas.Bounds(), theme.Day.Rule); got != 0 {
		t.Errorf("minimal mode drew %d range-ring pixels, want 0", got)
	}

	if countColour(minimalCanvas, minimalCanvas.Bounds(), theme.Day.AltLow) == 0 {
		t.Error("minimal mode drew no low-band pixels, want the aircraft's own sprite to survive")
	}
}

// TestMinimalModeDropsTheColumn checks that the right column, the legend and
// the key bar leave no trace: an empty sky paints nothing at all, and an
// aircraft kept away from the old column's own area leaves that area
// untouched.
func TestMinimalModeDropsTheColumn(t *testing.T) {
	t.Parallel()

	t.Run("an empty sky paints only the receiver marker", func(t *testing.T) {
		t.Parallel()

		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame())
		scene.Apply(radar.Settings{View: radar.ViewMinimal})
		scene.Draw(canv, 0)

		// With --recenter off the projection still sits on the receiver, so
		// the marker lands dead centre and is the only thing on the canvas.
		marker := image.Rect(panelWidth/2-8, panelHeight/2-8, panelWidth/2+8, panelHeight/2+8)
		if painted(canv, marker) == 0 {
			t.Error("minimal mode painted nothing at the centre, want the receiver marker")
		}

		if got := painted(canv, canv.Bounds()) - painted(canv, marker); got != 0 {
			t.Errorf("minimal mode with nothing on screen painted %d pixels away from the marker, want 0", got)
		}
	})

	t.Run("the old column's area is untouched with traffic elsewhere", func(t *testing.T) {
		t.Parallel()

		// Bearing 180 puts this aircraft due south of the receiver, which
		// under minimal's canvas-centred projection lands below the middle
		// rather than in the right third the column used to occupy.
		frame := sceneFrame(scenePlane("484AC1", "KLM123", 180, 12, 2400, 41))
		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
		scene.Apply(radar.Settings{View: radar.ViewMinimal})
		scene.Draw(canv, 0)

		rightThird := image.Rect(2*panelWidth/3, 0, panelWidth, panelHeight)
		if got := painted(canv, rightThird); got != 0 {
			t.Errorf("minimal mode painted %d pixels in the old column's area, want 0", got)
		}
	})
}

// TestMinimalModeSelectionWithoutLabel checks that minimal mode draws nothing
// at all for the selected aircraft: no ring, no leader line and no callsign
// label. The ring used to survive so n and p could show they had done
// something, but there is no panel in minimal mode for the ring to point at.
func TestMinimalModeSelectionWithoutLabel(t *testing.T) {
	t.Parallel()

	t.Run("nothing marks the selection", func(t *testing.T) {
		t.Parallel()

		frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
		scene.Apply(radar.Settings{View: radar.ViewMinimal})
		scene.Draw(canv, 0)

		if got := countColour(canv, canv.Bounds(), theme.Night.Accent); got != 0 {
			t.Errorf("minimal mode painted %d accent pixels, want none for the selection", got)
		}
	})

	t.Run("no label is drawn regardless of callsign length", func(t *testing.T) {
		t.Parallel()

		short := sceneFrame(scenePlane("484AC1", "KL1", 45, 12, 2400, 41))
		long := sceneFrame(scenePlane("484AC1", "KLM1234567LONG", 45, 12, 2400, 41))

		shortScene, shortCanvas, _ := sceneOn(t, panelWidth, panelHeight, short)
		shortScene.Apply(radar.Settings{View: radar.ViewMinimal})
		shortScene.Draw(shortCanvas, 0)

		longScene, longCanvas, _ := sceneOn(t, panelWidth, panelHeight, long)
		longScene.Apply(radar.Settings{View: radar.ViewMinimal})
		longScene.Draw(longCanvas, 0)

		if !identical(shortCanvas, longCanvas) {
			t.Error("a longer callsign changed the minimal-mode picture, want no label drawn at all")
		}
	})
}

// TestMinimalModeCornerTraffic checks the reason minimal mode widens its
// cut-off past the inscribed circle: an aircraft well beyond the nominal
// range still lands on the field when its own pixel sits within the canvas's
// own corner.
func TestMinimalModeCornerTraffic(t *testing.T) {
	t.Parallel()

	// Bearing 60 and 100 nm out lands this aircraft around (1160, 60) on the
	// panel canvas at the pinned 60 nm range: past the 360-pixel circle
	// minimal mode inscribes, but still inside the wider limit reaching
	// pushes out to the actual corner. RangeNm is pinned so auto range does
	// not widen past it to fit this same aircraft first.
	frame := sceneFrame(scenePlane("484AC1", "KLM123", 60, 100, 2400, 41))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(radar.Settings{View: radar.ViewMinimal, RangeNm: sceneRangeNm})
	scene.Draw(canv, 0)

	corner := image.Rect(1100, 0, panelWidth, 150)
	if countColour(canv, corner, theme.Night.AltLow) == 0 {
		t.Error("no low-band sprite pixels landed in the corner, want the far aircraft plotted there")
	}
}

// TestViewKeyCyclesFourWays checks that v and V walk the whole cycle through
// Handle, proved by the picture changing at every step and coming back to
// where it started on the fourth press.
//
// Every intermediate picture is compared against the scope rather than against
// its predecessor, which is the weaker of the two claims but the one that
// matters: a cycle that closed early would put the scope back on screen before
// the fourth press. TestViewKeyReachesEveryPicture is what proves the three
// in between are all different from each other as well.
func TestViewKeyCyclesFourWays(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	full, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene.Draw(full, 0)

	for step, key := range []rune{'v', 'V', 'v'} {
		if !press(scene, key) {
			t.Fatalf("Handle(%q) = false on press %d, want the scene to take it", key, step+1)
		}

		scene.Draw(canv, 0)

		if identical(canv, full) {
			t.Fatalf("press %d of v came back to the scope, want three other pictures first", step+1)
		}
	}

	press(scene, 'v')
	scene.Draw(canv, 0)

	if !identical(canv, full) {
		t.Error("the fourth v did not come back to the scope, so the cycle does not close")
	}
}

// TestViewKeyReachesEveryPicture checks that the four views really are four
// pictures and not three and a repeat.
//
// It is the other half of TestViewKeyCyclesFourWays, which only ever compares
// against the scope. Two views that drew the same canvas would pass that and
// fail this.
func TestViewKeyReachesEveryPicture(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	)

	shots := make([]*canvas.Canvas, 0, 4)
	scene, _, _ := sceneOn(t, panelWidth, panelHeight, frame)

	for step := range 4 {
		shot, err := canvas.New(panelWidth, panelHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		scene.Draw(shot, 0)
		shots = append(shots, shot)

		if step < 3 && !press(scene, 'v') {
			t.Fatalf("Handle('v') = false on press %d, want the scene to take it", step+1)
		}
	}

	for first := range shots {
		for second := first + 1; second < len(shots); second++ {
			if identical(shots[first], shots[second]) {
				t.Errorf("views %d and %d drew the same picture, want four different ones", first, second)
			}
		}
	}
}

// eastboundTrail is an aircraft whose whole track runs due east along the
// receiver's own latitude, so every fix projects onto one row of pixels and a
// test can read the trail off that row from tail to head.
//
// The aircraft itself is parked far outside the range, so neither a silhouette
// nor a selection ring lands on the row the trail is read from.
func eastboundTrail() airplane.Snapshot {
	const (
		nmPerDegree = 60.0
		halfCircle  = 180.0
		fixes       = 5
		firstNm     = 10.0
		spacingNm   = 5.0
		outsideNm   = 200.0
		trailAlt    = 2400.0
	)

	east := func(nm float64) float64 {
		return receiverLon + nm/(nmPerDegree*math.Cos(receiverLat*math.Pi/halfCircle))
	}

	trail := make([]airplane.PositionEntry, 0, fixes)
	for fix := range fixes {
		trail = append(trail, airplane.PositionEntry{
			Latitude:  receiverLat,
			Longitude: east(firstNm + float64(fix)*spacingNm),
			Altitude:  trailAlt,
		})
	}

	return airplane.Snapshot{
		ICAO: "484AC1", Callsign: "KLM123", Altitude: trailAlt, Heading: 90, Velocity: 420,
		Latitude: receiverLat, Longitude: east(outsideNm),
		LastUpdate: sceneClock, Squawk: "1000", PositionHistory: trail,
	}
}

// trailRun collects the colours of every pixel on one row of the scope that
// came from a trail: not the field, and not the rings and cardinals drawn in
// the palette's rule colour.
func trailRun(canv *canvas.Canvas, row, fromX, toX int) []color.RGBA {
	var out []color.RGBA

	for x := fromX; x < toX; x++ {
		col := canv.Image().RGBAAt(x, row)
		if col == theme.Night.Field || col == theme.Night.Rule {
			continue
		}

		out = append(out, col)
	}

	return out
}

// TestTrailAllDrawsTheWholeTrailInOneColour is what the all mode is for:
// every segment carries the aircraft's own colour instead of fading towards
// the field, so the tail reads exactly as the head does.
func TestTrailAllDrawsTheWholeTrailInOneColour(t *testing.T) {
	t.Parallel()

	// The row the eastbound trail projects onto, and a window along it that
	// starts clear of the home marker and ends clear of the outer ring.
	row := scopeBox.Min.Y + scopeBox.Dy()/2
	fromX := scopeBox.Min.X + scopeBox.Dx()/2 + 20
	toX := scopeBox.Max.X - 20

	draw := func(tb testing.TB, presses int) []color.RGBA {
		tb.Helper()

		scene, canv, _ := sceneOn(tb, panelWidth, panelHeight, sceneFrame(eastboundTrail()))

		// The airfield markers and their ICAO labels are the only other thing
		// that lands on this row, so they go: what is left is the trail.
		scene.Apply(bareScope())
		pressTrails(tb, scene, presses)
		scene.Draw(canv, 0)

		run := trailRun(canv, row, fromX, toX)
		if len(run) < 2 {
			tb.Fatalf("the trail drew %d pixels on row %d, want a run to read", len(run), row)
		}

		return run
	}

	flat := draw(t, pressAll)
	fading := draw(t, 0)

	if flat[0] != flat[len(flat)-1] {
		t.Errorf("the all mode's tail %v, head %v, want the same colour", flat[0], flat[len(flat)-1])
	}

	if flat[0] != theme.Night.AltLow {
		t.Errorf("the all mode's tail = %v, want the aircraft's own band colour %v", flat[0], theme.Night.AltLow)
	}

	if fading[0] == fading[len(fading)-1] {
		t.Error("the long mode drew its tail in the head's colour, want it faded towards the field")
	}
}

// The source label the operator's own live feed produces, which is what
// exposed the fixed rune cap: it is twenty-five characters, and the old cap
// of twenty-four cut the last digit off a port number on a band with nine
// hundred pixels to spare.
const (
	beastLabel      = "BEAST 192.168.1.159:30005"
	beastLabelTwin  = "BEAST 192.168.1.159:30009"
	narrowPanelWide = 480
)

// headerBand is the row of the header the source label and the clocks are set
// on, from just past the connection dot to the frame's right margin.
//
// The rows stop above the receiver line rather than at the bottom of the
// band. The line runs from the left margin well past the dot, so a band that
// reached it would report the receiver's own coordinates as the leftmost thing
// drawn and every caller below measures the clocks by exactly that.
//
// It moved up eight pixels when the header's type was centred in the fill
// rather than in the band the layout reserves under the margin.
func headerBand(width int) image.Rectangle {
	const (
		pastDot   = 98
		bandTop   = 16
		bandUnder = 36
	)

	return image.Rect(pastDot, bandTop, width-16, bandUnder)
}

// paintedEnd reports the rightmost column of box that has anything drawn in
// it, or -1 for an empty box.
//
// Every caller below only ever wants where a run of painted columns ends, to
// compare it against another run's end, so that is the only edge this reports:
// a leftmost edge nothing here reads is a result nobody can claim is tested.
//
//nolint:varnamelen // x is the pixel-addressing idiom used throughout uScope.
func paintedEnd(canv *canvas.Canvas, box image.Rectangle) int {
	last := -1

	for x := box.Min.X; x < box.Max.X; x++ {
		if painted(canv, image.Rect(x, box.Min.Y, x+1, box.Max.Y)) == 0 {
			continue
		}

		last = x
	}

	return last
}

// sourceLabelScene draws one frame with a given source label at a given width.
func sourceLabelScene(tb testing.TB, width int, label string) *canvas.Canvas {
	tb.Helper()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	frame.Source = adsb.SourceInfo{Label: label, Connected: true}

	scene, canv, _ := sceneOn(tb, width, panelHeight, frame)
	scene.Draw(canv, 0)

	return canv
}

// TestSourceLabelFitsTheBand checks the measured fit that replaced the rune
// cap: a label with room to spare is drawn whole, and one without room is cut
// short of the clocks rather than over them.
func TestSourceLabelFitsTheBand(t *testing.T) {
	t.Parallel()

	t.Run("a label with room to spare is drawn whole", func(t *testing.T) {
		t.Parallel()

		// The header band's own fill no longer sits flush with the field, so
		// paintedEnd would find the band painted to its far edge whatever the
		// label says. The source label and the clocks are both Data, so an
		// empty label leaves only the clocks to find with colourExtent.
		band := headerBand(panelWidth)
		blockLeft, _ := colourExtent(sourceLabelScene(t, panelWidth, ""), band, theme.Night.Data)
		labelBand := image.Rect(band.Min.X, band.Min.Y, blockLeft, band.Max.Y)

		_, full := colourExtent(sourceLabelScene(t, panelWidth, beastLabel), labelBand, theme.Night.Data)
		_, twin := colourExtent(sourceLabelScene(t, panelWidth, beastLabelTwin), labelBand, theme.Night.Data)
		_, shorter := colourExtent(
			sourceLabelScene(t, panelWidth, beastLabel[:len(beastLabel)-1]), labelBand, theme.Night.Data)

		// Two labels that differ only in their last character reach the same
		// column, and both reach one glyph further than the same label with
		// that character removed. A cut label would do neither.
		if full != twin {
			t.Errorf("two labels differing in their last character ended at %d and %d, want the same", full, twin)
		}

		if full <= shorter {
			t.Errorf("the whole label ended at %d and a shorter one at %d, want the whole one further right",
				full, shorter)
		}

		if full > blockLeft {
			t.Errorf("the label reached %d, past the clocks at %d", full, blockLeft)
		}
	})

	t.Run("a label with no room is cut short of the clocks", func(t *testing.T) {
		t.Parallel()

		const gap = 24 // radar's own sourceLabelGap.

		band := headerBand(narrowPanelWide)
		blockLeft, _ := colourExtent(sourceLabelScene(t, narrowPanelWide, ""), band, theme.Night.Data)
		labelBand := image.Rect(band.Min.X, band.Min.Y, blockLeft, band.Max.Y)

		cut := sourceLabelScene(t, narrowPanelWide, beastLabel)
		_, end := colourExtent(cut, labelBand, theme.Night.Data)

		if end < 0 {
			t.Fatal("nothing at all was drawn for the source label")
		}

		if end > blockLeft-gap {
			t.Errorf("the cut label ended at %d, want it to stop by %d", end, blockLeft-gap)
		}

		// Cut at the same place, so two labels that differ only past the cut
		// draw the same band. That is what proves a cut happened at all.
		twin := sourceLabelScene(t, narrowPanelWide, beastLabelTwin)
		if !identicalIn(cut, twin, labelBand) {
			t.Error("two labels differing only after the cut drew differently, want both cut at the same place")
		}

		// The marker is a glyph of its own, so the cut label reaches further
		// than the bare prefix it was cut to.
		runes := []rune(beastLabel)

		bare := sourceLabelScene(t, narrowPanelWide, string(runes[:len(runes)/2]))
		if identicalIn(cut, bare, labelBand) {
			t.Error("the cut label drew nothing a shorter label would not, want a marker after it")
		}
	})
}

// keyCapBox is where one cap lands in the bottom bar at the panel's
// resolution. The three caps before AUTO are all fixed-width, so AUTO's own
// box does not move when a label further along changes.
//
// Every cap is the same grey box regardless of its state now: what changes is
// a 2px bar in the OK colour along the inside of the bottom edge when the
// setting is engaged, inset keyEngagedInset from each side. autoEngagedBar
// below is that strip, worked out from autoCapBox the way drawEngaged works
// it out from a cap's own box.
//
//nolint:gochecknoglobals // a rectangle is data.
var (
	quitCapBox = image.Rect(16, 682, 64, 704)
	autoCapBox = image.Rect(220, 682, 268, 704)
	keyBarBox  = image.Rect(0, 682, 1280, 704)

	autoEngagedBar = image.Rect(
		autoCapBox.Min.X+keyEngagedInsetTest, autoCapBox.Max.Y-keyPadYTest,
		autoCapBox.Max.X-keyEngagedInsetTest, autoCapBox.Max.Y-keyPadYTest+keyEngagedHeightTest,
	)

	// biasCapBox is the B cap the bar draws once a frame says the source has
	// a bias-tee, at the panel's resolution. It sits right after the twelve
	// fixed caps in keyCaps, so its left edge is where the bar's painted
	// extent ends when the cap is not there at all.
	biasCapBox     = image.Rect(725, 682, 787, 704)
	biasEngagedBar = image.Rect(
		biasCapBox.Min.X+keyEngagedInsetTest, biasCapBox.Max.Y-keyPadYTest,
		biasCapBox.Max.X-keyEngagedInsetTest, biasCapBox.Max.Y-keyPadYTest+keyEngagedHeightTest,
	)
)

// The three measurements a softkey's engaged bar is built from, spelled out
// here rather than imported because the package under test keeps its own
// copies unexported.
const (
	keyPadYTest          = 3
	keyEngagedInsetTest  = 2
	keyEngagedHeightTest = 2
)

// TestKeyCapsShowToggleState checks the one thing the bar could not say
// before: whether a toggle is on. Every cap is the same grey box whatever its
// state, and an engaged one grows a green bar along its bottom edge that an
// idle one does not.
func TestKeyCapsShowToggleState(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	on, onCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	on.Draw(onCanvas, 0)

	off, offCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	press(off, 'r')
	off.Draw(offCanvas, 0)

	if got := countColour(onCanvas, autoEngagedBar, theme.Night.OK); got == 0 {
		t.Error("auto on: no OK pixels in the engaged bar, want it drawn")
	}

	if got := countColour(offCanvas, autoEngagedBar, theme.Night.OK); got != 0 {
		t.Errorf("auto off: %d OK pixels in the engaged bar, want none", got)
	}

	// q has no setting behind it, so its cap never grows a bar and is drawn
	// identically whatever the toggle under test is doing.
	if !identicalIn(onCanvas, offCanvas, quitCapBox) {
		t.Error("the quit cap changed with a toggle it has nothing to do with")
	}
}

// TestKeyCapsShowTheColourMode checks the other half of item seven: the
// cycling keys are labelled with the value they are on. ALT and AIRLINE are
// different lengths, so the caps after them move, which is what this measures.
func TestKeyCapsShowTheColourMode(t *testing.T) {
	t.Parallel()

	const (
		labelDelta = 4 // AIRLINE is four characters longer than ALT.
		smallGlyph = 6

		// labelTrackingTest is the letter spacing every softkey label is set
		// at, spelled out here for the same reason smallGlyph is: four more
		// glyphs also means four more gaps between them.
		labelTrackingTest = 1
	)

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	altitude, altitudeCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame)
	altitude.Draw(altitudeCanvas, 0)

	airline, airlineCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithColour(radar.ColourAirline))
	airline.Draw(airlineCanvas, 0)

	altitudeEnd := paintedEnd(altitudeCanvas, keyBarBox)
	airlineEnd := paintedEnd(airlineCanvas, keyBarBox)

	if want := labelDelta * (smallGlyph + labelTrackingTest); airlineEnd-altitudeEnd != want {
		t.Errorf("the key bar grew by %d pixels in airline mode, want %d", airlineEnd-altitudeEnd, want)
	}
}

// TestBiasCapAppearsOnlyWhenSupported checks that the B BIAS-T cap is only
// added to the bar when the frame says the source has one to flip. A cap for
// a control that does nothing is furniture pretending to be a switch, which
// is why the bar leaves it out entirely under --demo, --beast and
// --replay-iq rather than drawing it disabled.
func TestBiasCapAppearsOnlyWhenSupported(t *testing.T) {
	t.Parallel()

	unsupported := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	unsupported.BiasTee = source.BiasTeeState{Supported: false}

	supported := unsupported
	supported.BiasTee = source.BiasTeeState{Supported: true}

	without, withoutCanvas, _ := sceneOn(t, panelWidth, panelHeight, unsupported)
	without.Draw(withoutCanvas, 0)

	with, withCanvas, _ := sceneOn(t, panelWidth, panelHeight, supported)
	with.Draw(withCanvas, 0)

	withoutEnd := paintedEnd(withoutCanvas, keyBarBox)
	withEnd := paintedEnd(withCanvas, keyBarBox)

	if withEnd <= withoutEnd {
		t.Errorf("key bar end with a bias-tee supported = %d, without = %d, want it further right", withEnd, withoutEnd)
	}
}

// TestBiasCapShowsToggleState checks that the B cap follows the same on/off
// convention every other toggle cap does: the engaged bar along its bottom
// edge when the bias-tee is on, and nothing there when it is off.
func TestBiasCapShowsToggleState(t *testing.T) {
	t.Parallel()

	base := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	enabledFrame := base
	enabledFrame.BiasTee = source.BiasTeeState{Supported: true, Enabled: true}

	disabledFrame := base
	disabledFrame.BiasTee = source.BiasTeeState{Supported: true, Enabled: false}

	onScene, onCanvas, _ := sceneOn(t, panelWidth, panelHeight, enabledFrame)
	onScene.Draw(onCanvas, 0)

	offScene, offCanvas, _ := sceneOn(t, panelWidth, panelHeight, disabledFrame)
	offScene.Draw(offCanvas, 0)

	if got := countColour(onCanvas, biasEngagedBar, theme.Night.OK); got == 0 {
		t.Error("bias-tee on: no OK pixels in the engaged bar, want it drawn")
	}

	if got := countColour(offCanvas, biasEngagedBar, theme.Night.OK); got != 0 {
		t.Errorf("bias-tee off: %d OK pixels in the engaged bar, want none", got)
	}
}

// sweepScene draws one frame with a given source label and sweep state at the
// panel's resolution, which is what the two SWEEP marker tests below both
// build on.
func sweepScene(tb testing.TB, label string, sweeping bool) *canvas.Canvas {
	tb.Helper()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	frame.Source = adsb.SourceInfo{Label: label, Connected: true}
	frame.Sweeping = sweeping

	scene, canv, _ := sceneOn(tb, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	return canv
}

// TestSweepMarkerInHeader checks that SWEEP only shows up in the header while
// the gain sweep is running, and only in the caution colour. A sweep decodes
// nothing for a few seconds, so this is the one line on screen that explains
// why the scope looks empty rather than broken, and it must not linger once
// the sweep has finished.
func TestSweepMarkerInHeader(t *testing.T) {
	t.Parallel()

	band := headerBand(panelWidth)

	still := sweepScene(t, "SDR", false)
	sweeping := sweepScene(t, "SDR", true)

	if got := countColour(still, band, theme.Night.Caution); got != 0 {
		t.Errorf("caution pixels in the header with no sweep running = %d, want 0", got)
	}

	if got := countColour(sweeping, band, theme.Night.Caution); got == 0 {
		t.Error("no caution pixels in the header while the sweep is running, want the SWEEP marker drawn")
	}
}

// sweepLabelEnd is the rightmost column in box holding either the source
// label (Data) or the SWEEP marker after it (Caution), or -1 if neither
// drew anything: the header band's own fill no longer sits flush with the
// field, so paintedEnd would find the band painted to its far edge
// regardless of what the label says.
func sweepLabelEnd(canv *canvas.Canvas, box image.Rectangle) int {
	_, dataEnd := colourExtent(canv, box, theme.Night.Data)
	_, cautionEnd := colourExtent(canv, box, theme.Night.Caution)

	return max(dataEnd, cautionEnd)
}

// TestSweepMarkerLeavesRoomInALongLabel checks that the marker's room comes
// out of the label's own budget rather than being appended past the header's
// right edge. A long --beast address is already cut to fit before the clocks
// on the other side of the band, and the marker must never push it past them.
func TestSweepMarkerLeavesRoomInALongLabel(t *testing.T) {
	t.Parallel()

	const (
		longLabelRunes = 80
		gap            = 24 // radar's own sourceLabelGap.
	)

	longLabel := strings.Repeat("X", longLabelRunes)

	band := headerBand(panelWidth)
	blockLeft, _ := colourExtent(sourceLabelScene(t, panelWidth, ""), band, theme.Night.Data)
	labelBand := image.Rect(band.Min.X, band.Min.Y, blockLeft, band.Max.Y)

	still := sweepScene(t, longLabel, false)
	sweeping := sweepScene(t, longLabel, true)

	stillEnd := sweepLabelEnd(still, labelBand)
	sweepEnd := sweepLabelEnd(sweeping, labelBand)

	if stillEnd < 0 || sweepEnd < 0 {
		t.Fatal("nothing at all was drawn for the source label")
	}

	if stillEnd > blockLeft-gap {
		t.Errorf("the label alone ended at %d, want it to stop by %d", stillEnd, blockLeft-gap)
	}

	if sweepEnd > blockLeft-gap {
		t.Errorf("the label with the sweep marker ended at %d, want it to stop by %d", sweepEnd, blockLeft-gap)
	}
}

// liveScene builds a scene over a source whose frame a test can replace
// between draws, which is what a re-sorted aircraft list looks like from here.
func liveScene(tb testing.TB, frame source.Frame) (*radar.Scene, *fakeSource, *canvas.Canvas) {
	tb.Helper()

	src := &fakeSource{frame: frame}
	scene := radar.New(testFaces(tb), src, scope.New(scope.WithCurrent(sceneRangeNm)))

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	return scene, src, canv
}

// TestSelectionFollowsTheNearestUntilPinned is the rule the live scope was
// getting wrong: the first aircraft to arrive with a position kept the panel
// for as long as it stayed in range, however far away it drifted.
func TestSelectionFollowsTheNearestUntilPinned(t *testing.T) {
	t.Parallel()

	fleet := selectionFleet()

	t.Run("unpinned, the selection follows a re-sorted list", func(t *testing.T) {
		t.Parallel()

		scene, src, canv := liveScene(t, sceneFrame(fleet...))
		scene.Draw(canv, 0)

		nearest, nearestCanvas, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(fleet...))
		nearest.Draw(nearestCanvas, 0)

		if !identicalIn(canv, nearestCanvas, panelBox) {
			t.Fatal("the first frame did not select the nearest aircraft")
		}

		// A different aircraft is now nearest. The panel has to move with it.
		reordered := []airplane.Snapshot{fleet[1], fleet[0], fleet[2]}
		src.frame = sceneFrame(reordered...)

		scene.Draw(canv, 0)

		moved, movedCanvas, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(reordered...))
		moved.Draw(movedCanvas, 0)

		if !identicalIn(canv, movedCanvas, panelBox) {
			t.Error("the selection stayed behind when a nearer aircraft arrived, want it on the nearest")
		}
	})

	t.Run("pinned, the selection survives a re-sorted list", func(t *testing.T) {
		t.Parallel()

		scene, src, canv := liveScene(t, sceneFrame(fleet...))
		scene.Draw(canv, 0)
		press(scene, 'n') // pins MIDDLE.
		scene.Draw(canv, 0)

		pinned, pinnedCanvas, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(fleet...))
		pinned.Draw(pinnedCanvas, 0)
		press(pinned, 'n')
		pinned.Draw(pinnedCanvas, 0)

		// MIDDLE is pinned. Re-sort so a different aircraft is nearest and
		// MIDDLE is last: an unpinned scene would follow the new nearest, and
		// this one has to stay where it was put.
		reordered := []airplane.Snapshot{fleet[2], fleet[0], fleet[1]}
		src.frame = sceneFrame(reordered...)

		scene.Draw(canv, 0)

		nearestNow, nearestCanvas, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(reordered...))
		nearestNow.Draw(nearestCanvas, 0)

		if identicalIn(canv, nearestCanvas, panelBox) {
			t.Error("a pinned selection moved to the nearest aircraft, want it to stay where it was put")
		}
	})

	t.Run("esc lets go and the nearest wins again", func(t *testing.T) {
		t.Parallel()

		scene, _, canv := liveScene(t, sceneFrame(fleet...))
		scene.Draw(canv, 0)
		press(scene, 'n')
		scene.Draw(canv, 0)

		if !scene.Handle(input.Key{Kind: input.Esc}) {
			t.Fatal("Esc was not taken by the radar scene, want it to unpin rather than quit")
		}

		scene.Draw(canv, 0)

		nearest, nearestCanvas, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(fleet...))
		nearest.Draw(nearestCanvas, 0)

		if !identicalIn(canv, nearestCanvas, panelBox) {
			t.Error("Esc left the selection where it was, want it back on the nearest")
		}
	})
}

// sentinelPlane is one aircraft with a heading and a velocity set by hand, so
// a test can hand the scene the figures uAirwaves marks as undecoded.
func sentinelPlane(heading, velocity float64) airplane.Snapshot {
	plane := scenePlane("484AC1", "KLM123", 45, 12, 2400, heading)
	plane.Heading = heading
	plane.Velocity = velocity

	return plane
}

// TestSentinelsAreNotDrawnAsFigures covers the sentinels uAirwaves uses for a
// heading and a velocity no message has carried: -1 for both, where zero is
// due north and a genuine standstill. The live scope drew the velocity
// sentinel as "-1" and treated a heading of zero as undecoded, which are the
// same mistake made in both directions.
func TestSentinelsAreNotDrawnAsFigures(t *testing.T) {
	t.Parallel()

	const (
		sentinel  = -1.0
		dueNorth  = 0.0
		eastwards = 90.0
		cruise    = 420.0
	)

	draw := func(tb testing.TB, heading, velocity float64) *canvas.Canvas {
		tb.Helper()

		scene, canv, _ := sceneOn(tb, panelWidth, panelHeight, sceneFrame(sentinelPlane(heading, velocity)))
		scene.Draw(canv, 0)

		return canv
	}

	northKnown := draw(t, dueNorth, cruise)
	headingUnknown := draw(t, sentinel, cruise)
	eastKnown := draw(t, eastwards, cruise)
	speedUnknown := draw(t, dueNorth, sentinel)
	stationary := draw(t, dueNorth, 0)

	for _, testCase := range []struct {
		name  string
		left  *canvas.Canvas
		right *canvas.Canvas
		same  bool
	}{
		{
			name: "due north is a course, not the undecoded state",
			left: northKnown, right: headingUnknown, same: false,
		},
		{
			name: "due north draws a silhouette like any other course",
			left: northKnown, right: eastKnown, same: false,
		},
		{
			name: "an undecoded velocity is not the same figure as a standstill",
			left: speedUnknown, right: stationary, same: false,
		},
		{
			name: "a known speed and an undecoded one differ in the panel",
			left: northKnown, right: speedUnknown, same: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := identical(testCase.left, testCase.right); got != testCase.same {
				t.Errorf("the two frames were identical = %v, want %v", got, testCase.same)
			}
		})
	}

	// The undecoded heading is the only one drawn as a bare circle, so it is
	// the only one whose silhouette does not rotate with the course.
	if !identicalIn(headingUnknown, draw(t, sentinel, cruise), scopeBox) {
		t.Error("two frames with the same undecoded heading drew different scopes")
	}
}

// --- following the traffic in minimal mode --------------------------------

// The pixel geometry these tests read positions off. The canvas is 1280x720,
// so minimal mode's centre is (640, 360) and its radius is half the short
// edge; with the range pinned at 60 nautical miles that is six pixels to the
// nautical mile.
const (
	minimalCentreX = panelWidth / 2
	minimalCentreY = panelHeight / 2
	pixelsPerNm    = (panelHeight / 2) / sceneRangeNm

	// followProbe is the half-side of the box these tests count pixels in. It
	// is a little wider than a 15 pixel silhouette so a sprite that lands a
	// pixel either side of where the arithmetic says still counts.
	followProbe = 12
)

// glideSpanNs repeats internal/radar's own glideSpan, which an external test
// cannot see. The package's TestGlide is what pins the real one; this is only
// how long a draw here has to claim to be from the last for the glide to have
// run its course.
const glideSpanNs = 2 * time.Second

// stepClock is a clock a test moves by hand.
//
// The scene reads it through radar.WithClock on any frame that carries no
// timestamp of its own, which is how the recentring cadence gets driven
// without a test waiting three minutes for it.
type stepClock struct{ at time.Time }

func (c *stepClock) now() time.Time       { return c.at }
func (c *stepClock) step(d time.Duration) { c.at = c.at.Add(d) }

// undated strips a frame's timestamp so the scene falls back to the clock the
// test is holding.
func undated(frame source.Frame) source.Frame {
	frame.Now = time.Time{}

	return frame
}

// followInterval is the cadence every test here runs at. A minute is long
// enough to step a clock either side of without the arithmetic getting fussy,
// and the real default would make every case wait three times as long for
// nothing.
const followInterval = time.Minute

// probe is a box around a pixel on minimal mode's centre column, which is
// where every position these tests read lands: the fleets are due north or
// due south of the receiver, so only the row ever changes.
func probe(y int) image.Rectangle {
	return image.Rect(minimalCentreX-followProbe, y-followProbe, minimalCentreX+followProbe, y+followProbe)
}

// followScene builds a minimal-mode scene wired to a source and a clock the
// test drives, with the range pinned so the centring can be watched on its
// own without auto range moving the scale underneath it.
func followScene(tb testing.TB, src source.Source, clock *stepClock) (*radar.Scene, *canvas.Canvas) {
	tb.Helper()

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	scene := radar.New(testFaces(tb), src, scope.New(scope.WithCurrent(sceneRangeNm)),
		radar.WithClock(clock.now))
	scene.Apply(radar.Settings{View: radar.ViewMinimal, Recentre: followInterval, RangeNm: sceneRangeNm})

	return scene, canv
}

// TestRecentreCadence is the cadence itself: the first aircraft with a
// position is centred on at once, a fleet that moves inside the interval is
// left where the last centring put it, and the next interval brings it back
// to the middle.
//
// The aircraft is read off the canvas rather than out of the scene, because
// where it lands is the whole of what recentring is for.
func TestRecentreCadence(t *testing.T) {
	t.Parallel()

	clock := &stepClock{at: sceneClock}
	north := undated(sceneFrame(scenePlane("484AC1", "KLM123", 0, 20, 2400, 180)))
	src := &fakeSource{frame: north}

	scene, canv := followScene(t, src, clock)
	scene.Draw(canv, 0)

	if painted(canv, probe(minimalCentreY)) == 0 {
		t.Fatal("the first centring did not put the only aircraft in the middle")
	}

	// The same aeroplane, now the other side of the receiver: 40 nautical
	// miles from where the centre was left, which at six pixels to the mile
	// is 240 pixels down the canvas.
	src.frame = undated(sceneFrame(scenePlane("484AC1", "KLM123", 180, 20, 2400, 0)))

	clock.step(followInterval - time.Second)
	scene.Draw(canv, glideSpanNs)

	if painted(canv, probe(minimalCentreY)) != 0 {
		t.Error("the centre moved before the interval was up, want it held")
	}

	moved := minimalCentreY + 40*pixelsPerNm
	if painted(canv, probe(moved)) == 0 {
		t.Errorf("nothing landed at y=%d, want the aircraft drawn against the held centre", moved)
	}

	// Past the interval now. The frame the cadence fires on is where the
	// glide starts, not where it lands, so the move takes the frame after it.
	clock.step(2 * time.Second)
	scene.Draw(canv, 2*glideSpanNs)
	scene.Draw(canv, 4*glideSpanNs)

	if painted(canv, probe(minimalCentreY)) == 0 {
		t.Error("the centre did not move after the interval, want the aircraft back in the middle")
	}
}

// TestRecentreGlidesRatherThanJumping checks that the move between two
// centres is spread over frames instead of landing in one. Halfway through
// the glide the aircraft has to be halfway between where it was drawn and
// where it is going, which is what keeps the trails sliding with it.
func TestRecentreGlidesRatherThanJumping(t *testing.T) {
	t.Parallel()

	clock := &stepClock{at: sceneClock}
	src := &fakeSource{frame: undated(sceneFrame(scenePlane("484AC1", "KLM123", 0, 20, 2400, 180)))}

	scene, canv := followScene(t, src, clock)
	scene.Draw(canv, 0)

	// Move the fleet 40 nautical miles south and let the interval come round.
	src.frame = undated(sceneFrame(scenePlane("484AC1", "KLM123", 180, 20, 2400, 0)))

	clock.step(followInterval)

	// The frame the glide starts on: the centre has not moved yet, so the
	// aircraft is still drawn 240 pixels below the middle.
	scene.Draw(canv, 0)

	if painted(canv, probe(minimalCentreY+40*pixelsPerNm)) == 0 {
		t.Fatal("the first frame of the glide had already moved, want it to start where it was")
	}

	// Halfway along, with the ease curve exactly at a half.
	scene.Draw(canv, glideSpanNs/2)

	if painted(canv, probe(minimalCentreY+20*pixelsPerNm)) == 0 {
		t.Error("nothing landed halfway, want the centre part of the way across")
	}

	scene.Draw(canv, glideSpanNs)

	if painted(canv, probe(minimalCentreY)) == 0 {
		t.Error("the glide did not finish, want the aircraft in the middle")
	}
}

// TestRecentreFitsTheRangeAroundTheCentroid checks that minimal mode following
// the traffic sizes the scope by how far the fleet is spread rather than by
// how far away it is. A tight group 50 nautical miles out needs a 20 mile
// scope around itself, not a 60 mile one around the receiver.
func TestRecentreFitsTheRangeAroundTheCentroid(t *testing.T) {
	t.Parallel()

	// Three aircraft strung out along one bearing, 39, 49 and 59 nautical
	// miles from the receiver. Their centroid is the middle one, and nothing
	// is more than ten miles from it.
	frame := undated(sceneFrame(
		scenePlane("484AC1", "KLM123", 315, 39, 2400, 41),
		scenePlane("4CA2D3", "RYR7X", 315, 49, 8500, 41),
		scenePlane("3C6745", "DLH4EA", 315, 59, 36000, 41),
	))

	for _, testCase := range []struct {
		name     string
		recentre time.Duration
		want     float64
	}{
		{name: "following fits around the centroid", recentre: time.Minute, want: 20},
		{name: "the cadence off fits around the receiver", want: 60},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(panelWidth, panelHeight)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			clock := &stepClock{at: sceneClock}
			ranges := scope.New(scope.WithCurrent(sceneRangeNm))
			scene := radar.New(testFaces(t), &fakeSource{frame: frame}, ranges, radar.WithClock(clock.now))
			scene.Apply(radar.Settings{View: radar.ViewMinimal, Recentre: testCase.recentre})

			scene.Draw(canv, 0)

			if got := ranges.GetCurrent(); got != testCase.want {
				t.Errorf("range after Draw = %g NM, want %g", got, testCase.want)
			}
		})
	}
}

// TestReceiverMarker checks the marker minimal mode draws where the antenna
// is, now that the picture is no longer centred on it.
//
// It is the palette's muted colour, which nothing else in minimal mode uses:
// the aircraft here are low-band green, and there is no furniture.
func TestReceiverMarker(t *testing.T) {
	t.Parallel()

	t.Run("it lands where the receiver projects to", func(t *testing.T) {
		t.Parallel()

		// One aircraft 20 nautical miles due north. Centred on it, the
		// receiver is 20 miles due south of the middle of the canvas, which
		// at six pixels to the mile is 120 pixels down.
		clock := &stepClock{at: sceneClock}
		src := &fakeSource{frame: undated(sceneFrame(scenePlane("484AC1", "KLM123", 0, 20, 2400, 180)))}

		scene, canv := followScene(t, src, clock)
		scene.Draw(canv, 0)

		where := probe(minimalCentreY + 20*pixelsPerNm)
		if countColour(canv, where, theme.Night.Muted) == 0 {
			t.Error("no muted pixels where the receiver projects to, want the marker there")
		}
	})

	t.Run("it is not drawn when it falls off the canvas", func(t *testing.T) {
		t.Parallel()

		// 120 nautical miles north is 720 pixels below the middle once the
		// centre has followed the traffic up there, which is past the bottom
		// edge of a 720 pixel canvas.
		clock := &stepClock{at: sceneClock}
		src := &fakeSource{frame: undated(sceneFrame(scenePlane("484AC1", "KLM123", 0, 120, 2400, 180)))}

		scene, canv := followScene(t, src, clock)
		scene.Draw(canv, 0)

		if got := countColour(canv, canv.Bounds(), theme.Night.Muted); got != 0 {
			t.Errorf("drew %d muted pixels with the receiver off the canvas, want 0", got)
		}
	})

	t.Run("it is not drawn when there is no receiver position", func(t *testing.T) {
		t.Parallel()

		frame := undated(sceneFrame(scenePlane("484AC1", "KLM123", 0, 20, 2400, 180)))
		frame.Receiver = source.Receiver{Label: source.LabelNone, Mode: source.FixNone}

		clock := &stepClock{at: sceneClock}
		scene, canv := followScene(t, &fakeSource{frame: frame}, clock)
		scene.Draw(canv, 0)

		if got := countColour(canv, canv.Bounds(), theme.Night.Muted); got != 0 {
			t.Errorf("drew %d muted pixels with no receiver position, want 0", got)
		}
	})
}

// --- minimal mode's own overlays ------------------------------------------

// TestMinimalShoreToggle checks that m draws the coastline in minimal mode,
// that minimal starts without it, and that pressing it there leaves the full
// scope's own coastline exactly as it was.
func TestMinimalShoreToggle(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	set := syntheticShoreSet(t, shoreLineThroughReceiver())

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithShore(set))
	scene.Apply(radar.Settings{RangeNm: sceneRangeNm})

	scene.Draw(canv, 0)

	fullScope := countColour(canv, canv.Bounds(), theme.Night.Shore)
	if fullScope == 0 {
		t.Fatal("the full scope drew no coastline, so this comparison proves nothing")
	}

	pressView(t, scene, pressViewMinimal)
	scene.Draw(canv, 0)

	if got := countColour(canv, canv.Bounds(), theme.Night.Shore); got != 0 {
		t.Errorf("minimal drew %d shore pixels before m, want 0", got)
	}

	press(scene, 'm')
	scene.Draw(canv, 0)

	if countColour(canv, canv.Bounds(), theme.Night.Shore) == 0 {
		t.Error("minimal drew no shore pixels after m, want the coastline")
	}

	pressView(t, scene, pressViewRound-pressViewMinimal)
	scene.Draw(canv, 0)

	if got := countColour(canv, canv.Bounds(), theme.Night.Shore); got != fullScope {
		t.Errorf("the full scope drew %d shore pixels after m in minimal, want the original %d", got, fullScope)
	}
}

// TestMinimalAirportsToggle is TestMinimalShoreToggle for a. The markers are
// the palette's rule colour, which in minimal mode nothing else uses: there
// are no rings there for it to be confused with.
func TestMinimalAirportsToggle(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(radar.Settings{RangeNm: sceneRangeNm})

	scene.Draw(canv, 0)

	fullScope := countColour(canv, canv.Bounds(), theme.Night.Rule)
	if fullScope == 0 {
		t.Fatal("the full scope drew no rule-coloured pixels, so this comparison proves nothing")
	}

	pressView(t, scene, pressViewMinimal)
	scene.Draw(canv, 0)

	if got := countColour(canv, canv.Bounds(), theme.Night.Rule); got != 0 {
		t.Errorf("minimal drew %d airfield pixels before a, want 0", got)
	}

	press(scene, 'a')
	scene.Draw(canv, 0)

	if countColour(canv, canv.Bounds(), theme.Night.Rule) == 0 {
		t.Error("minimal drew no airfield pixels after a, want the markers")
	}

	pressView(t, scene, pressViewRound-pressViewMinimal)
	scene.Draw(canv, 0)

	if got := countColour(canv, canv.Bounds(), theme.Night.Rule); got != fullScope {
		t.Errorf("the full scope drew %d rule pixels after a in minimal, want the original %d", got, fullScope)
	}
}

// TestMinimalOverlaysFollowTheCentre checks that minimal's overlays are drawn
// around its own centre rather than around the receiver. A coastline that
// runs through the receiver has to move on the canvas once the picture has
// followed the traffic away from it.
func TestMinimalOverlaysFollowTheCentre(t *testing.T) {
	t.Parallel()

	set := syntheticShoreSet(t, shoreLineThroughReceiver())
	plane := scenePlane("484AC1", "KLM123", 90, 30, 2400, 270)

	canvases := make([]*canvas.Canvas, 0, 2)

	for _, every := range []time.Duration{0, time.Minute} {
		canv, err := canvas.New(panelWidth, panelHeight)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		clock := &stepClock{at: sceneClock}
		scene := radar.New(testFaces(t), &fakeSource{frame: undated(sceneFrame(plane))},
			scope.New(scope.WithCurrent(sceneRangeNm)), radar.WithClock(clock.now), radar.WithShore(set))
		scene.Apply(radar.Settings{View: radar.ViewMinimal, Recentre: every, RangeNm: sceneRangeNm})
		press(scene, 'm')
		scene.Draw(canv, 0)

		if countColour(canv, canv.Bounds(), theme.Night.Shore) == 0 {
			t.Fatalf("no coastline drawn with --recenter %v, want one", every)
		}

		canvases = append(canvases, canv)
	}

	if identical(canvases[0], canvases[1]) {
		t.Error("the coastline landed in the same place centred and following, want it to move with the centre")
	}
}

// --- the header band ------------------------------------------------------

// TestHeaderBandBleedsToTheEdges checks that the band fills the canvas from
// edge to edge rather than sitting inside the layout margin, and that the
// hairline under it runs the full width too. Paper is used because night's
// band repeats the field colour on purpose, which would make the count
// meaningless.
//
// Both outside columns are read, so a band that reached one edge and not the
// other would still fail.
func TestHeaderBandBleedsToTheEdges(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithPalette(theme.Day))
	scene.Draw(canv, 0)

	for _, column := range []int{0, panelWidth - 1} {
		if got := canv.Image().RGBAAt(column, 0); got != theme.Day.Band {
			t.Errorf("pixel (%d, 0) = %v, want the band colour %v", column, got, theme.Day.Band)
		}

		rule := bandBottom(t, canv, column)
		if got := canv.Image().RGBAAt(column, rule); got != theme.Day.Rule {
			t.Errorf("pixel (%d, %d) = %v, want the hairline %v", column, rule, got, theme.Day.Rule)
		}

		if got := canv.Image().RGBAAt(column, rule+1); got != theme.Day.Field {
			t.Errorf("pixel (%d, %d) = %v, want the field under the hairline %v",
				column, rule+1, got, theme.Day.Field)
		}
	}
}

// bandBottom walks one column down from the top and reports the first row
// that is no longer the band, which is where the hairline sits.
func bandBottom(tb testing.TB, canv *canvas.Canvas, column int) int {
	tb.Helper()

	for y := range canv.Bounds().Dy() {
		if canv.Image().RGBAAt(column, y) != theme.Day.Band {
			return y
		}
	}

	tb.Fatalf("column %d is band colour all the way down, so there is no hairline to find", column)

	return 0
}

// TestMinimalOverlaysWithNowhereToDrawThem checks the case where minimal mode
// has been asked for its overlays and has no idea where it is: no receiver
// position and nothing in the sky to work a centre out from. The layer is
// built, because the toggles say so, and nothing lands on it.
func TestMinimalOverlaysWithNowhereToDrawThem(t *testing.T) {
	t.Parallel()

	frame := sceneFrame()
	frame.Receiver = source.Receiver{Label: source.LabelNone, Mode: source.FixNone}

	set := syntheticShoreSet(t, shoreLineThroughReceiver())

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithShore(set))
	scene.Apply(radar.Settings{View: radar.ViewMinimal, Recentre: radar.DefaultRecentre})
	press(scene, 'm')
	press(scene, 'a')
	scene.Draw(canv, 0)

	if got := painted(canv, canv.Bounds()); got != 0 {
		t.Errorf("minimal painted %d pixels with no position to draw against, want 0", got)
	}
}

// The selected strip's DIST/BRG field at the panel's resolution, split into
// where the bearing figure (or its three dashes) lands and where the arrow
// that follows it lands.
//
// The numbers are read off a render rather than recomputed here, the same way
// the header band above them is: the faces are sized from their own metrics
// alone, so the first strip lands on the same pixels every time.
//
// The selected strip's fill sits behind both boxes, so a bare "was anything
// painted" check would always be true whether or not a figure landed in it;
// the tests below check for the specific ink or muted colour a figure or a
// placeholder is set in instead.
//
//nolint:gochecknoglobals // rectangles are data, and image.Rectangle cannot be const.
var (
	rowBearingDigits = image.Rect(1008, 108, 1042, 140)
	rowBearingArrow  = image.Rect(1058, 108, 1070, 140)
)

// cardTrackArrow is the selected strip's TRK field to the right of its
// degrees, which is where the arrow goes and where nothing else is ever
// drawn. It carries the same fill-background caveat as the two boxes above.
//
//nolint:gochecknoglobals // ditto.
var cardTrackArrow = image.Rect(979, 108, 989, 140)

// stripFillerPlane is a decoy aircraft that always sits first in a frame's
// plane list, so it takes the selected full strip and whatever is listed
// after it lands on a half strip instead: plain field background, with none
// of the selected strip's fill behind it.
func stripFillerPlane() airplane.Snapshot {
	return scenePlane("AAA111", "FILLER1", 0, 5, 2000, 10)
}

// fullStripAttitudeCell is the little aeroplane at the right end of the
// selected strip's ident field, and stripAttitudeCell the same cell one row
// down, on the half strip a second aircraft in the frame lands on.
//
//nolint:gochecknoglobals // ditto.
var (
	fullStripAttitudeCell = image.Rect(794, 113, 818, 137)
	stripAttitudeCell     = image.Rect(794, 160, 818, 184)
)

// The identity the single-aircraft fixtures below fly under. They are the demo
// fleet's own first aircraft, so a render from a test and a render from --demo
// hold the same characters in the same columns.
const (
	icaoSample     = "484AC1"
	callsignSample = "KLM123"
)

// ghostOf is the trail an aircraft leaves behind when it stops transmitting:
// the history it last showed, with no position and no heading, because there
// is no longer an aeroplane to have either.
func ghostOf(plane airplane.Snapshot) source.Trail {
	return source.Trail{
		ICAO:     plane.ICAO,
		Callsign: plane.Callsign,
		Altitude: plane.Altitude,
		Points:   plane.PositionHistory,
	}
}

// ghostFrame is a frame carrying ghosts and whatever live aircraft go with
// them.
func ghostFrame(ghosts []source.Trail, planes ...airplane.Snapshot) source.Frame {
	frame := sceneFrame(planes...)
	frame.Ghosts = ghosts

	return frame
}

// bareScope is the settings a trail test draws under: a fixed range, and the
// two overlays off so the only thing left on the row being read is a track.
//
// The trail mode is not in here because it is not in Settings: a test that
// wants one other than the default presses t for it, through pressTrails.
func bareScope() radar.Settings {
	return radar.Settings{
		RangeNm:  sceneRangeNm,
		Airports: radar.ToggleOff,
		Shore:    radar.ToggleOff,
	}
}

// trailWindow is the row of the scope the eastbound fixtures project onto and
// a window along it that clears the home marker at one end and the outer ring
// at the other.
func trailWindow() (int, int, int) {
	const clearance = 20

	return scopeBox.Min.Y + scopeBox.Dy()/2,
		scopeBox.Min.X + scopeBox.Dx()/2 + clearance,
		scopeBox.Max.X - clearance
}

// TestGhostTrailDrawsAtFullStrength is what a ghost is for. The aircraft is
// gone, so there is no head for a fade to point at, and the whole track is
// drawn in the colour its last altitude earned it.
//
// The all mode is the only one that draws a ghost at all, and it is also the
// only one with no fade in it, so the two halves of the rule land on the same
// picture and this reads both of them at once.
func TestGhostTrailDrawsAtFullStrength(t *testing.T) {
	t.Parallel()

	row, fromX, toX := trailWindow()
	frame := ghostFrame([]source.Trail{ghostOf(eastboundTrail())})

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(bareScope())
	pressTrails(t, scene, pressAll)
	scene.Draw(canv, 0)

	run := trailRun(canv, row, fromX, toX)
	if len(run) < 2 {
		t.Fatalf("the ghost drew %d pixels on row %d, want a run to read", len(run), row)
	}

	for index, col := range run {
		if col != theme.Night.AltLow {
			t.Fatalf("ghost pixel %d = %v, want the band colour %v at full strength",
				index, col, theme.Night.AltLow)
		}
	}
}

// TestGhostTrailDrawsUnderTheLiveOnes checks the order the two are painted in.
//
// The ghost and the aircraft are put on the same fixes and given altitudes in
// different bands, so the row can only come out one colour or the other. A
// track nobody is flying must never hide one somebody is.
func TestGhostTrailDrawsUnderTheLiveOnes(t *testing.T) {
	t.Parallel()

	row, fromX, toX := trailWindow()

	live := eastboundTrail()

	// The ghost takes the same fixes and a cruising altitude, so it lands on
	// exactly the same pixels in a colour two bands away from the live one.
	const ghostAltitude = 36000.0

	ghost := ghostOf(live)
	ghost.ICAO, ghost.Altitude = "3C6745", ghostAltitude

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, ghostFrame([]source.Trail{ghost}, live))
	scene.Apply(bareScope())
	pressTrails(t, scene, pressAll)
	scene.Draw(canv, 0)

	run := trailRun(canv, row, fromX, toX)
	if len(run) < 2 {
		t.Fatalf("nothing drew on row %d, want both tracks there", row)
	}

	for index, col := range run {
		if col == theme.Night.AltHigh {
			t.Fatalf("pixel %d is the ghost's colour %v, want the live aircraft's %v on top",
				index, col, theme.Night.AltLow)
		}
	}
}

// TestGhostsAreDrawnOnlyByTheAllMode checks which of the four trail modes puts
// a lost contact on the field.
//
// Only the all mode does. The other three are about the traffic that is
// flying, and a track with no aeroplane on the end of it under a trail that
// is itself cut short or turned off would be a line with nothing to explain
// it. The last case is the ordinary one: a frame that carries no ghosts draws
// none whatever the mode says.
func TestGhostsAreDrawnOnlyByTheAllMode(t *testing.T) {
	t.Parallel()

	row, fromX, toX := trailWindow()
	ghosts := []source.Trail{ghostOf(eastboundTrail())}

	for _, testCase := range []struct {
		name        string
		ghosts      []source.Trail
		presses     int
		wantPainted bool
	}{
		{name: "the all mode", ghosts: ghosts, presses: pressAll, wantPainted: true},
		{name: "the long mode it starts in", ghosts: ghosts},
		{name: "the short mode", ghosts: ghosts, presses: pressShort},
		{name: "the off mode", ghosts: ghosts, presses: pressOff},
		{name: "a frame that kept no ghosts", ghosts: nil, presses: pressAll},
		{
			// An aircraft heard once, with a single position decoded before it
			// went quiet, left one point and no line. There is nothing to draw
			// and nothing wrong.
			name:    "a ghost of one fix",
			ghosts:  []source.Trail{{ICAO: "4951BA", Points: eastboundTrail().PositionHistory[:1]}},
			presses: pressAll,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, _ := sceneOn(t, panelWidth, panelHeight, ghostFrame(testCase.ghosts))
			scene.Apply(bareScope())
			pressTrails(t, scene, testCase.presses)
			scene.Draw(canv, 0)

			got := len(trailRun(canv, row, fromX, toX)) > 0
			if got != testCase.wantPainted {
				t.Errorf("anything drawn on row %d = %v, want %v", row, got, testCase.wantPainted)
			}
		})
	}
}

// TestGhostTrailIsClippedToTheRing checks that a ghost is cut off where every
// other plotted thing is. It is drawn through the same projection, so a track
// that ran off the edge of the range has to stop at the ring rather than
// carry on across the column beside it.
func TestGhostTrailIsClippedToTheRing(t *testing.T) {
	t.Parallel()

	const farNm = 500.0

	ghost := ghostOf(eastboundTrail())
	for index := range ghost.Points {
		ghost.Points[index].Longitude += farNm / nmPerDegreeTest
	}

	draw := func(tb testing.TB, ghosts []source.Trail) *canvas.Canvas {
		tb.Helper()

		scene, canv, _ := sceneOn(tb, panelWidth, panelHeight, ghostFrame(ghosts))
		scene.Apply(bareScope())
		pressTrails(tb, scene, pressAll)
		scene.Draw(canv, 0)

		return canv
	}

	if !identicalIn(draw(t, nil), draw(t, []source.Trail{ghost}), scopeBox) {
		t.Errorf("a ghost %g nm out changed the scope, want it cut off at the ring like everything else", farNm)
	}
}

// TestGhostTrailsDrawInMinimalMode checks the second view draws them too.
//
// Minimal mode is the aircraft and their trails on a bare field, and a ghost
// is one of those trails. It is also the view a lost contact shows up in
// best, since there is no furniture to read it against.
func TestGhostTrailsDrawInMinimalMode(t *testing.T) {
	t.Parallel()

	frame := ghostFrame([]source.Trail{ghostOf(eastboundTrail())})

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Apply(bareScope())
	pressTrails(t, scene, pressAll)

	if !press(scene, 'v') {
		t.Fatal("the v key was not handled, want the view toggle to take it")
	}

	scene.Draw(canv, 0)

	if painted(canv, canv.Bounds()) == 0 {
		t.Error("minimal mode drew nothing with a ghost on the field, want the ghost")
	}
}

// TestGhostColourFollowsTheColourMode checks a ghost is coloured by the same
// rule live traffic is. In airline mode that is the operator's own colour,
// read off the callsign the aircraft was last heard under, so a lost contact
// stays the colour it had while it was still talking.
func TestGhostColourFollowsTheColourMode(t *testing.T) {
	t.Parallel()

	row, fromX, toX := trailWindow()
	frame := ghostFrame([]source.Trail{ghostOf(eastboundTrail())})

	draw := func(tb testing.TB, mode radar.ColourMode) []color.RGBA {
		tb.Helper()

		// The mode goes into the settings rather than into an option, because
		// Apply sets every field of the block and would put an option's colour
		// mode straight back to the default.
		set := bareScope()
		set.Colour = mode

		scene, canv, _ := sceneOn(tb, panelWidth, panelHeight, frame)
		scene.Apply(set)
		pressTrails(tb, scene, pressAll)
		scene.Draw(canv, 0)

		run := trailRun(canv, row, fromX, toX)
		if len(run) == 0 {
			tb.Fatalf("the ghost drew nothing on row %d in %s mode", row, mode)
		}

		return run
	}

	byAltitude := draw(t, radar.ColourAltitude)
	byAirline := draw(t, radar.ColourAirline)

	if byAltitude[0] == byAirline[0] {
		t.Errorf("the ghost drew %v in both colour modes, want the operator's colour in airline mode",
			byAltitude[0])
	}
}

// TestGhostsStayOutOfTheAutoRange checks the one thing a ghost must not do to
// the scope around it.
//
// Auto range widens to hold the farthest contact, and a contact is something
// still transmitting. A ghost two hundred miles out is a track somebody flew,
// not somewhere the receiver can hear, so letting it set the range would zoom
// the scope out from traffic that is actually there.
func TestGhostsStayOutOfTheAutoRange(t *testing.T) {
	t.Parallel()

	const farNm = 200.0

	near := scenePlane(icaoSample, callsignSample, 90, 12, 2400, 90)

	ghost := ghostOf(eastboundTrail())
	for index := range ghost.Points {
		ghost.Points[index].Longitude += farNm / nmPerDegreeTest
	}

	rangeAfter := func(tb testing.TB, ghosts []source.Trail) float64 {
		tb.Helper()

		scene, canv, ranges := sceneOn(tb, panelWidth, panelHeight, ghostFrame(ghosts, near))
		scene.Draw(canv, 0)

		return ranges.GetCurrent()
	}

	without := rangeAfter(t, nil)
	with := rangeAfter(t, []source.Trail{ghost})

	if with != without {
		t.Errorf("auto range settled at %g nm with a ghost %g nm out, want %g nm as without it",
			with, farNm, without)
	}
}

// nmPerDegreeTest is one degree of latitude in nautical miles, which is what
// these tests push a fixture's fixes out by. It is spelled out here rather
// than imported because the package under test keeps its own copy unexported.
const nmPerDegreeTest = 60.0

// TestRowBearingArrowTurnsWithTheBearing checks that the arrow after the BRG
// figure is actually rotated to it rather than stamped the same way every
// time. Four aircraft at the four cardinal bearings have to paint four
// different pictures in the same cell.
func TestRowBearingArrowTurnsWithTheBearing(t *testing.T) {
	t.Parallel()

	const nearNm = 12.0

	type cell struct {
		name string
		canv *canvas.Canvas
	}

	bearings := []struct {
		name    string
		bearing float64
	}{
		{name: "north", bearing: 0},
		{name: "east", bearing: 90},
		{name: "south", bearing: 180},
		{name: "west", bearing: 270},
	}

	cells := make([]cell, 0, len(bearings))

	for _, testCase := range bearings {
		scene, canv, _ := sceneOn(t, panelWidth, panelHeight,
			sceneFrame(scenePlane(icaoSample, callsignSample, testCase.bearing, nearNm, 2400, testCase.bearing)))
		scene.Draw(canv, 0)

		if countColour(canv, rowBearingArrow, theme.Night.Ink) == 0 {
			t.Fatalf("a bearing of %g drew no arrow in the BRG cell", testCase.bearing)
		}

		cells = append(cells, cell{name: testCase.name, canv: canv})
	}

	for left := range cells {
		for right := left + 1; right < len(cells); right++ {
			if identicalIn(cells[left].canv, cells[right].canv, rowBearingArrow) {
				t.Errorf("the arrows for %s and %s are the same picture, want one turned to each bearing",
					cells[left].name, cells[right].name)
			}
		}
	}
}

// TestRowBearingWithoutAPositionDrawsNoArrow checks the --- case. An aircraft
// with no decoded position has no bearing, and an arrow pointing north out of
// three dashes would be the scope inventing one.
func TestRowBearingWithoutAPositionDrawsNoArrow(t *testing.T) {
	t.Parallel()

	nowhere := airplane.Snapshot{
		ICAO: icaoSample, Callsign: callsignSample, Altitude: 2400, Heading: 41, Velocity: 420,
		LastUpdate: sceneClock, Squawk: "1000",
	}

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(nowhere))
	scene.Draw(canv, 0)

	if countColour(canv, rowBearingDigits, theme.Night.Muted) == 0 {
		t.Error("the BRG cell is empty, want the three dashes that stand in for a bearing")
	}

	if got := countColour(canv, rowBearingArrow, theme.Night.Ink); got != 0 {
		t.Errorf("an aircraft with no position drew %d arrow pixels, want none", got)
	}
}

// TestCardTrackArrowTurnsWithTheHeading is the same check on the panel's
// TRACK line, which carries the course the aircraft is flying rather than the
// bearing it sits at. The unknown case is in the same table, because the
// panel writing TRACK --- and drawing an arrow beside it would be the one
// aircraft contradicting itself on one screen.
func TestCardTrackArrowTurnsWithTheHeading(t *testing.T) {
	t.Parallel()

	const (
		nearNm       = 12.0
		undecoded    = -1.0
		headingNorth = 0.0
		headingEast  = 90.0
	)

	north, northCanvas, _ := sceneOn(t, panelWidth, panelHeight,
		sceneFrame(scenePlane(icaoSample, callsignSample, 45, nearNm, 2400, headingNorth)))
	north.Draw(northCanvas, 0)

	east, eastCanvas, _ := sceneOn(t, panelWidth, panelHeight,
		sceneFrame(scenePlane(icaoSample, callsignSample, 45, nearNm, 2400, headingEast)))
	east.Draw(eastCanvas, 0)

	unknown, unknownCanvas, _ := sceneOn(t, panelWidth, panelHeight,
		sceneFrame(scenePlane(icaoSample, callsignSample, 45, nearNm, 2400, undecoded)))
	unknown.Draw(unknownCanvas, 0)

	if countColour(northCanvas, cardTrackArrow, theme.Night.Ink) == 0 {
		t.Error("the TRACK line drew no arrow for a heading of due north")
	}

	if identicalIn(northCanvas, eastCanvas, cardTrackArrow) {
		t.Error("the TRACK arrow is the same picture at 000 and at 090, want it turned to the heading")
	}

	if got := countColour(unknownCanvas, cardTrackArrow, theme.Night.Ink); got != 0 {
		t.Errorf("TRACK --- drew %d arrow pixels, want none", got)
	}
}

// filteredPair is one aircraft in the low altitude band and one in the high
// band, at bearings far enough apart that neither sprite nor trail overlaps
// the other. It is the fixture the filter's pixel tests share, so a band
// picked to keep the first and drop the second means the same thing on the
// flat scope, in minimal mode and in the 3D view.
func filteredPair() source.Frame {
	return sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 38, 36000, 268),
	)
}

// TestFilterHidesAircraftOnTheScope checks the f key on the flat scope: the
// aircraft outside the band it lands on draws nothing at all, trail included,
// while the one inside the band is still on the field.
func TestFilterHidesAircraftOnTheScope(t *testing.T) {
	t.Parallel()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, filteredPair())
	scene.Draw(canv, 0)

	if countColour(canv, scopeBox, theme.Night.AltHigh) == 0 {
		t.Fatal("no high-band pixels before filtering, want the second aircraft on the field")
	}

	if !press(scene, 'f') {
		t.Fatal("the f key was not handled, want the filter key to take it")
	}

	scene.Draw(canv, 0)

	if got := countColour(canv, scopeBox, theme.Night.AltHigh); got != 0 {
		t.Errorf("high-band pixels after filtering to the low band = %d, want none", got)
	}

	if countColour(canv, scopeBox, theme.Night.AltLow) == 0 {
		t.Error("no low-band pixels after filtering to the low band, want the first aircraft still there")
	}
}

// TestFilterHidesAircraftInMinimalMode checks the same key in the bare-field
// view, which shares drawAircraft with the flat scope but has no column to
// keep the check clear of: the whole canvas is traffic or nothing.
func TestFilterHidesAircraftInMinimalMode(t *testing.T) {
	t.Parallel()

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, filteredPair())
	scene.Apply(radar.Settings{View: radar.ViewMinimal, RangeNm: sceneRangeNm})
	scene.Draw(canv, 0)

	field := canv.Bounds()

	if countColour(canv, field, theme.Night.AltHigh) == 0 {
		t.Fatal("no high-band pixels before filtering in minimal mode, want the second aircraft on the field")
	}

	if !press(scene, 'f') {
		t.Fatal("the f key was not handled, want the filter key to take it")
	}

	scene.Draw(canv, 0)

	if got := countColour(canv, field, theme.Night.AltHigh); got != 0 {
		t.Errorf("high-band pixels after filtering in minimal mode = %d, want none", got)
	}

	if countColour(canv, field, theme.Night.AltLow) == 0 {
		t.Error("no low-band pixels after filtering in minimal mode, want the first aircraft still there")
	}
}

// TestFilterRenderPNG writes the demo fleet under one step of each of the
// filter's two cycles to a directory an operator names, for looking at over a
// picture rather than proving anything a pixel count could check on its own.
//
// It is skipped unless USCOPE_PNG_DIR is set: nothing else in the suite
// touches disk, and a CI run has no directory worth writing these into.
func TestFilterRenderPNG(t *testing.T) {
	t.Parallel()

	dir := os.Getenv("USCOPE_PNG_DIR")
	if dir == "" {
		t.Skip("USCOPE_PNG_DIR is not set")
	}

	writeFilterPNG(t, dir, "filter-alt.png", radar.ColourAltitude, 'f')
	writeFilterPNG(t, dir, "filter-air.png", radar.ColourAirline, 'F')
}

// writeFilterPNG draws one frame of the demo fleet in colour, presses the
// filter key once to land on the first entry of that mode's cycle, and writes
// the result to name inside dir.
//
// The key is pressed after a first, unfiltered Draw rather than before it:
// airline mode's cycle walks the legend, and the legend is only known once
// countOperators has seen a frame, exactly as it is for an operator working
// the scope live.
func writeFilterPNG(tb testing.TB, dir, name string, colour radar.ColourMode, key rune) {
	tb.Helper()

	demo, err := source.NewDemo()
	if err != nil {
		tb.Fatalf("source.NewDemo: %v", err)
	}

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	scene := radar.New(testFaces(tb), demo, scope.New(), radar.WithColour(colour))
	scene.Draw(canv, 0)

	if !press(scene, key) {
		tb.Fatalf("the %c key was not handled, want the filter key to take it", key)
	}

	scene.Draw(canv, 0)

	path := filepath.Join(dir, name)

	file, err := os.Create(path) //nolint:gosec // an operator-named directory, the trust boundary --png also runs at.
	if err != nil {
		tb.Fatalf("os.Create(%s): %v", path, err)
	}

	defer func() { _ = file.Close() }()

	if err := png.Encode(file, canv.Image()); err != nil {
		tb.Fatalf("png.Encode(%s): %v", path, err)
	}
}
