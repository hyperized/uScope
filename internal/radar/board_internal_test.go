package radar

import (
	"image"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// boardAltitudeFt is the level every synthetic aircraft in this file flies at:
// a real one, so the level field draws a figure, in the low band so a filter
// keyed to that band keeps it. statusCanvasHeight is the strip of canvas the
// status-line cases are drawn on, which only has to be taller than the small
// face.
const (
	boardAltitudeFt    = 4000.0
	statusCanvasHeight = 20
)

// paintedIn counts the pixels in box that are not the night field, which is
// how the tests below ask whether one row of the board got anything drawn in
// it without pinning a whole picture the way filterPainted does for a whole
// canvas.
func paintedIn(canv *canvas.Canvas, box image.Rectangle) int {
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

// TestStripBoardMarks checks how many +N lines a board reports carrying, for
// every combination of the two ends being cut. Only the below-only case had
// coverage before this: the two lines are drawn by two separate calls in
// drawBoard, and a board that has lost aircraft off the top only is the case
// scrolling into a long list produces on every step but the first.
func TestStripBoardMarks(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		board stripBoard
		want  int
	}{
		{name: "neither end is cut", board: stripBoard{}, want: 0},
		{name: "aircraft left off the top", board: stripBoard{above: 4}, want: 1},
		{name: "aircraft left off the bottom", board: stripBoard{below: 4}, want: 1},
		{name: "both ends are cut", board: stripBoard{above: 4, below: 4}, want: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.board.marks(); got != testCase.want {
				t.Errorf("marks() = %d, want %d", got, testCase.want)
			}
		})
	}
}

// TestPlanBoardKeepsTheStripRatherThanAnEmptyMarkLine checks the one path
// planBoard's own search can take that is not "try one more mark and keep
// it": a room so short that charging a whole strip's height for a below line
// would leave no strip at all. The window it already had is what the list is
// worth at that height, so the search has to stop and keep it rather than
// accept a board with a count of zero.
func TestPlanBoardKeepsTheStripRatherThanAnEmptyMarkLine(t *testing.T) {
	t.Parallel()

	scene := &Scene{faces: layerTestFaces(t)}
	height := scene.halfStripHeight()

	// One strip's worth of room and two aircraft: the unmarked window already
	// gives up the second one, so trying a below line at all costs the only
	// strip the room has left.
	board := scene.planBoard(height, 2, 0)

	if board.count != 1 || board.above != 0 || board.below != 1 {
		t.Errorf("planBoard(%d, 2, 0) = %+v, want one strip and a below line, not an empty board", height, board)
	}
}

// TestPlanBoardAccountsForEveryAircraft checks the invariant the two +N lines
// exist to keep true at any size of room, list and selection: the strips
// drawn, the aircraft left off above and the aircraft left off below always
// add up to the whole list, so the board never quietly loses one off either
// end.
func TestPlanBoardAccountsForEveryAircraft(t *testing.T) {
	t.Parallel()

	scene := &Scene{faces: layerTestFaces(t)}
	height := scene.halfStripHeight()

	const (
		wholeFleet = 8

		// roomToSpare is a room far taller than the list needs, and crowd a
		// list far longer than the room, so the table has a case at each end
		// of the window's own arithmetic as well as on its boundary.
		roomToSpare = 20
		crowd       = 60
		midCrowd    = 30
	)

	for _, testCase := range []struct {
		name  string
		room  int
		total int
		sel   int
	}{
		{name: "an empty list", room: height * wholeFleet, total: 0, sel: notSelected},
		{name: "everything fits with room to spare", room: height * roomToSpare, total: wholeFleet, sel: 0},
		{name: "exactly as many strips as fit", room: height * wholeFleet, total: wholeFleet, sel: 0},
		{
			name: "one more aircraft than fit, selecting the first",
			room: height * wholeFleet, total: wholeFleet + 1, sel: 0,
		},
		{
			name: "one more aircraft than fit, selecting the last",
			room: height * wholeFleet, total: wholeFleet + 1, sel: wholeFleet,
		},
		{
			name: "far more aircraft than fit, selecting the middle",
			room: height * wholeFleet, total: crowd, sel: midCrowd,
		},
		{name: "nothing is selected", room: height * wholeFleet, total: crowd, sel: notSelected},
		{name: "a room too short for a strip and a mark line", room: height, total: 3, sel: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			board := scene.planBoard(testCase.room, testCase.total, testCase.sel)

			if got := board.above + board.count + board.below; got != testCase.total {
				t.Errorf("above(%d) + count(%d) + below(%d) = %d, want %d",
					board.above, board.count, board.below, got, testCase.total)
			}
		})
	}
}

