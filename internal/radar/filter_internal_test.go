package radar

import (
	"image"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The callsigns and designators the filter tests work with. Two real operators
// so airlines.Lookup answers for both, and one aircraft with nothing to look
// up, which is what the OTHER entry stands for.
const (
	lufthansaCallsign = "DLH456"
	lufthansaICAO     = "DLH"
	unknownCallsign   = "ZZZ999"

	// goneICAO never appears in filterFleet. It stands for an aircraft that
	// has left the frame altogether, which is the case a pin cannot survive.
	goneICAO = "FFF666"
)

// Altitudes inside each of the three bands, so a case reads as a height rather
// than as a number to check against a ceiling.
const (
	lowAltitude  = 2400.0
	midAltitude  = 18000.0
	highAltitude = 35000.0
)

// filterPlane is an aircraft with the one reading the filter cares about. The
// position is far enough from zero to count as decoded, because positioned
// reads exactly (0, 0) as "nothing known yet".
func filterPlane(icao, callsign string, altitude float64) airplane.Snapshot {
	return airplane.Snapshot{
		ICAO:      icao,
		Callsign:  callsign,
		Altitude:  altitude,
		Latitude:  layerBaseLat,
		Longitude: layerBaseLon,
	}
}

// filterFleet is one aircraft per legend entry: a KLM in the low band, a
// Lufthansa in the high band, and one with a callsign no database knows.
func filterFleet() airplanes.List {
	return airplanes.List{
		filterPlane(icaoFirst, sampleCallsign, lowAltitude),
		filterPlane(icaoSecond, lufthansaCallsign, highAltitude),
		filterPlane("CCC333", unknownCallsign, midAltitude),
	}
}

func TestBandOf(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		altitude float64
		want     int
	}{
		{name: caseZero + " is not a band", altitude: 0, want: bandUnknown},
		{name: caseNegative + " is not a band either", altitude: negativeAltitude, want: bandUnknown},
		{name: "just under the low ceiling", altitude: justBelowLowBand, want: bandLow},
		{name: "exactly the low ceiling moves up", altitude: lowCeiling, want: bandMid},
		{name: "just under the mid ceiling", altitude: justBelowMidBand, want: bandMid},
		{name: "exactly the mid ceiling moves up", altitude: midCeiling, want: bandHigh},
		{name: "well above the mid ceiling", altitude: wellAboveMidBand, want: bandHigh},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := bandOf(testCase.altitude); got != testCase.want {
				t.Errorf("bandOf(%v) = %d, want %d", testCase.altitude, got, testCase.want)
			}
		})
	}
}

// TestVisible walks every value the filter can hold against every kind of
// aircraft, which is the whole contract the rest of the scene reads through.
func TestVisible(t *testing.T) {
	t.Parallel()

	low := filterPlane(icaoFirst, sampleCallsign, lowAltitude)
	high := filterPlane(icaoSecond, lufthansaCallsign, highAltitude)
	nameless := filterPlane("CCC333", "", midAltitude)
	stranger := filterPlane("DDD444", unknownCallsign, midAltitude)
	undecoded := filterPlane("EEE555", sampleCallsign, 0)

	for _, testCase := range []struct {
		name   string
		filter filterState
		plane  airplane.Snapshot
		want   bool
	}{
		{name: "everything passes at ALL", filter: filterState{}, plane: high, want: true},
		{
			name:   "a band keeps the aircraft in it",
			filter: filterState{kind: filterBand, band: bandLow}, plane: low, want: true,
		},
		{
			name:   "a band drops the aircraft above it",
			filter: filterState{kind: filterBand, band: bandLow}, plane: high, want: false,
		},
		{
			name:   "a band drops an aircraft with no altitude decoded",
			filter: filterState{kind: filterBand, band: bandLow}, plane: undecoded, want: false,
		},
		{
			name:   "an operator keeps its own aircraft",
			filter: filterState{kind: filterOperator, icao: sampleICAO}, plane: low, want: true,
		},
		{
			name:   "an operator drops another airline",
			filter: filterState{kind: filterOperator, icao: sampleICAO}, plane: high, want: false,
		},
		{
			name:   "an operator drops an aircraft the database has no colour for",
			filter: filterState{kind: filterOperator, icao: sampleICAO}, plane: stranger, want: false,
		},
		{name: "OTHER keeps an unknown prefix", filter: filterState{kind: filterOther}, plane: stranger, want: true},
		{
			name:   "OTHER keeps an aircraft with no callsign",
			filter: filterState{kind: filterOther}, plane: nameless, want: true,
		},
		{name: "OTHER drops a known operator", filter: filterState{kind: filterOther}, plane: low, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{filter: testCase.filter}
			if got := scene.visible(testCase.plane); got != testCase.want {
				t.Errorf("visible(%+v) under %+v = %v, want %v",
					testCase.plane.Callsign, testCase.filter, got, testCase.want)
			}
		})
	}
}

