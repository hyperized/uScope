package radar

import (
	"image"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
)

// corridorSouthDeg is twenty nautical miles of latitude: inside the terminal
// area, far enough out that the departure-by-history rule never fires on a
// plane with no history.
const corridorSouthDeg = 20.0 / 60

// unknownClass is a TrackClass value no constant names, for the default
// branches.
const unknownClass source.TrackClass = 99

// classPlane is an aircraft twenty miles south of boardCorridors' field doing
// what the class says: pointed at the field low and level, climbing away
// from it, or crossing at cruise.
func classPlane(icao string, class source.TrackClass) airplane.Snapshot {
	plane := airplane.Snapshot{
		ICAO: icao, Callsign: sampleCallsign,
		Latitude: boardFieldLat - corridorSouthDeg, Longitude: boardFieldLon,
	}

	switch class {
	case source.TrackInbound:
		plane.Heading, plane.Altitude = 0, lowAltitude
	case source.TrackOutbound:
		plane.Heading, plane.Altitude, plane.VertRate = 180, lowAltitude, 1500
	case source.TrackTransit:
		fallthrough
	default:
		plane.Heading, plane.Altitude = 90, highAltitude
	}

	return plane
}

// corridorScene is a built scene in a colour mode with boardCorridors on its
// last frame, which is where Classify reads the field from.
func corridorScene(colour ColourMode) *Scene {
	scene := New(Faces{}, source.Empty{}, scope.New(), WithColour(colour))
	scene.exposure.corridors = boardCorridors()

	return scene
}

// TestCorridorModeColours checks every way into the colour rule reads the
// track class in corridor mode, and that the class costs nothing elsewhere.
func TestCorridorModeColours(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		class source.TrackClass
	}{
		{name: "an arrival", class: source.TrackInbound},
		{name: "a departure", class: source.TrackOutbound},
		{name: "an overflight", class: source.TrackTransit},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := corridorScene(ColourCorridor)
			plane := classPlane(icaoFirst, testCase.class)
			want := scene.classColour(testCase.class)

			if got := scene.aircraftColour(plane); got != want {
				t.Errorf("aircraftColour = %v, want %v", got, want)
			}

			if got := scene.callsignInk(plane); got != want {
				t.Errorf("callsignInk = %v, want %v", got, want)
			}

			ghost := source.Trail{ICAO: icaoFirst, Altitude: highAltitude, Class: testCase.class}
			if got := scene.ghostColour(ghost); got != want {
				t.Errorf("ghostColour = %v, want %v", got, want)
			}
		})
	}

	t.Run("no field draws every aircraft muted", func(t *testing.T) {
		t.Parallel()

		scene := corridorScene(ColourCorridor)
		scene.exposure.corridors = source.Corridors{}

		if got := scene.aircraftColour(classPlane(icaoFirst, source.TrackInbound)); got != theme.Night.Muted {
			t.Errorf("aircraftColour with no field = %v, want muted", got)
		}
	})

	t.Run("altitude mode does not classify", func(t *testing.T) {
		t.Parallel()

		scene := corridorScene(ColourAltitude)
		plane := classPlane(icaoFirst, source.TrackInbound)

		if got := scene.colourClass(plane); got != source.TrackTransit {
			t.Errorf("colourClass in altitude mode = %v, want transit", got)
		}

		if got := scene.aircraftColour(plane); got != scene.bandColour(lowAltitude) {
			t.Errorf("aircraftColour in altitude mode = %v, want the band", got)
		}
	})
}

// TestFilterClassOnlyClassifiesUnderAClassFilter checks visible leaves
// Classify alone unless the filter is holding to a class.
func TestFilterClassOnlyClassifiesUnderAClassFilter(t *testing.T) {
	t.Parallel()

	scene := corridorScene(ColourCorridor)
	plane := classPlane(icaoFirst, source.TrackInbound)

	if got := scene.filterClass(plane); got != source.TrackTransit {
		t.Errorf("filterClass with no class filter = %v, want transit", got)
	}

	scene.setFilter(filterState{kind: filterClass, class: source.TrackOutbound})

	if got := scene.filterClass(plane); got != source.TrackInbound {
		t.Errorf("filterClass under a class filter = %v, want inbound", got)
	}
}

