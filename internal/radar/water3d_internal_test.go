package radar

import (
	"image"
	"image/color"
	"math"
	"slices"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// The synthetic ring the near-plane cases are read against: a rectangle around
// the receiver whose southern edge is far enough away to pass behind a camera
// that orbits a couple of range radii out.
//
// Six degrees is about 360 nautical miles, and the camera at the range these
// tests draw sees a little over 140 ahead of the origin, so the southern half
// of the ring is behind the lens and the northern half is not. It is handed to
// collectLand3 as one ring rather than through a Set, because a Set would cut
// it into five degree cells first and the piece that straddles the lens would
// no longer be the piece the receiver stands on.
const (
	deepLandSouth = 47.0
	deepLandNorth = 53.0
	deepLandSpan  = 1.0
)

// deepLandRing is that rectangle, walked the way a land ring arrives: closed
// without its first point repeated.
func deepLandRing() shore.Polyline {
	return shore.Polyline{
		{Lat: deepLandSouth, Lon: layerBaseLon - deepLandSpan},
		{Lat: deepLandSouth, Lon: layerBaseLon + deepLandSpan},
		{Lat: deepLandNorth, Lon: layerBaseLon + deepLandSpan},
		{Lat: deepLandNorth, Lon: layerBaseLon - deepLandSpan},
	}
}

// water3DScene builds a scene already in one of the two tilted views, the
// canvas it draws on cleared to the field, and the 3D view measured the way
// draw3D measures it.
//
// The camera is measured through stalkLayout, which carves the frame exactly
// as Draw does, so a probe projected through the returned view lands on the
// pixel the picture was drawn at.
func water3DScene(tb testing.TB, shown View, opts ...Option) (*Scene, *canvas.Canvas, scene3) {
	tb.Helper()

	canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	frame := layerFrame(layerBaseLat)
	scene := New(layerTestFaces(tb), &stubSource{frame: frame},
		scope.New(scope.WithCurrent(waterProbeRangeNm)), opts...)
	scene.shown = shown
	scene.minimalShore = true

	view, drawable := scene.measure3D(stalkLayout(scene, canv), frame, 0)
	if !drawable {
		tb.Fatal("measure3D found no room for the tilted view at the panel's own size")
	}

	canv.Clear(scene.pal.Field)

	return scene, canv, view
}

// pixelAt3 is where a position landed on the canvas, or a failure when the
// camera could not place it. A probe the camera cannot see says nothing about
// the fill, so it is a fatal rather than a skip.
func pixelAt3(tb testing.TB, view scene3, latitude, longitude float64) image.Point {
	tb.Helper()

	//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
	x, y, ok := view.cam.at(view.ground(latitude, longitude))
	if !ok {
		tb.Fatalf("the camera could not place %v N %v E, so there is nothing to probe", latitude, longitude)
	}

	return image.Pt(x, y)
}

// TestWaterFill3DDrawOrder reads the three cases off the tilted view: the sea
// is flooded, the land takes it back, and a ring inside a ring comes out as
// water because one even-odd pass counts it twice.
//
// It is TestWaterFillDrawOrder's subject in perspective, over the same
// synthetic geography, and it is the test that would fail if the projected
// ground came out inside out or if the land rings were painted before the
// water rather than after it.
func TestWaterFill3DDrawOrder(t *testing.T) {
	t.Parallel()

	set := syntheticWaterSet(t,
		[]shore.Polyline{syntheticCoastline()},
		[]shore.Polyline{syntheticLand(), syntheticLake()},
	)

	scene, canv, view := water3DScene(t, View3D, WithShore(set))
	scene.drawShore3(canv, view)

	for _, testCase := range []struct {
		name string
		lon  float64
		want color.RGBA
	}{
		{name: "west of the coast is water", lon: syntheticSeaLon, want: scene.waterInk()},
		{name: "east of the coast is land", lon: syntheticLandLon, want: scene.pal.Field},
		{name: "inside the lake is water again", lon: syntheticInLakeLon, want: scene.waterInk()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			at := pixelAt3(t, view, syntheticProbeLat, testCase.lon)

			if got := canv.Image().RGBAAt(at.X, at.Y); got != testCase.want {
				t.Errorf("the pixel at %v N %v E is %v, want %v",
					syntheticProbeLat, testCase.lon, got, testCase.want)
			}
		})
	}
}

