package radar

import (
	"image"
	"image/color"
	"math"
	"slices"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/theme"
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
		{name: "east of the coast is land", lon: syntheticLandLon, want: scene.landInk()},
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

// TestTiltedGroundRunsPastTheReach checks where each tilted view's ground
// is at full strength and where it has started to fade.
//
// A point one and a half range radii out is past the full view's outer ring
// and inside the bare view's two radii. The bare view has it at the full tint;
// the full view has it in the haze, which is neither the full tint nor the
// bare field. Before the haze the full view left it bare.
func TestTiltedGroundRunsPastTheReach(t *testing.T) {
	t.Parallel()

	// Far enough out to be past the full view's reach and well inside the
	// bare view's, measured north so the probe stays in front of the camera.
	const pastTheRingLat = layerBaseLat + 1.5*waterProbeRangeNm/nmPerDegree

	for _, testCase := range []struct {
		name     string
		view     View
		wantFull bool
	}{
		{name: "the full view fades past the outer ring", view: View3D},
		{name: "the bare view is at full tint two range radii out", view: ViewMinimal3D, wantFull: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, view := water3DScene(t, testCase.view,
				WithShore(syntheticWaterSet(t, nil, nil)))
			scene.drawShore3(canv, view)

			at := pixelAt3(t, view, pastTheRingLat, layerBaseLon)
			got := canv.Image().RGBAAt(at.X, at.Y)

			if testCase.wantFull {
				if got != scene.waterInk() {
					t.Errorf("the pixel one and a half range radii out is %v, want the full tint %v",
						got, scene.waterInk())
				}

				return
			}

			if !isHazeBand(scene, got) {
				t.Errorf("the pixel one and a half range radii out is %v, want one of the haze bands", got)
			}
		})
	}
}

// isHazeBand reports whether a colour is the tint of one of the haze bands.
func isHazeBand(scene *Scene, col color.RGBA) bool {
	for band := 1; band <= hazeBands; band++ {
		if col == scene.hazeInk(scene.waterInk(), band) {
			return true
		}
	}

	return false
}

// isLandBand reports whether col is one of the land's haze bands, the way
// isHazeBand answers for the water's.
func isLandBand(scene *Scene, col color.RGBA) bool {
	for band := 1; band <= hazeBands; band++ {
		if col == scene.hazeInk(scene.landInk(), band) {
			return true
		}
	}

	return false
}

// TestHazeFadesIntoTheField walks the ground straight ahead of the camera
// from the receiver out past the far edge of the haze, and reads the tint off
// the picture at every step.
//
// It has to start at the full tint, never get stronger on the way out, and
// end at the field, which is the whole of the claim the haze makes: the
// ground does not stop, it fades. The camera sits due south at azimuth zero,
// so due north of the receiver is straight ahead of it.
func TestHazeFadesIntoTheField(t *testing.T) {
	t.Parallel()

	const steps = 400

	for _, testCase := range []struct {
		name  string
		shown View
	}{
		{name: "3d", shown: View3D},
		{name: "minimal3d", shown: ViewMinimal3D},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, _ := water3DScene(t, testCase.shown, WithShore(syntheticWaterSet(t, nil, nil)))
			scene.elevation = minElevation
			view := remeasure3D(t, scene, canv)
			scene.drawShore3(canv, view)

			haze := view.haze()
			beyond := haze.far - haze.foot.north + 1

			last := scene.waterInk()
			seen := 0

			for step := range steps + 1 {
				north := beyond * float64(step) / steps

				//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
				x, y, ok := view.cam.at(point3{north: north})
				if !ok || !image.Pt(x, y).In(canv.Bounds()) {
					continue
				}

				got := canv.Image().RGBAAt(x, y)
				if brightness(got) > brightness(last) {
					t.Fatalf("%v NM north the ground is %v, brighter than %v nearer in", north, got, last)
				}

				last = got
				seen++
			}

			if seen < steps/2 {
				t.Fatalf("only %d of %d steps landed on the picture, so the walk proves nothing", seen, steps)
			}

			if last != scene.pal.Field {
				t.Errorf("past the haze the ground is %v, want the field %v", last, scene.pal.Field)
			}
		})
	}
}

// remeasure3D measures the view again after a test has moved the camera.
func remeasure3D(tb testing.TB, scene *Scene, canv *canvas.Canvas) scene3 {
	tb.Helper()

	view, drawable := scene.measure3D(stalkLayout(scene, canv), layerFrame(layerBaseLat), 0)
	if !drawable {
		tb.Fatal("measure3D found no room for the tilted view")
	}

	return view
}