func TestVisibleByClass(t *testing.T) {
	t.Parallel()

	inbound := classPlane(icaoFirst, source.TrackInbound)
	outbound := classPlane(icaoSecond, source.TrackOutbound)
	transit := classPlane("CCC333", source.TrackTransit)

	for _, testCase := range []struct {
		name  string
		class source.TrackClass
		plane airplane.Snapshot
		want  bool
	}{
		{name: "IN keeps an arrival", class: source.TrackInbound, plane: inbound, want: true},
		{name: "IN drops a departure", class: source.TrackInbound, plane: outbound},
		{name: "OUT keeps a departure", class: source.TrackOutbound, plane: outbound, want: true},
		{name: "OUT drops an overflight", class: source.TrackOutbound, plane: transit},
		{name: "OTHER keeps an overflight", class: source.TrackTransit, plane: transit, want: true},
		{name: "OTHER drops an arrival", class: source.TrackTransit, plane: inbound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := corridorScene(ColourCorridor)
			scene.setFilter(filterState{kind: filterClass, class: testCase.class})

			if got := scene.visible(testCase.plane); got != testCase.want {
				t.Errorf("visible = %v, want %v", got, testCase.want)
			}
		})
	}

	t.Run("a coasting aircraft goes through on its trail's class", func(t *testing.T) {
		t.Parallel()

		scene := corridorScene(ColourCorridor)
		scene.setFilter(filterState{kind: filterClass, class: source.TrackOutbound})

		if !scene.passes(icaoFirst, highAltitude, "", source.TrackOutbound) {
			t.Error("an outbound trail was dropped by OUT")
		}

		if scene.passes(icaoSecond, highAltitude, "", source.TrackInbound) {
			t.Error("an inbound trail passed OUT")
		}
	})
}

// TestCycleFilterInCorridorMode presses f all the way round corridor mode's
// cycle.
func TestCycleFilterInCorridorMode(t *testing.T) {
	t.Parallel()

	scene := filterScene(ColourCorridor, filterFleet())

	for step, want := range []filterState{
		{kind: filterClass, class: source.TrackInbound},
		{kind: filterClass, class: source.TrackOutbound},
		{kind: filterClass, class: source.TrackTransit},
		{kind: filterBelow, band: bandMid},
		{},
	} {
		if !scene.Handle(input.Key{Kind: input.Rune, Rune: 'f'}) {
			t.Fatalf("press %d of f was not handled", step+1)
		}

		if scene.filter != want {
			t.Fatalf("filter after %d presses of f = %+v, want %+v", step+1, scene.filter, want)
		}
	}
}

func TestNextClassFilter(t *testing.T) {
	t.Parallel()

	arrivals := filterState{kind: filterClass, class: source.TrackInbound}
	ceiling := filterState{kind: filterBelow, band: bandMid}

	for _, testCase := range []struct {
		name    string
		current filterState
		want    filterState
	}{
		{name: "ALL opens on IN", current: filterState{}, want: arrivals},
		{name: "IN moves to OUT", current: arrivals, want: filterState{kind: filterClass, class: source.TrackOutbound}},
		{
			name:    "OUT moves to OTHER",
			current: filterState{kind: filterClass, class: source.TrackOutbound},
			want:    filterState{kind: filterClass, class: source.TrackTransit},
		},
		{
			name:    "OTHER moves to the ceiling",
			current: filterState{kind: filterClass, class: source.TrackTransit},
			want:    ceiling,
		},
		{
			name:    "an unknown class moves to the ceiling",
			current: filterState{kind: filterClass, class: unknownClass},
			want:    ceiling,
		},
		{name: "the ceiling closes the cycle", current: ceiling, want: filterState{}},
		{
			name:    "a band cycles the way ALL does",
			current: filterState{kind: filterBand, band: bandHigh}, want: arrivals,
		},
		{
			name:    "an operator cycles the way ALL does",
			current: filterState{kind: filterOperator, icao: sampleICAO},
			want:    arrivals,
		},
		{name: "airline OTHER cycles the way ALL does", current: filterState{kind: filterOther}, want: arrivals},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := nextClassFilter(testCase.current); got != testCase.want {
				t.Errorf("nextClassFilter(%+v) = %+v, want %+v", testCase.current, got, testCase.want)
			}
		})
	}
}

// TestOtherCyclesLeaveAClass checks the altitude and airline cycles treat a
// class value the way they treat ALL.
func TestOtherCyclesLeaveAClass(t *testing.T) {
	t.Parallel()

	class := filterState{kind: filterClass, class: source.TrackInbound}

	if got := nextBandFilter(class); got != (filterState{kind: filterBand, band: bandLow}) {
		t.Errorf("nextBandFilter(class) = %+v, want the low band", got)
	}

	scene := filterScene(ColourAirline, filterFleet())
	scene.filter = class
	scene.cycleFilter()

	if scene.filter != (filterState{kind: filterOperator, icao: lufthansaICAO}) {
		t.Errorf("airline cycle from a class = %+v, want the first operator", scene.filter)
	}
}

