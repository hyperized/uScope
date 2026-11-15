package radar

import (
	"testing"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/source"
)

// TestApplySetsTheFilter checks the stop Apply starts the filter on, from a
// scene that already had another one, including every combination of stop and
// colouring the stop does not belong to, which fall back to ALL.
func TestApplySetsTheFilter(t *testing.T) {
	t.Parallel()

	ceiling := filterState{kind: filterBelow, band: bandMid}

	for _, testCase := range []struct {
		name string
		set  Settings
		want filterState
	}{
		{name: "the zero block clears it", set: Settings{}},
		{
			name: "a band in altitude colouring",
			set:  Settings{Colour: ColourAltitude, Filter: FilterLow},
			want: filterState{kind: filterBand, band: bandLow},
		},
		{name: "the ceiling with the default colouring", set: Settings{Filter: FilterBelowHigh}, want: ceiling},
		{
			name: "the ceiling in corridor colouring",
			set:  Settings{Colour: ColourCorridor, Filter: FilterBelowHigh},
			want: ceiling,
		},
		{
			name: "a class in corridor colouring",
			set:  Settings{Colour: ColourCorridor, Filter: FilterOther},
			want: filterState{kind: filterClass, class: source.TrackTransit},
		},
		{
			name: "out in corridor colouring",
			set:  Settings{Colour: ColourCorridor, Filter: FilterOut},
			want: filterState{kind: filterClass, class: source.TrackOutbound},
		},
		{name: "a band in corridor colouring falls back", set: Settings{Colour: ColourCorridor, Filter: FilterMid}},
		{name: "a class in altitude colouring falls back", set: Settings{Colour: ColourAltitude, Filter: FilterIn}},
		{name: "a band in airline colouring falls back", set: Settings{Colour: ColourAirline, Filter: FilterHigh}},
		{
			name: "the ceiling in airline colouring falls back",
			set:  Settings{Colour: ColourAirline, Filter: FilterBelowHigh},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := New(Faces{}, source.Empty{}, scope.New())
			scene.filter = filterState{kind: filterBand, band: bandHigh}

			scene.Apply(testCase.set)

			if scene.filter != testCase.want {
				t.Errorf("filter after Apply(%+v) = %+v, want %+v", testCase.set, scene.filter, testCase.want)
			}
		})
	}
}

// TestFilterKeyCyclesFromTheStartingStop checks f moves on from the stop the
// settings started on exactly as it would from that stop reached by pressing.
func TestFilterKeyCyclesFromTheStartingStop(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		set  Settings
		want filterState
	}{
		{
			name: "mid to high",
			set:  Settings{Filter: FilterMid},
			want: filterState{kind: filterBand, band: bandHigh},
		},
		{name: "the ceiling back to all", set: Settings{Filter: FilterBelowHigh}},
		{
			name: "in to out",
			set:  Settings{Colour: ColourCorridor, Filter: FilterIn},
			want: filterState{kind: filterClass, class: source.TrackOutbound},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := New(Faces{}, source.Empty{}, scope.New(), WithSettings(testCase.set))

			if !scene.Handle(input.Key{Kind: input.Rune, Rune: 'f'}) {
				t.Fatal("f was not handled")
			}

			if scene.filter != testCase.want {
				t.Errorf("filter after f = %+v, want %+v", scene.filter, testCase.want)
			}
		})
	}
}

// TestAdmit checks which cycles hold which kinds of value, airline's
// holding none.
func TestAdmit(t *testing.T) {
	t.Parallel()

	ceiling := filterState{kind: filterBelow, band: bandMid}
	band := filterState{kind: filterBand, band: bandLow}
	class := filterState{kind: filterClass, class: source.TrackInbound}

	for _, testCase := range []struct {
		mode   ColourMode
		filter filterState
		held   bool
	}{
		{mode: ColourAltitude, filter: ceiling, held: true},
		{mode: ColourCorridor, filter: ceiling, held: true},
		{mode: ColourAirline, filter: ceiling},
		{mode: "", filter: ceiling, held: true},
		{mode: ColourAltitude, filter: band, held: true},
		{mode: ColourCorridor, filter: band},
		{mode: ColourAirline, filter: band},
		{mode: ColourCorridor, filter: class, held: true},
		{mode: ColourAltitude, filter: class},
		{mode: ColourAirline, filter: class},
		{mode: ColourAirline, filter: filterState{kind: filterOperator, icao: sampleICAO}},
	} {
		want := filterState{}
		if testCase.held {
			want = testCase.filter
		}

		if got := testCase.mode.admit(testCase.filter); got != want {
			t.Errorf("%q.admit(%+v) = %+v, want %+v", testCase.mode, testCase.filter, got, want)
		}
	}
}
