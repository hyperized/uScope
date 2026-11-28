package canvas_test

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/pkg/canvas"
)

// ringSide is the canvas the ring tests draw on, and ringCentre its middle.
const (
	ringSide   = 41
	ringCentre = 20
)

// ringCanvas is a black canvas of ringSide.
func ringCanvas(tb testing.TB) *canvas.Canvas {
	tb.Helper()

	canv, err := canvas.New(ringSide, ringSide)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	canv.Clear(canvas.Black)

	return canv
}

// TestBlendStrokeCircleOpaqueIsStroke checks that at full opacity, and past
// it, the blended ring is StrokeCircle's byte for byte, the midpoint ring at
// width one included.
func TestBlendStrokeCircleOpaqueIsStroke(t *testing.T) {
	t.Parallel()

	for _, width := range []int{0, 1, 2, 3} {
		for _, alpha := range []float64{1, 1.5} {
			stroked, blended := ringCanvas(t), ringCanvas(t)
			stroked.StrokeCircle(ringCentre, ringCentre, 12, width, canvas.White)
			blended.BlendStrokeCircle(ringCentre, ringCentre, 12, width, canvas.White, alpha)

			if !bytes.Equal(stroked.Image().Pix, blended.Image().Pix) {
				t.Errorf("width %d alpha %v: blended ring differs from StrokeCircle", width, alpha)
			}
		}
	}
}

// TestBlendStrokeCircleNothing checks the calls that must leave the canvas
// alone: no alpha, a NaN alpha, and a negative radius.
func TestBlendStrokeCircleNothing(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		radius int
		alpha  float64
	}{
		{name: "zero alpha", radius: 10, alpha: 0},
		{name: "negative alpha", radius: 10, alpha: -1},
		{name: "NaN alpha", radius: 10, alpha: math.NaN()},
		{name: "negative radius", radius: -1, alpha: 0.5},
	} {
		canv := ringCanvas(t)
		canv.BlendStrokeCircle(ringCentre, ringCentre, testCase.radius, 2, canvas.White, testCase.alpha)

		if got := litPixels(canv); got != 0 {
			t.Errorf("%s: %d pixels drawn, want none", testCase.name, got)
		}
	}
}

// TestBlendStrokeCircleHalf checks a part-transparent ring: one blended
// colour everywhere it lands, so no pixel is mixed twice, every pixel within
// half the width of the radius, and at widths above one exactly the pixels
// StrokeCircle sets.
func TestBlendStrokeCircleHalf(t *testing.T) {
	t.Parallel()

	const radius = 12

	for _, width := range []int{0, 1, 3, 4} {
		canv := ringCanvas(t)
		canv.BlendStrokeCircle(ringCentre, ringCentre, radius, width, canvas.White, 0.5)
		checkHalfRing(t, canv, radius, width)

		if width > 1 {
			stroked := ringCanvas(t)
			stroked.StrokeCircle(ringCentre, ringCentre, radius, width, canvas.White)

			if blended, set := litPixels(canv), litPixels(stroked); blended != set {
				t.Errorf("width %d: %d pixels blended, StrokeCircle sets %d", width, blended, set)
			}
		}
	}
}

// checkHalfRing fails unless every lit pixel on canv is white mixed in at
// half and lies on the ring.
func checkHalfRing(t *testing.T, canv *canvas.Canvas, radius, width int) {
	t.Helper()

	half := color.RGBA{R: 128, G: 128, B: 128, A: 0xFF}
	reach := float64(max(width, 1))/2 + 1

	for row := range ringSide {
		for column := range ringSide {
			got := canv.Image().RGBAAt(column, row)
			if got == canvas.Black {
				continue
			}

			if got != half {
				t.Fatalf("width %d: pixel (%d, %d) is %v, want %v", width, column, row, got, half)
			}

			off := math.Hypot(float64(column-ringCentre), float64(row-ringCentre)) - float64(radius)
			if math.Abs(off) > reach {
				t.Fatalf("width %d: pixel (%d, %d) is %.1f off the radius", width, column, row, off)
			}
		}
	}
}

// TestBlendStrokeCircleEdges checks a ring hanging off every edge is clipped,
// and that a ring of radius zero is the centre pixel.
func TestBlendStrokeCircleEdges(t *testing.T) {
	t.Parallel()

	for _, centre := range []image.Point{
		{-5, -5}, {ringSide + 4, ringCentre}, {ringCentre, ringSide + 4}, {-3, ringSide},
	} {
		canv := ringCanvas(t)
		canv.BlendStrokeCircle(centre.X, centre.Y, 10, 2, canvas.White, 0.5)

		if litPixels(canv) == 0 {
			t.Errorf("centre %v: nothing of the ring on the canvas", centre)
		}
	}

	dot := ringCanvas(t)
	dot.BlendStrokeCircle(ringCentre, ringCentre, 0, 1, canvas.White, 0.5)

	if got := litPixels(dot); got != 1 || dot.Image().RGBAAt(ringCentre, ringCentre) == canvas.Black {
		t.Errorf("radius zero drew %d pixels, want the centre", got)
	}
}

// TestBlendStrokeCircleAllocs prices a blended ring at zero allocations.
//
//nolint:paralleltest // AllocsPerRun panics when called from a parallel test.
func TestBlendStrokeCircleAllocs(t *testing.T) {
	canv := ringCanvas(t)

	ring := func() { canv.BlendStrokeCircle(ringCentre, ringCentre, 12, 2, canvas.White, 0.5) }

	if got := testing.AllocsPerRun(100, ring); got != 0 {
		t.Errorf("BlendStrokeCircle allocated %.1f times, want 0", got)
	}
}

// litPixels counts the pixels that are not black.
func litPixels(canv *canvas.Canvas) int {
	count := 0

	for row := range ringSide {
		for column := range ringSide {
			if canv.Image().RGBAAt(column, row) != canvas.Black {
				count++
			}
		}
	}

	return count
}
