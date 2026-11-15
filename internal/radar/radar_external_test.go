package radar_test

import (
	"errors"
	"fmt"
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
	"github.com/hyperized/uScope/pkg/shore"
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
//
// bump moves the ghost generation on every frame, which is a source losing a
// track every frame: the flat views' layer is then rebuilt each time, and a
// benchmark of it prices the rebuild rather than the copy.
type fakeSource struct {
	frame  source.Frame
	closed int
	bump   bool
}

func (f *fakeSource) Frame() source.Frame {
	if f.bump {
		f.frame.GhostGeneration++
	}

	return f.frame
}

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

// benchGrid is the coverage grid the 3D cases draw their measured envelope
// from: every bearing sector heard out to a different distance at every
// altitude band, which is the busiest wireframe a grid can produce and so the
// most expensive one to draw. Every vertex exists, so no edge is skipped.
func benchGrid() source.CoverageGrid {
	// benchFixes is comfortably over the per-bin floor internal/radar draws a
	// vertex at, which is three at the time of writing.
	const benchFixes = 20

	var grid source.CoverageGrid

	for sector := range coverage.BearingSectorCount {
		for band := range coverage.AltitudeBandCount {
			grid.Cells[sector][band][(sector+band)%coverage.DistanceBinCount] = benchFixes
		}
	}

	return grid
}

// benchFrame is the frame the benchmark and the allocation test draw.
func benchFrame() source.Frame {
	return source.Frame{
		Planes: fleet(benchPlanes, benchHistory),
		Grid:   benchGrid(),
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

// A 4K canvas, which draws every fixed size in the scene at twice its
// design size.
const (
	uhdWidth  = 3840
	uhdHeight = 2160
)

// sizedFor swaps the panel-sized canvas for a 3840x2160 one when uhd is set,
// so the allocation test and the benchmark can price the scaled layout with
// the same fixtures as the panel's.
//
//nolint:revive // flag-parameter: uhd picks which of two canvases to hand back, not a mode to branch deeper on.
func sizedFor(tb testing.TB, panel *canvas.Canvas, uhd bool) *canvas.Canvas {
	tb.Helper()

	if !uhd {
		return panel
	}

	canv, err := canvas.New(uhdWidth, uhdHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	return canv
}

// sceneFor is benchScene, or benchSceneWith a frame carrying the bench
// ghosts when ghosts is true and a corridor exposure when exposure asks for
// one. Both the allocation test and the benchmark pick their scene through
// it, so each fixture is built in one place.
//
//nolint:revive // flag-parameter: ghosts picks a fixture to build, not a mode to branch deeper on.
func sceneFor(tb testing.TB, ghosts bool, exposure exposureKind) (*radar.Scene, *canvas.Canvas) {
	tb.Helper()

	if !ghosts && exposure == exposureNone {
		return benchScene(tb)
	}

	frame := benchFrame()

	if ghosts {
		frame.Ghosts = ghostFleet(benchGhosts, benchGhostHistory)
	}

	exposeFrame(tb, &frame, exposure)

	canv, err := canvas.New(panelWidth, panelHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	src := &fakeSource{frame: frame, bump: exposure == exposureRingMoving}

	return radar.New(testFaces(tb), src, scope.New(scope.WithCurrent(60))), canv
}

// drawAllocCase is one row of TestDrawAllocations.
type drawAllocCase struct {
	name     string
	set      radar.Settings
	presses  int
	ghosts   bool
	exposure exposureKind
	wide     bool
	uhd      bool
	reveal   bool

	// coast picks one of coast.go's fixtures instead, drawn at the render
	// clock at; see coastAllocScene.
	coast coastAllocKind
	at    time.Duration
}

// drawAllocCases is TestDrawAllocations' table, kept out of the test so the
// test body stays short enough to read.
func drawAllocCases() []drawAllocCase {
	return append([]drawAllocCase{
		{name: "altitude mode", set: radar.Settings{Colour: radar.ColourAltitude}},
		{name: "airline mode", set: radar.Settings{Colour: radar.ColourAirline}},
		{name: "a coasting fleet", coast: coastAllocCoasting},
		{name: "a coasting fleet in 3D", set: radar.Settings{View: radar.View3D}, coast: coastAllocCoasting},
		{name: "a fleet fading in", coast: coastAllocFadingIn, at: coastAllocAt},
		{name: "the ceiling's dead band", coast: coastAllocDeadBand},
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
		{
			name: "minimal with its bar and shore",
			set: radar.Settings{
				Colour: radar.ColourAltitude, View: radar.ViewMinimal, MinimalBar: true, MinimalShore: true,
			},
		},
		{
			name: "the bare 3D view with its bar at 3840x2160",
			set: radar.Settings{
				Colour: radar.ColourAltitude, View: radar.ViewMinimal3D, MinimalBar: true, MinimalShore: true,
			},
			uhd: true,
		},
		{
			name:   "minimal with its key bar revealed",
			set:    radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal},
			reveal: true,
		},
		{
			name: "the bare 3D view with its key bar revealed at 3840x2160",
			set: radar.Settings{
				Colour: radar.ColourAltitude, View: radar.ViewMinimal3D, MinimalBar: true,
			},
			uhd: true, reveal: true,
		},
		{name: "the scope at 3840x2160", set: radar.Settings{Colour: radar.ColourAltitude}, uhd: true},
		{
			name: "airline mode at 3840x2160",
			set:  radar.Settings{Colour: radar.ColourAirline},
			uhd:  true,
		},
		{
			name: "the 3D view at 3840x2160",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			uhd:  true,
		},
		{
			name: "the bare 3D view at 3840x2160",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D},
			uhd:  true,
		},
	}, append(corridorAllocCases(), boardAllocCases()...)...)
}

// boardAllocCases is the rows of TestDrawAllocations for corridor colouring,
// its filters and the bare 3D view's board. Each carries the demo's corridor
// field, so every strip's DIR and every grouped row goes through Classify
// rather than stopping at an unplaced field.
func boardAllocCases() []drawAllocCase {
	return []drawAllocCase{
		{
			name:     "the bare 3D view with the strip board",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D, Board: true},
			exposure: exposureDemo,
		},
		{
			name: "the bare 3D view with the strip board, filtered, in 4K",
			set: radar.Settings{
				Colour: radar.ColourAltitude, View: radar.ViewMinimal3D, Board: true, Filter: radar.FilterBelowHigh,
			},
			exposure: exposureDemo,
			uhd:      true,
		},
		{name: "corridor mode", set: radar.Settings{Colour: radar.ColourCorridor}, exposure: exposureDemo},
		{
			name:     "corridor mode holding to IN",
			set:      radar.Settings{Colour: radar.ColourCorridor, Filter: radar.FilterIn},
			exposure: exposureDemo,
		},
		{
			name:     "the bare 3D board in corridor mode",
			set:      radar.Settings{Colour: radar.ColourCorridor, View: radar.ViewMinimal3D, Board: true},
			exposure: exposureDemo,
		},
		{
			name:     "the bare 3D board in corridor mode, grouped, in 4K",
			set:      radar.Settings{Colour: radar.ColourCorridor, View: radar.ViewMinimal3D, Board: true},
			exposure: exposureDemo,
			uhd:      true,
		},
		{
			name:     "the 3D view in corridor mode, grouped",
			set:      radar.Settings{Colour: radar.ColourCorridor, View: radar.View3D},
			exposure: exposureDemo,
		},
		{
			name:     "the scope in corridor mode, grouped, holding to OUT",
			set:      radar.Settings{Colour: radar.ColourCorridor, Filter: radar.FilterOut},
			exposure: exposureDemo,
		},
		{
			name:     "the scope with the exposure under the ceiling",
			set:      radar.Settings{Colour: radar.ColourAltitude, Filter: radar.FilterBelowHigh},
			exposure: exposureDemo,
		},
	}
}

// corridorAllocCases is the corridor exposure's rows of TestDrawAllocations'
// table.
func corridorAllocCases() []drawAllocCase {
	return []drawAllocCase{
		{
			name:     "the scope with the exposure",
			set:      radar.Settings{Colour: radar.ColourAltitude},
			exposure: exposureDemo,
		},
		{
			name:     "minimal with the exposure",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal},
			exposure: exposureDemo,
		},
		{
			name:     "the 3D view with the exposure",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			exposure: exposureDemo,
		},
		{
			name:     "the bare 3D view with the exposure",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D},
			exposure: exposureDemo,
		},
		{
			name:     "the bare 3D view with the exposure at 3840x2160",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D},
			exposure: exposureDemo,
			uhd:      true,
		},
		{
			name:     "the scope with the exposure in the mid band",
			set:      radar.Settings{Colour: radar.ColourAltitude, Filter: radar.FilterMid},
			exposure: exposureDemo,
		},
		{
			name:     "the 3D view with the exposure in the mid band",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D, Filter: radar.FilterMid},
			exposure: exposureDemo,
		},
		{
			name:     "the scope rebuilding a full ring every frame",
			set:      radar.Settings{Colour: radar.ColourAltitude},
			exposure: exposureRingMoving,
		},
		{
			name:     "the 3D view with a full ring",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			exposure: exposureRing,
		},
		{
			name:     "the bare 3D view with a full ring",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D},
			exposure: exposureRing,
		},
		{
			name:     "the scope with tubes at the caps",
			set:      radar.Settings{Colour: radar.ColourAltitude},
			exposure: exposureTubeCap,
		},
		{
			name:     "the 3D view with tubes at the caps",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			exposure: exposureTubeCap,
		},
		{
			name:     "the bare 3D view with tubes at the caps",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D},
			exposure: exposureTubeCap,
		},
	}
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
// of them fifteen vertices rotated, projected and filled. All of that runs
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
// The 3840x2160 cases draw the same frames at a scale of two, where every
// glyph, stroke and sprite goes through the scaled paths.
//
// The two reveal cases press a key in a bare view first, so every measured
// frame paints the key bar over the picture. A hidden bar costs one comparison
// and the rest of the cases already price that.
//
// The four coast cases price the seen table: a whole fleet coasting, flat and
// tilted, half a fleet still fading in, and a fleet held inside the band
// filter's dead band.
//
// The corridor cases carry the demo's invented history, a ring of five
// hundred lost tracks of two hundred fixes, or routes at the source's caps.
// The flat ones build the exposure onto the layer in the warm-up frame and
// copy it after, except the one whose source loses a track every frame and so
// rebuilds the layer on each; the tilted ones skin every tube on every frame.
// Two read the tracks through the band filter, which is the per-segment band
// test.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestDrawAllocations(t *testing.T) {
	for _, testCase := range drawAllocCases() {
		t.Run(testCase.name, func(t *testing.T) {
			scene, canv := sceneFor(t, testCase.ghosts, testCase.exposure)
			if testCase.coast != coastAllocNone {
				scene, canv = coastAllocScene(t, testCase.coast, testCase.set)
			} else {
				scene.Apply(testCase.set)
			}

			canv = sizedFor(t, canv, testCase.uhd)
			pressTrails(t, scene, testCase.presses)
			pressWide(t, scene, testCase.wide)
			pressReveal(scene, testCase.reveal)

			// Draw once outside the measurement so the one-time growth of the
			// ICAO index is not counted as a per-frame allocation.
			scene.Draw(canv, testCase.at)

			if got := testing.AllocsPerRun(allocRuns, func() { scene.Draw(canv, testCase.at) }); got != 0 {
				t.Errorf("Draw allocated %.1f times per frame in %s, want 0", got, testCase.name)
			}
		})
	}
}

