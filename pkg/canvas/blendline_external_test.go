package canvas_test

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/pkg/canvas"
)

// blendLines are the lines BlendLineAA is held against StrokeLineAA on:
// shallow, steep, both directions, fractional ends, a point, and lines that
// run off every edge, so the clipped columns and rows are walked too.
//
//nolint:gochecknoglobals // read-only test fixture.
var blendLines = []struct {
	name           string
	x0, y0, x1, y1 float64
}{
	{name: "shallow", x0: 4, y0: 10, x1: 50, y1: 22},
	{name: "steep", x0: 10, y0: 4, x1: 22, y1: 58},
	{name: "backwards", x0: 50, y0: 22, x1: 4, y1: 10},
	{name: "sub-pixel ends", x0: 3.3, y0: 4.7, x1: 25.2, y1: 18.9},
	{name: "point", x0: 10, y0: 10, x1: 10, y1: 10},
	{name: "across the canvas", x0: -20, y0: -5, x1: 90, y1: 70},
	{name: "along the top edge", x0: -5, y0: 0.4, x1: 70, y1: 0.4},
	{name: "along the bottom edge", x0: -5, y0: 63.6, x1: 70, y1: 63.6},
	{name: "steep off both ends", x0: 30, y0: -10, x1: 34, y1: 80},
}

// TestBlendLineAAOpaqueIsStroke checks that at full opacity the blended line
// is StrokeLineAA's, pixel for pixel, at every width above one, and that an
// alpha past one is held to one.
func TestBlendLineAAOpaqueIsStroke(t *testing.T) {
	t.Parallel()

	for _, line := range blendLines {
		for _, width := range []int{2, 3, 5} {
			t.Run(line.name, func(t *testing.T) {
				t.Parallel()

				want := mustCanvas(t, 64)
				want.StrokeLineAA(line.x0, line.y0, line.x1, line.y1, width, canvas.White)

				for _, alpha := range []float64{1, 7} {
					got := mustCanvas(t, 64)
					got.BlendLineAA(line.x0, line.y0, line.x1, line.y1, width, canvas.White, alpha)

					if !bytes.Equal(got.Image().Pix, want.Image().Pix) {
						t.Errorf("width %d alpha %v differs from StrokeLineAA", width, alpha)
					}
				}
			})
		}
	}
}

// TestBlendLineAAHalf checks a half-opaque line over black lands every
// pixel within a rounding of half StrokeLineAA's, and over a lit field
// halfway between the field and the ink, which is the point of blending.
func TestBlendLineAAHalf(t *testing.T) {
	t.Parallel()

	field := color.RGBA{R: 200, G: 100, B: 40, A: 255}

	for _, line := range blendLines {
		t.Run(line.name, func(t *testing.T) {
			t.Parallel()

			full := mustCanvas(t, 64)
			full.StrokeLineAA(line.x0, line.y0, line.x1, line.y1, 3, canvas.White)

			half := mustCanvas(t, 64)
			half.BlendLineAA(line.x0, line.y0, line.x1, line.y1, 3, canvas.White, 0.5)

			lit := mustCanvas(t, 64)
			lit.Clear(field)
			lit.BlendLineAA(line.x0, line.y0, line.x1, line.y1, 3, canvas.White, 0.5)

			for index, value := range full.Image().Pix {
				if index%4 == 3 {
					continue
				}

				if got := half.Image().Pix[index]; math.Abs(float64(got)-float64(value)/2) > 1 {
					t.Fatalf("byte %d: %d over black, want about half of %d", index, got, value)
				}

				base := float64([3]uint8{field.R, field.G, field.B}[index%4])
				want := base + (float64(value)/255)*(255-base)/2

				if got := lit.Image().Pix[index]; math.Abs(float64(got)-want) > 1 {
					t.Fatalf("byte %d: %d over the field, want about %.1f", index, got, want)
				}
			}
		})
	}
}

