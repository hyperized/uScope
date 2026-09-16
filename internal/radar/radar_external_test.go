package radar_test

import (
	"errors"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/adsb"
	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/coverage"
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

// The benchmark's ghosts: five hundred lost trails of two hundred fixes each.
// That is more contacts than the live fleet above, on purpose: a ghost is
// never freed once its aircraft goes quiet, so a long --no-decay session
// accumulates far more of them than the traffic ever flying at once.
const (
	benchGhosts       = 500
	benchGhostHistory = 200
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

// BiasTee reads the frame the test prepared, so the source and the scene see
// the same bias-tee state.
//
//nolint:nonamedreturns // mirrors the interface it satisfies.
func (f *fakeSource) BiasTee() (supported, enabled bool) {
	return f.frame.BiasTee.Supported, f.frame.BiasTee.Enabled
}

// SetBiasTee must never be reached: the scene hands the flip to a Toggler.
func (*fakeSource) SetBiasTee(bool) error { return errFakeBiasTee }

// errFakeBiasTee marks the call the scene is not allowed to make.
//
//nolint:gochecknoglobals // error sentinel, not state.
var errFakeBiasTee = errors.New("fake source: SetBiasTee must not be called from the scene")

// fakeToggler counts how many times the b key asked it to flip, which is what
// TestOptions uses to prove WithBiasTee actually wires the key to the
// Toggler it was given rather than to the source or to nothing at all.
type fakeToggler struct {
	calls int
}

// Toggle records that it was called. It does no work of its own: the point of
// the interface is that flipping a real bias-tee never happens on the
// goroutine that draws, and a test is the last place that should change.
func (f *fakeToggler) Toggle() { f.calls++ }

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

// ghostFleet builds count lost trails of history fixes each, spread around
// the receiver the same way fleet spreads its aircraft so they land inside
// the scope's range instead of being clipped away: a benchmark of trails the
// projection rejects prices nothing. Altitudes cycle through all three bands
// and callsigns carry a real airline prefix, so altitude mode and the
// airline lookup both have something to work with.
func ghostFleet(count, history int) []source.Trail {
	list := make([]source.Trail, 0, count)

	for index := range count {
		offsetLat := receiverLat + float64(index%11)*0.03 - 0.15
		offsetLon := receiverLon + float64(index%13)*0.02 - 0.12

		points := make([]airplane.PositionEntry, 0, history)
		for fix := range history {
			points = append(points, airplane.PositionEntry{
				Latitude:  offsetLat - float64(history-fix)*0.001,
				Longitude: offsetLon - float64(history-fix)*0.0015,
				Altitude:  float64(index%40) * 1000,
			})
		}

		list = append(list, source.Trail{
			ICAO:     string(rune('A'+index%26)) + "99999",
			Callsign: "KLM" + string(rune('0'+index%10)),
			Altitude: float64(index%40) * 1000,
			Points:   points,
		})
	}

	return list
}

// benchCoverage is the coverage snapshot the 3D cases draw their measured
// envelope from: every altitude band heard out to a different distance in
// every bearing sector, which is the busiest wireframe the tracker can
// produce and so the most expensive one to draw.
func benchCoverage() coverage.Snapshot {
	var snapshot coverage.Snapshot

	for band := range coverage.AltitudeBandCount {
		snapshot.Cells[band][band%coverage.DistanceBinCount] = 1
	}

	for sector := range coverage.BearingSectorCount {
		snapshot.Sectors[sector] = float64(sector%6+1) * coverage.DistanceBinNm
	}

	return snapshot
}

// benchFrame is the frame the benchmark and the allocation test draw.
func benchFrame() source.Frame {
	return source.Frame{
		Planes:   fleet(benchPlanes, benchHistory),
		Coverage: benchCoverage(),
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

// benchSceneWith is benchScene for a caller-built frame, for the cases that
// need more on the scope than the ordinary fleet.
func benchSceneWith(tb testing.TB, frame source.Frame) (*radar.Scene, *canvas.Canvas) {
	tb.Helper()

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	src := &fakeSource{frame: frame}
	scene := radar.New(testFaces(tb), src, scope.New(scope.WithCurrent(60)))

	return scene, canv
}

// sceneFor is benchScene, or benchSceneWith a frame carrying the bench
// ghosts when ghosts is true. Both the allocation test and the benchmark
// pick their scene through it, so the ghosts fixture is built in one place.
//
//nolint:revive // flag-parameter: ghosts picks which of two fixtures to build, not a mode to branch deeper on.
func sceneFor(tb testing.TB, ghosts bool) (*radar.Scene, *canvas.Canvas) {
	tb.Helper()

	if !ghosts {
		return benchScene(tb)
	}

	frame := benchFrame()
	frame.Ghosts = ghostFleet(benchGhosts, benchGhostHistory)

	return benchSceneWith(tb, frame)
}

// TestDrawAllocations is the promise the whole of format.go exists to keep:
// once the first frame has grown the ICAO index, drawing costs nothing on the
// heap, in either colour mode. Airline mode is the one that calls
// airlines.Lookup and adapts a brand colour to the palette on every aircraft,
// so it is not a given that it stays free the way altitude mode is.
//
// The third case is minimal mode following the traffic, which walks the whole
// fleet again for a centroid. The fixture's clock never moves, so what this
// measures is the steady frame between two centrings, which is all but one
// frame in three minutes of them; internal/radar's TestFollowAllocations is
// what prices the other one.
//
// The fifth case is the 3D view, which is the one that draws its furniture
// straight into the frame rather than copying a cached layer under it: the
// rings, the coastline, the bowl and the measured envelope are all projected
// again on every frame, and none of that may reach the heap.
//
// The fourth case is the all mode carrying five hundred ghosts. A ghost is
// never freed once its aircraft goes quiet, so it is the one thing on the
// scope that grows without bound over a long session; this is the case that
// would catch a per-ghost allocation in drawGhost or ghostColour if one crept
// in.
//
// The two model cases are the aircraft shapes. Every scope frame draws one in
// the row table's attitude cell and every 3D frame draws one per contact, each
// of them fourteen vertices rotated, projected and filled. All of that runs
// through fixed arrays on the Scene, and this is what says so: a model that
// reached for a slice would show up here as one allocation per aircraft per
// frame.
//
// The last three are the views and the column the w key hides. The bare 3D
// view walks the fleet again for a centroid and projects the traffic through
// the same camera; the two wide cases draw into a sub-canvas half again as
// large, which is where a per-frame canvas allocation would show up if the
// window were ever rebuilt on a frame that had not resized.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestDrawAllocations(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		set     radar.Settings
		presses int
		ghosts  bool
		wide    bool
	}{
		{name: "altitude mode", set: radar.Settings{Colour: radar.ColourAltitude}},
		{name: "airline mode", set: radar.Settings{Colour: radar.ColourAirline}},
		{
			name: "minimal mode following the traffic",
			set: radar.Settings{
				Colour: radar.ColourAltitude, View: radar.ViewMinimal, Recentre: radar.DefaultRecentre,
			},
		},
		{
			name:    "the all mode with ghosts",
			set:     radar.Settings{Colour: radar.ColourAltitude},
			presses: pressAll,
			ghosts:  true,
		},
		{
			name: "the 3D view",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
		},
		{
			name:    "the 3D view with the short trails",
			set:     radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			presses: pressShort,
		},
		{
			name:    "the 3D view with no trails at all",
			set:     radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			presses: pressOff,
		},
		{
			name: "the bare 3D view following the traffic",
			set: radar.Settings{
				Colour: radar.ColourAltitude, View: radar.ViewMinimal3D, Recentre: radar.DefaultRecentre,
			},
		},
		{name: "the scope with the column hidden", set: radar.Settings{Colour: radar.ColourAltitude}, wide: true},
		{
			name: "the 3D view with the column hidden",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			wide: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			scene, canv := sceneFor(t, testCase.ghosts)
			scene.Apply(testCase.set)
			pressTrails(t, scene, testCase.presses)
			pressWide(t, scene, testCase.wide)

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
// both do more work per aircraft than the defaults, and the numbers together
// are what the benchmark is for. Minimal mode following the traffic has no
// furniture to draw and one more pass over the fleet.
//
// The ghosts case adds five hundred ghosts at two hundred fixes each to the
// ordinary bench frame, drawn in the all mode because that is the only mode a
// ghost is ever drawn in at all. A ghost is kept for as long as the program
// runs rather than for as long as its aircraft does, so it is the one thing
// on the scope that accumulates without bound over a long session: this is
// the case that would show a per-ghost cost if drawGhost or ghostColour ever
// grew one.
//
// The 3D cases are where the aircraft models are priced. 3d draws forty of
// them over their trails; 3d-models-only turns the trails off, so the
// difference between the two is what a frame's worth of models costs on its
// own. The flat cases price the fortieth of that the row table's attitude
// cells come to.
//
// The last three price the views the four-way cycle added and the wider box
// the w key gives the picture. Against their narrow twins, the two wide cases
// are what a scope with the whole frame to itself costs.
func BenchmarkDraw(b *testing.B) {
	for _, testCase := range []struct {
		name    string
		set     radar.Settings
		pal     theme.Palette
		presses int
		ghosts  bool
		wide    bool
	}{
		{name: "altitude/night", set: radar.Settings{Colour: radar.ColourAltitude}, pal: theme.Night},
		{name: "altitude/day", set: radar.Settings{Colour: radar.ColourAltitude}, pal: theme.Day},
		{name: "airline/night", set: radar.Settings{Colour: radar.ColourAirline}, pal: theme.Night},
		{name: "airline/day", set: radar.Settings{Colour: radar.ColourAirline}, pal: theme.Day},
		{
			name: "minimal-following/night",
			set: radar.Settings{
				Colour: radar.ColourAltitude, View: radar.ViewMinimal, Recentre: radar.DefaultRecentre,
			},
			pal: theme.Night,
		},
		{
			name:    "ghosts",
			set:     radar.Settings{Colour: radar.ColourAltitude},
			pal:     theme.Night,
			presses: pressAll,
			ghosts:  true,
		},
		{name: "3d", set: radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D}, pal: theme.Night},
		{
			name:    "3d-models-only",
			set:     radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			pal:     theme.Night,
			presses: pressOff,
		},
		{
			name: "minimal3d-following/night",
			set: radar.Settings{
				Colour: radar.ColourAltitude, View: radar.ViewMinimal3D, Recentre: radar.DefaultRecentre,
			},
			pal: theme.Night,
		},
		{name: "wide/night", set: radar.Settings{Colour: radar.ColourAltitude}, pal: theme.Night, wide: true},
		{
			name: "3d-wide/night",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			pal:  theme.Night,
			wide: true,
		},
	} {
		b.Run(testCase.name, func(b *testing.B) {
			scene, canv := sceneFor(b, testCase.ghosts)
			scene.Apply(testCase.set)
			scene.SetPalette(testCase.pal)
			pressTrails(b, scene, testCase.presses)
			pressWide(b, scene, testCase.wide)

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				scene.Draw(canv, 0)
			}
		})
	}
}

// TestParseRecentre checks the --recenter allow list. Zero is a value rather
// than a refusal: it is how the flag says "stay on the receiver", which is
// what minimal mode did before the cadence existed.
func TestParseRecentre(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		in   string
		want time.Duration
	}{
		{name: "zero turns it off", in: "0", want: 0},
		{name: "the floor", in: "10s", want: radar.MinRecentre},
		{name: "the default", in: "3m", want: radar.DefaultRecentre},
		{name: "the ceiling", in: "1h", want: radar.MaxRecentre},
		{name: "a compound duration", in: "1m30s", want: 90 * time.Second},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := radar.ParseRecentre(testCase.in)
			if err != nil {
				t.Fatalf("ParseRecentre(%q) error = %v, want nil", testCase.in, err)
			}

			if got != testCase.want {
				t.Errorf("ParseRecentre(%q) = %v, want %v", testCase.in, got, testCase.want)
			}
		})
	}

	for _, testCase := range []struct {
		name string
		in   string
	}{
		{name: "under the floor", in: "9s"},
		{name: "over the ceiling", in: "2h"},
		{name: "negative", in: "-1m"},
		{name: "not a duration", in: "forever"},
		{name: "a bare number with no unit", in: "180"},
		{name: "empty", in: ""},
	} {
		t.Run(testCase.name+" is refused", func(t *testing.T) {
			t.Parallel()

			if _, err := radar.ParseRecentre(testCase.in); !errors.Is(err, radar.ErrRecentre) {
				t.Errorf("ParseRecentre(%q) error = %v, want ErrRecentre", testCase.in, err)
			}
		})
	}
}
