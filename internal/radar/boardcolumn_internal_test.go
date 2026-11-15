package radar

import (
	"image"
	"image/color"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// boardField is the airfield the DIR tests classify against, and the
// distances the synthetic aircraft are placed at, east of it.
const (
	boardFieldLat = 52.3086
	boardFieldLon = 4.7639

	// boardFarLon is about twenty miles east of the field: outside the ten
	// mile ring the departure test uses, close enough to be pointing at it.
	boardFarLon = 5.3

	// boardProbeWidth is a column wide enough for every board field.
	boardProbeWidth = 400
)

// boardCorridors is a placed field for Classify to measure against.
func boardCorridors() source.Corridors {
	return source.Corridors{Field: source.Airfield{ICAO: "EHAM", Latitude: boardFieldLat, Longitude: boardFieldLon}}
}

// boardCanvas is a night-cleared canvas of the given size.
func boardCanvas(tb testing.TB, width, height int) *canvas.Canvas {
	tb.Helper()

	canv, err := canvas.New(width, height)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(theme.Night.Field)

	return canv
}

// inkIn counts the pixels in box that are exactly col.
func inkIn(canv *canvas.Canvas, box image.Rectangle, col color.RGBA) int {
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

func TestToggleBoard(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		view View
		want bool
	}{
		{view: ViewMinimal3D, want: true},
		{view: ViewMinimal},
		{view: View3D},
		{view: ViewScope},
	} {
		t.Run(string(testCase.view), func(t *testing.T) {
			t.Parallel()

			scene := &Scene{shown: testCase.view}

			if got := scene.toggleBoard(); got != testCase.want {
				t.Fatalf("toggleBoard() = %v in %s, want %v", got, testCase.view, testCase.want)
			}

			if scene.board != testCase.want {
				t.Errorf("board = %v after one press in %s, want %v", scene.board, testCase.view, testCase.want)
			}

			if scene.boardShown() != testCase.want {
				t.Errorf("boardShown() = %v in %s, want %v", scene.boardShown(), testCase.view, testCase.want)
			}
		})
	}
}

// TestCarveBoardAndFraming checks the column takes a quarter of the canvas off
// the right of the box, and that the camera's framing share follows it.
func TestCarveBoardAndFraming(t *testing.T) {
	t.Parallel()

	canv := boardCanvas(t, 1280, 720)

	for _, testCase := range []struct {
		name       string
		view       View
		board      bool
		wantColumn image.Rectangle
		wantFill   float64
	}{
		{name: "switched off", view: ViewMinimal3D, wantFill: 1},
		{name: "on in the full 3D view", view: View3D, board: true, wantFill: 1},
		{
			name: "on in the bare 3D view", view: ViewMinimal3D, board: true,
			wantColumn: image.Rect(960, 30, 1280, 720), wantFill: 0.75,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{shown: testCase.view, board: testCase.board}
			lay := layout{dst: canv, scope: image.Rect(0, 30, 1280, 720)}

			if got := scene.carveBoard(&lay); got != testCase.wantColumn {
				t.Errorf("carveBoard() = %v, want %v", got, testCase.wantColumn)
			}

			if got := scene.boardFraming(&lay); got != testCase.wantFill {
				t.Errorf("boardFraming() = %v, want %v", got, testCase.wantFill)
			}
		})
	}
}

// TestDrawBoardColumnEarlyReturns checks the three ways the column stops short:
// no column at all, no room for the title, and no room for a board.
func TestDrawBoardColumnEarlyReturns(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		column      image.Rectangle
		wantFilled  bool
		wantStrips  bool
		stripsProbe image.Rectangle
	}{
		{name: "no column"},
		{name: "too short for the title", column: image.Rect(100, 0, 400, 20), wantFilled: true},
		{
			name: "too narrow for a board", column: image.Rect(300, 0, 400, 400), wantFilled: true,
			stripsProbe: image.Rect(301, 60, 400, 400),
		},
		{
			name: "wide enough", column: image.Rect(0, 0, boardProbeWidth, 400), wantFilled: true, wantStrips: true,
			stripsProbe: image.Rect(1, 60, boardProbeWidth, 400),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv := boardCanvas(t, boardProbeWidth, 400)
			canv.Clear(theme.Night.Accent)

			scene, _ := barScene(t, ViewMinimal3D, boardProbeWidth, 400)
			scene.board = true
			lay := scene.newLayout(canv)
			frame := scene.src.Frame()

			scene.syncSelection(frame)
			scene.drawBoardColumn(&lay, frame, testCase.column)

			filled := inkIn(canv, canv.Bounds(), theme.Night.Accent) < boardProbeWidth*400
			if filled != testCase.wantFilled {
				t.Errorf("column filled = %v, want %v", filled, testCase.wantFilled)
			}

			if testCase.stripsProbe.Empty() {
				return
			}

			strips := inkIn(canv, testCase.stripsProbe, theme.Night.Rule) > testCase.stripsProbe.Dx()
			if strips != testCase.wantStrips {
				t.Errorf("strip rules drawn = %v, want %v", strips, testCase.wantStrips)
			}
		})
	}
}