func TestFilterStateActive(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		filter filterState
		want   bool
	}{
		{name: "ALL holds nothing back", filter: filterState{}},
		{name: "a band does", filter: filterState{kind: filterBand, band: bandHigh}, want: true},
		{name: "so does an operator", filter: filterState{kind: filterOperator, icao: sampleICAO}, want: true},
		{name: "and so does OTHER", filter: filterState{kind: filterOther}, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.filter.active(); got != testCase.want {
				t.Errorf("active() on %+v = %v, want %v", testCase.filter, got, testCase.want)
			}
		})
	}
}

// TestNextBandFilter walks altitude mode's whole cycle, plus the value airline
// mode can leave behind when c is pressed with a filter that was never reset.
func TestNextBandFilter(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		current filterState
		want    filterState
	}{
		{name: "ALL opens on the low band", current: filterState{}, want: filterState{kind: filterBand, band: bandLow}},
		{
			name:    "low moves to mid",
			current: filterState{kind: filterBand, band: bandLow}, want: filterState{kind: filterBand, band: bandMid},
		},
		{
			name:    "mid moves to high",
			current: filterState{kind: filterBand, band: bandMid}, want: filterState{kind: filterBand, band: bandHigh},
		},
		{name: "high closes the cycle", current: filterState{kind: filterBand, band: bandHigh}, want: filterState{}},
		{
			name:    "an operator value cycles the way ALL does",
			current: filterState{kind: filterOperator, icao: sampleICAO},
			want:    filterState{kind: filterBand, band: bandLow},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := nextBandFilter(testCase.current); got != testCase.want {
				t.Errorf("nextBandFilter(%+v) = %+v, want %+v", testCase.current, got, testCase.want)
			}
		})
	}
}

// filterScene is a scene with the fleet counted into its tally, which is the
// state the f key finds between two drawn frames.
func filterScene(colour ColourMode, planes airplanes.List) *Scene {
	scene := &Scene{colour: colour}
	scene.countOperators(source.Frame{Planes: planes})

	return scene
}

// TestCycleFilterInAltitudeMode presses f the way an operator does and checks
// the value it lands on each time, all the way round.
func TestCycleFilterInAltitudeMode(t *testing.T) {
	t.Parallel()

	scene := filterScene(ColourAltitude, filterFleet())

	for step, want := range []filterState{
		{kind: filterBand, band: bandLow},
		{kind: filterBand, band: bandMid},
		{kind: filterBand, band: bandHigh},
		{},
	} {
		if !scene.Handle(input.Key{Kind: input.Rune, Rune: 'f'}) {
			t.Fatalf("press %d of f was not handled, want the filter key to take it", step+1)
		}

		if scene.filter != want {
			t.Fatalf("filter after %d presses of f = %+v, want %+v", step+1, scene.filter, want)
		}
	}
}

// TestCycleFilterInAirlineMode does the same in the other mode, where the
// cycle is the legend rather than a fixed list: the two operators the fleet
// carries, then OTHER for the aircraft with a callsign nothing knows.
func TestCycleFilterInAirlineMode(t *testing.T) {
	t.Parallel()

	scene := filterScene(ColourAirline, filterFleet())

	for step, want := range []filterState{
		{kind: filterOperator, icao: lufthansaICAO},
		{kind: filterOperator, icao: sampleICAO},
		{kind: filterOther},
		{},
	} {
		if !scene.Handle(input.Key{Kind: input.Rune, Rune: 'F'}) {
			t.Fatalf("press %d of F was not handled, want the filter key to take it", step+1)
		}

		if scene.filter != want {
			t.Fatalf("filter after %d presses of F = %+v, want %+v", step+1, scene.filter, want)
		}
	}
}

