package radar_test

import (
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/adsb"
	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/fonts"
	"github.com/hyperized/uScope/pkg/psf"
)

// The canvas the panel runs at, which is what the layout is designed against.
const (
	panelWidth  = 1280
	panelHeight = 720
)

// The benchmark fleet: forty aircraft with two hundred fixes of history each,
// which is a busier scope than a real receiver in the Netherlands sees.
const (
	benchPlanes  = 40
	benchHistory = 200
)

// receiverLat and receiverLon are Schiphol, which is where the demo fleet
// flies and where these numbers were checked against a real scope.
const (
	receiverLat = 52.3105
	receiverLon = 4.7683
)

// fakeSource hands back one prepared frame for ever, so a test measures the
// scene and not the thing feeding it.
type fakeSource struct {
	frame  source.Frame
	closed int
}

func (f *fakeSource) Frame() source.Frame { return f.frame }

func (f *fakeSource) Close() error {
	f.closed++

	return nil
}

// testFaces loads the four embedded faces. A radar drawn with synthetic fonts
// would not exercise the fallback glyph or the real metrics the layout is
// built on.
func testFaces(tb testing.TB) radar.Faces {
	tb.Helper()

	load := func(name string, loader func() (*psf.Font, error)) *psf.Font {
		face, err := loader()
		if err != nil {
			tb.Fatalf("loading %s: %v", name, err)
		}

		return face
	}

	return radar.Faces{
		Small:    load("small", fonts.Small),
		Body:     load("body", fonts.Body),
		BodyBold: load("bold", fonts.BodyBold),
		Large:    load("large", fonts.Large),
	}
}

// fleet builds count aircraft spread around the receiver, each with history
// fixes of trail behind it.
func fleet(count, history int) airplanes.List {
	list := make(airplanes.List, 0, count)

	for index := range count {
		offsetLat := receiverLat + float64(index%7)*0.05 - 0.15
		offsetLon := receiverLon + float64(index%5)*0.07 - 0.14

		trail := make([]airplane.PositionEntry, 0, history)
		for fix := range history {
			trail = append(trail, airplane.PositionEntry{
				Latitude:  offsetLat - float64(history-fix)*0.002,
				Longitude: offsetLon - float64(history-fix)*0.003,
				Altitude:  float64(index) * 1000,
			})
		}

		list = append(list, airplane.Snapshot{
			ICAO:            string(rune('A'+index%26)) + "12345",
			Callsign:        "KLM" + string(rune('0'+index%10)),
			Altitude:        float64(index) * 1000,
			Heading:         float64(index*9) + 1,
			Velocity:        200 + float64(index)*5,
			Latitude:        offsetLat,
			Longitude:       offsetLon,
			Squawk:          "1000",
			MessageCount:    int64(index),
			PositionHistory: trail,
		})
	}

	return list
}

// benchFrame is the frame the benchmark and the allocation test draw.
func benchFrame() source.Frame {
	return source.Frame{
		Planes: fleet(benchPlanes, benchHistory),
		Receiver: source.Receiver{
			Latitude:  receiverLat,
			Longitude: receiverLon,
			HasFix:    true,
			Label:     source.LabelManual,
			Mode:      source.FixManual,
		},
		Source: adsb.SourceInfo{Label: "DEMO", Connected: true},
		Stats:  adsb.Stats{TotalFrames: 4096},
		Now:    time.Date(2026, time.September, 15, 20, 57, 0, 0, time.UTC),
	}
}

// benchScene builds a scene and the canvas it draws on.
func benchScene(tb testing.TB) (*radar.Scene, *canvas.Canvas) {
	tb.Helper()

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	src := &fakeSource{frame: benchFrame()}
	scene := radar.New(testFaces(tb), src, scope.New(scope.WithCurrent(60)))

	return scene, canv
}

// TestDrawAllocations is the promise the whole of format.go exists to keep:
// once the first frame has grown the ICAO index, drawing costs nothing on the
// heap, in either colour mode. Airline mode is the one that calls
// airlines.Lookup and adapts a brand colour to the palette on every aircraft,
// so it is not a given that it stays free the way altitude mode is.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestDrawAllocations(t *testing.T) {
	for _, testCase := range []struct {
		name string
		mode radar.ColourMode
	}{
		{name: "altitude mode", mode: radar.ColourAltitude},
		{name: "airline mode", mode: radar.ColourAirline},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			scene, canv := benchScene(t)
			scene.Apply(radar.Settings{Colour: testCase.mode})

			// Draw once outside the measurement so the one-time growth of the
			// ICAO index is not counted as a per-frame allocation.
			scene.Draw(canv, 0)

			if got := testing.AllocsPerRun(50, func() { scene.Draw(canv, 0) }); got != 0 {
				t.Errorf("Draw allocated %.1f times per frame in %s, want 0", got, testCase.name)
			}
		})
	}
}

// BenchmarkDraw measures one whole frame at the panel's resolution, in every
// combination of colour mode and palette: airline mode and the paper palette
// both do more work per aircraft than the defaults, and the four numbers
// together are what the benchmark is for.
func BenchmarkDraw(b *testing.B) {
	for _, testCase := range []struct {
		name string
		mode radar.ColourMode
		pal  theme.Palette
	}{
		{name: "altitude/night", mode: radar.ColourAltitude, pal: theme.Night},
		{name: "altitude/paper", mode: radar.ColourAltitude, pal: theme.Paper},
		{name: "airline/night", mode: radar.ColourAirline, pal: theme.Night},
		{name: "airline/paper", mode: radar.ColourAirline, pal: theme.Paper},
	} {
		b.Run(testCase.name, func(b *testing.B) {
			scene, canv := benchScene(b)
			scene.Apply(radar.Settings{Colour: testCase.mode})
			scene.SetPalette(testCase.pal)

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				scene.Draw(canv, 0)
			}
		})
	}
}
