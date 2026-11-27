package radar

import (
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
)

// airwaySide is the canvas the internal airway cases draw on.
const airwaySide = 200

// testAirway runs north to south across the fixture receiver at FL350,
// lengthNm long, at full strength.
func testAirway(lengthNm float64) source.Airway {
	half := lengthNm / 2 / nmPerDegree

	return source.Airway{
		FromLatitude: layerBaseLat + half, FromLongitude: layerBaseLon,
		ToLatitude: layerBaseLat - half, ToLongitude: layerBaseLon,
		Altitude: 35000, Tracks: 3, Strength: 1,
	}
}

// airwayFlat is a scene with the airways on over the given ones, a canvas
// and a projector for the flat views with a 20 nm range.
func airwayFlat(t *testing.T, pal theme.Palette, airways ...source.Airway) (*Scene, *canvas.Canvas, projector) {
	t.Helper()

	canv, err := canvas.New(airwaySide, airwaySide)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	geom := scopeGeometry{centerX: airwaySide / 2, centerY: airwaySide / 2, outer: 95, rangeR: 90}

	proj, ok := newProjector(geom, geo{lat: layerBaseLat, lon: layerBaseLon}, 20)
	if !ok {
		t.Fatal("newProjector refused a plain geometry")
	}

	scene := &Scene{airways: true}
	scene.SetPalette(pal)
	scene.exposure.airways = airways

	return scene, canv, proj
}

// inked counts the pixels that are no longer the canvas's black.
func inked(canv *canvas.Canvas) int {
	count := 0

	for offset := 0; offset < len(canv.Image().Pix); offset += 4 {
		pix := canv.Image().Pix[offset : offset+3]
		if pix[0] != 0 || pix[1] != 0 || pix[2] != 0 {
			count++
		}
	}

	return count
}

func TestAirwayEnds(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		t, want float64
	}{
		{0, 0}, {1, 0}, {-0.5, 0}, {airwayEndShare / 2, 0.5}, {1 - airwayEndShare/2, 0.5},
		{airwayEndShare, 1}, {0.5, 1}, {1 - airwayEndShare, 1},
	} {
		if got := airwayEnds(testCase.t); math.Abs(got-testCase.want) > 1e-9 {
			t.Errorf("airwayEnds(%v) = %v, want %v", testCase.t, got, testCase.want)
		}
	}
}

func TestAirwayShown(t *testing.T) {
	t.Parallel()

	faded := testAirway(40)
	faded.Strength = corridorMinStrength / 2

	for _, testCase := range []struct {
		name   string
		airway source.Airway
		bands  bandMask
		want   bool
	}{
		{name: "fresh and unfiltered", airway: testAirway(40), bands: allBands, want: true},
		{name: "the high band alone", airway: testAirway(40), bands: 1 << bandHigh, want: true},
		{name: "filtered to the low band", airway: testAirway(40), bands: 1 << bandLow},
		{name: "faded", airway: faded, bands: allBands},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := airwayShown(&testCase.airway, testCase.bands); got != testCase.want {
				t.Errorf("airwayShown = %t, want %t", got, testCase.want)
			}
		})
	}
}

// TestDrawAirwaysFlat draws one airway across the receiver and checks what
// is left out: everything while the setting is off, a filtered or too short
// airway, and the ends, which fade.
func TestDrawAirwaysFlat(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		airway source.Airway
		off    bool
		bands  bandMask
		drawn  bool
	}{
		{name: "across the receiver", airway: testAirway(40), bands: allBands, drawn: true},
		{name: "setting off", airway: testAirway(40), off: true, bands: allBands},
		{name: "filtered", airway: testAirway(40), bands: 1 << bandMid},
		{name: "under a pixel long", airway: testAirway(0.05), bands: allBands},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, canv, proj := airwayFlat(t, theme.Night, testCase.airway)
			scene.airways = !testCase.off
			scene.drawAirways(canv, proj, testCase.bands, 1)

			if got := inked(canv) > 0; got != testCase.drawn {
				t.Errorf("drawn = %t, want %t", got, testCase.drawn)
			}
		})
	}
}

// TestDrawAirwaysEndsFade checks the dashes are strongest in the middle and
// leave the very ends undrawn.
func TestDrawAirwaysEndsFade(t *testing.T) {
	t.Parallel()

	scene, canv, proj := airwayFlat(t, theme.Night, testAirway(40))
	scene.drawAirways(canv, proj, allBands, 1)

	brightest := func(fromY, toY int) uint8 {
		var top uint8

		for y := fromY; y < toY; y++ {
			for x := airwaySide/2 - 2; x <= airwaySide/2+2; x++ {
				top = max(top, canv.Image().RGBAAt(x, y).B)
			}
		}

		return top
	}

	// The airway runs 40 nm on a 20 nm scope, so the range ring cuts it a
	// quarter of the way in from each end, where the fade is over.
	middle := brightest(airwaySide/2-10, airwaySide/2+10)
	if middle == 0 {
		t.Fatal("nothing drawn across the receiver")
	}

	short, shortCanv, shortProj := airwayFlat(t, theme.Night, testAirway(12))
	short.drawAirways(shortCanv, shortProj, allBands, 1)

	canv = shortCanv
	end := brightest(airwaySide/2-27, airwaySide/2-20)

	if end >= middle {
		t.Errorf("dash near the end at %d, want fainter than the middle's %d", end, middle)
	}
}

