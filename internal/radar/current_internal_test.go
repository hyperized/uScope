package radar

import (
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/source"
)

// TestApplySetsTheFilterInAltitudeMode checks that a block in altitude
// colouring puts its own filter in place whatever was there, including the
// zero block, whose zero Filter reads as ALL. A band filter is described
// completely by the setting, so there is nothing on the scene worth keeping.
func TestApplySetsTheFilterInAltitudeMode(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		set  Settings
		want filterState
	}{
		{name: "the zero block clears it", set: Settings{}, want: filterState{}},
		{
			name: "a band replaces it",
			set:  Settings{Colour: ColourAltitude, Filter: FilterLow},
			want: filterState{kind: filterBand, band: bandLow},
		},
		{
			name: "the ceiling replaces it",
			set:  Settings{Filter: FilterBelowHigh},
			want: filterState{kind: filterBelow, band: bandMid},
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

// TestApplyKeepsTheOperatorFilterInAirlineMode is the airline half: Filter
// has no say there, so a block that keeps airline colouring leaves an
// operator picked with f alone, and handing Current back changes nothing.
func TestApplyKeepsTheOperatorFilterInAirlineMode(t *testing.T) {
	t.Parallel()

	scene := New(Faces{}, source.Empty{}, scope.New(), WithSettings(Settings{Colour: ColourAirline}))
	picked := filterState{kind: filterOperator, icao: sampleICAO}
	scene.filter = picked

	if got := scene.Current().Filter; got != FilterAll {
		t.Errorf("Current().Filter in airline mode = %q, want %q", got, FilterAll)
	}

	before := scene.Current()
	scene.Apply(before)

	if scene.filter != picked {
		t.Errorf("filter after Apply(Current()) = %+v, want %+v", scene.filter, picked)
	}

	if after := scene.Current(); after != before {
		t.Errorf("Current() after Apply(Current()) = %+v, want %+v", after, before)
	}

	scene.Apply(Settings{Colour: ColourAirline, Filter: FilterHigh})

	if scene.filter != picked {
		t.Errorf("filter after an airline block naming high = %+v, want %+v", scene.filter, picked)
	}
}

// TestBandFilterOfAnOperator checks the fallback Current never reaches in
// altitude mode: a value no spelling covers reads back as ALL.
func TestBandFilterOfAnOperator(t *testing.T) {
	t.Parallel()

	for _, filter := range []filterState{{kind: filterOperator, icao: sampleICAO}, {kind: filterOther}, {}} {
		if got := bandFilterOf(filter); got != FilterAll {
			t.Errorf("bandFilterOf(%+v) = %q, want %q", filter, got, FilterAll)
		}
	}
}

// TestApplyKeepsTheCentreWithTheCadence checks that re-applying the cadence a
// bare view is already following leaves the centre it worked out, and that a
// new cadence still forgets it.
func TestApplyKeepsTheCentreWithTheCadence(t *testing.T) {
	t.Parallel()

	scene := New(Faces{}, source.Empty{}, scope.New())
	scene.Apply(Settings{View: ViewMinimal, Recentre: time.Minute})
	scene.centre, scene.haveCentre = geo{lat: 52, lon: 4}, true

	scene.Apply(Settings{View: ViewMinimal, Recentre: time.Minute})

	if !scene.haveCentre {
		t.Fatal("the same cadence forgot the centre")
	}

	scene.Apply(Settings{View: ViewMinimal, Recentre: 2 * time.Minute})

	if scene.haveCentre {
		t.Error("a new cadence kept the old centre")
	}
}