// TestBare3DGroundIsTheWholeReach checks that the bare view floods the two
// range radii it draws to rather than the range itself.
//
// The two views share every line of the fill and differ only in reachNm, so
// this is the one thing worth reading off the picture: a point one and a half
// range radii out is bare field in the full view, where the ground stops at
// the outer ring, and water in the bare one.
func TestBare3DGroundIsTheWholeReach(t *testing.T) {
	t.Parallel()

	// Far enough out to be past the full view's ground and well inside the
	// bare view's, measured north so the probe stays in front of the camera.
	const pastTheRingLat = layerBaseLat + 1.5*waterProbeRangeNm/nmPerDegree

	for _, testCase := range []struct {
		name      string
		view      View
		wantWater bool
	}{
		{name: "the full view stops at the outer ring", view: View3D},
		{name: "the bare view reaches two range radii", view: ViewMinimal3D, wantWater: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, view := water3DScene(t, testCase.view,
				WithShore(syntheticWaterSet(t, nil, nil)))
			scene.drawShore3(canv, view)

			want := scene.pal.Field
			if testCase.wantWater {
				want = scene.waterInk()
			}

			at := pixelAt3(t, view, pastTheRingLat, layerBaseLon)

			if got := canv.Image().RGBAAt(at.X, at.Y); got != want {
				t.Errorf("the pixel one and a half range radii out is %v, want %v", got, want)
			}
		})
	}
}

// TestCollectLand3ClipsAtTheNearPlane is the case the tilted fill exists to
// get right and the flat one never meets.
//
// A land ring arrives cut to a five degree cell, three hundred nautical miles
// across, and the camera orbits a couple of range radii out, so part of such a
// ring is routinely behind the lens. Dropping the ring there, the way a
// coastline segment with an end off the picture is dropped, would take the
// country out of the fill. Cutting it leaves the two corners the camera can
// see plus the two points where the ring crosses the plane it sees past, which
// is the four this counts.
func TestCollectLand3ClipsAtTheNearPlane(t *testing.T) {
	t.Parallel()

	scene, canv, view := water3DScene(t, View3D)
	scene.collectLand3(view, canv.Bounds(), deepLandRing())

	if len(scene.landSpans) != 1 {
		t.Fatalf("collectLand3 recorded %d rings, want the straddling one kept", len(scene.landSpans))
	}

	const wantPoints = 4

	span := scene.landSpans[0]
	if got := span.end - span.start; got != wantPoints {
		t.Fatalf("the clipped ring has %d points, want %d: two corners in front and two on the near plane",
			got, wantPoints)
	}

	// The corners the camera can see have to come through untouched. Only the
	// pair behind the lens is replaced.
	for _, corner := range []float64{layerBaseLon + deepLandSpan, layerBaseLon - deepLandSpan} {
		want := view.cam.pixel(view.ground(deepLandNorth, corner))

		if !slices.Contains(scene.landPoints[span.start:span.end], want) {
			t.Errorf("the corner at %v N %v E projected to %v, which the clipped ring does not carry",
				deepLandNorth, corner, want)
		}
	}
}

