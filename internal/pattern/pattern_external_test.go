package pattern_test

import (
	"bytes"
	"image/color"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/pattern"
	"github.com/hyperized/uScope/pkg/canvas"
)

// assertPixel fails the test if the pixel at (x, y) is not want. what names
// what the pixel represents, so a failure says which check broke.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout this file.
func assertPixel(t *testing.T, canv *canvas.Canvas, x, y int, want color.RGBA, what string) {
	t.Helper()

	if got := canv.Image().RGBAAt(x, y); got != want {
		t.Errorf("%s: pixel (%d, %d) = %v, want %v", what, x, y, got, want)
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("returns a scene", func(t *testing.T) {
		t.Parallel()

		if scene := pattern.New(); scene == nil {
			t.Fatal("New() = nil, want a scene")
		}
	})

	t.Run("two calls are independent", func(t *testing.T) {
		t.Parallel()

		const width, height = 16, 16

		first := pattern.New()
		second := pattern.New()

		if first == second {
			t.Fatal("New() returned the same instance twice, want distinct scenes")
		}

		canvasA, err := canvas.New(width, height)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		canvasB, err := canvas.New(width, height)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		first.Draw(canvasA, 0)
		second.Draw(canvasB, 0)

		if !bytes.Equal(canvasA.Image().Pix, canvasB.Image().Pix) {
			t.Error("two independent scenes drew different frames for the same input")
		}
	})
}

// TestSceneDrawCornerColours pins the four corners on a real 1280x720 frame,
// the logical size of the device panel.
//
// The border is drawn before the corner squares, so the squares would win
// outright, except the diagonal is drawn after the corner squares and its
// two endpoints are exactly the top-left and bottom-right corners. Those two
// pixels end up white, not the square colour. The other two corners are
// never touched by the diagonal and keep their square colour.
func TestSceneDrawCornerColours(t *testing.T) {
	t.Parallel()

	const (
		width        = 1280
		height       = 720
		insideOffset = 40
	)

	canv, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pattern.New().Draw(canv, 0)

	for _, testCase := range []struct {
		name string
		x    int
		y    int
		want color.RGBA
	}{
		{name: "top-left corner is white, the diagonal wins over red", x: 0, y: 0, want: canvas.White},
		{name: "top-right corner keeps the green square", x: width - 1, y: 0, want: canvas.Green},
		{name: "bottom-left corner keeps the blue square", x: 0, y: height - 1, want: canvas.Blue},
		{
			name: "bottom-right corner is white, the diagonal wins over yellow",
			x:    width - 1, y: height - 1, want: canvas.White,
		},
		{name: "top-left square interior is red", x: insideOffset, y: insideOffset, want: canvas.Red},
		{name: "top-right square interior is green", x: width - insideOffset, y: insideOffset, want: canvas.Green},
		{name: "bottom-left square interior is blue", x: insideOffset, y: height - insideOffset, want: canvas.Blue},
		{
			name: "bottom-right square interior is yellow",
			x:    width - insideOffset, y: height - insideOffset, want: canvas.Yellow,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assertPixel(t, canv, testCase.x, testCase.y, testCase.want, testCase.name)
		})
	}
}

func TestSceneDrawBorder(t *testing.T) {
	t.Parallel()

	const (
		width  = 1280
		height = 720
	)

	canv, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pattern.New().Draw(canv, 0)

	for _, testCase := range []struct {
		name string
		x    int
		y    int
	}{
		{name: "top edge midpoint", x: width / 2, y: 0},
		{name: "bottom edge midpoint", x: width / 2, y: height - 1},
		{name: "left edge midpoint", x: 0, y: height / 2},
		{name: "right edge midpoint", x: width - 1, y: height / 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assertPixel(t, canv, testCase.x, testCase.y, canvas.White, testCase.name)
		})
	}
}

func TestSceneDrawDiagonal(t *testing.T) {
	t.Parallel()

	const (
		width  = 1280
		height = 720
		pointX = 200
		// pointY follows the diagonal's slope from (0, 0) to (width-1,
		// height-1). Bresenham rounding can shift the exact path by a
		// pixel, so this point was checked against the real frame rather
		// than assumed: it sits well clear of the corner squares, the
		// triangle, the ring and the sweep needle's column.
		pointY = pointX * (height - 1) / (width - 1)
	)

	canv, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pattern.New().Draw(canv, 0)

	assertPixel(t, canv, pointX, pointY, canvas.White, "point on the diagonal")
}

func TestSceneDrawRing(t *testing.T) {
	t.Parallel()

	const (
		width       = 1280
		height      = 720
		centerX     = width / 2
		centerY     = height / 2
		sweepPeriod = 4 * time.Second
		offCenter   = 100
		offRow      = 30
	)

	radius := min(width, height) / 3

	canv, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// The sweep needle is drawn after the ring and, at elapsed 0, points
	// straight up to exactly the ring point this test checks. A quarter
	// turn moves the needle aside so the pixel reflects the ring, not the
	// needle.
	pattern.New().Draw(canv, sweepPeriod/4)

	assertPixel(t, canv, centerX, centerY-radius, canvas.Magenta, "ring point directly above centre")
	assertPixel(t, canv, centerX-offCenter, centerY+offRow, canvas.Black, "inside the ring, off the diagonal and sweep")
}

