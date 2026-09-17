package radar

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// The three places the draw order is read off the real data. They are picked
// to sit well clear of any coastline, so the probe lands on the fill rather
// than on the outline drawn over it, and well inside the range the tests draw
// at.
const (
	// northSeaLat and northSeaLon are about forty kilometres off the Dutch
	// coast, which is open water at every scale Natural Earth carries.
	northSeaLat = 52.30
	northSeaLon = 3.80

	// utrechtLat and utrechtLon are a city in the middle of the country, far
	// enough from both the coast and the IJsselmeer to be nothing but land.
	utrechtLat = 52.09
	utrechtLon = 5.12

	// ijsselmeerLat and ijsselmeerLon sit in the open middle of the
	// IJsselmeer. It is the case the land polygons alone get wrong: Natural
	// Earth's land layer does not cut its lakes out, so a fill that ignored
	// the lakes file would paint this pixel as land.
	ijsselmeerLat = 52.75
	ijsselmeerLon = 5.35

	// waterProbeRangeNm is the range the probes are read at. Sixty nautical
	// miles puts all three inside the scope with room to spare.
	waterProbeRangeNm = 60
)

// TestWaterInk pins the tint each palette fills the sea with.
//
// The numbers are written out rather than computed with the same fade the
// code uses, because a test that recomputed the answer would agree with any
// mistake the implementation made. What they are checking is that eight per
// cent towards Data really does land where the look wants it: cyan-black on
// Glass night, a deeper green on Phosphor, a lighter grey on Mono where there
// is no hue to borrow, and a faint blue-grey on each of the three day pages.
func TestWaterInk(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		pal  theme.Palette
		want color.RGBA
	}{
		{name: "glass night is cyan-black", pal: theme.Night, want: rgba(5, 18, 20)},
		{name: "glass day is a faint blue-grey", pal: theme.Day, want: rgba(220, 232, 238)},
		{name: "phosphor night is a deeper green", pal: theme.PhosphorNight, want: rgba(17, 33, 24)},
		{name: "phosphor day is a deeper green on paper", pal: theme.PhosphorDay, want: rgba(222, 233, 222)},
		{name: "mono night is a lighter grey", pal: theme.MonoNight, want: rgba(29, 29, 31)},
		{name: "mono day is a darker grey on paper", pal: theme.MonoDay, want: rgba(225, 222, 216)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{pal: testCase.pal}

			if got := scene.waterInk(); got != testCase.want {
				t.Errorf("waterInk() on %s = %v, want %v", testCase.pal.Look, got, testCase.want)
			}
		})
	}
}

// TestWaterTintStandsOffTheField checks the one property the fade was chosen
// for and a number cannot be eyeballed from: the tint has to differ from the
// field in every palette, or the fill says nothing, and it has to stay closer
// to the field than the shore colour is, or the fill shouts over the outline
// it is there to explain.
func TestWaterTintStandsOffTheField(t *testing.T) {
	t.Parallel()

	for _, pal := range []theme.Palette{
		theme.Night, theme.Day, theme.PhosphorNight, theme.PhosphorDay, theme.MonoNight, theme.MonoDay,
	} {
		t.Run(string(pal.Look)+"/"+fieldKind(pal), func(t *testing.T) {
			t.Parallel()

			scene := &Scene{pal: pal}
			water := scene.waterInk()

			if water == pal.Field {
				t.Fatalf("waterInk() = %v, the same as the field, so the sea would be invisible", water)
			}

			if channelGap(water, pal.Field) >= channelGap(pal.Shore, pal.Field) {
				t.Errorf("waterInk() is %d off the field and the shore colour is %d, want the tint to be the quieter",
					channelGap(water, pal.Field), channelGap(pal.Shore, pal.Field))
			}
		})
	}
}

