package canvas_test

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/pkg/canvas"
)

// TestBlendQuadMatchesBlendPolygon draws the same quadrilaterals both ways
// on the same background and wants the same bytes: convex, concave, crossed
// over into a bow tie, flat, partly and wholly off the clip, at alphas below,
// at and above one.
func TestBlendQuadMatchesBlendPolygon(t *testing.T) {
	t.Parallel()

	const side = 48

	state := uint64(7)
	next := func(span int) int {
		state = state*6364136223846793005 + 1442695040888963407

		return int(state>>33)%span - span/4
	}

	quads := make([][4]image.Point, 0, 306)
	quads = append(quads, [][4]image.Point{
		{{2, 2}, {30, 4}, {28, 40}, {5, 33}},
		{{2, 2}, {30, 30}, {30, 2}, {2, 30}},
		{{0, 10}, {40, 10}, {40, 10}, {0, 10}},
		{{-20, -5}, {70, 8}, {60, 60}, {-9, 44}},
		{{100, 100}, {120, 100}, {120, 130}, {100, 130}},
		{{10, 10}, {20, 10}, {20, 20}, {10, 20}},
	}...)

	for range 300 {
		var quad [4]image.Point
		for corner := range quad {
			quad[corner] = image.Pt(next(2*side), next(2*side))
		}

		quads = append(quads, quad)
	}

	clip := image.Rect(3, 1, side-2, side-5)
	ink := color.RGBA{R: 0x5B, G: 0xA4, B: 0x2E, A: 0xFF}

	for _, alpha := range []float64{math.NaN(), 0, 0.07, 0.31, 0.5, 0.93, 1, 1.5} {
		for index, quad := range quads {
			want, got := patterned(t, side), patterned(t, side)

			want.BlendPolygon([][]image.Point{quad[:]}, clip, ink, alpha)
			got.BlendQuad(quad, clip, ink, alpha)

			if !bytes.Equal(got.Image().Pix, want.Image().Pix) {
				t.Fatalf("alpha %v, quad %d %v: BlendQuad differs from BlendPolygon", alpha, index, quad)
			}
		}
	}
}

func TestBlendQuadOffTheCanvas(t *testing.T) {
	t.Parallel()

	canv := patterned(t, 8)
	before := bytes.Clone(canv.Image().Pix)

	canv.BlendQuad([4]image.Point{{0, 0}, {4, 0}, {4, 4}, {0, 4}}, image.Rect(20, 20, 30, 30), canvas.White, 0.5)

	if !bytes.Equal(canv.Image().Pix, before) {
		t.Error("a clip off the canvas painted on it")
	}
}

// patterned is a side by side canvas with a different byte everywhere, so a
// blend that lands on the wrong pixel or rounds the other way shows.
func patterned(t *testing.T, side int) *canvas.Canvas {
	t.Helper()

	canv, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for index := range canv.Image().Pix {
		canv.Image().Pix[index] = byte(index*31 + 5) //nolint:gosec // a pattern, wrapping on purpose.
	}

	return canv
}
