package radar

import (
	"image"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
)

// The five aircraft of groupedFrame, named by what they are doing.
const (
	nearOverflight = "00000A"
	departure      = "00000B"
	nearArrival    = "00000C"
	farOverflight  = "00000D"
	farArrival     = "00000E"
)

// groupedFrame is five aircraft in distance order whose classes interleave:
// the nearest is an overflight, then a departure, an arrival, a second
// overflight and a second arrival. Grouped, the list reads C E B A D.
func groupedFrame() source.Frame {
	return source.Frame{
		Corridors: boardCorridors(),
		Planes: []airplane.Snapshot{
			classPlane(nearOverflight, source.TrackTransit),
			classPlane(departure, source.TrackOutbound),
			classPlane(nearArrival, source.TrackInbound),
			classPlane(farOverflight, source.TrackTransit),
			classPlane(farArrival, source.TrackInbound),
		},
	}
}

// groupedScene is a corridor scene with groupedFrame's field on it and the
// selection synced to that frame.
func groupedScene(colour ColourMode, filter filterState) (*Scene, source.Frame) {
	scene := corridorScene(colour)
	scene.filter = filter
	frame := groupedFrame()
	scene.syncSelection(frame)

	return scene, frame
}

// TestShareLines is the allocation rule as a table: headings first, then the
// rest dealt a line at a time, IN then OUT then OTHER, to whichever still
// needs one.
func TestShareLines(t *testing.T) {
	t.Parallel()

	all := [groupCount]bool{false, true, true, true}

	for _, testCase := range []struct {
		name  string
		lines int
		need  [groupCount]int
		drawn [groupCount]bool
		want  [groupCount]int
	}{
		{name: "all fit", lines: 20, need: [groupCount]int{0, 1, 3, 8}, drawn: all, want: [groupCount]int{0, 1, 3, 8}},
		{
			name: "one overflowing takes the spare", lines: 12,
			need: [groupCount]int{0, 1, 3, 20}, drawn: all, want: [groupCount]int{0, 1, 3, 5},
		},
		{
			name: "two overflowing split the spare", lines: 12,
			need: [groupCount]int{0, 1, 20, 20}, drawn: all, want: [groupCount]int{0, 1, 4, 4},
		},
		{name: "zero lines", lines: 0, need: [groupCount]int{0, 1, 1, 1}, drawn: all},
		{name: "fewer lines than headings", lines: 2, need: [groupCount]int{0, 1, 1, 1}, drawn: all},
		{name: "only the headings", lines: 3, need: [groupCount]int{0, 1, 1, 1}, drawn: all},
		{
			name: "exact thirds", lines: 12,
			need: [groupCount]int{0, 9, 9, 9}, drawn: all, want: [groupCount]int{0, 3, 3, 3},
		},
		{
			name: "a remainder of one goes to IN", lines: 13,
			need: [groupCount]int{0, 9, 9, 9}, drawn: all, want: [groupCount]int{0, 4, 3, 3},
		},
		{
			name: "a remainder of two goes to IN and OUT", lines: 14,
			need: [groupCount]int{0, 9, 9, 9}, drawn: all, want: [groupCount]int{0, 4, 4, 3},
		},
		{
			name: "an empty section gives its third away", lines: 9,
			need: [groupCount]int{0, 0, 9, 9}, drawn: all, want: [groupCount]int{0, 0, 3, 3},
		},
		{
			name: "one section drawn has the lot", lines: 10,
			need:  [groupCount]int{0, 0, 30, 0},
			drawn: [groupCount]bool{false, false, true, false},
			want:  [groupCount]int{0, 0, 9, 0},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := shareLines(testCase.lines, testCase.need, testCase.drawn); got != testCase.want {
				t.Errorf("shareLines(%d, %v) = %v, want %v", testCase.lines, testCase.need, got, testCase.want)
			}
		})
	}
}