// TestBlendLineAAThin checks a width under two draws the two-pixel band's
// one-pixel sibling: zero and negative widths draw what width one draws.
func TestBlendLineAAThin(t *testing.T) {
	t.Parallel()

	want := mustCanvas(t, 64)
	want.BlendLineAA(3.3, 4.7, 40.2, 30.9, 1, canvas.White, 0.8)

	if !lit(want, 20, 18) && !lit(want, 20, 17) {
		t.Fatal("width one drew nothing on the line")
	}

	for _, width := range []int{0, -3} {
		got := mustCanvas(t, 64)
		got.BlendLineAA(3.3, 4.7, 40.2, 30.9, width, canvas.White, 0.8)

		if !bytes.Equal(got.Image().Pix, want.Image().Pix) {
			t.Errorf("width %d differs from width one", width)
		}
	}
}

// TestBlendLineAANothing checks the calls that must leave the canvas alone.
func TestBlendLineAANothing(t *testing.T) {
	t.Parallel()

	nan, inf := math.NaN(), math.Inf(1)

	for _, testCase := range []struct {
		name           string
		x0, y0, x1, y1 float64
		alpha          float64
	}{
		{name: "transparent", x0: 1, y0: 1, x1: 40, y1: 40, alpha: 0},
		{name: "negative alpha", x0: 1, y0: 1, x1: 40, y1: 40, alpha: -1},
		{name: "NaN", x0: nan, y0: 1, x1: 40, y1: 40, alpha: 1},
		{name: "infinite", x0: 1, y0: inf, x1: 40, y1: 40, alpha: 1},
		{name: "infinite end", x0: 1, y0: 1, x1: -inf, y1: 40, alpha: 1},
		{name: "NaN end", x0: 1, y0: 1, x1: 40, y1: nan, alpha: 1},
		{name: "left of the canvas", x0: -40, y0: 1, x1: -10, y1: 40, alpha: 1},
		{name: "under the canvas", x0: 1, y0: 90, x1: 40, y1: 120, alpha: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv := mustCanvas(t, 64)
			canv.BlendLineAA(testCase.x0, testCase.y0, testCase.x1, testCase.y1, 3, canvas.White, testCase.alpha)

			if !bytes.Equal(canv.Image().Pix, make([]byte, len(canv.Image().Pix))) {
				t.Error("drew on the canvas")
			}
		})
	}
}

// TestBlendLineAASub checks a sub-canvas draws in its own coordinates, the
// same pixels a whole canvas draws at that place.
func TestBlendLineAASub(t *testing.T) {
	t.Parallel()

	whole := mustCanvas(t, 64)
	box := image.Rect(16, 8, 48, 56)

	sub, ok := whole.Sub(box)
	if !ok {
		t.Fatal("Sub refused a box inside the canvas")
	}

	sub.BlendLineAA(10, 0, 40, 70, 2, canvas.White, 0.6)

	want := mustCanvas(t, 64)
	clipped, _ := want.Sub(box)
	clipped.StrokeLineAA(10, 0, 40, 70, 2, canvas.White)

	for y := range 64 {
		for x := range 64 {
			inside := image.Pt(x, y).In(box)
			if got := lit(whole, x, y); got != (inside && lit(want, x, y)) {
				t.Fatalf("pixel %d,%d lit %t, want %t", x, y, got, inside && lit(want, x, y))
			}
		}
	}
}

// TestBlendLineAAAllocs does not call t.Parallel: testing.AllocsPerRun panics
// if it runs while the test is marked parallel.
//
//nolint:paralleltest // AllocsPerRun forbids running as a parallel test; see comment above.
func TestBlendLineAAAllocs(t *testing.T) {
	canv := mustCanvas(t, 64)

	for _, width := range []int{1, 3} {
		draw := func() { canv.BlendLineAA(2.5, 3.5, 60.5, 41.5, width, canvas.White, 0.5) }
		if allocs := testing.AllocsPerRun(100, draw); allocs != 0 {
			t.Errorf("width %d allocated %v per run, want 0", width, allocs)
		}
	}
}

func BenchmarkBlendLineAA(b *testing.B) {
	const side = 256

	canv, err := canvas.New(side, side)
	if err != nil {
		b.Fatalf("New: %v", err)
	}

	for _, width := range []int{1, 2} {
		b.Run(map[int]string{1: "width 1", 2: "width 2"}[width], func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				canv.BlendLineAA(0, 0, side-1, side-1, width, canvas.White, 0.5)
			}
		})
	}
}
