package radar

import (
	"image"
	"image/color"
	"slices"
	"testing"

	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// TestCacheLines3WalksOnlyWhenItMust checks each reason a shore cache walks
// the shore set again, and that it does not walk when none of them holds, for
// the land cache and the coastline cache alike.
//
// A walk is read off radiusNm, which is set to the radius asked for times
// landSlack on every walk and left alone otherwise.
func TestCacheLines3WalksOnlyWhenItMust(t *testing.T) {
	t.Parallel()

	for _, kind := range []struct {
		name  string
		cache func(scene *Scene) *lineCache3
		kind  shoreKind
	}{
		{name: "land", cache: func(scene *Scene) *lineCache3 { return &scene.land3 }, kind: landRings},
		{name: "coast", cache: func(scene *Scene) *lineCache3 { return &scene.coast3 }, kind: coastLines},
	} {
		t.Run(kind.name, func(t *testing.T) {
			t.Parallel()

			checkCacheWalks(t, kind.cache, kind.kind)
		})
	}
}

// checkCacheWalks is TestCacheLines3WalksOnlyWhenItMust for one cache.
func checkCacheWalks(t *testing.T, cacheOf func(scene *Scene) *lineCache3, kind shoreKind) {
	t.Helper()

	const need = 400.0

	moved := geo{lat: layerBaseLat + 1, lon: layerBaseLon}

	for _, testCase := range []struct {
		name     string
		change   func(scene *Scene, view *scene3) float64
		wantWalk bool
	}{
		{
			name:   "the same frame again keeps the cache",
			change: func(*Scene, *scene3) float64 { return need },
		},
		{
			name:   "a little less keeps it",
			change: func(*Scene, *scene3) float64 { return need * 0.9 },
		},
		{
			name:     "further than it reaches walks again",
			change:   func(*Scene, *scene3) float64 { return need * landSlack * 1.01 },
			wantWalk: true,
		},
		{
			name:     "much less walks again",
			change:   func(*Scene, *scene3) float64 { return need / landSlack / landSlack },
			wantWalk: true,
		},
		{
			name: "a new origin walks again",
			change: func(_ *Scene, view *scene3) float64 {
				view.origin = moved

				return need
			},
			wantWalk: true,
		},
		{
			name: "a new shore set walks again",
			change: func(scene *Scene, _ *scene3) float64 {
				scene.shoreSet = syntheticWaterSet(t, nil, nil)

				return need
			},
			wantWalk: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, _, view := water3DScene(t, ViewMinimal3D, WithShore(syntheticWaterSet(t, nil, nil)))
			cache := cacheOf(scene)
			scene.cacheLines3(cache, kind, view, need)

			// A sentinel radius no walk would ever set, so a walk shows.
			const sentinel = need * landSlack * 1.001

			cache.radiusNm = sentinel

			again := testCase.change(scene, &view)
			scene.cacheLines3(cache, kind, view, again)

			if walked := cache.radiusNm != sentinel; walked != testCase.wantWalk {
				t.Errorf("walked = %v, want %v", walked, testCase.wantWalk)
			}
		})
	}
}

// TestCacheLines3BoxesEveryLine checks that both caches hold the lines around
// the origin with a box on the floor that holds every point of each.
func TestCacheLines3BoxesEveryLine(t *testing.T) {
	t.Parallel()

	set := syntheticWaterSet(t, []shore.Polyline{syntheticCoastline()},
		[]shore.Polyline{syntheticLand(), syntheticLake()})

	scene, _, view := water3DScene(t, ViewMinimal3D, WithShore(set))
	scene.cacheLines3(&scene.land3, landRings, view, waterProbeRangeNm)
	scene.cacheLines3(&scene.coast3, coastLines, view, waterProbeRangeNm)

	if len(scene.land3.lines) == 0 || len(scene.coast3.lines) == 0 {
		t.Fatalf("the caches hold %d rings and %d coastlines around the synthetic land, want some of each",
			len(scene.land3.lines), len(scene.coast3.lines))
	}

	for _, ring := range slices.Concat(scene.land3.lines, scene.coast3.lines) {
		for _, point := range ring.line {
			at := view.ground(point.Lat, point.Lon)
			if at.east < ring.low.east || at.east > ring.high.east ||
				at.north < ring.low.north || at.north > ring.high.north {
				t.Errorf("%v lands at %+v, outside its ring's box %+v to %+v", point, at, ring.low, ring.high)
			}
		}
	}
}

// TestFrustumMisses reads the cull against the camera water3DScene measures,
// which sits due south of the receiver looking north.
func TestFrustumMisses(t *testing.T) {
	t.Parallel()

	_, canv, view := water3DScene(t, View3D)
	sight := view.cam.frustum(canv.Bounds())

	for _, testCase := range []struct {
		name      string
		low, high point3
		want      bool
	}{
		{
			name: "the ground around the receiver is seen",
			low:  point3{east: -1, north: -1}, high: point3{east: 1, north: 1},
		},
		{
			name: "a box straddling the camera is kept",
			low:  point3{east: -1, north: -1000}, high: point3{east: 1, north: 1000},
		},
		{
			name: "a box behind the camera is not",
			low:  point3{east: -1, north: -2000}, high: point3{east: 1, north: -1900},
			want: true,
		},
		{
			name: "a box far to the east is not",
			low:  point3{east: 5000, north: 0}, high: point3{east: 5100, north: 10},
			want: true,
		},
		{
			name: "a box far to the west is not",
			low:  point3{east: -5100, north: 0}, high: point3{east: -5000, north: 10},
			want: true,
		},
		{
			name: "ground between the camera and the bottom of the picture is not",
			low:  point3{east: -1, north: view.cam.eye.north + 0.5},
			high: point3{east: 1, north: view.cam.eye.north + 1},
			want: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := sight.misses(view.cam, testCase.low, testCase.high); got != testCase.want {
				t.Errorf("misses(%+v, %+v) = %v, want %v", testCase.low, testCase.high, got, testCase.want)
			}
		})
	}
}