// TestWindowFor checks a section's window: the whole section when it fits,
// one line fewer and the rest counted when it does not, scrolled to keep the
// selection in view.
func TestWindowFor(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name             string
		need, lines, sel int
		want             groupWindow
	}{
		{name: "fits", need: 3, lines: 5, sel: notSelected, want: groupWindow{count: 3}},
		{name: "fits exactly", need: 3, lines: 3, sel: 2, want: groupWindow{count: 3}},
		{name: "overflows from the top", need: 8, lines: 4, sel: notSelected, want: groupWindow{count: 3, hidden: 5}},
		{name: "scrolls to the selection", need: 8, lines: 4, sel: 6, want: groupWindow{first: 5, count: 3, hidden: 5}},
		{name: "one line is the count alone", need: 2, lines: 1, sel: notSelected, want: groupWindow{hidden: 2}},
		{name: "no lines", need: 2, lines: 0, sel: notSelected, want: groupWindow{hidden: 2}},
		{name: "empty", need: 0, lines: 0, sel: notSelected},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := windowFor(testCase.need, testCase.lines, testCase.sel); got != testCase.want {
				t.Errorf("windowFor(%d, %d, %d) = %+v, want %+v",
					testCase.need, testCase.lines, testCase.sel, got, testCase.want)
			}
		})
	}
}

// TestGroupOf checks the three classes land in the legend's order and an
// unknown class is listed with the rest.
func TestGroupOf(t *testing.T) {
	t.Parallel()

	for group, class := range classOrder {
		if got := groupOf(class); int(got) != group+1 {
			t.Errorf("groupOf(%d) = %d, want %d, its place in classOrder after EMERGENCY", class, got, group+1)
		}

		if got := classOfGroup(group + 1); got != class {
			t.Errorf("classOfGroup(%d) = %d, want %d", group+1, got, class)
		}
	}

	if got := groupOf(unknownClass); got != groupCount-1 {
		t.Errorf("groupOf(unknown) = %d, want %d, OTHER", got, groupCount-1)
	}
}

// TestGroupedIndexOrder checks the index is IN, OUT, OTHER with distance
// order kept inside each, that the unpinned selection stands on the nearest
// aircraft wherever its section put it, and that each strip's number counts
// from one in its own section.
func TestGroupedIndexOrder(t *testing.T) {
	t.Parallel()

	scene, _ := groupedScene(ColourCorridor, filterState{})

	want := []string{nearArrival, farArrival, departure, nearOverflight, farOverflight}
	if len(scene.icaos) != len(want) {
		t.Fatalf("icaos = %v, want %v", scene.icaos, want)
	}

	for index, icao := range want {
		if scene.icaos[index] != icao {
			t.Fatalf("icaos = %v, want %v", scene.icaos, want)
		}
	}

	if got, wantStart := scene.groups.start, [groupCount + 1]int{0, 0, 2, 3, 5}; got != wantStart {
		t.Errorf("groups.start = %v, want %v", got, wantStart)
	}

	if scene.selIndex != 3 || scene.selICAO != nearOverflight {
		t.Errorf("unpinned selection at row %d (%s), want row 3 (00000A), the nearest", scene.selIndex, scene.selICAO)
	}

	for row, rank := range []int{1, 2, 1, 1, 2} {
		if got := scene.rowRank(row); got != rank {
			t.Errorf("rowRank(%d) = %d, want %d", row, got, rank)
		}
	}

	if got := scene.selRank(selection{found: true}); got != 1 {
		t.Errorf("selRank on the nearest = %d, want 1, its number in OTHER", got)
	}
}

// TestGroupedStepWalksTheListAsDrawn checks n goes down the list in the order
// it is drawn and wraps from the last OTHER to the first IN, and that a pin
// survives the next frame at its new row.
func TestGroupedStepWalksTheListAsDrawn(t *testing.T) {
	t.Parallel()

	scene, frame := groupedScene(ColourCorridor, filterState{})

	for _, want := range []string{farOverflight, nearArrival, farArrival, departure} {
		scene.step(1)

		if scene.selICAO != want {
			t.Fatalf("step(1) landed on %s, want %s", scene.selICAO, want)
		}
	}

	scene.syncSelection(frame)

	if !scene.pinned || scene.selIndex != 2 {
		t.Errorf("pinned %v at row %d after a frame, want pinned at row 2", scene.pinned, scene.selIndex)
	}
}

// TestUngroupedIndexKeepsTheFrameOrder checks altitude colouring leaves the
// index alone: frame order, the nearest at row zero, ranks down the list.
func TestUngroupedIndexKeepsTheFrameOrder(t *testing.T) {
	t.Parallel()

	scene, frame := groupedScene(ColourAltitude, filterState{})

	if scene.groups.on || scene.selIndex != 0 || len(scene.icaos) != len(frame.Planes) {
		t.Fatalf("on %v, selection at %d, %d rows; want off, 0, %d",
			scene.groups.on, scene.selIndex, len(scene.icaos), len(frame.Planes))
	}

	for index, plane := range frame.Planes {
		if scene.icaos[index] != plane.ICAO {
			t.Errorf("row %d = %s, want %s", index, scene.icaos[index], plane.ICAO)
		}

		if got := scene.rowRank(index); got != index+1 {
			t.Errorf("rowRank(%d) = %d, want %d", index, got, index+1)
		}
	}
}