// TestCycleFilterSkipsOtherWhenNothingIsUncoloured checks the cycle following
// the legend rather than offering an entry the legend does not draw.
func TestCycleFilterSkipsOtherWhenNothingIsUncoloured(t *testing.T) {
	t.Parallel()

	scene := filterScene(ColourAirline, airplanes.List{
		filterPlane(icaoFirst, sampleCallsign, lowAltitude),
	})

	scene.cycleFilter()

	if want := (filterState{kind: filterOperator, icao: sampleICAO}); scene.filter != want {
		t.Fatalf("first press = %+v, want %+v", scene.filter, want)
	}

	scene.cycleFilter()

	if scene.filter != (filterState{}) {
		t.Errorf("second press = %+v, want ALL with no OTHER entry to stop at", scene.filter)
	}
}

// TestCycleFilterOnAnEmptyLegend checks the one press that has nowhere to go:
// airline mode with nothing on the field leaves the filter where it was.
func TestCycleFilterOnAnEmptyLegend(t *testing.T) {
	t.Parallel()

	scene := filterScene(ColourAirline, nil)
	scene.cycleFilter()

	if scene.filter != (filterState{}) {
		t.Errorf("filter on an empty field = %+v, want ALL", scene.filter)
	}
}

// TestCycleFilterFromALostOperator checks the value whose airline has left the
// scope since it was picked: it falls to the end of the cycle rather than
// hunting for a slot that is no longer there.
func TestCycleFilterFromALostOperator(t *testing.T) {
	t.Parallel()

	scene := filterScene(ColourAirline, filterFleet())
	scene.filter = filterState{kind: filterOperator, icao: "AFR"}

	scene.cycleFilter()

	if want := (filterState{kind: filterOther}); scene.filter != want {
		t.Errorf("filter after a lost operator = %+v, want %+v", scene.filter, want)
	}
}

// TestColourKeyResetsTheFilter is the rule that ties the two keys together: a
// filter value is a legend entry, and c replaces the legend.
func TestColourKeyResetsTheFilter(t *testing.T) {
	t.Parallel()

	scene := filterScene(ColourAltitude, filterFleet())
	scene.cycleFilter()

	if !scene.filter.active() {
		t.Fatal("f left the filter on ALL, want a band")
	}

	scene.Handle(input.Key{Kind: input.Rune, Rune: 'c'})

	if scene.filter != (filterState{}) {
		t.Errorf("filter after c = %+v, want ALL", scene.filter)
	}

	if scene.colour != ColourAirline {
		t.Errorf("colour after c = %q, want %q", scene.colour, ColourAirline)
	}
}

// TestApplyResetsTheFilter checks the other way into a colour mode. A settings
// block changes the legend exactly as c does, and no flag carries a filter for
// it to put back in place.
func TestApplyResetsTheFilter(t *testing.T) {
	t.Parallel()

	scene := New(Faces{}, source.Empty{}, scope.New())
	scene.filter = filterState{kind: filterBand, band: bandHigh}

	scene.Apply(Settings{Colour: ColourAirline})

	if scene.filter != (filterState{}) {
		t.Errorf("filter after Apply = %+v, want ALL", scene.filter)
	}
}

// TestCountOperators checks the tally the legend and the cycle share: counted
// and ranked in airline mode, emptied in the other, so a stale table cannot
// outlive the mode that filled it.
func TestCountOperators(t *testing.T) {
	t.Parallel()

	scene := filterScene(ColourAirline, filterFleet())

	if scene.counts.shown != 2 {
		t.Fatalf("legend entries in airline mode = %d, want 2", scene.counts.shown)
	}

	if !scene.counts.other {
		t.Error("the tally reports nothing uncoloured, want the unknown callsign counted as OTHER")
	}

	scene.colour = ColourAltitude
	scene.countOperators(source.Frame{Planes: filterFleet()})

	if scene.counts.shown != 0 || scene.counts.other {
		t.Errorf("tally in altitude mode = %d entries, other %v, want an empty table",
			scene.counts.shown, scene.counts.other)
	}
}