func TestSceneDrawTriangleOrientation(t *testing.T) {
	t.Parallel()

	const (
		width            = 1280
		height           = 720
		centerX          = width / 2
		apexInset        = 20
		triangleHeight   = 60
		triangleHalfBase = 40
		baseY            = apexInset + triangleHeight
	)

	canv, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pattern.New().Draw(canv, 0)

	assertPixel(t, canv, centerX, apexInset, canvas.Cyan, "triangle apex")
	assertPixel(t, canv, centerX+triangleHalfBase+1, baseY, canvas.Black, "just outside the base half width")

	// If the triangle were drawn upside down, this mirrored offset from
	// the bottom edge would be cyan instead of the apex position near the
	// top. This is the property that actually tells the user which way is
	// up.
	assertPixel(t, canv, centerX, height-1-apexInset, canvas.Black, "mirrored apex position in the bottom half")

	t.Run("base row is cyan across its half width", func(t *testing.T) {
		t.Parallel()

		for x := centerX - triangleHalfBase; x <= centerX+triangleHalfBase; x++ {
			assertPixel(t, canv, x, baseY, canvas.Cyan, "base row")
		}
	})
}

func TestSceneDrawSweep(t *testing.T) {
	t.Parallel()

	const (
		width       = 1280
		height      = 720
		centerX     = width / 2
		centerY     = height / 2
		sweepPeriod = 4 * time.Second
	)

	radius := min(width, height) / 3

	zero, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pattern.New().Draw(zero, 0)
	assertPixel(t, zero, centerX, centerY-radius/2, canvas.White, "sweep needle at elapsed 0, pointing up")

	quarter, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pattern.New().Draw(quarter, sweepPeriod/4)

	if bytes.Equal(zero.Image().Pix, quarter.Image().Pix) {
		t.Error("frame at a quarter turn equals the frame at elapsed 0, want the needle to have moved")
	}

	full, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pattern.New().Draw(full, sweepPeriod)

	if !bytes.Equal(zero.Image().Pix, full.Image().Pix) {
		t.Error("frame after one full turn differs from the frame at elapsed 0, want them identical")
	}
}

// TestSceneDrawNegativeElapsed covers the phase < 0 branch in drawSweep: a
// negative elapsed must fold forward into the period instead of mirroring
// the sweep, landing on the same phase as its positive equivalent one
// period later.
func TestSceneDrawNegativeElapsed(t *testing.T) {
	t.Parallel()

	const (
		width       = 1280
		height      = 720
		sweepPeriod = 4 * time.Second
	)

	negative, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pattern.New().Draw(negative, -sweepPeriod/4)

	folded, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pattern.New().Draw(folded, 3*sweepPeriod/4)

	if !bytes.Equal(negative.Image().Pix, folded.Image().Pix) {
		t.Error("Draw(-sweepPeriod/4) differs from Draw(3*sweepPeriod/4), want the negative elapsed to fold")
	}
}

func TestSceneDrawSmallCanvas(t *testing.T) {
	t.Parallel()

	t.Run("10x10 canvas does not panic and paints sane pixels", func(t *testing.T) {
		t.Parallel()

		const side = 10

		canv, err := canvas.New(side, side)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		pattern.New().Draw(canv, 0)

		// cornerSize (80) is bigger than the canvas, so all four corner
		// squares cover it fully, last one drawn wins, and the diagonal
		// runs exactly corner to corner because the canvas is square.
		assertPixel(t, canv, 0, 0, canvas.White, "top-left corner, on the diagonal")
		assertPixel(t, canv, side-1, side-1, canvas.White, "bottom-right corner, on the diagonal")
		assertPixel(t, canv, side-1, 0, canvas.Yellow, "top-right corner, off the diagonal")
		assertPixel(t, canv, 0, side-1, canvas.Yellow, "bottom-left corner, off the diagonal")
	})

	t.Run("1x1 canvas does not panic and paints a sane pixel", func(t *testing.T) {
		t.Parallel()

		canv, err := canvas.New(1, 1)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		pattern.New().Draw(canv, 0)

		// Every shape collapses onto the single pixel; the sweep line is
		// drawn last, so its colour, white, is what remains.
		assertPixel(t, canv, 0, 0, canvas.White, "the only pixel")
	})
}

func BenchmarkDraw(b *testing.B) {
	const (
		width  = 1280
		height = 720
	)

	canv, err := canvas.New(width, height)
	if err != nil {
		b.Fatalf("New: %v", err)
	}

	scene := pattern.New()

	b.ReportAllocs()

	for range b.N {
		scene.Draw(canv, 0)
	}
}