// TestCollectLand3Rejects covers the four ways a ring is thrown away before it
// reaches the fill: too few points to enclose anything, every point landing on
// the same pixel, a ring that lands nowhere near the picture, and one entirely
// behind the camera.
//
// The last is the tilted view's own. The first three are collectLand's, and
// they are read again here because the perspective path walks the ring through
// its own clip before it reaches the same two tests.
func TestCollectLand3Rejects(t *testing.T) {
	t.Parallel()

	// Far enough east that the ring lands tens of thousands of pixels off the
	// canvas while staying in front of the camera, and far enough south that
	// the whole of it is behind the lens.
	const (
		wayEastDeg  = 60.0
		waySouthDeg = 20.0
	)

	for _, testCase := range []struct {
		name string
		line shore.Polyline
	}{
		{
			name: "a ring of two points encloses nothing",
			line: shore.Polyline{{Lat: layerBaseLat, Lon: layerBaseLon}, {Lat: layerBaseLat + 0.1, Lon: layerBaseLon}},
		},
		{
			name: "a ring inside one pixel collapses to nothing",
			line: shore.Polyline{
				{Lat: layerBaseLat, Lon: layerBaseLon},
				{Lat: layerBaseLat + 1e-9, Lon: layerBaseLon},
				{Lat: layerBaseLat, Lon: layerBaseLon + 1e-9},
			},
		},
		{
			name: "a ring off the side of the picture misses it",
			line: shore.Polyline{
				{Lat: layerBaseLat, Lon: layerBaseLon + wayEastDeg},
				{Lat: layerBaseLat + 1, Lon: layerBaseLon + wayEastDeg},
				{Lat: layerBaseLat + 1, Lon: layerBaseLon + wayEastDeg + 1},
			},
		},
		{
			name: "a ring behind the camera is cut away entirely",
			line: shore.Polyline{
				{Lat: layerBaseLat - waySouthDeg, Lon: layerBaseLon},
				{Lat: layerBaseLat - waySouthDeg, Lon: layerBaseLon + 1},
				{Lat: layerBaseLat - waySouthDeg - 1, Lon: layerBaseLon + 1},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, view := water3DScene(t, View3D)

			scene.collectLand3(view, canv.Bounds(), testCase.line)

			if len(scene.landSpans) != 0 {
				t.Errorf("collectLand3 recorded %d rings, want none", len(scene.landSpans))
			}

			if len(scene.landPoints) != 0 {
				t.Errorf("collectLand3 left %d points behind, want none", len(scene.landPoints))
			}
		})
	}
}

// TestCrossNearLandsOnThePlane checks the one number the clip has to get
// right: the point it inserts sits on the plane the camera sees past, not
// somewhere near it. A crossing that fell short would put a vertex behind the
// lens and the projection would mirror it back into the picture.
func TestCrossNearLandsOnThePlane(t *testing.T) {
	t.Parallel()

	_, _, view := water3DScene(t, View3D)

	behind := view.groundPointAt(layerBaseLat-10, layerBaseLon)
	front := view.groundPointAt(layerBaseLat, layerBaseLon)

	if behind.inFront() {
		t.Fatalf("the southern point has depth %g, want it behind the lens", behind.depth)
	}

	if !front.inFront() {
		t.Fatalf("the receiver has depth %g, want it in front of the lens", front.depth)
	}

	if got := view.cam.depth(crossNear(behind, front)); math.Abs(got-minDepth) > 1e-9 {
		t.Errorf("crossNear landed at depth %g, want %g", got, minDepth)
	}
}

// TestCameraPixelAnswersWhereAtRefuses pins the difference between the two
// projections.
//
// at guards against a point far outside the box, because drawing a line to one
// is a million rejected Set calls. A fill cannot take that answer: dropping one
// vertex of a closed ring moves the edges either side of it and opens the
// shape, so pixel answers wherever the point lands and the scanline fill
// discards the rows and columns that miss its clip rectangle.
func TestCameraPixelAnswersWhereAtRefuses(t *testing.T) {
	t.Parallel()

	_, canv, view := water3DScene(t, View3D)

	// Far enough off the view axis to clear guardBoxes while staying well in
	// front of the camera.
	const wayEastNm = 3000.0

	far := point3{east: wayEastNm}

	if _, _, ok := view.cam.at(far); ok {
		t.Fatal("at placed a point thousands of miles off the axis, want it refused by the guard")
	}

	if got := view.cam.pixel(far); got.In(canv.Bounds()) {
		t.Errorf("pixel put the same point at %v, which is on the canvas: want the unguarded answer", got)
	}
}