// brightness is the sum of a colour's channels, which is enough to order the
// tints of one fade: every band is the same colour mixed into the field by a
// smaller share.
func brightness(col color.RGBA) int { return int(col.R) + int(col.G) + int(col.B) }

// TestHazeKeepsLandBeyondTheReach is the reason the land is queried out to the
// far edge of the haze: a country past the reach has to stay land rather than
// turn into sea. The ring is a square two and a half range radii north of the
// receiver, which is past the full view's reach and in its haze.
func TestHazeKeepsLandBeyondTheReach(t *testing.T) {
	t.Parallel()

	const (
		farLandSouth = layerBaseLat + 2.0*waterProbeRangeNm/nmPerDegree
		farLandNorth = layerBaseLat + 3.0*waterProbeRangeNm/nmPerDegree
		farLandSpan  = 2.0
		probeLat     = (farLandSouth + farLandNorth) / 2
	)

	ring := shore.Polyline{
		{Lat: farLandSouth, Lon: layerBaseLon - farLandSpan},
		{Lat: farLandSouth, Lon: layerBaseLon + farLandSpan},
		{Lat: farLandNorth, Lon: layerBaseLon + farLandSpan},
		{Lat: farLandNorth, Lon: layerBaseLon - farLandSpan},
	}

	scene, canv, view := water3DScene(t, View3D, WithShore(syntheticWaterSet(t, nil, []shore.Polyline{ring})))
	scene.drawShore3(canv, view)

	land := pixelAt3(t, view, probeLat, layerBaseLon)
	if got := canv.Image().RGBAAt(land.X, land.Y); !isLandBand(scene, got) {
		t.Errorf("inside the far land the ground is %v, want one of the land's haze bands", got)
	}

	sea := pixelAt3(t, view, farLandSouth-(farLandNorth-farLandSouth)/2, layerBaseLon)
	if got := canv.Image().RGBAAt(sea.X, sea.Y); !isHazeBand(scene, got) {
		t.Errorf("short of the far land the ground is %v, want one of the haze bands", got)
	}
}

// TestHazeGeometry pins the haze's measurements: the bands run from the full
// tint's edge to the far edge without going back on themselves, the far edge
// is hazeDepth times the near one until maxHazeNm caps it, and the first and
// last band sit a step inside the two colours the fade runs between.
func TestHazeGeometry(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		near    float64
		wantFar float64
	}{
		{name: "a short reach fades over hazeDepth of itself", near: 100, wantFar: 100 * hazeDepth},
		{name: "a long one is capped", near: 1000, wantFar: 1000 + maxHazeNm},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			view := scene3{reachNm: testCase.near, cam: camera3{eye: point3{up: 10}}}
			haze := view.haze()

			if math.Abs(haze.far-testCase.wantFar) > 1e-9 {
				t.Errorf("far = %g, want %g", haze.far, testCase.wantFar)
			}

			if haze.edge(0) != haze.near || math.Abs(haze.edge(hazeBands)-haze.far) > 1e-9 {
				t.Errorf("the bands run %g to %g, want %g to %g",
					haze.edge(0), haze.edge(hazeBands), haze.near, haze.far)
			}

			for band := 1; band <= hazeBands; band++ {
				if haze.edge(band) <= haze.edge(band-1) {
					t.Errorf("band %d ends at %g, not past where it starts at %g",
						band, haze.edge(band), haze.edge(band-1))
				}
			}
		})
	}

	scene := &Scene{pal: theme.Night}

	// At thirty-two bands the first step is under half a level of the night
	// tint, so it may round onto the full tint; it may never pass it, and it
	// must still be ground rather than field.
	data := scene.waterInk()

	if first := scene.hazeInk(data, 1); brightness(first) > brightness(scene.waterInk()) || first == scene.pal.Field {
		t.Errorf("the first band is %v, want no brighter than the full tint %v", first, scene.waterInk())
	}

	if last := scene.hazeInk(data, hazeBands); brightness(last) > brightness(scene.hazeInk(data, hazeBands-1)) {
		t.Errorf("the last band %v is brighter than the one inside it", last)
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

// The stretch of the 5 degree meridian TestTiltedFillHasNoSeamOnTheCellBoundary
// walks, across the Netherlands and the sea north of it, in steps fine enough
// to visit every row it crosses.
const (
	seamLon   = 5.0
	seamSouth = 51.0
	seamNorth = 54.0
	seamSteps = 4000

	// seamReach is how many columns either side of the meridian are read.
	// The projected boundary sits within half a pixel of the point probed;
	// two leaves room for both pieces' edges and a pixel past them.
	seamReach = 2
)

// TestTiltedFillHasNoSeamOnTheCellBoundary walks the 5 degree meridian
// through the receiver's own country in both tilted views and looks for the
// dashed line that used to run down it.
//
// The meridian is a boundary between two of the land file's cells, so the
// fill there is two pieces of one ring meeting edge to edge. Where the pieces
// walked the boundary in different steps, the two edges rounded onto
// different columns on alternate rows and left a single pixel of the other
// colour between them. With only the fill on the canvas, a pixel that differs
// from both its neighbours while they agree with each other cannot be
// anything else.
//
// The real embedded land is used on purpose: the seam came from the shape of
// the shipped data, and a synthetic ring cut by today's clipper would pass
// whether or not the file had been rebuilt with it.
func TestTiltedFillHasNoSeamOnTheCellBoundary(t *testing.T) {
	t.Parallel()

	// Fewer rows than this and the walk missed the picture, so a clean result
	// would mean nothing.
	const minRowsRead = 100

	set, err := shore.Load()
	if err != nil {
		t.Fatalf("shore.Load: %v", err)
	}

	for _, testCase := range []struct {
		name  string
		shown View
	}{
		{name: "3d", shown: View3D},
		{name: "minimal3d", shown: ViewMinimal3D},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, view := water3DScene(t, testCase.shown, WithShore(set))

			scene.fillGround3(canv, view, view.haze())

			if rows := checkSeamRows(t, canv.Image(), view); rows < minRowsRead {
				t.Fatalf("the meridian crossed %d rows of the picture, want at least %d", rows, minRowsRead)
			}
		})
	}
}

