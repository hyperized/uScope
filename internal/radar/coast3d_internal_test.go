package radar

import (
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// drawCoastLine3 draws one coastline the way drawCoast3 draws each of its own,
// against the view's own haze.
func drawCoastLine3(scene *Scene, dst *canvas.Canvas, view scene3, line shore.Polyline) {
	inks := scene.coastInks3()
	scene.drawShoreLine3(dst, view, view.haze(), &inks, view.boxed3(line))
}

// drawCoastSegment3 draws one piece of coast against the view's own haze.
func drawCoastSegment3(scene *Scene, dst *canvas.Canvas, view scene3, ring circle, first, last point3) {
	inks := scene.coastInks3()
	scene.drawShoreSegment3(dst, view, view.haze(), &inks, ring, first, last)
}

// contrast is how far a colour stands off the field, summed over the three
// channels, which is what "brighter" means for a line fading into it on any
// palette, dark or light.
func contrast(col, field color.RGBA) int {
	channel := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }

	return channel(col.R, field.R) + channel(col.G, field.G) + channel(col.B, field.B)
}

// TestHazeBand reads band against edge, which it inverts, and at the three
// places it answers without doing the arithmetic.
func TestHazeBand(t *testing.T) {
	t.Parallel()

	_, _, view := water3DScene(t, ViewMinimal3D)
	haze := view.haze()

	ahead := func(distance float64) point3 {
		return point3{east: haze.foot.east, north: haze.foot.north + distance}
	}

	for _, testCase := range []struct {
		name string
		at   point3
		want int
	}{
		{name: "the camera's foot is full tint", at: haze.foot, want: 0},
		{name: "the far side of the reach is full tint", at: ahead(haze.near), want: 0},
		{name: "the far edge is past", at: ahead(haze.far), want: hazePast},
		{name: "beyond the far edge is past", at: ahead(2 * haze.far), want: hazePast},
		{name: "a point that is not a number is past", at: point3{east: math.NaN()}, want: hazePast},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := haze.band(testCase.at); got != testCase.want {
				t.Errorf("band(%+v) = %d, want %d", testCase.at, got, testCase.want)
			}
		})
	}

	for band := 1; band <= hazeBands; band++ {
		middle := (haze.edge(band-1) + haze.edge(band)) / 2
		if got := haze.band(ahead(middle)); got != band {
			t.Errorf("band at %.1f nm, between edges %d and %d, = %d, want %d", middle, band-1, band, got, band)
		}
	}
}

// TestCoastFadesWithDistance walks straight ahead from the camera's foot to
// beyond the haze and reads the colour a coastline would be drawn in at each
// step: the shore colour first, never a shade stronger than the step before,
// and the field itself at the end.
func TestCoastFadesWithDistance(t *testing.T) {
	t.Parallel()

	const steps = 400

	scene, _, view := water3DScene(t, ViewMinimal3D)
	haze := view.haze()
	inks := scene.coastInks3()
	field := scene.pal.Field

	inkAt := func(distance float64) color.RGBA {
		band := haze.band(point3{east: haze.foot.east, north: haze.foot.north + distance})
		if band == hazePast {
			return field
		}

		return inks[band]
	}

	if got := inkAt(0); got != scene.pal.Shore {
		t.Errorf("at the camera's foot the coast is %v, want the shore colour %v", got, scene.pal.Shore)
	}

	last := contrast(scene.pal.Shore, field)

	for step := range steps + 1 {
		distance := haze.far * 1.2 * float64(step) / steps
		now := contrast(inkAt(distance), field)

		if now > last {
			t.Errorf("at %.1f nm the coast stands %d off the field, more than %d a step nearer", distance, now, last)
		}

		last = now
	}

	if last != 0 {
		t.Errorf("past the haze the coast stands %d off the field, want 0", last)
	}
}