func TestDrawBoardTitleWithoutABodyFace(t *testing.T) {
	t.Parallel()

	faces := layerTestFaces(t)
	faces.Body = nil
	scene := &Scene{faces: faces}
	col := layout{dst: boardCanvas(t, 10, 10), right: 10, bottom: 10}

	if scene.drawBoardTitle(&col, 1) {
		t.Error("drawBoardTitle drew a title with no body face")
	}
}

// TestDrawBoardLegend checks the legend's room off the bottom of the column in
// each of the shapes it can take.
func TestDrawBoardLegend(t *testing.T) {
	t.Parallel()

	faces := layerTestFaces(t)
	small := (&layout{}).lineHeight(faces.Small)
	pitch := small + rowLead

	for _, testCase := range []struct {
		name    string
		width   int
		height  int
		colour  ColourMode
		filter  filterState
		noSmall bool
		want    int
	}{
		{name: "one row", width: boardProbeWidth, height: 200, want: pitch + blockGap},
		{
			name: "one row once the filter hides a band", width: 240, height: 200,
			filter: filterState{kind: filterBelow, band: bandMid}, want: 2*pitch + blockGap,
		},
		{name: "wraps as often as it has to", width: 180, height: 200, want: 3*pitch + blockGap},
		{name: "drops an entry wider than a row", width: 60, height: 200, want: 2*pitch + blockGap},
		{name: "no room at all", width: boardProbeWidth, height: small},
		{name: "no small face", width: boardProbeWidth, height: 200, noSmall: true},
		{
			name: "airline mode keeps its own legend under IN and OUT", width: boardProbeWidth, height: 200,
			colour: ColourAirline, want: pitch + blockGap,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			testFaces := faces
			if testCase.noSmall {
				testFaces.Small = nil
			}

			scene := &Scene{faces: testFaces, colour: testCase.colour, filter: testCase.filter}
			scene.SetPalette(theme.Night)

			canv := boardCanvas(t, testCase.width, testCase.height)
			col := layout{dst: canv, right: testCase.width, bottom: testCase.height}
			before := col.bottom

			scene.drawBoardLegend(&col)

			// Airline mode takes the operator row off first, whatever is
			// left for IN and OUT, so the test reads what the board's own
			// rows took.
			if testCase.colour == ColourAirline {
				before -= small + blockGap
			}

			if got := before - col.bottom; got != testCase.want {
				t.Errorf("legend took %d pixels, want %d", got, testCase.want)
			}
		})
	}
}

// TestBoardKeysFollowTheFilter checks the board's legend lists the bands the
// filter lets through and then the two corridor entries.
func TestBoardKeysFollowTheFilter(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		colour ColourMode
		filter filterState
		want   []string
	}{
		{name: "everything", want: []string{legendLow, legendMid, legendHigh, legendIn, legendOut}},
		{
			name: "below the high band", filter: filterState{kind: filterBelow, band: bandMid},
			want: []string{legendLow, legendMid, legendIn, legendOut},
		},
		{name: "airline colouring", colour: ColourAirline, want: []string{legendIn, legendOut}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{colour: testCase.colour, filter: testCase.filter}
			scene.SetPalette(theme.Night)

			keys, count := scene.boardKeys()
			if count != len(testCase.want) {
				t.Fatalf("boardKeys() count = %d, want %d", count, len(testCase.want))
			}

			for index, label := range testCase.want {
				if keys[index].label != label {
					t.Errorf("entry %d = %q, want %q", index, keys[index].label, label)
				}
			}

			if keys[count-2].col != theme.Night.Data || keys[count-1].col != theme.Night.Caution {
				t.Error("IN and OUT are not in the arrival and departure tube colours")
			}
		})
	}
}

// boardPen is a pen on a board plan wide enough for every field, with the
// codes and the bearing given up the way a quarter-width board gives them up.
func boardPen(t *testing.T, scene *Scene, canv *canvas.Canvas, ident int) stripPen {
	t.Helper()

	col := &layout{dst: canv, right: canv.Bounds().Dx(), bottom: canv.Bounds().Dy()}

	plan, ok := scene.planBoardStrips(col, 0, boardProbeWidth)
	if !ok {
		t.Fatal("planBoardStrips refused a generous width")
	}

	plan.codes, plan.bearing = false, false
	plan.width[fieldIdent] = ident

	return stripPen{lay: col, plan: plan, value: 0, small: 0}
}