// TestDrawBoardHeadSkipsFieldsThePlanDropped checks the header row's own
// loop: a field the plan has given up must not get its word drawn, which is
// what the continue is for. GS is compared on and off with everything else
// about the plan held equal, so the difference in painted pixels can only
// come from that one field's header.
func TestDrawBoardHeadSkipsFieldsThePlanDropped(t *testing.T) {
	t.Parallel()

	const (
		headCanvasWidth = 400
		identLeft       = 10
		speedLeft       = 200
	)

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}
	height := scene.boardHeadHeight()

	draw := func(speedOn bool) int {
		canv, err := canvas.New(headCanvasWidth, height)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		canv.Clear(theme.Night.Field)

		var plan stripPlan

		plan.on[fieldIdent] = true
		plan.left[fieldIdent] = identLeft
		plan.on[fieldSpeed] = speedOn
		plan.left[fieldSpeed] = speedLeft

		col := &layout{dst: canv, left: 0, right: headCanvasWidth, top: 0, bottom: height}
		scene.drawBoardHead(col, plan)

		return filterPainted(canv)
	}

	dropped := draw(false)
	kept := draw(true)

	if dropped >= kept {
		t.Errorf("painted %d pixels with GS dropped and %d with it kept, want fewer with it dropped", dropped, kept)
	}
}

// TestDrawBoardDrawsTheAboveLine checks the branch drawBoard takes once the
// window has scrolled past the start of the list: a "+N ABOVE" line has to
// appear over the strips. Every other test that reaches drawBoard through a
// full Draw keeps the selection near the top of a short list, so the below
// line was covered long before this one was.
func TestDrawBoardDrawsTheAboveLine(t *testing.T) {
	t.Parallel()

	const (
		boardCanvasWidth = 2000
		visibleCount     = 3
		hiddenAbove      = 5
	)

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}

	plan, ok := scene.planStrips(0, boardCanvasWidth)
	if !ok {
		t.Fatal("planStrips refused a generous width")
	}

	total := hiddenAbove + visibleCount
	planes := make([]airplane.Snapshot, total)

	for index := range planes {
		planes[index] = airplane.Snapshot{ICAO: sampleCallsign, Callsign: sampleCallsign, Altitude: boardAltitudeFt}
	}

	frame := source.Frame{Planes: planes}
	height := scene.halfStripHeight()
	canvasHeight := height * (visibleCount + 1)

	canv, err := canvas.New(boardCanvasWidth, canvasHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(theme.Night.Field)

	col := &layout{dst: canv, left: 0, right: boardCanvasWidth, top: 0, bottom: canvasHeight}
	board := stripBoard{first: hiddenAbove, count: visibleCount, above: hiddenAbove}

	scene.drawBoard(col, plan, frame, board)

	topRow := image.Rect(0, 0, boardCanvasWidth, height)
	if got := colourCount(canv, topRow, theme.Night.Muted); got == 0 {
		t.Error(`drawBoard painted no muted pixels in the top row, want the "+N ABOVE" line`)
	}
}