// TestDrawAirwaysLightField checks a light palette draws the dashes at the
// lighter opacity: the same airway moves a light field less, measured as
// the distance from the field, than it moves a dark one.
func TestDrawAirwaysLightField(t *testing.T) {
	t.Parallel()

	shift := func(pal theme.Palette) float64 {
		scene, canv, proj := airwayFlat(t, pal, testAirway(40))
		canv.Clear(pal.Field)
		scene.drawAirways(canv, proj, allBands, 1)

		most := 0.0

		for y := range airwaySide {
			share := channelSum(canv.Image().RGBAAt(airwaySide/2, y), pal.Field) / channelSum(pal.Airway, pal.Field)
			most = max(most, share)
		}

		return most
	}

	if light, dark := shift(theme.Day), shift(theme.Night); light >= dark {
		t.Errorf("day dashes reach %.2f of the way to the ink, night %.2f; want day lighter", light, dark)
	}
}

// channelSum is the summed distance between two colours' channels.
func channelSum(left, right color.RGBA) float64 {
	return math.Abs(float64(left.R)-float64(right.R)) + math.Abs(float64(left.G)-float64(right.G)) +
		math.Abs(float64(left.B)-float64(right.B))
}

// TestDrawAirways3 draws one airway in the tilted view and checks the same
// cuts as the flat one, plus the range: the full view clips to it and the
// bare one does not.
func TestDrawAirways3(t *testing.T) {
	t.Parallel()

	outside := testAirway(20)
	outside.FromLatitude += 3
	outside.ToLatitude += 3

	for _, testCase := range []struct {
		name   string
		airway source.Airway
		off    bool
		bare   bool
		bands  bandMask
		drawn  bool
	}{
		{name: "in front of the camera", airway: testAirway(20), bands: allBands, drawn: true},
		{name: "setting off", airway: testAirway(20), off: true, bands: allBands},
		{name: "filtered", airway: testAirway(20), bands: 1 << bandLow},
		{name: "under a mile long", airway: testAirway(0.5), bands: allBands},
		{name: "past the range", airway: outside, bands: allBands},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(cameraSide, cameraSide)
			if err != nil {
				t.Fatalf("canvas.New: %v", err)
			}

			scene := &Scene{airways: !testCase.off}
			scene.SetPalette(theme.Night)
			scene.exposure.airways = []source.Airway{testCase.airway}
			scene.drawAirways3(canv, ringView(), testCase.bands)

			if got := inked(canv) > 0; got != testCase.drawn {
				t.Errorf("drawn = %t, want %t", got, testCase.drawn)
			}
		})
	}
}

// TestAirwayPenBareReach checks the bare 3D view does not clip an airway to
// the range: a place past it that the full view refuses is placed there.
func TestAirwayPenBareReach(t *testing.T) {
	t.Parallel()

	far := testAirway(20)
	far.FromLatitude += 2
	far.ToLatitude += 2
	view := ringView()

	full := &Scene{shown: View3D}
	bare := &Scene{shown: ViewMinimal3D}

	fullPen, fullOK := full.tiltedPen(&far, &view)
	barePen, bareOK := bare.tiltedPen(&far, &view)

	if !fullOK || !bareOK {
		t.Fatal("tiltedPen refused a 20 nm airway")
	}

	if _, _, in := fullPen.at(0.5); in {
		t.Error("the full view placed a point past its range")
	}

	middle := point3{
		east:  (barePen.a.east + barePen.b.east) / 2,
		north: (barePen.a.north + barePen.b.north) / 2,
		up:    barePen.a.up,
	}
	wantX, wantY, wantIn := view.cam.at(middle)

	if x, y, in := barePen.at(0.5); in != wantIn || x != float64(wantX) || y != float64(wantY) {
		t.Errorf("the bare view placed the middle at %v, %v, %t; the camera at %d, %d, %t",
			x, y, in, wantX, wantY, wantIn)
	}

	if !wantIn {
		t.Error("the fixture's camera cannot see the far airway; move it closer")
	}
}

// TestDrawAirwaysAllocatesNothing draws the flat and the tilted airways
// repeatedly. It does not call t.Parallel: testing.AllocsPerRun panics when
// it is.
//
//nolint:paralleltest // see above.
func TestDrawAirwaysAllocatesNothing(t *testing.T) {
	scene, canv, proj := airwayFlat(t, theme.Night, testAirway(40), testAirway(30))
	view := ringView()

	draw := func() {
		scene.drawAirways(canv, proj, allBands, 2)
		scene.drawAirways3(canv, view, allBands)
	}

	if allocs := testing.AllocsPerRun(20, draw); allocs != 0 {
		t.Errorf("drawing the airways allocates %.0f times, want 0", allocs)
	}
}