func TestClassFilterState(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		class source.TrackClass
		label string
	}{
		{class: source.TrackInbound, label: legendIn},
		{class: source.TrackOutbound, label: legendOut},
		{class: source.TrackTransit, label: legendOther},
		{class: unknownClass, label: legendOther},
	} {
		t.Run(testCase.label, func(t *testing.T) {
			t.Parallel()

			filter := filterState{kind: filterClass, class: testCase.class}

			if !filter.active() {
				t.Error("a class filter reads as inactive")
			}

			if got := filter.label(); got != testCase.label {
				t.Errorf("label = %q, want %q", got, testCase.label)
			}

			if !filter.marksClass(testCase.class) || filter.marksClass(testCase.class+1) {
				t.Error("marksClass does not pick out exactly its own class")
			}

			if filter.marksBand(bandLow) || filter.marksOther() {
				t.Error("a class filter marks a band or the airline OTHER")
			}
		})
	}

	if (filterState{kind: filterOther}).marksClass(source.TrackTransit) {
		t.Error("the airline OTHER marks the corridor OTHER")
	}
}

func TestColourLabel(t *testing.T) {
	t.Parallel()

	for mode, want := range map[ColourMode]string{
		ColourAltitude: labelAltitude,
		ColourAirline:  labelAirline,
		ColourCorridor: labelCorridor,
		"":             labelAltitude,
	} {
		if got := colourLabel(mode); got != want {
			t.Errorf("colourLabel(%q) = %q, want %q", mode, got, want)
		}
	}
}

// TestDrawClassLegend checks the right column's corridor legend: three filled
// swatches in the class colours, the filtered one ringed, and an entry that
// will not fit dropped whole.
func TestDrawClassLegend(t *testing.T) {
	t.Parallel()

	faces := layerTestFaces(t)

	t.Run("all three, the filtered one ringed", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{faces: faces, colour: ColourCorridor}
		scene.SetPalette(theme.Night)

		canv := boardCanvas(t, boardProbeWidth, 40)
		col := layout{dst: canv, right: boardProbeWidth, bottom: 40}
		scene.drawLegend(&col)

		box := canv.Bounds()
		for _, entry := range scene.classEntries() {
			if inkIn(canv, box, entry.col) == 0 {
				t.Errorf("no %s swatch", entry.label)
			}
		}

		ink := inkIn(canv, box, theme.Night.Ink)

		scene.setFilter(filterState{kind: filterClass, class: source.TrackOutbound})

		ringed := boardCanvas(t, boardProbeWidth, 40)
		col = layout{dst: ringed, right: boardProbeWidth, bottom: 40}
		scene.drawLegend(&col)

		if inkIn(ringed, box, theme.Night.Ink) <= ink {
			t.Error("the filtered class was not ringed")
		}
	})

	t.Run("a narrow column drops what does not fit", func(t *testing.T) {
		t.Parallel()

		scene := &Scene{faces: faces, colour: ColourCorridor}
		scene.SetPalette(theme.Night)

		const narrow = 40

		canv := boardCanvas(t, narrow, 40)
		col := layout{dst: canv, right: narrow, bottom: 40}
		scene.drawLegend(&col)

		if inkIn(canv, image.Rect(0, 0, narrow, 40), theme.Night.Caution) != 0 {
			t.Error("OUT was drawn in a column that only holds IN")
		}
	})
}

// TestBoardKeysInCorridorMode checks the board's legend is the three classes,
// filled, less any class the filter hides.
func TestBoardKeysInCorridorMode(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		filter filterState
		want   []string
	}{
		{name: "everything", want: []string{legendIn, legendOut, legendOther}},
		{
			name:   "under the ceiling",
			filter: filterState{kind: filterBelow, band: bandMid},
			want:   []string{legendIn, legendOut, legendOther},
		},
		{
			name:   "one class",
			filter: filterState{kind: filterClass, class: source.TrackOutbound}, want: []string{legendOut},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{colour: ColourCorridor, filter: testCase.filter}
			scene.SetPalette(theme.Night)

			keys, count := scene.boardKeys()
			if count != len(testCase.want) {
				t.Fatalf("boardKeys() count = %d, want %d", count, len(testCase.want))
			}

			for index, label := range testCase.want {
				if keys[index].label != label || keys[index].outline {
					t.Errorf("entry %d = %+v, want a filled %q", index, keys[index], label)
				}
			}
		})
	}
}
