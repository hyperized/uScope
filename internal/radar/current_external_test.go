package radar_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
)

// TestCurrentReadsBackWhatApplySet checks that a block handed to Apply comes
// back out of Current field for field, with every default spelled out, and
// that handing Current's answer back to Apply changes nothing.
func TestCurrentReadsBackWhatApplySet(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		set  radar.Settings
		want radar.Settings
	}{
		{
			name: "the zero block reads back as the defaults",
			set:  radar.Settings{},
			want: radar.Settings{
				Colour: radar.ColourAltitude, Filter: radar.FilterAll, Airports: radar.ToggleOn, Shore: radar.ToggleOn,
				View: radar.ViewScope, Exaggerate: radar.DefaultExaggerate, Tilt: radar.DefaultTilt,
				Orbit: radar.ToggleOn, Stars: radar.ToggleOn, Corridors: radar.ToggleOn,
			},
		},
		{
			name: "every field set",
			set: radar.Settings{
				Colour: radar.ColourAirline, Filter: radar.FilterLow, Airports: radar.ToggleOff, Shore: radar.ToggleOff,
				MinimalShore: true, MinimalAirports: true, MinimalBar: true, Board: true, RangeNm: 60,
				View:       radar.ViewMinimal3D,
				Exaggerate: 4, Zoom: -2, Tilt: 25, Recentre: time.Minute, Orbit: radar.ToggleOff,
				Azimuth: 450, Lights: true, Stars: radar.ToggleOff, Corridors: radar.ToggleOff,
			},
			want: radar.Settings{
				Colour: radar.ColourAirline, Filter: radar.FilterAll, Airports: radar.ToggleOff, Shore: radar.ToggleOff,
				MinimalShore: true, MinimalAirports: true, MinimalBar: true, Board: true, RangeNm: 60,
				View:       radar.ViewMinimal3D,
				Exaggerate: 4, Zoom: -2, Tilt: 25, Recentre: time.Minute, Orbit: radar.ToggleOff,
				Azimuth: 90, Lights: true, Stars: radar.ToggleOff, Corridors: radar.ToggleOff,
			},
		},
		{
			name: "a non-finite azimuth reads as north",
			set:  radar.Settings{Orbit: radar.ToggleOff, Azimuth: math.Inf(1)},
			want: radar.Settings{
				Colour: radar.ColourAltitude, Filter: radar.FilterAll, Airports: radar.ToggleOn, Shore: radar.ToggleOn,
				View: radar.ViewScope, Exaggerate: radar.DefaultExaggerate, Tilt: radar.DefaultTilt,
				Orbit: radar.ToggleOff, Stars: radar.ToggleOn, Corridors: radar.ToggleOn,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, _, _ := liveScene(t, source.Frame{})
			scene.Apply(testCase.set)

			if got := scene.Current(); got != testCase.want {
				t.Fatalf("Current() = %+v, want %+v", got, testCase.want)
			}

			scene.Apply(scene.Current())

			if got := scene.Current(); got != testCase.want {
				t.Errorf("Current() after Apply(Current()) = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

// TestApplyCurrentDoesNotMoveTheCamera is the reason Current exists: the
// control port reads the block, changes one field and hands it back, and a
// camera that jumped back to where the orbit began every time that happened
// would make the port useless on a live feed.
func TestApplyCurrentDoesNotMoveTheCamera(t *testing.T) {
	t.Parallel()

	scene, _, canv := liveScene(t, source.Frame{})
	scene.Apply(radar.Settings{View: radar.View3D, Azimuth: 10})

	// A quarter of a two-minute revolution.
	scene.Draw(canv, 30*time.Second)

	before := scene.Current().Azimuth
	if math.Abs(before-100) > 1e-9 {
		t.Fatalf("azimuth after 30s = %g, want 100", before)
	}

	scene.Apply(scene.Current())

	if got := scene.Current().Azimuth; math.Abs(got-before) > 1e-9 {
		t.Errorf("azimuth after Apply(Current()) = %g, want %g", got, before)
	}

	// The orbit carries on from where it was rebased, not from where it began.
	scene.Draw(canv, 60*time.Second)

	if got := scene.Current().Azimuth; math.Abs(got-190) > 1e-9 {
		t.Errorf("azimuth after 60s = %g, want 190", got)
	}
}

// TestCurrentReadsBackTheAltitudeFilter applies each spelling of the altitude
// filter and checks that Current spells it back and that Apply(Current())
// leaves it where it was, which is what the control port's set relies on.
func TestCurrentReadsBackTheAltitudeFilter(t *testing.T) {
	t.Parallel()

	for _, filter := range []radar.BandFilter{
		radar.FilterAll, radar.FilterLow, radar.FilterMid, radar.FilterHigh, radar.FilterBelowHigh,
	} {
		t.Run(string(filter), func(t *testing.T) {
			t.Parallel()

			scene, _, _ := liveScene(t, source.Frame{})
			scene.Apply(radar.Settings{Filter: filter})

			if got := scene.Current().Filter; got != filter {
				t.Fatalf("Current().Filter = %q, want %q", got, filter)
			}

			scene.Apply(scene.Current())

			if got := scene.Current().Filter; got != filter {
				t.Errorf("Current().Filter after Apply(Current()) = %q, want %q", got, filter)
			}
		})
	}
}

// TestCurrentReadsBackTheCorridorFilter is the same in corridor colouring,
// whose cycle holds the three classes and the ceiling.
func TestCurrentReadsBackTheCorridorFilter(t *testing.T) {
	t.Parallel()

	for _, filter := range []radar.BandFilter{
		radar.FilterAll, radar.FilterIn, radar.FilterOut, radar.FilterOther, radar.FilterBelowHigh,
	} {
		t.Run(string(filter), func(t *testing.T) {
			t.Parallel()

			scene, _, _ := liveScene(t, source.Frame{})
			scene.Apply(radar.Settings{Colour: radar.ColourCorridor, Filter: filter})

			if got := scene.Current().Filter; got != filter {
				t.Fatalf("Current().Filter = %q, want %q", got, filter)
			}

			scene.Apply(scene.Current())

			if got := scene.Current().Filter; got != filter {
				t.Errorf("Current().Filter after Apply(Current()) = %q, want %q", got, filter)
			}
		})
	}
}

// TestParseFilter covers the --filter allow list: the eight spellings, and the
// refusals of a wrong case, a cap's own word and nothing at all.
func TestParseFilter(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"all", "low", "mid", "high", "below-high", "in", "out", "other"} {
		got, err := radar.ParseFilter(text)
		if err != nil || string(got) != text {
			t.Errorf("ParseFilter(%q) = %q, %v, want %q", text, got, err, text)
		}
	}

	for _, text := range []string{"", "Low", "<25K", "below", "IN", "transit"} {
		got, err := radar.ParseFilter(text)
		if !errors.Is(err, radar.ErrFilter) || got != radar.FilterAll {
			t.Errorf("ParseFilter(%q) = %q, %v, want FilterAll and ErrFilter", text, got, err)
		}
	}
}