// TestDrawBoardStripsStopsAtTheWindowsEnd checks the walk's own early break:
// handed more visible aircraft than the window holds, it has to stop once the
// window is full rather than walking the rest of the fleet for nothing. The
// return value is the y under the last strip drawn, so a walk that kept going
// past the window would report a lower one than a walk that stopped at it.
//
// A filtered-out aircraft goes in ahead of the window as well, which is the
// walk's other branch: the filter keeps it out of the index the window is
// measured against, so it must cost the walk nothing at all rather than
// taking one of the window's own slots.
func TestDrawBoardStripsStopsAtTheWindowsEnd(t *testing.T) {
	t.Parallel()

	const (
		canvasWidth  = 2000
		windowCount  = 3
		extra        = 10
		lowAltitude  = 2000.0
		highAltitude = 35000.0
	)

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night, filter: filterState{kind: filterBand, band: bandLow}}

	plan, ok := scene.planStrips(0, canvasWidth)
	if !ok {
		t.Fatal("planStrips refused a generous width")
	}

	planes := make([]airplane.Snapshot, 0, windowCount+extra+1)
	planes = append(planes, airplane.Snapshot{ICAO: "FFF999", Callsign: sampleCallsign, Altitude: highAltitude})

	for range windowCount + extra {
		planes = append(planes,
			airplane.Snapshot{ICAO: sampleCallsign, Callsign: sampleCallsign, Altitude: lowAltitude})
	}

	frame := source.Frame{Planes: planes}
	height := scene.halfStripHeight()

	canv, err := canvas.New(canvasWidth, height*(windowCount+extra))
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	col := &layout{dst: canv, left: 0, right: canvasWidth, top: 0, bottom: canv.Bounds().Dy()}
	board := stripBoard{first: 0, count: windowCount, below: extra}

	got := scene.drawBoardStrips(col, plan, frame, board, 0)

	if want := height * windowCount; got != want {
		t.Errorf("drawBoardStrips returned top=%d, want %d after the filtered aircraft and %d strips",
			got, want, windowCount)
	}
}

// TestDrawCardPlainEarlyReturns checks the sentence's first two exits: a
// shape with no plain line at all, and an empty sky with no aeroplane for the
// sentence to be about. Either one has to leave the whole block under the
// panel untouched.
func TestDrawCardPlainEarlyReturns(t *testing.T) {
	t.Parallel()

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}
	plane := airplane.Snapshot{ICAO: icaoFirst, Callsign: sampleCallsign, Altitude: boardAltitudeFt}
	sel := selection{plane: plane, found: true}
	frame := source.Frame{Now: time.Now()}

	const canvasSize = 400

	draw := func(plan cardPlan, sel selection) int {
		canv, err := canvas.New(canvasSize, canvasSize)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		canv.Clear(theme.Night.Field)
		col := &layout{dst: canv, left: 0, right: canvasSize, top: 0, bottom: canvasSize}
		scene.drawCardPlain(col, plan, frame, sel)

		return filterPainted(canv)
	}

	noPlainLine := cardPlan{scale: cardScale, rows: cardRows}
	if got := draw(noPlainLine, sel); got != 0 {
		t.Errorf("a shape with no plain line painted %d pixels, want none", got)
	}

	plain := cardPlan{scale: cardScale, rows: cardRows, plain: true}
	if got := draw(plain, selection{}); got != 0 {
		t.Errorf("an empty sky painted %d pixels for the sentence, want none", got)
	}
}

// TestDrawCardPlainRefusesWhenTheLevelClauseWillNotFit checks the third exit:
// a column with room for everything above it but not for the sentence's own
// first clause draws none of it, rather than starting a clause it cannot
// finish.
func TestDrawCardPlainRefusesWhenTheLevelClauseWillNotFit(t *testing.T) {
	t.Parallel()

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}
	plan := cardPlan{scale: cardScale, rows: cardRows, plain: true}
	plane := airplane.Snapshot{ICAO: icaoFirst, Callsign: sampleCallsign, Altitude: boardAltitudeFt}
	sel := selection{plane: plane, found: true}
	frame := source.Frame{Now: time.Now()}

	left := cardRule + cardPadX
	tooNarrow := left + measure(scene.faces.Small, widestPlainLevel) - 1

	canv, err := canvas.New(tooNarrow+1, tooNarrow+1)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(theme.Night.Field)
	col := &layout{dst: canv, left: 0, right: tooNarrow, top: 0, bottom: tooNarrow + 1}
	scene.drawCardPlain(col, plan, frame, sel)

	if got := filterPainted(canv); got != 0 {
		t.Errorf("a column one pixel short of the level clause painted %d pixels, want none", got)
	}
}