// TestWaterFillDrawOrderOnRealData reads the three places the fill has to get
// right straight off the embedded data.
//
// This is the one test that goes through shore.Load rather than a set built by
// hand. What it is proving is the whole chain at once: that the land file
// really holds the Dutch coast, that the lakes really came through as holes in
// it, and that the three fills land in the order water, land, lake. A
// synthetic set can prove the drawing; only the real one can prove the data.
func TestWaterFillDrawOrderOnRealData(t *testing.T) {
	t.Parallel()

	set, err := shore.Load()
	if err != nil {
		t.Fatalf("shore.Load: %v", err)
	}

	scene, canv, frame := waterScene(t, WithShore(set))
	scene.renderLayer(canv, scene.layerKeyFor(canv, frame), frame)

	// The picture is on the layer, which is what renderLayer draws into and
	// what Draw copies under every frame. Reading it there rather than after a
	// Draw keeps the probes off any aircraft the frame might carry.
	layer := scene.layer
	view := scopeViewOf(t, scene, layer, frame)

	for _, testCase := range []struct {
		name string
		lat  float64
		lon  float64
		want color.RGBA
	}{
		{name: "the North Sea is water", lat: northSeaLat, lon: northSeaLon, want: scene.waterInk()},
		{name: "Utrecht is land", lat: utrechtLat, lon: utrechtLon, want: scene.pal.Field},
		{name: "the IJsselmeer is water", lat: ijsselmeerLat, lon: ijsselmeerLon, want: scene.waterInk()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			at := pixelOf(view.proj.offset(testCase.lat, testCase.lon))

			if got := layer.Image().RGBAAt(at.X, at.Y); got != testCase.want {
				t.Errorf("the pixel at %v N %v E is %v, want %v",
					testCase.lat, testCase.lon, got, testCase.want)
			}
		})
	}
}

// The synthetic geography the draw-order probes below are read against: sea
// west of a meridian a tenth of a degree east of the receiver, land east of
// it, and a lake in the middle of that land.
//
// The land runs across the cell boundary at five degrees east on purpose, so
// the fill is reading two pieces cut from one ring and the probe east of that
// boundary is proof they met.
const (
	syntheticCoastLon = layerBaseLon + 0.1
	syntheticLandEast = 6.6
	syntheticLandLow  = 51.0
	syntheticLandHigh = 53.6

	syntheticLakeLow   = 52.2
	syntheticLakeHigh  = 52.45
	syntheticLakeWest  = 5.3
	syntheticLakeEast  = 5.6
	syntheticProbeLat  = 52.31
	syntheticSeaLon    = 4.0
	syntheticLandLon   = 5.0
	syntheticInLakeLon = 5.45
)

// TestWaterFillDrawOrder reads the three cases off a set built by hand, where
// the answer is known from the geometry rather than from a map.
//
// The real-data test above proves the file holds the right world. This one
// proves the drawing: the sea is flooded, the land takes it back, and a ring
// inside a ring comes out as water because one even-odd pass counts it twice.
func TestWaterFillDrawOrder(t *testing.T) {
	t.Parallel()

	set := syntheticWaterSet(t,
		[]shore.Polyline{syntheticCoastline()},
		[]shore.Polyline{syntheticLand(), syntheticLake()},
	)

	scene, canv, frame := waterScene(t, WithShore(set))
	scene.renderLayer(canv, scene.layerKeyFor(canv, frame), frame)

	layer := scene.layer
	view := scopeViewOf(t, scene, layer, frame)

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

			at := pixelOf(view.proj.offset(syntheticProbeLat, testCase.lon))

			if got := layer.Image().RGBAAt(at.X, at.Y); got != testCase.want {
				t.Errorf("the pixel at %v N %v E is %v, want %v",
					syntheticProbeLat, testCase.lon, got, testCase.want)
			}
		})
	}
}

// TestWaterStaysInsideTheRing checks that the flood is the range ring's disc
// and not the square around it. The scope shares its canvas with the flight
// strips, so a tint that ran to the corners of the scope box would put sea
// under the column.
func TestWaterStaysInsideTheRing(t *testing.T) {
	t.Parallel()

	set := syntheticWaterSet(t, nil, []shore.Polyline{syntheticLand()})

	scene, canv, frame := waterScene(t, WithShore(set))
	scene.renderLayer(canv, scene.layerKeyFor(canv, frame), frame)

	layer := scene.layer
	view := scopeViewOf(t, scene, layer, frame)

	// A pixel on the diagonal, just past the ring. The corner of the disc's
	// own bounding square is the furthest the fill could have reached.
	corner := view.geom.rangeR + 2
	at := image.Pt(view.geom.centerX+corner, view.geom.centerY-corner)

	if got := layer.Image().RGBAAt(at.X, at.Y); got != scene.pal.Field {
		t.Errorf("the pixel %d past the range ring is %v, want the field %v", corner-view.geom.rangeR,
			got, scene.pal.Field)
	}
}

