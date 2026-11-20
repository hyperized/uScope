package radar

import (
	"image"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
)

// distressed is an aircraft of groupedFrame's field squawking an emergency.
const distressed = "00000F"

// distressedPlane is a high overflight squawking squawk, which the band and
// class filters below would both otherwise keep off the field.
func distressedPlane(squawk string) airplane.Snapshot {
	plane := classPlane(distressed, source.TrackTransit)
	plane.Squawk, plane.Emergency = squawk, true

	return plane
}

// emergencyFrame is groupedFrame with the far arrival squawking 7700 and a
// sixth aircraft, an overflight squawking 7600, on the end.
func emergencyFrame() source.Frame {
	frame := groupedFrame()
	frame.Planes[4].Squawk, frame.Planes[4].Emergency = "7700", true
	frame.Planes = append(frame.Planes, distressedPlane("7600"))

	return frame
}

// emergencyScene is a scene with the selection synced to emergencyFrame.
func emergencyScene(colour ColourMode, filter filterState) (*Scene, source.Frame) {
	scene := corridorScene(colour)
	scene.filter = filter
	frame := emergencyFrame()
	scene.syncSelection(frame)

	return scene, frame
}

// TestShareLinesPutsEmergenciesFirst is the EMERGENCY half of the allocation
// rule: its heading and every one of its aircraft come off before the
// classes see a line, and the classes give up their rows, headings included,
// when the column is short.
func TestShareLinesPutsEmergenciesFirst(t *testing.T) {
	t.Parallel()

	all := [groupCount]bool{true, true, true, true}

	for _, testCase := range []struct {
		name  string
		lines int
		need  [groupCount]int
		want  [groupCount]int
	}{
		{name: "everything fits", lines: 20, need: [groupCount]int{2, 1, 3, 8}, want: [groupCount]int{2, 1, 3, 8}},
		{
			name: "the classes share what it leaves", lines: 10,
			need: [groupCount]int{2, 9, 9, 9}, want: [groupCount]int{2, 2, 1, 1},
		},
		{
			name: "a short column is all emergency", lines: 4,
			need: [groupCount]int{5, 9, 9, 9}, want: [groupCount]int{3, 0, 0, 0},
		},
		{name: "one line is its heading", lines: 1, need: [groupCount]int{1, 1, 1, 1}},
		{name: "no lines", lines: 0, need: [groupCount]int{1, 1, 1, 1}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := shareLines(testCase.lines, testCase.need, all); got != testCase.want {
				t.Errorf("shareLines(%d, %v) = %v, want %v", testCase.lines, testCase.need, got, testCase.want)
			}
		})
	}
}

// TestEmergencyIndexOrder checks the two emergencies open the index in
// distance order, ahead of IN, and that neither is listed again under its
// class: the far arrival is gone from IN, the overflight from OTHER.
func TestEmergencyIndexOrder(t *testing.T) {
	t.Parallel()

	scene, _ := emergencyScene(ColourCorridor, filterState{})

	want := []string{farArrival, distressed, nearArrival, departure, nearOverflight, farOverflight}
	if len(scene.icaos) != len(want) {
		t.Fatalf("icaos = %v, want %v", scene.icaos, want)
	}

	for index, icao := range want {
		if scene.icaos[index] != icao {
			t.Fatalf("icaos = %v, want %v", scene.icaos, want)
		}
	}

	if got, wantStart := scene.groups.start, [groupCount + 1]int{0, 2, 3, 4, 6}; got != wantStart {
		t.Errorf("groups.start = %v, want %v", got, wantStart)
	}

	if got, wantDrawn := scene.groupsDrawn(), [groupCount]bool{true, true, true, true}; got != wantDrawn {
		t.Errorf("groupsDrawn = %v, want %v", got, wantDrawn)
	}

	if scene.selIndex != 4 || scene.selICAO != nearOverflight {
		t.Errorf("unpinned selection at row %d (%s), want row 4 (%s), the nearest",
			scene.selIndex, scene.selICAO, nearOverflight)
	}

	for row, rank := range []int{1, 2, 1, 1, 1, 2} {
		if got := scene.rowRank(row); got != rank {
			t.Errorf("rowRank(%d) = %d, want %d", row, got, rank)
		}
	}
}