// TestDrawCardPlainStopsAfterAClauseThatWillNotFit checks the fourth exit: a
// column with room for the level clause but not for the place clause after it
// has to stop there, with nothing painted past the level clause. The plane
// carries the exact altitude and rate widestPlainLevel is built from, so its
// rendered width lands exactly at the worst case and the boundary can be
// placed against it precisely rather than guessed at.
func TestDrawCardPlainStopsAfterAClauseThatWillNotFit(t *testing.T) {
	t.Parallel()

	const (
		worstAltitude = 999999.0
		worstRate     = -99999.0
		canvasSize    = 800
	)

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}
	plan := cardPlan{scale: cardScale, rows: cardRows, plain: true}
	plane := airplane.Snapshot{
		ICAO: icaoFirst, Callsign: sampleCallsign,
		Altitude: worstAltitude, VertRate: worstRate,
		Latitude: coordinateSampleLat, Longitude: coordinateSampleLat,
	}
	sel := selection{plane: plane, found: true}
	frame := source.Frame{Now: time.Now()}

	left := cardRule + cardPadX
	face := scene.faces.Small

	scratch, err := canvas.New(canvasSize, canvasSize)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	pen := scene.drawPlainLevel(scratch, left, 0, plane)
	right := pen + measure(face, widestPlainPlace) - 1

	canv, err := canvas.New(canvasSize, canvasSize)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(theme.Night.Field)
	col := &layout{dst: canv, left: 0, right: right, top: 0, bottom: canvasSize}
	scene.drawCardPlain(col, plan, frame, sel)

	if got := paintedIn(canv, image.Rect(pen, 0, canvasSize, canvasSize)); got != 0 {
		t.Errorf("painted %d pixels past the level clause, want the place clause dropped for want of room", got)
	}

	if got := filterPainted(canv); got == 0 {
		t.Error("the level clause itself painted nothing, so the boundary above proves nothing")
	}
}

// TestDrawPlainPlaceBranches checks the second clause's own two exits,
// reached directly rather than through the sentence around it: an aircraft
// with no decoded position drops the clause and says the sentence can carry
// on, and a clause that will not fit in the room left stops it there instead.
func TestDrawPlainPlaceBranches(t *testing.T) {
	t.Parallel()

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}
	face := scene.faces.Small

	const (
		left       = 20
		top        = 0
		canvasSize = 400
	)

	newCanv := func(t *testing.T) *canvas.Canvas {
		t.Helper()

		canv, err := canvas.New(canvasSize, canvasSize)
		if err != nil {
			t.Fatalf("canvas.New: %v", err)
		}

		canv.Clear(theme.Night.Field)

		return canv
	}

	t.Run("no decoded position carries the sentence on", func(t *testing.T) {
		t.Parallel()

		canv := newCanv(t)

		pen, drawn := scene.drawPlainPlace(canv, left, top, canvasSize, airplane.Snapshot{})
		if pen != left || !drawn {
			t.Errorf("drawPlainPlace on an undecoded position = (%d, %v), want (%d, true)", pen, drawn, left)
		}

		if got := filterPainted(canv); got != 0 {
			t.Errorf("painted %d pixels for a position nobody decoded, want none", got)
		}
	})

	t.Run("a clause too wide for the room stops there", func(t *testing.T) {
		t.Parallel()

		canv := newCanv(t)
		plane := airplane.Snapshot{Latitude: coordinateSampleLat, Longitude: coordinateSampleLat}
		right := left + measure(face, widestPlainPlace) - 1

		pen, drawn := scene.drawPlainPlace(canv, left, top, right, plane)
		if pen != left || drawn {
			t.Errorf("drawPlainPlace with no room = (%d, %v), want (%d, false)", pen, drawn, left)
		}

		if got := filterPainted(canv); got != 0 {
			t.Errorf("painted %d pixels for a clause that does not fit, want none", got)
		}
	})
}

// TestDrawPlainSeenRefusesWhenTooNarrow checks the sentence's closing clause:
// a column with no room for it draws nothing, rather than running the seen
// bucket past the card's own right edge.
func TestDrawPlainSeenRefusesWhenTooNarrow(t *testing.T) {
	t.Parallel()

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}
	face := scene.faces.Small

	const (
		left       = 20
		top        = 0
		canvasSize = 400
	)

	canv, err := canvas.New(canvasSize, canvasSize)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(theme.Night.Field)

	right := left + measure(face, widestPlainSeen) - 1
	plane := airplane.Snapshot{LastUpdate: time.Now()}

	scene.drawPlainSeen(canv, left, top, right, time.Now(), plane)

	if got := filterPainted(canv); got != 0 {
		t.Errorf("painted %d pixels when the seen clause could not fit, want none", got)
	}
}