// checkSeamRows reports every lone pixel on the rows the meridian crosses and
// returns how many rows it read.
func checkSeamRows(tb testing.TB, img *image.RGBA, view scene3) int {
	tb.Helper()

	box := img.Bounds().Inset(seamReach + 1)
	rows := make(map[int]bool)

	for step := range seamSteps + 1 {
		lat := seamSouth + (seamNorth-seamSouth)*float64(step)/seamSteps

		//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
		x, y, ok := view.cam.at(view.ground(lat, seamLon))
		if !ok || !image.Pt(x, y).In(box) || rows[y] {
			continue
		}

		rows[y] = true

		for column := x - seamReach; column <= x+seamReach; column++ {
			left, here, right := img.RGBAAt(column-1, y), img.RGBAAt(column, y), img.RGBAAt(column+1, y)
			if left == right && here != left {
				tb.Errorf("pixel (%d, %d) at %.4f N on the meridian is %v between two of %v",
					column, y, lat, here, left)
			}
		}
	}

	return len(rows)
}

// TestHazePast checks the far cull against a haze around a foot due south of
// the origin: a ring is past the haze only when the nearest corner or edge of
// its box is, whichever side of the foot it lies on.
func TestHazePast(t *testing.T) {
	t.Parallel()

	haze := haze3{foot: point3{north: -100}, near: 200, far: 800}

	for _, testCase := range []struct {
		name      string
		low, high point3
		want      bool
	}{
		{name: "a box around the foot", low: point3{east: -1, north: -101}, high: point3{east: 1, north: -99}},
		{name: "a box just inside the far edge", low: point3{north: 690}, high: point3{east: 10, north: 750}},
		{
			name: "a box out beyond it to the north",
			low:  point3{north: 701}, high: point3{east: 10, north: 750},
			want: true,
		},
		{
			name: "a box out beyond it to the south",
			low:  point3{north: -1000}, high: point3{north: -901},
			want: true,
		},
		{
			name: "a box out beyond it to the east",
			low:  point3{east: 801, north: -100}, high: point3{east: 900},
			want: true,
		},
		{
			name: "a box out beyond it to the west",
			low:  point3{east: -900, north: -100}, high: point3{east: -801},
			want: true,
		},
		{
			name: "a box off the diagonal past it though each axis alone is not",
			low:  point3{east: 600, north: 500}, high: point3{east: 700, north: 600},
			want: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := haze.past(boxedLine3{low: testCase.low, high: testCase.high}); got != testCase.want {
				t.Errorf("past(%+v to %+v) = %v, want %v", testCase.low, testCase.high, got, testCase.want)
			}
		})
	}
}