func TestShownCount(t *testing.T) {
	t.Parallel()

	scene := &Scene{}

	// Both numbers are read back while the other is still held, which is the
	// whole reason shownCount has a buffer of its own.
	total := scene.count(81)
	shown := scene.shownCount(12)

	if got, want := string(shown), "12"; got != want {
		t.Errorf("shownCount(12) = %q, want %q", got, want)
	}

	if got, want := string(total), "81"; got != want {
		t.Errorf("count(81) after shownCount = %q, want %q", got, want)
	}
}

func TestFindPlane(t *testing.T) {
	t.Parallel()

	frame := source.Frame{Planes: filterFleet()}

	for _, testCase := range []struct {
		name  string
		icao  string
		found bool
	}{
		{name: "an aircraft in the frame", icao: icaoSecond, found: true},
		{name: "one that is not", icao: goneICAO},
		{name: "no selection at all", icao: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			plane, found := findPlane(frame, testCase.icao)
			if found != testCase.found {
				t.Fatalf("findPlane(%q) found = %v, want %v", testCase.icao, found, testCase.found)
			}

			if found && plane.ICAO != testCase.icao {
				t.Errorf("findPlane(%q) = %q, want the aircraft asked for", testCase.icao, plane.ICAO)
			}
		})
	}
}

// TestSyncSelectionUnderAFilter covers the three states a pin can be in once
// the filter can hide an aircraft that is still flying.
func TestSyncSelectionUnderAFilter(t *testing.T) {
	t.Parallel()

	frame := source.Frame{Planes: filterFleet()}

	for _, testCase := range []struct {
		name      string
		pinned    bool
		selICAO   string
		filter    filterState
		wantICAO  string
		wantIndex int
		wantHide  bool
		wantPin   bool
	}{
		{
			name:   "an unpinned selection follows the nearest visible aircraft",
			filter: filterState{kind: filterBand, band: bandHigh},
			// The low-band aircraft is first in the list and filtered out, so
			// the nearest visible one is the Lufthansa behind it.
			wantICAO: icaoSecond, wantIndex: 0,
		},
		{
			name:   "a pinned aircraft the filter is showing keeps its row",
			pinned: true, selICAO: icaoSecond,
			filter:   filterState{kind: filterBand, band: bandHigh},
			wantICAO: icaoSecond, wantIndex: 0, wantPin: true,
		},
		{
			name:   "a pinned aircraft the filter hides stays selected and loses its row",
			pinned: true, selICAO: icaoFirst,
			filter:   filterState{kind: filterBand, band: bandHigh},
			wantICAO: icaoFirst, wantIndex: notSelected, wantHide: true, wantPin: true,
		},
		{
			name:   "a pinned aircraft that has left the list lets go",
			pinned: true, selICAO: goneICAO,
			filter:   filterState{kind: filterBand, band: bandHigh},
			wantICAO: icaoSecond, wantIndex: 0,
		},
		{
			name:     "a filter that hides everything leaves nothing selected",
			filter:   filterState{kind: filterOperator, icao: "AFR"},
			wantICAO: "", wantIndex: notSelected,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{filter: testCase.filter, pinned: testCase.pinned, selICAO: testCase.selICAO}
			scene.syncSelection(frame)

			if scene.selICAO != testCase.wantICAO {
				t.Errorf("selICAO = %q, want %q", scene.selICAO, testCase.wantICAO)
			}

			if scene.selIndex != testCase.wantIndex {
				t.Errorf("selIndex = %d, want %d", scene.selIndex, testCase.wantIndex)
			}

			if scene.selHidden != testCase.wantHide {
				t.Errorf("selHidden = %v, want %v", scene.selHidden, testCase.wantHide)
			}

			if scene.pinned != testCase.wantPin {
				t.Errorf("pinned = %v, want %v", scene.pinned, testCase.wantPin)
			}
		})
	}
}