func TestDrawShortEmergency(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		emergency bool
		ident     int
		want      bool
	}{
		{name: "no emergency", ident: boardProbeWidth},
		{name: "room for the box", emergency: true, ident: boardProbeWidth, want: true},
		{name: "no room for the box", emergency: true, ident: 10},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{faces: layerTestFaces(t)}
			scene.SetPalette(theme.Night)

			canv := boardCanvas(t, boardProbeWidth, 40)
			pen := boardPen(t, scene, canv, testCase.ident)

			scene.drawShortEmergency(pen, 0, airplane.Snapshot{Emergency: testCase.emergency})

			if got := inkIn(canv, canv.Bounds(), theme.Night.Warn) > 0; got != testCase.want {
				t.Errorf("warning box drawn = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestDrawStripDir(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		plane airplane.Snapshot
		want  color.RGBA
		off   bool
	}{
		{
			name: "inbound", want: theme.Night.Data,
			plane: airplane.Snapshot{Latitude: boardFieldLat, Longitude: boardFarLon, Heading: 270, Altitude: 4000},
		},
		{
			name: "outbound", want: theme.Night.Caution,
			plane: airplane.Snapshot{Latitude: boardFieldLat, Longitude: boardFarLon, Heading: 90, VertRate: 1500},
		},
		{
			name: "transit", want: theme.Night.Muted,
			plane: airplane.Snapshot{Latitude: boardFieldLat, Longitude: boardFarLon, Heading: 0, Altitude: 30000},
		},
		{name: "off the plan", off: true, want: theme.Night.Muted},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{faces: layerTestFaces(t)}
			scene.SetPalette(theme.Night)

			canv := boardCanvas(t, boardProbeWidth, 40)
			pen := boardPen(t, scene, canv, boardProbeWidth)
			pen.plan.on[fieldDir] = !testCase.off

			scene.drawStripDir(pen, boardCorridors(), testCase.plane)

			drawn := inkIn(canv, canv.Bounds(), testCase.want) > 0
			if drawn == testCase.off {
				t.Errorf("DIR drawn in %v = %v, want %v", testCase.want, drawn, !testCase.off)
			}
		})
	}
}

// TestBoardPlanGivesUpHalfFields checks the board's own drop order: the hex
// and the bearing go before the speed, and DIR never goes.
func TestBoardPlanGivesUpHalfFields(t *testing.T) {
	t.Parallel()

	scene := &Scene{faces: layerTestFaces(t)}
	sizes := scene.stripWidths(design())

	fit := func(right int) stripPlan {
		plan, ok := scene.planBoardStrips(design(), 0, right)
		if !ok {
			t.Fatalf("planBoardStrips refused %d pixels", right)
		}

		return plan
	}

	noAtt := newStripPlan(0)
	noAtt.on[fieldDir], noAtt.board = true, true
	noAtt.on[fieldPos], noAtt.on[fieldSeen], noAtt.on[fieldTrack], noAtt.att = false, false, false, false
	noAtt.size(&sizes)

	noCodes := noAtt
	noCodes.give(stripDrop{codes: true}, &sizes)

	noBearing := noCodes
	noBearing.give(stripDrop{bearing: true}, &sizes)

	if plan := fit(noCodes.total()); plan.codes || !plan.bearing || !plan.on[fieldSpeed] {
		t.Errorf("at %d the plan kept codes=%v bearing=%v speed=%v, want the codes alone gone",
			noCodes.total(), plan.codes, plan.bearing, plan.on[fieldSpeed])
	}

	plan := fit(noBearing.total())
	if plan.bearing || !plan.on[fieldSpeed] || !plan.on[fieldDir] {
		t.Errorf("at %d the plan kept bearing=%v speed=%v dir=%v, want the bearing gone and speed and DIR kept",
			noBearing.total(), plan.bearing, plan.on[fieldSpeed], plan.on[fieldDir])
	}

	if plan.header(fieldIdent) != identHeadShort || plan.header(fieldRange) != rangeHeadShort {
		t.Errorf("headers = %q, %q, want the short pair", plan.header(fieldIdent), plan.header(fieldRange))
	}

	if full := newStripPlan(0); full.header(fieldIdent) != stripHeaders[fieldIdent] || full.on[fieldDir] {
		t.Error("the scope's own plan changed its ident header or carries DIR")
	}
}
