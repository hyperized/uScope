package radar

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// wideLandDeg is how far a ring of land reaches from the probe receiver in
// every direction. Twenty degrees is past the haze's far edge at the probe
// range and past every corner of the flat scope, so the ring covers the whole
// picture and crosses the corners the flat fill has to paint back.
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

// TestLandInk pins the tint each palette fills the land with, written out for
// the reason TestWaterInk gives. On the handheld it is eight per cent of OK
// into the field, so green in the looks that have one and grey in mono; the
// vector pair is a grey-green picked to carry neither tube's hue.
func TestLandInk(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		pal  theme.Palette
		want color.RGBA
	}{
		{name: "glass night is green-black", pal: theme.Night, want: rgba(3, 18, 7)},
		{name: "glass day is a faint sage", pal: theme.Day, want: rgba(221, 233, 230)},
		{name: "phosphor night is a deeper green", pal: theme.PhosphorNight, want: rgba(11, 31, 19)},
		{name: "phosphor day is a green on a light field", pal: theme.PhosphorDay, want: rgba(221, 235, 221)},
		{name: "mono night is a grey", pal: theme.MonoNight, want: rgba(25, 25, 27)},
		{name: "mono day is a grey on a light field", pal: theme.MonoDay, want: rgba(229, 226, 220)},
		{name: "vector night is a slate grey-green", pal: theme.VectorNight, want: rgba(42, 54, 52)},
		{name: "vector day is a pale yellow-green", pal: theme.VectorDay, want: rgba(217, 226, 196)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{}
			scene.SetPalette(testCase.pal)

			got := scene.landInk()
			if got != testCase.want {
				t.Errorf("landInk() on %s = %v, want %v", testCase.pal.Look, got, testCase.want)
			}

			if got == testCase.pal.Field || got == scene.waterInk() {
				t.Errorf("landInk() = %v, want it apart from the field %v and the sea %v",
					got, testCase.pal.Field, scene.waterInk())
			}

			if channelGap(got, testCase.pal.Field) >= channelGap(testCase.pal.Shore, testCase.pal.Field) {
				t.Errorf("landInk() is %d off the field and the shore %d, want the tint the quieter",
					channelGap(got, testCase.pal.Field), channelGap(testCase.pal.Shore, testCase.pal.Field))
			}
		})
	}
}

// TestFullScopeCornersStayField fills the whole scope with land and checks
// that none of it got past the range ring. The fill is clipped to the square
// around the ring, so without clearCorners the four corners between the two
// would come out green. Every pixel of that square outside the disc
// FillCircle paints is checked, so a corner painted back one pixel short of
// the water's own edge fails too, and the column beside the scope must carry
// no land at all.
func TestFullScopeCornersStayField(t *testing.T) {
	t.Parallel()

	scene, canv, frame := waterScene(t, WithShore(syntheticWaterSet(t, nil, []shore.Polyline{wideLand()})))
	scene.renderLayer(canv, scene.layerKeyFor(canv, frame), frame)

	layer := scene.layer
	view := scopeViewOf(t, scene, layer, frame)
	disc := circle{
		centerX: float64(view.geom.centerX),
		centerY: float64(view.geom.centerY),
		radius:  math.Round(view.proj.limitNm * view.proj.scale),
	}

	inside := image.Pt(view.geom.centerX+int(disc.radius)/2, view.geom.centerY+int(disc.radius)/5)
	if got := layer.Image().RGBAAt(inside.X, inside.Y); got != scene.landInk() {
		t.Fatalf("inside the ring the ground is %v, want the land %v", got, scene.landInk())
	}

	assertNoGroundOutside(t, scene, layer, disc)

	column := stalkLayout(scene, layer).column
	if column.Empty() {
		t.Fatal("the layout has no column, so there is nothing to check")
	}

	assertNoColour(t, layer, column, scene.landInk())
}

// assertNoGroundOutside fails on the first pixel of the square around disc
// that lies outside the disc FillCircle paints and still carries the land or
// the water tint, and checks the square's four corners are the field.
func assertNoGroundOutside(tb testing.TB, scene *Scene, layer *canvas.Canvas, disc circle) {
	tb.Helper()

	centerX, centerY, radius := int(disc.centerX), int(disc.centerY), int(disc.radius)
	land, water := scene.landInk(), scene.waterInk()

	for row := -radius; row <= radius; row++ {
		half := int(math.Sqrt(float64(radius*radius - row*row)))

		for col := centerX - radius; col <= centerX+radius; col++ {
			if col >= centerX-half && col <= centerX+half {
				continue
			}

			if got := layer.Image().RGBAAt(col, centerY+row); got == land || got == water {
				tb.Fatalf("(%d, %d) is outside the ring and came out %v, want no ground there", col, centerY+row, got)
			}
		}
	}

	for _, corner := range []image.Point{
		{X: centerX - radius, Y: centerY - radius}, {X: centerX + radius, Y: centerY - radius},
		{X: centerX - radius, Y: centerY + radius}, {X: centerX + radius, Y: centerY + radius},
	} {
		if got := layer.Image().RGBAAt(corner.X, corner.Y); got != scene.pal.Field {
			tb.Errorf("the corner %v is %v, want the field %v", corner, got, scene.pal.Field)
		}
	}
}

// assertNoColour fails on the first pixel inside box that is col.
func assertNoColour(tb testing.TB, layer *canvas.Canvas, box image.Rectangle, col color.RGBA) {
	tb.Helper()

	for row := box.Min.Y; row < box.Max.Y; row++ {
		for column := box.Min.X; column < box.Max.X; column++ {
			if layer.Image().RGBAAt(column, row) == col {
				tb.Fatalf("(%d, %d) inside %v is %v", column, row, box, col)
			}
		}
	}
}

// TestMinimalLandRunsEdgeToEdge checks the bare flat view, which has no ring
// and so nothing to paint back: land that covers the picture covers all of
// it, corners included.
func TestMinimalLandRunsEdgeToEdge(t *testing.T) {
	t.Parallel()

	scene, canv, frame := waterScene(t, WithShore(syntheticWaterSet(t, nil, []shore.Polyline{wideLand()})))
	scene.shown = ViewMinimal
	scene.minimalShore = true
	scene.renderLayer(canv, scene.layerKeyFor(canv, frame), frame)

	bounds := scene.layer.Bounds()
	land := scene.landInk()

	for _, probe := range []image.Point{
		bounds.Min, {X: bounds.Max.X - 1, Y: bounds.Min.Y},
		{X: bounds.Min.X, Y: bounds.Max.Y - 1}, bounds.Max.Sub(image.Pt(1, 1)),
		{X: bounds.Dx() / 3, Y: bounds.Dy() / 3},
	} {
		if got := scene.layer.Image().RGBAAt(probe.X, probe.Y); got != land {
			t.Errorf("the bare view at %v is %v, want the land %v edge to edge", probe, got, land)
		}
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