// TestFiltersLetEmergenciesThrough checks no filter takes an emergency off
// the field: a class filter in corridor colouring keeps both in the
// EMERGENCY section and draws it, and a band, a ceiling or an operator in
// the other colourings keeps them on the ungrouped list.
func TestFiltersLetEmergenciesThrough(t *testing.T) {
	t.Parallel()

	t.Run("a class filter", func(t *testing.T) {
		t.Parallel()

		scene, _ := emergencyScene(ColourCorridor, filterState{kind: filterClass, class: source.TrackOutbound})

		want := []string{farArrival, distressed, departure}
		if len(scene.icaos) != len(want) || scene.icaos[0] != want[0] || scene.icaos[1] != want[1] ||
			scene.icaos[2] != want[2] {
			t.Fatalf("icaos = %v, want %v", scene.icaos, want)
		}

		if got, wantDrawn := scene.groupsDrawn(), [groupCount]bool{true, false, true, false}; got != wantDrawn {
			t.Errorf("groupsDrawn = %v, want %v", got, wantDrawn)
		}
	})

	for _, testCase := range []struct {
		name   string
		colour ColourMode
		filter filterState
	}{
		{name: "the low band", colour: ColourAltitude, filter: filterState{kind: filterBand, band: bandLow}},
		{name: "a ceiling", colour: ColourAltitude, filter: filterState{kind: filterBelow, band: bandLow}},
		{name: "an operator", colour: ColourAirline, filter: filterState{kind: filterOperator, icao: "ZZZ"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := corridorScene(testCase.colour)
			scene.filter = testCase.filter

			if !scene.visible(distressedPlane("7500")) {
				t.Error("the filter hid an aircraft squawking 7500")
			}

			if scene.visible(classPlane(farOverflight, source.TrackTransit)) {
				t.Error("the filter let a high overflight through, so the case proves nothing")
			}
		})
	}
}

// TestNoEmergencyDrawsNoSection checks a frame with nobody squawking leaves
// the section empty and its heading undrawn.
func TestNoEmergencyDrawsNoSection(t *testing.T) {
	t.Parallel()

	scene, _ := groupedScene(ColourCorridor, filterState{})

	if scene.groups.start[groupEmergency+1] != 0 || scene.groupsDrawn()[groupEmergency] {
		t.Errorf("groups.start = %v, drawn = %v; want an empty, undrawn EMERGENCY",
			scene.groups.start, scene.groupsDrawn())
	}
}

// TestSquawkMeaning checks each emergency code's word.
func TestSquawkMeaning(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ squawk, want string }{
		{squawk: "7500", want: meaningHijack},
		{squawk: "7600", want: meaningNoRadio},
		{squawk: "7700", want: meaningEmergency},
	} {
		t.Run(testCase.squawk, func(t *testing.T) {
			t.Parallel()

			if got := squawkMeaning(testCase.squawk); got != testCase.want {
				t.Errorf("squawkMeaning(%q) = %q, want %q", testCase.squawk, got, testCase.want)
			}
		})
	}
}

// TestSquawkMeaningsFitTheBox measures every meaning against EMERGENCY, the
// word the strip's codes slot is sized for, in the small face the box is set
// in, at both scales. A wider word would run the box past the field's rule.
func TestSquawkMeaningsFitTheBox(t *testing.T) {
	t.Parallel()

	small := layerTestFaces(t).Small

	for _, scale := range []int{1, 2} {
		col := &layout{scale: scale}
		room := col.measure(small, emergencyText)

		for _, word := range []string{meaningHijack, meaningNoRadio, meaningEmergency} {
			if got := col.measure(small, word); got > room {
				t.Errorf("scale %d: %q measures %d, wider than %q at %d", scale, word, got, emergencyText, room)
			}
		}
	}
}