// TestBareViewFloodsTheWholeCanvas checks the other half of floodWater. The
// scope view has a ring to flood inside and the bare view has none, so there
// the ground is the canvas: a corner, which no disc centred on the middle
// would ever reach, has to come out as sea.
func TestBareViewFloodsTheWholeCanvas(t *testing.T) {
	t.Parallel()

	set, err := shore.Load()
	if err != nil {
		t.Fatalf("shore.Load: %v", err)
	}

	scene, canv, frame := waterScene(t, WithShore(set))
	scene.shown = ViewMinimal
	scene.minimalShore = true

	scene.renderLayer(canv, scene.layerKeyFor(canv, frame), frame)

	// The bottom-left corner of the canvas. At this range it is a few hundred
	// miles out into the Atlantic, which is sea in the data and outside any
	// disc in the geometry.
	if got := scene.layer.Image().RGBAAt(0, layerCanvasHeight-1); got != scene.waterInk() {
		t.Errorf("the bare view's corner pixel is %v, want the water tint %v", got, scene.waterInk())
	}
}

// TestCollectLandRejects covers the three ways a ring is thrown away before it
// reaches the fill: too few points to enclose anything, every point landing on
// the same pixel so there is nothing left after the collapse, and a ring that
// misses the picture altogether.
//
// The last is the one worth the test. Five degree cells are a couple of
// thousand pixels across at the narrow ranges, so most of what LandWithin
// hands over is nowhere near the scope, and dropping it is only safe because a
// closed ring crosses any row an even number of times.
func TestCollectLandRejects(t *testing.T) {
	t.Parallel()

	scene, canv, frame := waterScene(t)
	view := scopeViewOf(t, scene, canv, frame)
	box := canv.Bounds()

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
			name: "a ring on the far side of the world misses the picture",
			line: shore.Polyline{{Lat: -40, Lon: 174}, {Lat: -41, Lon: 174}, {Lat: -41, Lon: 175}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fresh, _, _ := waterScene(t)

			fresh.collectLand(view.proj, box, testCase.line)

			if len(fresh.landSpans) != 0 {
				t.Errorf("collectLand recorded %d rings, want none", len(fresh.landSpans))
			}

			if len(fresh.landPoints) != 0 {
				t.Errorf("collectLand left %d points behind, want none", len(fresh.landPoints))
			}
		})
	}
}

// TestCollectLandCollapsesRepeatedPixels checks the only thinning the fill
// does. Natural Earth carries points a few hundred metres apart, which at any
// range uScope draws is several to a pixel, and a ring that kept them all
// would hand the sweep a pile of edges that cannot move a span.
func TestCollectLandCollapsesRepeatedPixels(t *testing.T) {
	t.Parallel()

	scene, canv, frame := waterScene(t)
	view := scopeViewOf(t, scene, canv, frame)

	// Four corners of a square a tenth of a degree across, each one written
	// twice over with a step far under a pixel between the copies.
	const (
		side = 0.1
		hair = 1e-9
	)

	corners := [][2]float64{{0, 0}, {side, 0}, {side, side}, {0, side}}

	line := make(shore.Polyline, 0, 2*len(corners))
	for _, corner := range corners {
		line = append(line,
			shore.Point{Lat: layerBaseLat + corner[0], Lon: layerBaseLon + corner[1]},
			shore.Point{Lat: layerBaseLat + corner[0] + hair, Lon: layerBaseLon + corner[1] + hair},
		)
	}

	scene.collectLand(view.proj, canv.Bounds(), line)

	if len(scene.landSpans) != 1 {
		t.Fatalf("collectLand recorded %d rings, want 1", len(scene.landSpans))
	}

	if got := len(scene.landPoints); got != len(line)/2 {
		t.Errorf("collectLand kept %d points out of %d, want the %d distinct pixels",
			got, len(line), len(line)/2)
	}
}

// TestGrowSeedsFromEmpty checks the one thing about grow that is not obvious
// from reading it: an empty rectangle is the seed rather than a sentinel, and
// image.Rectangle.Union treats an empty one as nothing at all, so the first
// point sets the box instead of stretching it back to the origin.
func TestGrowSeedsFromEmpty(t *testing.T) {
	t.Parallel()

	first := grow(image.Rectangle{}, image.Pt(10, 20))
	if want := image.Rect(10, 20, 11, 21); first != want {
		t.Fatalf("grow(empty, (10, 20)) = %v, want %v", first, want)
	}

	if got, want := grow(first, image.Pt(4, 30)), image.Rect(4, 20, 11, 31); got != want {
		t.Errorf("grow(%v, (4, 30)) = %v, want %v", first, got, want)
	}
}