// allocRuns is how many frames each case of TestDrawAllocations averages
// over. Any allocation at all fails the test, so one frame would do; the
// extra frames catch a scratch that grows every few frames. Fifty was the
// figure until the tube cases at the caps came in, and under the race
// detector those alone took minutes at fifty.
const allocRuns = 8

// drawBenchCase is one row of BenchmarkDraw.
type drawBenchCase struct {
	name     string
	set      radar.Settings
	pal      theme.Palette
	presses  int
	ghosts   bool
	exposure exposureKind
	wide     bool
	uhd      bool
}

// exposureBenchCases is the corridor exposure's rows of BenchmarkDraw.
func exposureBenchCases() []drawBenchCase {
	return []drawBenchCase{
		{
			name:     "exposure",
			set:      radar.Settings{Colour: radar.ColourAltitude},
			pal:      theme.Night,
			exposure: exposureDemo,
		},
		{
			name:     "exposure-rebuild",
			set:      radar.Settings{Colour: radar.ColourAltitude},
			pal:      theme.Night,
			exposure: exposureRingMoving,
		},
		{
			name:     "3d-exposure",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			pal:      theme.Night,
			exposure: exposureDemo,
		},
		{
			name:     "3d-exposure-ring",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			pal:      theme.Night,
			exposure: exposureRing,
		},
		{
			name:     "minimal3d-exposure-ring",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D},
			pal:      theme.Night,
			exposure: exposureRing,
		},
		{
			name:     "minimal3d-exposure",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D},
			pal:      theme.Night,
			exposure: exposureDemo,
		},
		{
			name:     "3d-tube-cap",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			pal:      theme.Night,
			exposure: exposureTubeCap,
		},
		{
			name:     "minimal3d-tube-cap",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D},
			pal:      theme.Night,
			exposure: exposureTubeCap,
		},
	}
}