// TestCoastFadesWithItsBand checks the rate a coastline fades at past the
// reach: in every band it stands off that band's bare ground (the field
// carried towards the horizon, with no tint on it) by the square of the
// band's strength times what it stands off the field, and it never keeps
// more of its lead than the sea keeps of its own. The bare ground rather than
// the land, because the land has a tint of its own now and the coast is built
// on the ground under both. Before, the line kept the
// same share as the ground, and because it starts three to four times
// further off, it was still plainly visible after the ground had gone.
// Rounding is allowed a level a channel on either colour.
func TestCoastFadesWithItsBand(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		sun  float64
		pal  theme.Palette
	}{
		{name: "night with no sky", sun: math.NaN(), pal: theme.Night},
		{name: "day", sun: 30, pal: theme.Day},
		{name: "dusk", sun: -3, pal: theme.Night},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene, _, _ := water3DScene(t, ViewMinimal3D)
			if !math.IsNaN(testCase.sun) {
				scene, _ = skyScene(t, testCase.sun, testCase.pal)
			}

			inks := scene.coastInks3()
			field := scene.pal.Field
			full := float64(contrast(scene.pal.Shore, field))
			water := float64(contrast(scene.waterInk(), field))

			for band := 1; band <= hazeBands; band++ {
				strength := hazeStrength(band)
				ground := scene.hazed(field, 0, strength)
				lead := float64(contrast(inks[band], ground))

				if want := full * strength * strength; math.Abs(lead-want) > 6 {
					t.Errorf("band %d: the coast stands %.0f off its ground, want %.1f", band, lead, want)
				}

				sea := float64(contrast(scene.hazeInk(scene.waterInk(), band), ground)) / water
				if share := lead / full; share > sea+6/full+6/water {
					t.Errorf("band %d: the coast keeps %.3f of its lead and the sea %.3f", band, share, sea)
				}
			}
		})
	}
}

// TestCoastInsideTheReachIsUnchanged checks that a piece of coast inside the
// range is the very stroke it was before the haze: the shore colour, at the
// view's weight, between the same two pixels.
func TestCoastInsideTheReachIsUnchanged(t *testing.T) {
	t.Parallel()

	scene, view := envelopeFixture()
	first := point3{east: -view.reachNm / 3, north: view.reachNm / 4}
	last := point3{east: view.reachNm / 3, north: view.reachNm / 3}

	got := blankCanvas(t)
	drawCoastSegment3(scene, got, view, circle{radius: view.reachNm}, first, last)

	want := blankCanvas(t)
	startX, startY, _ := view.cam.at(first)
	endX, endY, _ := view.cam.at(last)
	want.StrokeLineAA(float64(startX), float64(startY), float64(endX), float64(endY), view.weight, scene.pal.Shore)

	if identicalPixels(got, blankCanvas(t)) {
		t.Fatal("the piece of coast drew nothing, so this proves nothing")
	}

	if !identicalPixels(got, want) {
		t.Error("a piece of coast inside the range is not the plain shore stroke it used to be")
	}
}

// TestCoastRunsPastTheReach checks the two ways a coastline reaches beyond the
// range: a segment that crosses the ring is drawn on both sides of it, and a
// coastline wholly out in the haze is drawn at all.
func TestCoastRunsPastTheReach(t *testing.T) {
	t.Parallel()

	t.Run("a segment through the range is drawn either side of it", func(t *testing.T) {
		t.Parallel()

		scene, view := envelopeFixture()
		ring := circle{radius: view.reachNm}
		first := point3{east: -1.5 * view.reachNm, north: view.reachNm / 2}
		last := point3{east: 1.5 * view.reachNm, north: view.reachNm / 2}

		whole := blankCanvas(t)
		drawCoastSegment3(scene, whole, view, ring, first, last)

		enter, leave, _ := ring.span(segment{fromX: first.east, fromY: first.north, toX: last.east, toY: last.north})
		inside := blankCanvas(t)
		drawCoastSegment3(scene, inside, view, ring, first.along(last, enter), first.along(last, leave))

		if identicalPixels(whole, inside) {
			t.Error("the coast stopped at the range, want it drawn on into the haze")
		}
	})

	t.Run("a coastline out in the haze is drawn", func(t *testing.T) {
		t.Parallel()

		scene, canv, view := water3DScene(t, ViewMinimal3D)
		haze := view.haze()

		// In the second band, straight ahead of the camera. Halfway through
		// the haze is above the top of the picture at this camera.
		north := haze.foot.north + (haze.edge(1)+haze.edge(2))/2
		lat := view.origin.lat + north/nmPerDegree

		line := shore.Polyline{{Lat: lat, Lon: view.origin.lon - 1}, {Lat: lat, Lon: view.origin.lon + 1}}
		scene.shoreSet = syntheticWaterSet(t, []shore.Polyline{line}, nil)

		bare := blankCopy(t, canv, scene.pal.Field)
		scene.fillGround3(bare, view, haze)
		scene.drawShore3(canv, view)

		if identicalPixels(canv, bare) {
			t.Error("a coastline out in the haze drew nothing, want it faded")
		}
	})
}

// blankCopy is a canvas the size of canv cleared to field.
func blankCopy(tb testing.TB, canv *canvas.Canvas, field color.RGBA) *canvas.Canvas {
	tb.Helper()

	box := canv.Bounds()

	other, err := canvas.New(box.Dx(), box.Dy())
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	other.Clear(field)

	return other
}