// TestSelectedPlaneUnderAFilter checks what the card is handed in each of
// those states, which is what decides between a panel full of readings, a
// panel with a FILTERED tag on it, and NO TRAFFIC.
func TestSelectedPlaneUnderAFilter(t *testing.T) {
	t.Parallel()

	frame := source.Frame{Planes: filterFleet()}

	for _, testCase := range []struct {
		name       string
		scene      Scene
		wantFound  bool
		wantHidden bool
		wantICAO   string
	}{
		{
			name:      "a selection in the index",
			scene:     Scene{icaos: []string{icaoFirst, icaoSecond}, selIndex: 1},
			wantFound: true, wantICAO: icaoSecond,
		},
		{
			name:      "a pinned aircraft the filter is hiding",
			scene:     Scene{selHidden: true, selICAO: icaoFirst, selIndex: notSelected},
			wantFound: true, wantHidden: true, wantICAO: icaoFirst,
		},
		{
			name:  "a hidden selection whose aircraft has gone",
			scene: Scene{selHidden: true, selICAO: goneICAO, selIndex: notSelected},
		},
		{
			name:  "nothing selected at all",
			scene: Scene{selIndex: notSelected},
		},
		{
			name:  "a selection past the end of the index",
			scene: Scene{icaos: []string{icaoFirst}, selIndex: 4},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := testCase.scene

			sel := scene.selectedPlane(frame)
			if sel.found != testCase.wantFound {
				t.Fatalf("found = %v, want %v", sel.found, testCase.wantFound)
			}

			if sel.hidden != testCase.wantHidden {
				t.Errorf("hidden = %v, want %v", sel.hidden, testCase.wantHidden)
			}

			if sel.found && sel.plane.ICAO != testCase.wantICAO {
				t.Errorf("plane = %q, want %q", sel.plane.ICAO, testCase.wantICAO)
			}
		})
	}
}

// TestCentroidSkipsFilteredAircraft is the half of centroidOf that made it a
// method: minimal mode centres on the traffic it is drawing, not on the
// traffic it was handed.
func TestCentroidSkipsFilteredAircraft(t *testing.T) {
	t.Parallel()

	planes := airplanes.List{
		filterPlane(icaoFirst, sampleCallsign, lowAltitude),
		filterPlane(icaoSecond, lufthansaCallsign, highAltitude),
	}
	planes[1].Latitude, planes[1].Longitude = layerBaseLat+2, layerBaseLon+2

	scene := &Scene{filter: filterState{kind: filterBand, band: bandLow}}

	got, found := scene.centroidOf(planes)
	if !found {
		t.Fatal("no centroid, want the one visible aircraft to provide it")
	}

	if got.lat != layerBaseLat || got.lon != layerBaseLon {
		t.Errorf("centroid = %+v, want the low-band aircraft's own position", got)
	}
}

// TestAutoRangeSkipsFilteredAircraft is the same rule for the range: filtering
// to a band that holds only the near traffic pulls the scope in rather than
// leaving it sized for an aeroplane nothing draws.
func TestAutoRangeSkipsFilteredAircraft(t *testing.T) {
	t.Parallel()

	near := filterPlane(icaoFirst, sampleCallsign, lowAltitude)
	far := filterPlane(icaoSecond, lufthansaCallsign, highAltitude)
	far.Latitude = layerBaseLat + 2

	frame := source.Frame{
		Planes:   airplanes.List{near, far},
		Receiver: source.Receiver{Latitude: layerBaseLat, Longitude: layerBaseLon},
	}

	wide := &Scene{autoRange: true, scopeRange: scope.New()}
	wide.fitRange(frame)

	narrow := &Scene{
		autoRange: true, scopeRange: scope.New(),
		filter: filterState{kind: filterBand, band: bandLow},
	}
	narrow.fitRange(frame)

	if narrow.scopeRange.GetCurrent() >= wide.scopeRange.GetCurrent() {
		t.Errorf("range under the low-band filter = %g, unfiltered = %g, want the filtered one narrower",
			narrow.scopeRange.GetCurrent(), wide.scopeRange.GetCurrent())
	}
}

