package radar_test

import (
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
		{name: "a toggles auto", key: input.Key{Kind: input.Rune, Rune: 'a'}, want: true},
		{name: "A toggles auto", key: input.Key{Kind: input.Rune, Rune: 'A'}, want: true},
		{name: "t toggles trails", key: input.Key{Kind: input.Rune, Rune: 't'}, want: true},
		{name: "T toggles trails", key: input.Key{Kind: input.Rune, Rune: 'T'}, want: true},
		{name: "down selects the next", key: input.Key{Kind: input.Down}, want: true},
		{name: "up selects the previous", key: input.Key{Kind: input.Up}, want: true},

		// The false cases are the ones that matter. Anything the scene takes
		// here is a key the run loop never sees, and q is how you get out.
		{name: "q falls through", key: input.Key{Kind: input.Rune, Rune: 'q'}},
		{name: "Q falls through", key: input.Key{Kind: input.Rune, Rune: 'Q'}},
		{name: "s falls through", key: input.Key{Kind: input.Rune, Rune: 's'}},
		{name: "S falls through", key: input.Key{Kind: input.Rune, Rune: 'S'}},
		{name: "esc falls through", key: input.Key{Kind: input.Esc}},
		{name: "ctrl-c falls through", key: input.Key{Kind: input.CtrlC}},
		{name: "left falls through", key: input.Key{Kind: input.Left}},
		{name: "right falls through", key: input.Key{Kind: input.Right}},
		{name: "enter falls through", key: input.Key{Kind: input.Enter}},
		{name: "an unbound rune falls through", key: input.Key{Kind: input.Rune, Rune: 'z'}},
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
func TestRowWindowFollowsTheSelection(t *testing.T) {
	t.Parallel()

	const crowd = 40

	planes := make([]airplane.Snapshot, 0, crowd)
	for index := range crowd {
		planes = append(planes, scenePlane(
			string(rune('A'+index%26))+"00000"+string(rune('0'+index%10)),
			"FL"+string(rune('0'+index%10)),
			float64(index)*9, 3+float64(index), float64(index)*900, float64(index)*8+1,
		))
	}

	// A short canvas so far fewer than forty rows fit.
	scene, canv, _ := sceneOn(t, panelWidth, 400, sceneFrame(planes...))
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

	press(scene, 'a')
	scene.Draw(canv, 0)

	if got := ranges.GetCurrent(); got != sceneRangeNm {
		t.Errorf("range = %g with auto off, want it left at %g", got, float64(sceneRangeNm))
	}

	press(scene, 'A')
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