// TestGroupedFilterHoldsOneSection checks a class filter leaves one section
// in the index and one heading to draw.
func TestGroupedFilterHoldsOneSection(t *testing.T) {
	t.Parallel()

	scene, _ := groupedScene(ColourCorridor, filterState{kind: filterClass, class: source.TrackOutbound})

	if len(scene.icaos) != 1 || scene.icaos[0] != departure {
		t.Fatalf("icaos = %v, want the one departure", scene.icaos)
	}

	if got, want := scene.groups.start, [groupCount + 1]int{0, 0, 0, 1, 1}; got != want {
		t.Errorf("groups.start = %v, want %v", got, want)
	}

	if got, want := scene.groupsDrawn(), [groupCount]bool{false, false, true, false}; got != want {
		t.Errorf("groupsDrawn = %v, want %v", got, want)
	}

	if got, want := (&Scene{}).groupsDrawn(), [groupCount]bool{false, true, true, true}; got != want {
		t.Errorf("groupsDrawn with no filter = %v, want %v", got, want)
	}
}

// TestGroupedBoardDropsDir checks the bare 3D board's plan carries DIR only
// while the list is not grouped.
func TestGroupedBoardDropsDir(t *testing.T) {
	t.Parallel()

	for _, grouped := range []bool{false, true} {
		scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}
		scene.groups.on = grouped

		plan, ok := scene.planBoardStrips(design(), 0, boardProbeWidth)
		if !ok {
			t.Fatal("planBoardStrips refused a generous width")
		}

		if plan.on[fieldDir] == grouped {
			t.Errorf("grouped %v: DIR on = %v, want %v", grouped, plan.on[fieldDir], !grouped)
		}
	}
}

// TestDrawGroups draws the grouped list into a column and checks the three
// cases the drawing decides on its own: a section short of lines spending
// its last on the count, the accent on the selected row, and a column too
// short for every heading.
func TestDrawGroups(t *testing.T) {
	t.Parallel()

	const width = 2000

	// Every subtest builds its own scene: drawing writes the scene's digits
	// scratch, so one shared by parallel subtests is a data race.
	fixture := func(t *testing.T) (*Scene, source.Frame, stripPlan, int) {
		t.Helper()

		scene, frame := groupedScene(ColourCorridor, filterState{})
		scene.faces = layerTestFaces(t)
		scene.pal = theme.Night

		plan, ok := scene.planStrips(design(), stripEdge+stripPadX, width)
		if !ok {
			t.Fatal("planStrips refused a generous width")
		}

		return scene, frame, plan, scene.halfStripHeight(design())
	}

	t.Run("a section short of lines counts the rest", func(t *testing.T) {
		t.Parallel()

		scene, frame, plan, pitch := fixture(t)

		// Seven lines: three headings, IN and OUT need three between them,
		// which leaves OTHER one: its count line and no strip, so the
		// selection in it is the one row the drawing cannot show.
		canv := boardCanvas(t, width, 7*pitch)
		col := &layout{dst: canv, right: width, bottom: 7 * pitch}

		scene.drawGroups(col, plan, frame, 0)

		if inkIn(canv, image.Rect(0, pitch, stripEdge, 2*pitch), theme.Night.Accent) == 0 {
			t.Error("no accent on the first IN strip, where the selection is")
		}

		if paintedIn(canv, image.Rect(0, 6*pitch, width, 7*pitch)) == 0 {
			t.Error("nothing on the last line, want OTHER's +2 MORE")
		}
	})

	t.Run("a column too short for every heading", func(t *testing.T) {
		t.Parallel()

		scene, frame, plan, pitch := fixture(t)

		canv := boardCanvas(t, width, 3*pitch)
		col := &layout{dst: canv, right: width, bottom: pitch}

		scene.drawGroups(col, plan, frame, notSelected)

		if paintedIn(canv, image.Rect(0, 0, width, pitch)) == 0 {
			t.Error("the one line the column has drew no IN heading")
		}

		if got := paintedIn(canv, image.Rect(0, pitch, width, 3*pitch)); got != 0 {
			t.Errorf("%d pixels drawn below a one-line column", got)
		}
	})
}
