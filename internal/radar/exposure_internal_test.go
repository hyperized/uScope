package radar

import (
	"image/color"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

//nolint:gochecknoglobals // read-only test fixture.
var exposureEpoch = time.Date(2026, time.November, 2, 9, 0, 0, 0, time.UTC)

// exposureField is the airfield the internal fixtures classify against.
//
//nolint:gochecknoglobals // read-only test fixture.
var exposureField = source.Corridors{
	Field:    source.Airfield{ICAO: "EHAM", Latitude: layerBaseLat, Longitude: layerBaseLon},
	HalfLife: time.Hour,
}

// exposureTrack is a track running north-east from the fixture receiver,
// climbing from the low band to the high one and ending on an altitude
// nobody decoded, lost age before exposureEpoch. It runs off the diagonal
// rather than due north because the 3D view's bowl has a meridian standing
// straight up in front of the camera, and a track behind it would be hidden.
func exposureTrack(class source.TrackClass, age time.Duration) source.Trail {
	points := make([]airplane.PositionEntry, 0, 4)

	for index, altitude := range []float64{3000, 15000, 30000, 0} {
		points = append(points, airplane.PositionEntry{
			Latitude:  layerBaseLat + float64(index)*0.1,
			Longitude: layerBaseLon + float64(index)*0.13,
			Altitude:  altitude,
		})
	}

	return source.Trail{ICAO: "E00001", Points: points, LostAt: exposureEpoch.Add(-age), Class: class}
}

func TestExposureInk(t *testing.T) {
	t.Parallel()

	scene := &Scene{pal: theme.Night, exposure: exposure{corridors: exposureField, now: exposureEpoch}}

	for _, testCase := range []struct {
		name  string
		trail source.Trail
		want  color.RGBA
		drawn bool
	}{
		{
			name:  "an arrival just lost",
			trail: exposureTrack(source.TrackInbound, 0),
			want:  scene.fade(theme.Night.Data, corridorTintMax), drawn: true,
		},
		{
			name:  "a departure a half-life old",
			trail: exposureTrack(source.TrackOutbound, time.Hour),
			want:  scene.fade(theme.Night.Caution, corridorTintMax/2), drawn: true,
		},
		{
			name:  "an overflight lost after the clock",
			trail: exposureTrack(source.TrackTransit, -time.Hour),
			want:  scene.fade(theme.Night.Muted, corridorTintMax), drawn: true,
		},
		{name: "a track faded past drawing", trail: exposureTrack(source.TrackInbound, 5*time.Hour)},
		{
			name:  "a class nobody knows",
			trail: exposureTrack(source.TrackClass(9), 0),
			want:  scene.fade(theme.Night.Muted, corridorTintMax), drawn: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, drawn := scene.exposureInk(&testCase.trail)
			if drawn != testCase.drawn || got != testCase.want {
				t.Errorf("exposureInk = %v, %t, want %v, %t", got, drawn, testCase.want, testCase.drawn)
			}
		})
	}
}

func TestExposureDrawn(t *testing.T) {
	t.Parallel()

	trails := []source.Trail{exposureTrack(source.TrackInbound, 0)}
	noHalfLife := exposureField
	noHalfLife.HalfLife = 0

	for _, testCase := range []struct {
		name  string
		scene Scene
		want  bool
	}{
		{
			name:  "drawn",
			scene: Scene{corridors: true, exposure: exposure{trails: trails, corridors: exposureField}},
			want:  true,
		},
		{name: "the setting is off", scene: Scene{exposure: exposure{trails: trails, corridors: exposureField}}},
		{name: "no airfield", scene: Scene{corridors: true, exposure: exposure{trails: trails}}},
		{
			name:  "no half-life",
			scene: Scene{corridors: true, exposure: exposure{trails: trails, corridors: noHalfLife}},
		},
		{name: "nothing lost yet", scene: Scene{corridors: true, exposure: exposure{corridors: exposureField}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.scene.exposureDrawn(); got != testCase.want {
				t.Errorf("exposureDrawn = %t, want %t", got, testCase.want)
			}

			// Minimal with nothing else behind its aircraft takes a layer
			// exactly when there is an exposure to put on it.
			testCase.scene.shown = ViewMinimal
			if got := testCase.scene.layerless(); got == testCase.want {
				t.Errorf("layerless = %t with exposureDrawn %t", got, testCase.want)
			}
		})
	}
}

func TestBandMaskHas(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		mask bandMask
		want [bandCount]bool
	}{
		{name: "none", mask: 0},
		{name: "low", mask: 1 << bandLow, want: [bandCount]bool{true, false, false}},
		{name: "high", mask: 1 << bandHigh, want: [bandCount]bool{false, false, true}},
		{name: "all", mask: allBands, want: [bandCount]bool{true, true, true}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			for band, want := range testCase.want {
				if got := testCase.mask.has(band); got != want {
					t.Errorf("has(%d) = %t, want %t", band, got, want)
				}
			}

			for _, band := range []int{-1, bandUnknown} {
				if testCase.mask.has(band) {
					t.Errorf("has(%d) = true for a band that does not exist", band)
				}
			}
		})
	}
}