// TestDrawStripStatusShowsTheFilterHeld checks what the line above the panel
// actually paints: "SEL -- \u00b7 2 AIRCRAFT" at ALL, "SEL -- \u00b7 1 OF 2 AIRCRAFT"
// once a filter is active, so a short board under a filter reads as the filter
// doing its job rather than as a quiet sky.
//
// It calls drawStripStatus directly on a layout of its own rather than through
// a full Draw, because the panel and the board share the column with it and
// would sit close enough to anything measured out of the whole frame to make
// the three hard to tell apart by counting pixels alone.
func TestDrawStripStatusShowsTheFilterHeld(t *testing.T) {
	t.Parallel()

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}

	canv, err := canvas.New(420, 20)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	const (
		shown = 1
		total = 2
		right = 400
	)

	col := &layout{dst: canv, left: 0, right: right, top: 0, bottom: 20}

	draw := func() int {
		canv.Clear(theme.Night.Field)
		scene.drawStripStatus(col, selection{}, shown, total)

		return filterPainted(canv)
	}

	plain := draw()
	if plain == 0 {
		t.Fatal(`drawStripStatus at ALL painted nothing, want "SEL -- 2 AIRCRAFT"`)
	}

	scene.filter = filterState{kind: filterBand, band: bandLow}

	if got := draw(); got <= plain {
		t.Errorf(`pixels filtered = %d, at ALL = %d, want more with the "1 OF 2" prefix`, got, plain)
	}
}

// TestMarkLegendEntryDrawsNothingUnmarked pins the half of the legend marker
// that is easy to get wrong: every entry calls it and only one of them may
// leave a mark.
func TestMarkLegendEntryDrawsNothingUnmarked(t *testing.T) {
	t.Parallel()

	canv, err := canvas.New(swatchSide*4, swatchSide*4)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(theme.Night.Field)

	scene := &Scene{pal: theme.Night}
	box := image.Rect(swatchSide, swatchSide, 2*swatchSide, 2*swatchSide)

	scene.markLegendEntry(canv, box, false)

	if got := filterPainted(canv); got != 0 {
		t.Fatalf("an unmarked entry painted %d pixels, want none", got)
	}

	scene.markLegendEntry(canv, box, true)

	if got := filterPainted(canv); got == 0 {
		t.Error("a marked entry painted nothing, want a ring round the swatch")
	}
}

// TestDrawCardLabelShowsTheFilteredTag checks the one thing sel.hidden adds to
// the word over the selected aircraft's panel: the extra " \u00b7 FILTERED" after
// it, in the caution colour. The same label with nothing hidden must not pick
// the tag up on its own.
func TestDrawCardLabelShowsTheFilteredTag(t *testing.T) {
	t.Parallel()

	scene := &Scene{faces: layerTestFaces(t), pal: theme.Night}

	canv, err := canvas.New(400, 20)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	draw := func(sel selection) (int, int) {
		canv.Clear(theme.Night.Field)
		scene.drawCardLabel(canv, 0, 0, sel)

		return filterPainted(canv), colourCount(canv, canv.Bounds(), theme.Night.Caution)
	}

	plain, plainAmber := draw(selection{})

	hidden, hiddenAmber := draw(selection{hidden: true})

	if hidden <= plain {
		t.Errorf("pixels painted with the FILTERED tag = %d, without it = %d, want more with the tag",
			hidden, plain)
	}

	if plainAmber != 0 {
		t.Errorf("an unfiltered label painted %d caution pixels, want none", plainAmber)
	}

	if hiddenAmber == 0 {
		t.Error("the FILTERED tag painted nothing in the caution colour")
	}
}

// filterPainted counts the pixels on a canvas that are not the night field,
// which is how the marker test asks whether anything was drawn at all.
func filterPainted(canv *canvas.Canvas) int {
	bounds := canv.Bounds()
	count := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) != theme.Night.Field {
				count++
			}
		}
	}

	return count
}