// TestStatusWidthCountsTheFilteredTag checks the one thing sel.hidden adds to
// the status line's own measured width: room for " (FILTERED)". The pen has
// to reserve it before drawing a single character, or the line would not stay
// right-aligned on the column's own edge.
func TestStatusWidthCountsTheFilteredTag(t *testing.T) {
	t.Parallel()

	scene := &Scene{faces: layerTestFaces(t)}

	const (
		shown = 3
		total = 9
	)

	plain := scene.statusWidth(selection{found: true}, shown, total)
	hidden := scene.statusWidth(selection{found: true, hidden: true}, shown, total)

	want := measureTracked(scene.faces.Small, selFilteredTag)
	if got := hidden - plain; got != want {
		t.Errorf("statusWidth grew by %d pixels for the filtered tag, want %d", got, want)
	}
}

// TestDrawStripStatusFourSelectionStates walks the four things the line
// above the panel can say about the selection: a pinned aircraft with a place
// on the board, the nearest aircraft nobody has pinned, a pinned aircraft the
// filter is hiding, and an empty sky.
//
// The first two are the same line by design: selRank reads the board index
// rather than the pin, because the line is answering "where is the full
// strip", not "did the operator choose it". Only the third paints the
// caution-coloured tag, and only the third and fourth have no rank to show.
func TestDrawStripStatusFourSelectionStates(t *testing.T) {
	t.Parallel()

	const (
		total      = 5
		canvasSize = 400

		// icaoThird is a third aircraft on the index, distinct from icaoFirst
		// and icaoSecond, which the filter-hiding case needs so the pinned
		// aircraft can be missing from the index without it being empty.
		icaoThird = "GGG777"
	)

	plane := airplane.Snapshot{ICAO: icaoFirst, Callsign: sampleCallsign}

	for _, testCase := range []struct {
		name      string
		icaos     []string
		selIndex  int
		pinned    bool
		selHidden bool
		sel       selection
		wantRank  bool
		wantAmber bool
	}{
		{
			name: "a pinned aircraft with a place on the board", icaos: []string{icaoFirst, icaoSecond, icaoThird},
			selIndex: 1, pinned: true, sel: selection{plane: plane, found: true}, wantRank: true,
		},
		{
			name: "the nearest aircraft nobody has pinned", icaos: []string{icaoFirst, icaoSecond, icaoThird},
			selIndex: 0, sel: selection{plane: plane, found: true}, wantRank: true,
		},
		{
			name: "a pinned aircraft the filter is hiding", icaos: []string{icaoSecond, icaoThird},
			selIndex: notSelected, pinned: true, selHidden: true,
			sel: selection{plane: plane, found: true, hidden: true}, wantAmber: true,
		},
		{name: "an empty sky", icaos: nil, selIndex: notSelected, sel: selection{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{
				faces: layerTestFaces(t), pal: theme.Night,
				icaos: testCase.icaos, selIndex: testCase.selIndex,
				pinned: testCase.pinned, selHidden: testCase.selHidden,
			}

			canv, err := canvas.New(canvasSize, statusCanvasHeight)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			canv.Clear(theme.Night.Field)
			col := &layout{dst: canv, left: 0, right: canvasSize, top: 0, bottom: statusCanvasHeight}
			scene.drawStripStatus(col, testCase.sel, scene.shownPlanes(), total)

			if got := colourCount(canv, canv.Bounds(), theme.Night.Caution) > 0; got != testCase.wantAmber {
				t.Errorf("caution pixels present = %v, want %v", got, testCase.wantAmber)
			}

			if got := scene.selRank(testCase.sel) > 0; got != testCase.wantRank {
				t.Errorf("selRank > 0 = %v, want %v", got, testCase.wantRank)
			}
		})
	}
}