// BenchmarkDraw measures one whole frame at the panel's resolution, in every
// combination of colour mode and palette: airline mode, corridor mode and the
// paper palette all do more work per aircraft than the defaults, and the numbers together
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
//
// The two 3840x2160 cases are the 4K case: the same frames on a canvas nine
// times the panel's area, drawn at a scale of two.
//
// The exposure cases carry the demo's invented history, its seventy tracks
// mostly bundled into tubes, or a ring of five hundred tracks of two hundred
// fixes, a hundred thousand segments, with no routes. The flat ones draw onto
// a layer that stays warm, so they should cost the copy and nothing more,
// except exposure-rebuild, whose source loses a track every frame: that one
// prices the layer rebuilt with the whole ring on it. The 3D ones skin every
// tube every frame. The tube-cap cases hand the scene sixty-four routes of a
// hundred and sixty stations, the most the source will ever carry.
//
// The board cases are the bare 3D view with its strip board, in altitude
// colouring with DIR and in corridor colouring grouped into sections.
func BenchmarkDraw(b *testing.B) {
	for _, testCase := range append([]drawBenchCase{
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
		{
			name: "altitude/night/3840x2160",
			set:  radar.Settings{Colour: radar.ColourAltitude},
			pal:  theme.Night,
			uhd:  true,
		},
		{
			name: "3d/3840x2160",
			set:  radar.Settings{Colour: radar.ColourAltitude, View: radar.View3D},
			pal:  theme.Night,
			uhd:  true,
		},
		{
			name: "corridor/night", set: radar.Settings{Colour: radar.ColourCorridor},
			pal: theme.Night, exposure: exposureDemo,
		},
		{
			name: "corridor-3d/night", set: radar.Settings{Colour: radar.ColourCorridor, View: radar.View3D},
			pal: theme.Night, exposure: exposureDemo,
		},
		{
			name:     "minimal3d-board/night",
			set:      radar.Settings{Colour: radar.ColourAltitude, View: radar.ViewMinimal3D, Board: true},
			pal:      theme.Night,
			exposure: exposureDemo,
		},
		{
			name:     "minimal3d-board-corridor/night",
			set:      radar.Settings{Colour: radar.ColourCorridor, View: radar.ViewMinimal3D, Board: true},
			pal:      theme.Night,
			exposure: exposureDemo,
		},
	}, exposureBenchCases()...) {
		b.Run(testCase.name, func(b *testing.B) {
			scene, canv := sceneFor(b, testCase.ghosts, testCase.exposure)
			canv = sizedFor(b, canv, testCase.uhd)

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

// BenchmarkDraw3DBare4K prices a 4K frame: the bare 3D view at 3840x2160
// over the demo fleet and the real embedded coastline, with the shore on the
// way --minimal-shore turns it on, which is the hazed ground and the
// coastline faded out across it to the far edge. tilt-10 is a shallow tilt
// and puts the most ground near the horizon; tilt-35 is the default camera,
// where the far side of the ground is still on the picture.
//
// The demo's clock is pinned, so every iteration draws the same frame rather
// than a fleet that moves under the measurement.
func BenchmarkDraw3DBare4K(b *testing.B) {
	set, err := shore.Load()
	if err != nil {
		b.Fatalf("shore.Load: %v", err)
	}

	pinned := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)

	for _, tilt := range []float64{10, 35} {
		b.Run(fmt.Sprintf("tilt-%g", tilt), func(b *testing.B) {
			demo, err := source.NewDemo(
				source.WithDemoLocation(schipholLat, schipholLon),
				source.WithDemoClock(func() time.Time { return pinned }),
			)
			if err != nil {
				b.Fatalf("source.NewDemo: %v", err)
			}

			canv, err := canvas.New(uhdWidth, uhdHeight)
			if err != nil {
				b.Fatalf("canvas.New: %v", err)
			}

			scene := radar.New(testFaces(b), demo, scope.New(), radar.WithShore(set))
			scene.Apply(radar.Settings{View: radar.ViewMinimal3D, MinimalShore: true, Tilt: tilt})
			scene.SetPalette(theme.Night)
			scene.Draw(canv, 0)

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				scene.Draw(canv, 0)
			}
		})
	}
}