// TestCorridorBandsFollowVisible checks the exposure reads exactly the bands
// the filter lets aircraft through in: for every filter value and every band,
// the mask holds the band when visible accepts an aircraft flying in it.
func TestCorridorBandsFollowVisible(t *testing.T) {
	t.Parallel()

	inside := [bandCount]float64{bandLow: 5000, bandMid: 15000, bandHigh: 35000}

	for _, filter := range []filterState{
		{},
		{kind: filterBand, band: bandLow},
		{kind: filterBand, band: bandMid},
		{kind: filterBand, band: bandHigh},
	} {
		t.Run(filter.label(), func(t *testing.T) {
			t.Parallel()

			scene := &Scene{filter: filter}
			mask := scene.corridorBands()

			for band, altitude := range inside {
				shown := scene.visible(airplane.Snapshot{Altitude: altitude})
				if mask.has(band) != shown {
					t.Errorf("band %d: mask has it %t, visible says %t", band, mask.has(band), shown)
				}
			}
		})
	}

	t.Run("an operator filter reads every band", func(t *testing.T) {
		t.Parallel()

		for _, state := range []filterState{{kind: filterOperator, icao: "KLM"}, {kind: filterOther}} {
			scene := &Scene{filter: state}
			if got := scene.corridorBands(); got != allBands {
				t.Errorf("%+v: corridorBands() = %b, want every band", state, got)
			}
		}
	})
}

// midBandFilter is the filter on the middle altitude band, which keeps the
// part of a test track flown between 10,000 and 25,000 ft and drops the rest.
//
//nolint:gochecknoglobals // a fixture, read only.
var midBandFilter = filterState{kind: filterBand, band: bandMid}

// exposedLayerScene is layerScene with one track lost on the frame.
func exposedLayerScene(t *testing.T) (*Scene, *canvas.Canvas, *stubSource) {
	t.Helper()

	scene, canv, src := layerScene(t)
	src.frame.Now = exposureEpoch
	src.frame.Corridors = exposureField
	src.frame.Ghosts = []source.Trail{exposureTrack(source.TrackInbound, 0)}
	src.frame.GhostGeneration = 1

	return scene, canv, src
}

// TestExposureKeysTheLayer checks each thing the exposure keys the layer on
// rebuilds it while the exposure is drawn, and that none of them does while
// it is off.
func TestExposureKeysTheLayer(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		off   bool
		touch func(scene *Scene, src *stubSource)
		want  int
	}{
		{name: "nothing moves", touch: func(*Scene, *stubSource) {}, want: 1},
		{name: "a track is lost", touch: func(_ *Scene, src *stubSource) { src.frame.GhostGeneration++ }, want: 2},
		{name: "seconds pass", touch: func(_ *Scene, src *stubSource) {
			src.frame.Now = src.frame.Now.Add(30 * time.Second)
		}, want: 1},
		{name: "a minute passes", touch: func(_ *Scene, src *stubSource) {
			src.frame.Now = src.frame.Now.Add(time.Minute)
		}, want: 2},
		{name: "f is pressed", touch: func(scene *Scene, _ *stubSource) {
			scene.Handle(input.Key{Kind: input.Rune, Rune: 'f'})
		}, want: 2},
		{name: "u turns the exposure off", touch: func(scene *Scene, _ *stubSource) {
			scene.Handle(input.Key{Kind: input.Rune, Rune: 'u'})
		}, want: 2},
		{name: "u turns the exposure on", off: true, touch: func(scene *Scene, _ *stubSource) {
			scene.Handle(input.Key{Kind: input.Rune, Rune: 'u'})
		}, want: 2},
		{name: "a track is lost with the exposure off", off: true, touch: func(_ *Scene, src *stubSource) {
			src.frame.GhostGeneration++
		}, want: 1},
		{name: "f is pressed with the exposure off", off: true, touch: func(scene *Scene, _ *stubSource) {
			scene.Handle(input.Key{Kind: input.Rune, Rune: 'f'})
		}, want: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, src := exposedLayerScene(t)
			scene.corridors = !testCase.off

			scene.Draw(canv, 0)
			testCase.touch(scene, src)
			scene.Draw(canv, 0)

			if scene.layerRuns != testCase.want {
				t.Errorf("layerRuns = %d, want %d", scene.layerRuns, testCase.want)
			}
		})
	}
}

// exposurePixels counts what trails change on the fixture scene in view
// under filter, against the same scene with the exposure off.
func exposurePixels(t *testing.T, view View, trails []source.Trail, filter filterState) int {
	t.Helper()

	draw := func(corridors Toggle) []byte {
		scene, canv, src := exposedLayerScene(t)
		scene.Apply(Settings{View: view, Shore: ToggleOff, Corridors: corridors})
		scene.setFilter(filter)

		src.frame.Ghosts = trails

		scene.Draw(canv, 0)

		return aboveKeyBar(canv)
	}

	exposed, bare := draw(ToggleOn), draw(ToggleOff)
	count := 0

	for index := range exposed {
		if exposed[index] != bare[index] {
			count++
		}
	}

	return count
}

// TestExposurePathsSkipWhatTheyCannotDraw checks a track of one fix and one
// faded past drawing draw nothing, and that a segment ending at an undecoded
// altitude or in a band the filter hides is left out while the rest of the
// track is drawn. Only the flat views draw hairlines.
func TestExposurePathsSkipWhatTheyCannotDraw(t *testing.T) {
	t.Parallel()

	for _, view := range []View{ViewScope, ViewMinimal} {
		t.Run(string(view), func(t *testing.T) {
			t.Parallel()

			whole := exposureTrack(source.TrackInbound, 0)
			single := whole
			single.Points = whole.Points[:1]
			faint := exposureTrack(source.TrackInbound, 5*time.Hour)

			if got := exposurePixels(t, view, []source.Trail{single, faint}, filterState{}); got != 0 {
				t.Errorf("a one-fix track and a faded one drew %d pixels", got)
			}

			all := exposurePixels(t, view, []source.Trail{whole}, filterState{})
			under := exposurePixels(t, view, []source.Trail{whole}, midBandFilter)

			if all == 0 || under == 0 || under >= all {
				t.Errorf("pixels: every band %d, the mid band %d; want both drawn and the mid band fewer",
					all, under)
			}
		})
	}
}