// TestGroupWord checks the EMERGENCY heading takes the warning colour and a
// class heading keeps its class's word and colour.
func TestGroupWord(t *testing.T) {
	t.Parallel()

	scene := corridorScene(ColourCorridor)
	scene.pal = theme.Night

	if word, ink := scene.groupWord(groupEmergency); word != emergencyText || ink != theme.Night.Warn {
		t.Errorf("groupWord(EMERGENCY) = %q, %v; want %q, %v", word, ink, emergencyText, theme.Night.Warn)
	}

	inbound := groupOf(source.TrackInbound)
	if word, ink := scene.groupWord(int(inbound)); word != classLabel(source.TrackInbound) ||
		ink != scene.classColour(source.TrackInbound) {
		t.Errorf("groupWord(IN) = %q, %v", word, ink)
	}
}

// TestDrawEmergencySection draws the list with emergencies into columns of
// two heights: a tall one, where the section sits on top with its heading in
// the warning colour and each strip's box saying what the squawk means, and
// one too short for anything else, where only the emergencies are drawn.
func TestDrawEmergencySection(t *testing.T) {
	t.Parallel()

	const width = 2000

	fixture := func(t *testing.T) (*Scene, source.Frame, stripPlan, int) {
		t.Helper()

		scene, frame := emergencyScene(ColourCorridor, filterState{})
		scene.faces = layerTestFaces(t)
		scene.pal = theme.Night

		plan, ok := scene.planStrips(design(), stripEdge+stripPadX, width)
		if !ok {
			t.Fatal("planStrips refused a generous width")
		}

		return scene, frame, plan, scene.halfStripHeight(design())
	}

	t.Run("on top of the classes", func(t *testing.T) {
		t.Parallel()

		scene, frame, plan, pitch := fixture(t)

		const lines = 12

		canv := boardCanvas(t, width, lines*pitch)
		col := &layout{dst: canv, right: width, bottom: lines * pitch}

		scene.drawGroups(col, plan, frame, notSelected)

		for slot, what := range []string{"EMERGENCY heading", "7700 strip's box", "7600 strip's box"} {
			if inkIn(canv, image.Rect(0, slot*pitch, width, (slot+1)*pitch), theme.Night.Warn) == 0 {
				t.Errorf("no warning red on line %d, want the %s", slot, what)
			}
		}

		// IN's heading follows, and the far arrival, listed under
		// EMERGENCY, is not drawn again: IN holds the near arrival alone,
		// so OUT's heading is two lines further down.
		if inkIn(canv, image.Rect(0, 3*pitch, width, 4*pitch), theme.Night.Data) == 0 {
			t.Error("line 3 is not IN's heading")
		}

		if inkIn(canv, image.Rect(0, 5*pitch, width, 6*pitch), theme.Night.Caution) == 0 {
			t.Error("line 5 is not OUT's heading")
		}
	})

	t.Run("a short column keeps the emergencies", func(t *testing.T) {
		t.Parallel()

		scene, frame, plan, pitch := fixture(t)

		canv := boardCanvas(t, width, 6*pitch)
		col := &layout{dst: canv, right: width, bottom: 3 * pitch}

		scene.drawGroups(col, plan, frame, notSelected)

		if inkIn(canv, image.Rect(0, 2*pitch, width, 3*pitch), theme.Night.Warn) == 0 {
			t.Error("the last line of a three-line column is not the second emergency")
		}

		if got := paintedIn(canv, image.Rect(0, 3*pitch, width, 6*pitch)); got != 0 {
			t.Errorf("%d pixels drawn below a three-line column", got)
		}
	})
}