// syntheticWaterSet builds a *shore.Set holding both halves by hand, by
// encoding each and decoding the pair straight back. shore.Set has no
// exported constructor, so this round trip is the only way a test makes one.
func syntheticWaterSet(tb testing.TB, outlines, rings []shore.Polyline) *shore.Set {
	tb.Helper()

	var lines, land bytes.Buffer

	if err := shore.Encode(&lines, outlines); err != nil {
		tb.Fatalf("shore.Encode: %v", err)
	}

	if err := shore.EncodeLand(&land, rings); err != nil {
		tb.Fatalf("shore.EncodeLand: %v", err)
	}

	set, err := shore.DecodeWithLand(&lines, &land)
	if err != nil {
		tb.Fatalf("shore.DecodeWithLand: %v", err)
	}

	return set
}

// syntheticLand is the rectangle of land east of the synthetic coast.
func syntheticLand() shore.Polyline {
	return shore.Polyline{
		{Lat: syntheticLandLow, Lon: syntheticCoastLon},
		{Lat: syntheticLandLow, Lon: syntheticLandEast},
		{Lat: syntheticLandHigh, Lon: syntheticLandEast},
		{Lat: syntheticLandHigh, Lon: syntheticCoastLon},
	}
}

// syntheticLake is the rectangle of water inside it.
func syntheticLake() shore.Polyline {
	return shore.Polyline{
		{Lat: syntheticLakeLow, Lon: syntheticLakeWest},
		{Lat: syntheticLakeLow, Lon: syntheticLakeEast},
		{Lat: syntheticLakeHigh, Lon: syntheticLakeEast},
		{Lat: syntheticLakeHigh, Lon: syntheticLakeWest},
	}
}

// syntheticCoastline is the land's western edge as an open line, which is what
// the outline half of the data would carry for it.
func syntheticCoastline() shore.Polyline {
	return shore.Polyline{
		{Lat: syntheticLandLow, Lon: syntheticCoastLon},
		{Lat: syntheticLandHigh, Lon: syntheticCoastLon},
	}
}

// waterScene builds the scene, canvas and frame the tests above draw with: the
// panel's own size, a receiver over the Netherlands and a range that puts all
// three probe positions on the scope.
func waterScene(tb testing.TB, opts ...Option) (*Scene, *canvas.Canvas, source.Frame) {
	tb.Helper()

	canv, err := canvas.New(layerCanvasWidth, layerCanvasHeight)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	frame := layerFrame(layerBaseLat)
	scene := New(layerTestFaces(tb), &stubSource{frame: frame},
		scope.New(scope.WithCurrent(waterProbeRangeNm)), opts...)

	return scene, canv, frame
}

// scopeViewOf measures the scope the way renderLayer does, so a test can ask
// the projection where a position landed on the layer it just drew.
//
// It repeats renderLayer's carving rather than reading a field, because
// nothing holds the answer: the layer measures the frame afresh on every
// rebuild, which is what keeps it to the pixel with the traffic drawn on top.
func scopeViewOf(tb testing.TB, scene *Scene, dst *canvas.Canvas, frame source.Frame) scopeFrame {
	tb.Helper()

	lay := scene.newLayout(dst)
	lay.bottom -= scene.keyBarHeight(&lay)
	lay.top += scene.headerHeight(&lay)
	lay.split()

	view, drawable := scene.measureScope(&lay, frame.Receiver)
	if !drawable {
		tb.Fatal("measureScope found no room for a scope on the panel's own size")
	}

	return view
}

// rgba is an opaque colour from three channels, which is all the palette ever
// holds.
func rgba(red, green, blue uint8) color.RGBA {
	return color.RGBA{R: red, G: green, B: blue, A: opaque}
}

// channelGap is the largest difference between two colours on any one channel.
// It is a coarse measure of how far apart they look, which is all the
// comparison above needs.
func channelGap(left, right color.RGBA) int {
	return max(
		absInt(int(left.R)-int(right.R)),
		absInt(int(left.G)-int(right.G)),
		absInt(int(left.B)-int(right.B)),
	)
}

// absInt is the integer absolute value; math.Abs would round-trip through
// float64 for no reason.
func absInt(value int) int {
	if value < 0 {
		return -value
	}

	return value
}

// fieldKind names which of a look's two palettes this is, so the subtests
// above have distinct names.
func fieldKind(pal theme.Palette) string {
	if pal.Light() {
		return string(theme.KindDay)
	}

	return string(theme.KindNight)
}
