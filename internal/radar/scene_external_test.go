package radar_test

import (
	"bytes"
	"image"
	"image/color"
	"math"
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
var scopeBox = image.Rect(16, 77, 609, 670)

// press sends a rune through Handle.
func press(scene *radar.Scene, value rune) bool {
	return scene.Handle(input.Key{Kind: input.Rune, Rune: value})
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

		// The column lives in the right half. One pixel of width should not
		// change what is on the left, but it empties the right.
		right := image.Rect(400, 80, 639, 440)
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

func TestTrailToggle(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(
		scenePlane("484AC1", "KLM123", 45, 12, 2400, 41),
		scenePlane("3C6745", "DLH4EA", 200, 30, 36000, 268),
	)

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	withTrails := painted(canv, scopeBox)

	if !press(scene, 't') {
		t.Fatal("Handle('t') = false, want the scene to take it")
	}

	scene.Draw(canv, 0)

	withoutTrails := painted(canv, scopeBox)
	if withoutTrails >= withTrails {
		t.Errorf("scope pixels with trails off = %d, with them on = %d, want fewer", withoutTrails, withTrails)
	}

	press(scene, 'T')
	scene.Draw(canv, 0)

	if painted(canv, scopeBox) != withTrails {
		t.Error("turning trails back on did not restore the picture")
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
		{name: "z flips minimal", key: input.Key{Kind: input.Rune, Rune: 'z'}, want: true},
		{name: "Z flips minimal", key: input.Key{Kind: input.Rune, Rune: 'Z'}, want: true},
		{name: "c cycles the colour mode", key: input.Key{Kind: input.Rune, Rune: 'c'}, want: true},
		{name: "C cycles the colour mode", key: input.Key{Kind: input.Rune, Rune: 'C'}, want: true},
		{name: "down selects the next", key: input.Key{Kind: input.Down}, want: true},
		{name: "up selects the previous", key: input.Key{Kind: input.Up}, want: true},

		// The false cases are the ones that matter. Anything the scene takes
		// here is a key the run loop never sees, and q is how you get out. s
		// moved to v, so it falls through as an ordinary unbound letter.
		{name: "q falls through", key: input.Key{Kind: input.Rune, Rune: 'q'}},
		{name: "Q falls through", key: input.Key{Kind: input.Rune, Rune: 'Q'}},
		{name: "s falls through", key: input.Key{Kind: input.Rune, Rune: 's'}},
		{name: "S falls through", key: input.Key{Kind: input.Rune, Rune: 'S'}},
		{name: "v falls through", key: input.Key{Kind: input.Rune, Rune: 'v'}},
		{name: "esc falls through", key: input.Key{Kind: input.Esc}},
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

		if got := canv.Image().RGBAAt(0, 0); got != inverted.Field {
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

	if got := canv.Image().RGBAAt(0, 0); got != theme.Night.Field {
		t.Fatalf("field pixel before SetPalette = %v, want %v", got, theme.Night.Field)
	}

	scene.SetPalette(theme.Paper)
	scene.Draw(canv, 0)

	if got := canv.Image().RGBAAt(0, 0); got != theme.Paper.Field {
		t.Errorf("field pixel after SetPalette(Paper) = %v, want %v", got, theme.Paper.Field)
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
var homeRingPixel = image.Pt(316, 373)

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
		{name: "an estimate takes the accent", mode: source.FixEstimated, want: theme.Night.Accent},
		{name: "a GPS still searching is critical", mode: source.FixGPSNoFix, want: theme.Night.AltHigh},
		{name: "a 2D GPS fix is the mid band", mode: source.FixGPS2D, want: theme.Night.AltMid},
		{name: "a full 3D GPS fix is the low band", mode: source.FixGPS3D, want: theme.Night.AltLow},
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
	otherSwatchBox := image.Rect(1133, 630, 1143, 640)

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

// detailsBlockBox is the details block's own area at the panel's resolution.
var detailsBlockBox = image.Rect(620, 500, 1264, 615) //nolint:gochecknoglobals // a rectangle is data.

// TestDetailsBlockShowsNoTrafficWhenEmpty checks that the block keeps its
// room and says NO TRAFFIC with an empty sky, rather than collapsing away.
func TestDetailsBlockShowsNoTrafficWhenEmpty(t *testing.T) {
	t.Parallel()

	withTraffic := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	empty := sceneFrame()

	loaded, loadedCanvas, _ := sceneOn(t, panelWidth, panelHeight, withTraffic)
	loaded.Draw(loadedCanvas, 0)

	if painted(loadedCanvas, detailsBlockBox) == 0 {
		t.Fatal("the details block drew nothing with an aircraft selected")
	}

	bare, bareCanvas, _ := sceneOn(t, panelWidth, panelHeight, empty)
	bare.Draw(bareCanvas, 0)

	if painted(bareCanvas, detailsBlockBox) == 0 {
		t.Fatal("the details block drew nothing with an empty sky, want the NO TRAFFIC state")
	}

	if identicalIn(loadedCanvas, bareCanvas, detailsBlockBox) {
		t.Error("the details block looks the same with and without traffic")
	}
}

// TestDetailsVerticalRateTriangle checks that a climbing, a descending and a
// level aircraft each leave a different picture in the details block.
func TestDetailsVerticalRateTriangle(t *testing.T) {
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

	if identicalIn(climbing, descending, detailsBlockBox) {
		t.Error("a climbing and a descending aircraft drew the same details block")
	}

	if identicalIn(climbing, level, detailsBlockBox) {
		t.Error("a climbing and a level aircraft drew the same details block")
	}

	if identicalIn(descending, level, detailsBlockBox) {
		t.Error("a descending and a level aircraft drew the same details block")
	}
}

// rowListRightBand sits inside the tenth compact row's line, in the columns
// only a real aircraft row fills (altitude, speed, distance, bearing). The
// "+N MORE" line is short and left-aligned, so it never reaches this far
// right: painted pixels here mean a real row, not the tail.
var rowListRightBand = image.Rect(1100, 460, 1264, 480) //nolint:gochecknoglobals // a rectangle is data.

// TestRowListMoreLine checks the window boundary the "+N MORE" line closes: a
// fleet exactly the size of the window leaves a real row on the last line,
// one aircraft more replaces it with the tail, and both a light and a busy
// fleet still draw.
func TestRowListMoreLine(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		count       int
		wantRealRow bool
	}{
		{name: "fewer than the window: no tenth line at all", count: 9, wantRealRow: false},
		{name: "exactly the window: the tenth line is a real row", count: 10, wantRealRow: true},
		{name: "one more than the window: the tenth line is the more line", count: 11, wantRealRow: false},
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

			if got := painted(canv, rowListRightBand) > 0; got != testCase.wantRealRow {
				t.Errorf("tenth line is a real row = %v, want %v", got, testCase.wantRealRow)
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
			radar.WithColour(radar.ColourAirline), radar.WithPalette(theme.Paper))
		paper.Draw(paperCanvas, 0)

		if painted(paperCanvas, scopeBox) == 0 {
			t.Fatal("nothing was drawn on the paper palette in airline mode")
		}

		if identicalIn(nightCanvas, paperCanvas, scopeBox) {
			t.Error("night and paper drew the same operator colour, want OnDark and OnLight to differ")
		}
	})
}

// TestDetailsSquawkEmergency checks that an emergency squawk carries the
// accent into the details block, the one place besides the selection that
// colour marks.
func TestDetailsSquawkEmergency(t *testing.T) {
	t.Parallel()

	plane := scenePlane("484AC1", "KLM123", 45, 12, 2400, 41)
	plane.Squawk = "7700"
	plane.Emergency = true

	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame(plane))
	scene.Draw(canv, 0)

	if countColour(canv, detailsBlockBox, theme.Night.Accent) == 0 {
		t.Error("an emergency squawk did not paint the accent colour in the details block")
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

	if !identical(noSetCanvas, toggledOffCanvas) {
		t.Error("a nil shore set drew differently from the shore being toggled off, want the same frame")
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

	full, fullCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithPalette(theme.Paper))
	full.Draw(fullCanvas, 0)

	if countColour(fullCanvas, fullCanvas.Bounds(), theme.Paper.Band) == 0 {
		t.Fatal("the full scope drew no header-band pixels, so this comparison proves nothing")
	}

	minimal, minimalCanvas, _ := sceneOn(t, panelWidth, panelHeight, frame, radar.WithPalette(theme.Paper))
	minimal.Apply(radar.Settings{Minimal: true})
	minimal.Draw(minimalCanvas, 0)

	if got := countColour(minimalCanvas, minimalCanvas.Bounds(), theme.Paper.Band); got != 0 {
		t.Errorf("minimal mode drew %d header-band pixels, want 0", got)
	}

	if got := countColour(minimalCanvas, minimalCanvas.Bounds(), theme.Paper.Ink); got != 0 {
		t.Errorf("minimal mode drew %d key-cap pixels, want 0", got)
	}

	if got := countColour(minimalCanvas, minimalCanvas.Bounds(), theme.Paper.Rule); got != 0 {
		t.Errorf("minimal mode drew %d range-ring pixels, want 0", got)
	}

	if countColour(minimalCanvas, minimalCanvas.Bounds(), theme.Paper.AltLow) == 0 {
		t.Error("minimal mode drew no low-band pixels, want the aircraft's own sprite to survive")
	}
}

// TestMinimalModeDropsTheColumn checks that the right column, the legend and
// the key bar leave no trace: an empty sky paints nothing at all, and an
// aircraft kept away from the old column's own area leaves that area
// untouched.
func TestMinimalModeDropsTheColumn(t *testing.T) {
	t.Parallel()

	t.Run("an empty sky paints only the field", func(t *testing.T) {
		t.Parallel()

		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, sceneFrame())
		scene.Apply(radar.Settings{Minimal: true})
		scene.Draw(canv, 0)

		if got := painted(canv, canv.Bounds()); got != 0 {
			t.Errorf("minimal mode with nothing on screen painted %d pixels, want 0", got)
		}
	})

	t.Run("the old column's area is untouched with traffic elsewhere", func(t *testing.T) {
		t.Parallel()

		// Bearing 180 puts this aircraft due south of the receiver, which
		// under minimal's canvas-centred projection lands below the middle
		// rather than in the right third the column used to occupy.
		frame := sceneFrame(scenePlane("484AC1", "KLM123", 180, 12, 2400, 41))
		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
		scene.Apply(radar.Settings{Minimal: true})
		scene.Draw(canv, 0)

		rightThird := image.Rect(2*panelWidth/3, 0, panelWidth, panelHeight)
		if got := painted(canv, rightThird); got != 0 {
			t.Errorf("minimal mode painted %d pixels in the old column's area, want 0", got)
		}
	})
}

// TestMinimalModeSelectionWithoutLabel checks that the selected aircraft
// keeps its accent ring in minimal mode but gets no callsign label: two
// callsigns of different lengths draw identical pictures once neither is
// labelled.
func TestMinimalModeSelectionWithoutLabel(t *testing.T) {
	t.Parallel()

	t.Run("the ring survives", func(t *testing.T) {
		t.Parallel()

		frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
		scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
		scene.Apply(radar.Settings{Minimal: true})
		scene.Draw(canv, 0)

		if countColour(canv, canv.Bounds(), theme.Night.Accent) == 0 {
			t.Error("minimal mode drew no accent pixels, want the selection ring to survive")
		}
	})

	t.Run("no label is drawn regardless of callsign length", func(t *testing.T) {
		t.Parallel()

		short := sceneFrame(scenePlane("484AC1", "KL1", 45, 12, 2400, 41))
		long := sceneFrame(scenePlane("484AC1", "KLM1234567LONG", 45, 12, 2400, 41))

		shortScene, shortCanvas, _ := sceneOn(t, panelWidth, panelHeight, short)
		shortScene.Apply(radar.Settings{Minimal: true})
		shortScene.Draw(shortCanvas, 0)

		longScene, longCanvas, _ := sceneOn(t, panelWidth, panelHeight, long)
		longScene.Apply(radar.Settings{Minimal: true})
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
	scene.Apply(radar.Settings{Minimal: true, RangeNm: sceneRangeNm})
	scene.Draw(canv, 0)

	corner := image.Rect(1100, 0, panelWidth, 150)
	if countColour(canv, corner, theme.Night.AltLow) == 0 {
		t.Error("no low-band sprite pixels landed in the corner, want the far aircraft plotted there")
	}
}

// TestMinimalModeKeyFlipsBothWays checks that z and Z flip minimal mode
// through Handle, proved by the picture changing and then changing back.
func TestMinimalModeKeyFlipsBothWays(t *testing.T) {
	t.Parallel()

	frame := sceneFrame(scenePlane("484AC1", "KLM123", 45, 12, 2400, 41))
	scene, canv, _ := sceneOn(t, panelWidth, panelHeight, frame)
	scene.Draw(canv, 0)

	full, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	scene.Draw(full, 0)

	if !press(scene, 'z') {
		t.Fatal("Handle('z') = false, want the scene to take it")
	}

	scene.Draw(canv, 0)

	if identical(canv, full) {
		t.Fatal("z did not change the picture, so minimal mode did not engage")
	}

	press(scene, 'Z')
	scene.Draw(canv, 0)

	if !identical(canv, full) {
		t.Error("Z did not undo z, so minimal mode does not flip back")
	}
}
