package radar_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// airwayCase is one picture the airway tests draw: the flat scope at
// 1280x720, or the stream's bare 3D view at 2560x1440 with the shore, the
// airfields, the bar and the board at range 100, tilt 20, azimuth 143.
type airwayCase struct {
	name     string
	stream   bool
	palette  theme.Palette
	settings radar.Settings
}

// airwaySettings is the case's settings with the airways and the corridors
// set as asked.
func (c airwayCase) airwaySettings(airways bool, corridors radar.Toggle) radar.Settings {
	set := c.settings
	set.Airways, set.Corridors = airways, corridors

	if c.stream {
		set.View = radar.ViewMinimal3D
		set.MinimalShore, set.MinimalAirports, set.MinimalBar, set.Board = true, true, true, true
		set.RangeNm, set.Tilt, set.Azimuth, set.Orbit = 100, 20, 143, radar.ToggleOff
	}

	return set
}

//nolint:gochecknoglobals // read-only test fixture.
var airwayCases = []airwayCase{
	{name: "flat-night", palette: theme.Night},
	{name: "flat-day", palette: theme.Day},
	{name: "stream-night", stream: true, palette: theme.VectorNight},
	{name: "stream-day", stream: true, palette: theme.VectorDay},
	{name: "3d-night", palette: theme.Night, settings: radar.Settings{View: radar.View3D, Orbit: radar.ToggleOff}},
}

// airwayScene draws the case once over the demo with its invented airway
// traffic and hands back the scene and the canvas for the next frame.
func airwayScene(tb testing.TB, testCase airwayCase, set radar.Settings) (*radar.Scene, *canvas.Canvas) {
	tb.Helper()

	coast, err := shore.Load()
	if err != nil {
		tb.Fatalf("shore.Load: %v", err)
	}

	pinned := time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC)

	demo, err := source.NewDemo(
		source.WithDemoLocation(schipholLat, schipholLon),
		source.WithDemoClock(func() time.Time { return pinned }),
		source.WithDemoAirways(),
	)
	if err != nil {
		tb.Fatalf("source.NewDemo: %v", err)
	}

	width, height := 1280, 720
	if testCase.stream {
		width, height = 2560, 1440
	}

	canv, err := canvas.New(width, height)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	scene := radar.New(testFaces(tb), demo, scope.New(), radar.WithShore(coast))
	scene.Apply(set)
	scene.SetPalette(testCase.palette)
	scene.Draw(canv, 0)

	return scene, canv
}

// airwayPixels draws the case with the airways and the corridors as asked.
func airwayPixels(t *testing.T, testCase airwayCase, airways bool, corridors radar.Toggle) []byte {
	t.Helper()

	_, canv := airwayScene(t, testCase, testCase.airwaySettings(airways, corridors))

	return canv.Image().Pix
}

// TestAirwaysDrawn checks the setting reaches the picture in every view and
// palette, and that the zero Settings leaves it off. It does not call
// t.Parallel: the demo is built and drawn at the stream's size five times
// over, and the suite's other parallel scenes already fill the machine.
//
//nolint:paralleltest // see above.
func TestAirwaysDrawn(t *testing.T) {
	for _, testCase := range airwayCases {
		t.Run(testCase.name, func(t *testing.T) {
			off := airwayPixels(t, testCase, false, radar.ToggleOn)
			on := airwayPixels(t, testCase, true, radar.ToggleOn)

			if bytes.Equal(off, on) {
				t.Error("--airways on draws the same picture as off")
			}

			_, plain := airwayScene(t, testCase, testCase.settings)
			if testCase.stream {
				_, plain = airwayScene(t, testCase, testCase.airwaySettings(false, ""))
			}

			if !bytes.Equal(off, plain.Image().Pix) {
				t.Error("the zero Settings draws airways; the default is off")
			}
		})
	}
}

// TestAirwaysNeedCorridors checks the airways are part of the exposure: with
// the corridors off, turning them on changes nothing.
//
//nolint:paralleltest // see TestAirwaysDrawn.
func TestAirwaysNeedCorridors(t *testing.T) {
	for _, testCase := range airwayCases[:3] {
		t.Run(testCase.name, func(t *testing.T) {
			off := airwayPixels(t, testCase, false, radar.ToggleOff)
			on := airwayPixels(t, testCase, true, radar.ToggleOff)

			if !bytes.Equal(off, on) {
				t.Error("airways drawn with the corridors off")
			}
		})
	}
}

// TestAirwaysRebuildTheLayer checks a scene switched on after its first
// frame shows the airways on the next one, which it only does when the
// setting is in the background layer's key.
//
//nolint:paralleltest // see TestAirwaysDrawn.
func TestAirwaysRebuildTheLayer(t *testing.T) {
	testCase := airwayCases[0]
	want := airwayPixels(t, testCase, true, radar.ToggleOn)

	scene, canv := airwayScene(t, testCase, testCase.airwaySettings(false, radar.ToggleOn))
	scene.Apply(testCase.airwaySettings(true, radar.ToggleOn))
	scene.Draw(canv, 0)

	if !bytes.Equal(canv.Image().Pix, want) {
		t.Error("switching the airways on kept the old background layer")
	}
}

// TestAirwaysDrawAllocatesNothing does not call t.Parallel: testing.AllocsPerRun
// panics when it is.
//
//nolint:paralleltest // see above.
func TestAirwaysDrawAllocatesNothing(t *testing.T) {
	for _, testCase := range airwayCases {
		scene, canv := airwayScene(t, testCase, testCase.airwaySettings(true, radar.ToggleOn))

		if allocs := testing.AllocsPerRun(5, func() { scene.Draw(canv, 0) }); allocs != 0 {
			t.Errorf("%s: Draw allocates %.0f times a frame, want 0", testCase.name, allocs)
		}
	}
}

func BenchmarkDrawAirways(b *testing.B) {
	for _, testCase := range []airwayCase{airwayCases[0], airwayCases[2]} {
		for _, airways := range []bool{false, true} {
			name := testCase.name + "/off"
			if airways {
				name = testCase.name + "/on"
			}

			b.Run(name, func(b *testing.B) {
				scene, canv := airwayScene(b, testCase, testCase.airwaySettings(airways, radar.ToggleOn))

				b.ReportAllocs()
				b.ResetTimer()

				for b.Loop() {
					scene.Draw(canv, 0)
				}
			})
		}
	}
}