// TestFrustumTopPlane covers the plane along the top of the picture, which
// no box on the floor ever lands outside at a tilt the camera allows: a box
// ten thousand miles up is outside it and the ground around the receiver is
// not.
func TestFrustumTopPlane(t *testing.T) {
	t.Parallel()

	const (
		topPlane = 3
		skyNm    = 10000.0
	)

	_, canv, view := water3DScene(t, View3D)
	top := view.cam.frustum(canv.Bounds())[topPlane]

	corners := func(up float64) *[4]lensPoint {
		return &[4]lensPoint{
			view.cam.lens(point3{east: -1, north: -1, up: up}),
			view.cam.lens(point3{east: 1, north: -1, up: up}),
			view.cam.lens(point3{east: -1, north: 1, up: up}),
			view.cam.lens(point3{east: 1, north: 1, up: up}),
		}
	}

	if !top.outsideAll(corners(skyNm)) {
		t.Error("a box ten thousand miles up is inside the top of the picture")
	}

	if top.outsideAll(corners(0)) {
		t.Error("the ground around the receiver is outside the top of the picture")
	}
}

// TestGround3Allocations is the per-frame promise for the tilted ground with
// the real coastline on it, outlines and all: once the scratch and both caches
// have grown, a frame allocates nothing, and neither does a frame that moves
// the origin and walks the shore set again, which is what the bare view's
// recentring does.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestGround3Allocations(t *testing.T) {
	set, err := shore.Load()
	if err != nil {
		t.Fatalf("shore.Load: %v", err)
	}

	scene, canv, view := water3DScene(t, ViewMinimal3D, WithShore(set))
	other := view
	other.origin.lat += 0.5

	scene.drawShore3(canv, view)
	scene.drawShore3(canv, other)

	steady := testing.AllocsPerRun(50, func() { scene.drawShore3(canv, view) })
	if steady != 0 {
		t.Errorf("a steady tilted ground allocated %.1f times per frame, want 0", steady)
	}

	flip := false
	moving := testing.AllocsPerRun(50, func() {
		flip = !flip
		if flip {
			scene.drawShore3(canv, other)
		} else {
			scene.drawShore3(canv, view)
		}
	})

	if moving != 0 {
		t.Errorf("a tilted ground that walks the shore set again allocated %.1f times per frame, want 0", moving)
	}
}

// wideLandDeg is how far a ring of land reaches from the probe receiver in
// every direction. Twenty degrees is past the haze's far edge at the probe
// range, so the ring covers the whole tilted picture.
const wideLandDeg = 20

// wideLand is a square of land centred on the probe receiver and much larger
// than anything the scope shows.
func wideLand() shore.Polyline {
	return shore.Polyline{
		{Lat: layerBaseLat - wideLandDeg, Lon: layerBaseLon - wideLandDeg},
		{Lat: layerBaseLat - wideLandDeg, Lon: layerBaseLon + wideLandDeg},
		{Lat: layerBaseLat + wideLandDeg, Lon: layerBaseLon + wideLandDeg},
		{Lat: layerBaseLat + wideLandDeg, Lon: layerBaseLon - wideLandDeg},
	}
}

// TestLandPastTheHazeIsTheHorizon walks the ground straight ahead of the
// camera across one wide ring of land under a dusk sky, from the receiver to
// a few miles past the haze's far edge. Under the camera the land is the full
// tint and past the far edge it is the sky's horizon colour exactly, the
// colour the clear leaves where nothing is filled, so land never shows as a
// band against the sky.
//
// The walk stops just past the far edge rather than well beyond it because a
// cell of land wholly past the haze is never filled at all: the clear already
// painted it that colour. The cell this one ends in straddles the edge, so
// its far part goes through the fill, and the canvas starts red so a pixel
// the fill missed cannot pass for the horizon.
func TestLandPastTheHazeIsTheHorizon(t *testing.T) {
	t.Parallel()

	const (
		steps       = 400
		pastFarEdge = 5
	)

	scene, _ := skyScene(t, -3, theme.Night)
	scene.shoreSet = syntheticWaterSet(t, nil, []shore.Polyline{wideLand()})
	scene.minimalShore = true

	canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(canvas.Red)

	view := remeasure3D(t, scene, canv)
	scene.drawShore3(canv, view)

	horizon := scene.skyHorizon()
	if horizon == scene.pal.Field {
		t.Fatal("the dusk sky's horizon is the field, so the test cannot tell land past the haze from none")
	}

	haze := view.haze()
	end := haze.foot.north + haze.far + pastFarEdge
	first, last := color.RGBA{}, color.RGBA{}

	for step := range steps + 1 {
		//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
		x, y, ok := view.cam.at(point3{north: end * float64(step) / steps})
		if !ok || !image.Pt(x, y).In(canv.Bounds()) {
			continue
		}

		if first == (color.RGBA{}) {
			first = canv.Image().RGBAAt(x, y)
		}

		last = canv.Image().RGBAAt(x, y)
	}

	if first != scene.landInk() {
		t.Errorf("under the camera the land is %v, want the full land tint %v", first, scene.landInk())
	}

	if last != horizon {
		t.Errorf("past the haze the land is %v, want the horizon %v", last, horizon)
	}
}
