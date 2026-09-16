package canvas_test

import (
	"errors"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/pkg/canvas"
)

// assertPixel fails the test if the pixel at (x, y) is not want.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout this package.
func assertPixel(t *testing.T, canv *canvas.Canvas, x, y int, want color.RGBA) {
	t.Helper()

	if got := canv.Image().RGBAAt(x, y); got != want {
		t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, want)
	}
}

// countPixels returns how many pixels on the canvas equal want.
func countPixels(canv *canvas.Canvas, want color.RGBA) int {
	bounds := canv.Bounds()
	count := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) == want {
				count++
			}
		}
	}

	return count
}

// mustCanvas creates a side by side canvas or fails the test immediately.
// The tests below call it instead of checking canvas.New's error inline,
// which keeps their per-case setup down to one line each.
func mustCanvas(t *testing.T, side int) *canvas.Canvas {
	t.Helper()

	canv, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return canv
}

func TestNew(t *testing.T) {
	t.Parallel()

	const (
		validWidth  = 8
		validHeight = 6
	)

	for _, testCase := range []struct {
		name    string
		width   int
		height  int
		wantErr bool
	}{
		{name: "zero width", width: 0, height: validHeight, wantErr: true},
		{name: "zero height", width: validWidth, height: 0, wantErr: true},
		{name: "negative width", width: -validWidth, height: validHeight, wantErr: true},
		{name: "negative height", width: validWidth, height: -validHeight, wantErr: true},
		{name: "valid size", width: validWidth, height: validHeight, wantErr: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(testCase.width, testCase.height)
			if testCase.wantErr {
				if !errors.Is(err, canvas.ErrSize) {
					t.Fatalf("New(%d, %d) error = %v, want ErrSize", testCase.width, testCase.height, err)
				}

				if canv != nil {
					t.Errorf("New(%d, %d) canvas = %v, want nil", testCase.width, testCase.height, canv)
				}

				return
			}

			if err != nil {
				t.Fatalf("New(%d, %d) unexpected error: %v", testCase.width, testCase.height, err)
			}

			if canv == nil {
				t.Fatal("New returned nil canvas with no error")
			}
		})
	}
}

func TestCanvasBounds(t *testing.T) {
	t.Parallel()

	const (
		width  = 7
		height = 5
	)

	canv, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	want := image.Rect(0, 0, width, height)

	if got := canv.Bounds(); got != want {
		t.Errorf("Bounds() = %v, want %v", got, want)
	}
}

func TestCanvasImage(t *testing.T) {
	t.Parallel()

	const side = 4

	canv, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	img := canv.Image()
	if img == nil {
		t.Fatal("Image() returned nil")
	}

	if got := img.Bounds(); got != canv.Bounds() {
		t.Errorf("Image().Bounds() = %v, want %v", got, canv.Bounds())
	}

	// Image aliases the canvas instead of copying it, so a Set call must
	// show up immediately through the *image.RGBA returned earlier.
	canv.Set(1, 1, canvas.Red)

	if got := img.RGBAAt(1, 1); got != canvas.Red {
		t.Errorf("Image() pixel (1, 1) = %v, want %v", got, canvas.Red)
	}
}

func TestCanvasClear(t *testing.T) {
	t.Parallel()

	const (
		side        = 6
		customRed   = 10
		customGreen = 20
		customBlue  = 30
		customAlpha = 40
	)

	for _, testCase := range []struct {
		name string
		fill color.RGBA
	}{
		{name: "black", fill: canvas.Black},
		{name: "custom color", fill: color.RGBA{R: customRed, G: customGreen, B: customBlue, A: customAlpha}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(side, side)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			canv.Clear(testCase.fill)

			bounds := canv.Bounds()
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				for x := bounds.Min.X; x < bounds.Max.X; x++ {
					assertPixel(t, canv, x, y, testCase.fill)
				}
			}
		})
	}
}

func TestCanvasSet(t *testing.T) {
	t.Parallel()

	const side = 5

	for _, testCase := range []struct {
		name        string
		x           int
		y           int
		wantPainted bool
	}{
		{name: "left of canvas", x: -1, y: 0, wantPainted: false},
		{name: "above canvas", x: 0, y: -1, wantPainted: false},
		{name: "right of canvas", x: side, y: 0, wantPainted: false},
		{name: "below canvas", x: 0, y: side, wantPainted: false},
		{name: "in bounds", x: 2, y: 2, wantPainted: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(side, side)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			canv.Set(testCase.x, testCase.y, canvas.Red)

			if testCase.wantPainted {
				assertPixel(t, canv, testCase.x, testCase.y, canvas.Red)

				return
			}

			if got := countPixels(canv, canvas.Red); got != 0 {
				t.Errorf("Set(%d, %d) painted %d pixels, want 0", testCase.x, testCase.y, got)
			}
		})
	}
}

func TestCanvasFillRect(t *testing.T) {
	t.Parallel()

	const side = 8

	for _, testCase := range []struct {
		name      string
		rect      image.Rectangle
		wantCount int
	}{
		{name: "fully outside", rect: image.Rect(side, side, side+1, side+1), wantCount: 0},
		{name: "partially overlapping", rect: image.Rect(side-2, side-2, side+2, side+2), wantCount: 4},
		{name: "fully inside", rect: image.Rect(1, 1, 3, 3), wantCount: 4},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(side, side)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			canv.FillRect(testCase.rect, canvas.Green)

			if got := countPixels(canv, canvas.Green); got != testCase.wantCount {
				t.Errorf("FillRect(%v) painted %d pixels, want %d", testCase.rect, got, testCase.wantCount)
			}
		})
	}
}

// TestCanvasLine covers the basic shapes: flat in each direction, both
// diagonals, and the single-point case that returns on the first iteration.
func TestCanvasLine(t *testing.T) {
	t.Parallel()

	const (
		canvasSide = 6
		lineLength = 4
	)

	for _, testCase := range []struct {
		name      string
		x0        int
		y0        int
		x1        int
		y1        int
		wantCount int
	}{
		{name: "horizontal", x0: 0, y0: 1, x1: 3, y1: 1, wantCount: lineLength},
		{name: "vertical", x0: 1, y0: 0, x1: 1, y1: 3, wantCount: lineLength},
		{name: "diagonal down-right", x0: 0, y0: 0, x1: 3, y1: 3, wantCount: lineLength},
		{name: "diagonal down-left", x0: 3, y0: 0, x1: 0, y1: 3, wantCount: lineLength},
		{name: "single point", x0: 2, y0: 2, x1: 2, y1: 2, wantCount: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(canvasSide, canvasSide)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			canv.Line(testCase.x0, testCase.y0, testCase.x1, testCase.y1, canvas.Red)

			assertPixel(t, canv, testCase.x0, testCase.y0, canvas.Red)
			assertPixel(t, canv, testCase.x1, testCase.y1, canvas.Red)

			if got := countPixels(canv, canvas.Red); got != testCase.wantCount {
				t.Errorf("Line(%d, %d, %d, %d) painted %d pixels, want %d",
					testCase.x0, testCase.y0, testCase.x1, testCase.y1, got, testCase.wantCount)
			}
		})
	}
}

// TestCanvasLineSteepness drives one line where y changes faster than x and
// one where x changes faster than y. Bresenham's two error-term checks
// (e2 >= dy and e2 <= dx) only both fire across a run like this; a pure
// diagonal keeps them in lockstep and would not tell the branches apart.
func TestCanvasLineSteepness(t *testing.T) {
	t.Parallel()

	const (
		canvasSide = 6
		pointCount = 4
	)

	for _, testCase := range []struct {
		name string
		x0   int
		y0   int
		x1   int
		y1   int
	}{
		{name: "steep", x0: 0, y0: 0, x1: 1, y1: 3},
		{name: "shallow", x0: 0, y0: 0, x1: 3, y1: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(canvasSide, canvasSide)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			canv.Line(testCase.x0, testCase.y0, testCase.x1, testCase.y1, canvas.Green)

			assertPixel(t, canv, testCase.x0, testCase.y0, canvas.Green)
			assertPixel(t, canv, testCase.x1, testCase.y1, canvas.Green)

			if got := countPixels(canv, canvas.Green); got != pointCount {
				t.Errorf("Line(%d, %d, %d, %d) painted %d pixels, want %d",
					testCase.x0, testCase.y0, testCase.x1, testCase.y1, got, pointCount)
			}
		})
	}
}

// TestCanvasLineOffCanvas runs a line whose endpoints both sit outside the
// canvas, with only its middle stretch crossing the drawable area. Set
// clips silently, so this checks that the visible stretch still lands
// correctly rather than the call panicking or drawing nothing at all.
func TestCanvasLineOffCanvas(t *testing.T) {
	t.Parallel()

	const (
		canvasSide  = 2
		wantVisible = 2
	)

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	canv.Line(-1, 0, canvasSide, 0, canvas.Red)

	assertPixel(t, canv, 0, 0, canvas.Red)
	assertPixel(t, canv, 1, 0, canvas.Red)

	if got := countPixels(canv, canvas.Red); got != wantVisible {
		t.Errorf("Line painted %d pixels, want %d", got, wantVisible)
	}
}

// TestCanvasCircle uses radius 3, which is the smallest radius that drives
// the midpoint algorithm through both its continue branch (err < 0) and its
// x-- branch, so one call exercises the whole loop body.
func TestCanvasCircle(t *testing.T) {
	t.Parallel()

	const (
		canvasSide = 10
		centerX    = 5
		centerY    = 5
		radius     = 3
		wantCount  = 16
	)

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	canv.Circle(centerX, centerY, radius, canvas.Cyan)

	assertPixel(t, canv, centerX+radius, centerY, canvas.Cyan)
	assertPixel(t, canv, centerX-radius, centerY, canvas.Cyan)
	assertPixel(t, canv, centerX, centerY+radius, canvas.Cyan)
	assertPixel(t, canv, centerX, centerY-radius, canvas.Cyan)
	assertPixel(t, canv, centerX, centerY, color.RGBA{})

	if got := countPixels(canv, canvas.Cyan); got != wantCount {
		t.Errorf("Circle painted %d pixels, want %d", got, wantCount)
	}
}

// TestCanvasCircleAndFillCircleDegenerate covers the negative-radius and
// zero-radius cases for both circle methods. They share the same two
// outcomes (nothing drawn, centre pixel only), so one table drives both.
func TestCanvasCircleAndFillCircleDegenerate(t *testing.T) {
	t.Parallel()

	const (
		canvasSide       = 10
		centerX, centerY = 5, 5
	)

	for _, shape := range []struct {
		name string
		draw func(*canvas.Canvas, int, int, int, color.RGBA)
	}{
		{name: "Circle", draw: (*canvas.Canvas).Circle},
		{name: "FillCircle", draw: (*canvas.Canvas).FillCircle},
	} {
		for _, testCase := range []struct {
			name      string
			radius    int
			wantCount int
		}{
			{name: "negative radius", radius: -1, wantCount: 0},
			{name: "zero radius", radius: 0, wantCount: 1},
		} {
			t.Run(shape.name+"/"+testCase.name, func(t *testing.T) {
				t.Parallel()

				canv, err := canvas.New(canvasSide, canvasSide)
				if err != nil {
					t.Fatalf("New: %v", err)
				}

				shape.draw(canv, centerX, centerY, testCase.radius, canvas.Magenta)

				if got := countPixels(canv, canvas.Magenta); got != testCase.wantCount {
					t.Errorf("%s radius %d painted %d pixels, want %d",
						shape.name, testCase.radius, got, testCase.wantCount)
				}
			})
		}
	}
}

// TestCanvasCircleNearEdge centres the circle on the corner of the canvas so
// half of every octant point falls outside it and gets clipped by Set.
func TestCanvasCircleNearEdge(t *testing.T) {
	t.Parallel()

	const (
		canvasSide = 6
		radius     = 3
	)

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	canv.Circle(0, 0, radius, canvas.Yellow)

	assertPixel(t, canv, radius, 0, canvas.Yellow)
	assertPixel(t, canv, 0, radius, canvas.Yellow)
}

// TestCanvasFillCircle checks the centre pixel and the pixel just past the
// radius on the same row, then confirms the full disc area against the
// row-by-row sum FillCircle's own doc comment describes.
func TestCanvasFillCircle(t *testing.T) {
	t.Parallel()

	const (
		canvasSide = 12
		centerX    = 6
		centerY    = 6
		radius     = 3
		wantCount  = 29
	)

	canv, err := canvas.New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	canv.FillCircle(centerX, centerY, radius, canvas.White)

	assertPixel(t, canv, centerX, centerY, canvas.White)
	assertPixel(t, canv, centerX+radius+1, centerY, color.RGBA{})

	if got := countPixels(canv, canvas.White); got != wantCount {
		t.Errorf("FillCircle painted %d pixels, want %d", got, wantCount)
	}
}

func TestPalette(t *testing.T) {
	t.Parallel()

	const full = 0xFF

	for _, testCase := range []struct {
		name string
		got  color.RGBA
		want color.RGBA
	}{
		{name: "black", got: canvas.Black, want: color.RGBA{R: 0, G: 0, B: 0, A: full}},
		{name: "white", got: canvas.White, want: color.RGBA{R: full, G: full, B: full, A: full}},
		{name: "red", got: canvas.Red, want: color.RGBA{R: full, G: 0, B: 0, A: full}},
		{name: "green", got: canvas.Green, want: color.RGBA{R: 0, G: full, B: 0, A: full}},
		{name: "blue", got: canvas.Blue, want: color.RGBA{R: 0, G: 0, B: full, A: full}},
		{name: "yellow", got: canvas.Yellow, want: color.RGBA{R: full, G: full, B: 0, A: full}},
		{name: "magenta", got: canvas.Magenta, want: color.RGBA{R: full, G: 0, B: full, A: full}},
		{name: "cyan", got: canvas.Cyan, want: color.RGBA{R: 0, G: full, B: full, A: full}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.got != testCase.want {
				t.Errorf("%s = %v, want %v", testCase.name, testCase.got, testCase.want)
			}

			if testCase.got.A != full {
				t.Errorf("%s alpha = %#x, want %#x", testCase.name, testCase.got.A, full)
			}
		})
	}
}

func BenchmarkClear(b *testing.B) {
	const side = 256

	canv, err := canvas.New(side, side)
	if err != nil {
		b.Fatalf("New: %v", err)
	}

	b.ReportAllocs()

	for b.Loop() {
		canv.Clear(canvas.Black)
	}
}

func BenchmarkLine(b *testing.B) {
	const side = 256

	canv, err := canvas.New(side, side)
	if err != nil {
		b.Fatalf("New: %v", err)
	}

	b.ReportAllocs()

	for b.Loop() {
		canv.Line(0, 0, side-1, side-1, canvas.White)
	}
}

// TestCanvasBlend covers bounds clipping, the NaN/negative/overflow ends of
// the alpha clamp, and the two edges the doc comment promises: alpha 1
// matching Set exactly and alpha 0 leaving the pixel completely untouched.
func TestCanvasBlend(t *testing.T) {
	t.Parallel()

	const (
		side       = 5
		targetX    = 2
		targetY    = 2
		overAlpha  = 1.5
		underAlpha = -0.5
	)

	base := color.RGBA{R: 20, G: 40, B: 60, A: 0xFF}
	full := color.RGBA{R: 200, G: 150, B: 100, A: 0xFF}

	for _, testCase := range []struct {
		name  string
		x     int
		y     int
		alpha float64
		want  color.RGBA
	}{
		{name: "out of bounds is dropped", x: side, y: targetY, alpha: 1, want: base},
		{name: "alpha one matches Set", x: targetX, y: targetY, alpha: 1, want: full},
		{name: "alpha zero is untouched", x: targetX, y: targetY, alpha: 0, want: base},
		{name: "negative alpha clamps to untouched", x: targetX, y: targetY, alpha: underAlpha, want: base},
		{name: "NaN alpha is untouched", x: targetX, y: targetY, alpha: math.NaN(), want: base},
		{
			name: "alpha above one clamps to Set", x: targetX, y: targetY, alpha: overAlpha, want: full,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv := mustCanvas(t, side)

			canv.Set(targetX, targetY, base)
			canv.Blend(testCase.x, testCase.y, full, testCase.alpha)

			assertPixel(t, canv, targetX, targetY, testCase.want)
		})
	}
}

// TestCanvasBlendHalfway checks the interpolated middle of the alpha range,
// which the boundary cases in TestCanvasBlend do not exercise.
func TestCanvasBlendHalfway(t *testing.T) {
	t.Parallel()

	const (
		side       = 4
		startValue = 0
		endValue   = 100
		halfAlpha  = 0.5
		wantValue  = 50
	)

	canv := mustCanvas(t, side)

	canv.Set(0, 0, color.RGBA{R: startValue, G: startValue, B: startValue, A: 0xFF})
	canv.Blend(0, 0, color.RGBA{R: endValue, G: endValue, B: endValue, A: 0xFF}, halfAlpha)

	assertPixel(t, canv, 0, 0, color.RGBA{R: wantValue, G: wantValue, B: wantValue, A: 0xFF})
}

// TestCanvasLineAA covers the shapes LineAA's doc comment makes promises
// about: it draws something, an off-axis line lands in more than one
// column, a zero-length line still paints its single pixel, and a bad
// coordinate is rejected outright.
func TestCanvasLineAA(t *testing.T) {
	t.Parallel()

	const side = 20

	for _, testCase := range []struct {
		name      string
		x0, y0    float64
		x1, y1    float64
		wantCount bool
	}{
		{name: "diagonal draws pixels", x0: 1, y0: 1, x1: 10, y1: 6, wantCount: true},
		{name: "steep draws pixels", x0: 1, y0: 1, x1: 6, y1: 10, wantCount: true},
		{name: "zero length draws one pixel", x0: 5, y0: 5, x1: 5, y1: 5, wantCount: true},
		{name: "NaN coordinate draws nothing", x0: math.NaN(), y0: 1, x1: 10, y1: 6, wantCount: false},
		{name: "infinite coordinate draws nothing", x0: 1, y0: math.Inf(1), x1: 10, y1: 6, wantCount: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv := mustCanvas(t, side)

			canv.LineAA(testCase.x0, testCase.y0, testCase.x1, testCase.y1, canvas.White)

			got := countPixels(canv, color.RGBA{}) != side*side
			if got != testCase.wantCount {
				t.Errorf("LineAA(%v, %v, %v, %v) painted something = %v, want %v",
					testCase.x0, testCase.y0, testCase.x1, testCase.y1, got, testCase.wantCount)
			}
		})
	}
}

// TestCanvasLineAASymmetric draws the same line with its endpoints swapped
// on two separate canvases and requires byte-identical images, since the
// normalisation LineAA does internally is only useful if it actually holds.
func TestCanvasLineAASymmetric(t *testing.T) {
	t.Parallel()

	const (
		side           = 16
		startX, startY = 2.0, 3.0
		endX, endY     = 13.0, 9.0
	)

	forward := mustCanvas(t, side)
	reversed := mustCanvas(t, side)

	forward.LineAA(startX, startY, endX, endY, canvas.Green)
	reversed.LineAA(endX, endY, startX, startY, canvas.Green)

	for y := range side {
		for x := range side {
			if got, want := forward.Image().RGBAAt(x, y), reversed.Image().RGBAAt(x, y); got != want {
				t.Errorf("pixel (%d, %d) = %v, want %v (reversed endpoints)", x, y, got, want)
			}
		}
	}
}

// TestCanvasLineAAHorizontal draws a horizontal line at an integer row and
// requires full coverage on that row and nothing at all on the rows either
// side of it, which is only true if the fractional part of the
// interpolated y stays exactly zero all the way across.
func TestCanvasLineAAHorizontal(t *testing.T) {
	t.Parallel()

	const (
		side   = 24
		startX = 2
		endX   = 20
		row    = 5
	)

	canv := mustCanvas(t, side)

	canv.LineAA(startX, row, endX, row, canvas.White)

	for x := startX; x <= endX; x++ {
		assertPixel(t, canv, x, row, canvas.White)
		assertPixel(t, canv, x, row-1, color.RGBA{})
		assertPixel(t, canv, x, row+1, color.RGBA{})
	}
}

// TestLineAAAllocs does not call t.Parallel: testing.AllocsPerRun panics if
// it runs while the test is marked parallel, since it needs the runtime's
// undivided attention to count allocations accurately.
//
//nolint:paralleltest // AllocsPerRun forbids running as a parallel test; see comment above.
func TestLineAAAllocs(t *testing.T) {
	const side = 64

	canv := mustCanvas(t, side)

	allocs := testing.AllocsPerRun(100, func() {
		canv.LineAA(0, 0, side-1, side-1, canvas.White)
	})

	if allocs != 0 {
		t.Errorf("LineAA allocated %v times per call, want 0", allocs)
	}
}

func BenchmarkLineAA(b *testing.B) {
	const side = 256

	canv, err := canvas.New(side, side)
	if err != nil {
		b.Fatalf("New: %v", err)
	}

	b.ReportAllocs()

	for b.Loop() {
		canv.LineAA(0, 0, side-1, side-1, canvas.White)
	}
}

// TestCanvasDashedCircle covers the four specified behaviours: a negative
// radius or non-positive dash draws nothing, a non-positive gap falls back
// to a solid Circle, and otherwise the dashes cover strictly less area than
// the solid circle they are broken from.
func TestCanvasDashedCircle(t *testing.T) {
	t.Parallel()

	const (
		side    = 30
		centerX = 15
		centerY = 15
		radius  = 10
		dash    = 3
		gap     = 2
	)

	t.Run("negative radius draws nothing", func(t *testing.T) {
		t.Parallel()

		canv := mustCanvas(t, side)

		canv.DashedCircle(centerX, centerY, -1, dash, gap, canvas.Red)

		if got := countPixels(canv, canvas.Red); got != 0 {
			t.Errorf("DashedCircle painted %d pixels, want 0", got)
		}
	})

	t.Run("non-positive dash draws nothing", func(t *testing.T) {
		t.Parallel()

		canv := mustCanvas(t, side)

		canv.DashedCircle(centerX, centerY, radius, 0, gap, canvas.Red)

		if got := countPixels(canv, canvas.Red); got != 0 {
			t.Errorf("DashedCircle painted %d pixels, want 0", got)
		}
	})

	t.Run("non-positive gap matches solid Circle", func(t *testing.T) {
		t.Parallel()

		dashed := mustCanvas(t, side)
		solid := mustCanvas(t, side)

		dashed.DashedCircle(centerX, centerY, radius, dash, 0, canvas.Cyan)
		solid.Circle(centerX, centerY, radius, canvas.Cyan)

		for y := range side {
			for x := range side {
				if got, want := dashed.Image().RGBAAt(x, y), solid.Image().RGBAAt(x, y); got != want {
					t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, want)
				}
			}
		}
	})

	t.Run("zero radius draws only the centre pixel", func(t *testing.T) {
		t.Parallel()

		canv := mustCanvas(t, side)

		canv.DashedCircle(centerX, centerY, 0, dash, gap, canvas.Yellow)

		assertPixel(t, canv, centerX, centerY, canvas.Yellow)

		if got := countPixels(canv, canvas.Yellow); got != 1 {
			t.Errorf("DashedCircle painted %d pixels, want 1", got)
		}
	})
}

// TestCanvasDashedCircleCoverage is the other half of DashedCircle's cases,
// split out because six subtests in one function push revive's
// cognitive-complexity limit.
func TestCanvasDashedCircleCoverage(t *testing.T) {
	t.Parallel()

	const (
		side    = 30
		centerX = 15
		centerY = 15
		radius  = 10
		dash    = 3
		gap     = 2
	)

	t.Run("dashes cover strictly less than a solid circle", func(t *testing.T) {
		t.Parallel()

		dashed := mustCanvas(t, side)
		solid := mustCanvas(t, side)

		dashed.DashedCircle(centerX, centerY, radius, dash, gap, canvas.Magenta)
		solid.Circle(centerX, centerY, radius, canvas.Magenta)

		dashedCount := countPixels(dashed, canvas.Magenta)
		solidCount := countPixels(solid, canvas.Magenta)

		if dashedCount == 0 {
			t.Fatal("DashedCircle painted 0 pixels, want more than 0")
		}

		if dashedCount >= solidCount {
			t.Errorf("DashedCircle painted %d pixels, want fewer than the solid circle's %d", dashedCount, solidCount)
		}
	})

	t.Run("identical arguments produce identical images", func(t *testing.T) {
		t.Parallel()

		first := mustCanvas(t, side)
		second := mustCanvas(t, side)

		first.DashedCircle(centerX, centerY, radius, dash, gap, canvas.Blue)
		second.DashedCircle(centerX, centerY, radius, dash, gap, canvas.Blue)

		for y := range side {
			for x := range side {
				if got, want := first.Image().RGBAAt(x, y), second.Image().RGBAAt(x, y); got != want {
					t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, want)
				}
			}
		}
	})
}

// TestCanvasFillTriangle covers a right triangle's known pixel count, its
// vertices being filled, a point outside it being left alone, both winding
// orders producing the same image, a fully off-canvas triangle leaving the
// canvas untouched, and a degenerate triangle not panicking.
func TestCanvasFillTriangle(t *testing.T) {
	t.Parallel()

	const (
		side = 12
		leg  = 5
		// A right triangle with legs of length `leg` along the axes covers
		// (leg+1)*(leg+2)/2 pixels: leg+1 rows, each one pixel longer than
		// the last starting from a single pixel at the apex.
		wantCount = (leg + 1) * (leg + 2) / 2
	)

	t.Run("right triangle covers the expected area", func(t *testing.T) {
		t.Parallel()

		canv := mustCanvas(t, side)

		canv.FillTriangle(0, 0, leg, 0, 0, leg, canvas.Red)

		if got := countPixels(canv, canvas.Red); got != wantCount {
			t.Errorf("FillTriangle painted %d pixels, want %d", got, wantCount)
		}

		assertPixel(t, canv, 0, 0, canvas.Red)
		assertPixel(t, canv, leg, 0, canvas.Red)
		assertPixel(t, canv, 0, leg, canvas.Red)
		assertPixel(t, canv, leg, leg, color.RGBA{})
	})

	t.Run("reversed winding order matches", func(t *testing.T) {
		t.Parallel()

		clockwise := mustCanvas(t, side)
		counterClockwise := mustCanvas(t, side)

		clockwise.FillTriangle(0, 0, leg, 0, 0, leg, canvas.Green)
		counterClockwise.FillTriangle(0, leg, leg, 0, 0, 0, canvas.Green)

		for y := range side {
			for x := range side {
				got, want := clockwise.Image().RGBAAt(x, y), counterClockwise.Image().RGBAAt(x, y)
				if got != want {
					t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, want)
				}
			}
		}
	})

	t.Run("fully off canvas changes nothing", func(t *testing.T) {
		t.Parallel()

		canv := mustCanvas(t, side)

		canv.FillTriangle(side, side, side+leg, side, side, side+leg, canvas.Blue)

		if got := countPixels(canv, canvas.Blue); got != 0 {
			t.Errorf("FillTriangle painted %d pixels, want 0", got)
		}
	})

	t.Run("degenerate collinear triangle does not panic", func(t *testing.T) {
		t.Parallel()

		canv := mustCanvas(t, side)

		canv.FillTriangle(0, 0, leg, leg, 2*leg, 2*leg, canvas.Yellow)
	})

	t.Run("degenerate identical vertices does not panic", func(t *testing.T) {
		t.Parallel()

		canv := mustCanvas(t, side)

		canv.FillTriangle(leg, leg, leg, leg, leg, leg, canvas.Yellow)
	})
}

// TestCanvasRect covers the corners of an outlined rectangle, the interior
// staying untouched, the empty and 1x1 edge cases, and clipping when the
// rectangle runs off the canvas.
func TestCanvasRect(t *testing.T) {
	t.Parallel()

	const side = 12

	t.Run("corners are set and interior is untouched", func(t *testing.T) {
		t.Parallel()

		const (
			minX, minY = 2, 3
			maxX, maxY = 8, 9
		)

		canv := mustCanvas(t, side)

		canv.Rect(image.Rect(minX, minY, maxX, maxY), canvas.White)

		assertPixel(t, canv, minX, minY, canvas.White)
		assertPixel(t, canv, maxX-1, minY, canvas.White)
		assertPixel(t, canv, minX, maxY-1, canvas.White)
		assertPixel(t, canv, maxX-1, maxY-1, canvas.White)
		assertPixel(t, canv, (minX+maxX-1)/2, (minY+maxY-1)/2, color.RGBA{})
	})

	t.Run("empty rectangle draws nothing", func(t *testing.T) {
		t.Parallel()

		canv := mustCanvas(t, side)

		canv.Rect(image.Rect(2, 2, 2, 2), canvas.Red)

		if got := countPixels(canv, canvas.Red); got != 0 {
			t.Errorf("Rect painted %d pixels, want 0", got)
		}
	})

	t.Run("1x1 rectangle sets exactly one pixel", func(t *testing.T) {
		t.Parallel()

		canv := mustCanvas(t, side)

		canv.Rect(image.Rect(4, 4, 5, 5), canvas.Green)

		assertPixel(t, canv, 4, 4, canvas.Green)

		if got := countPixels(canv, canvas.Green); got != 1 {
			t.Errorf("Rect painted %d pixels, want 1", got)
		}
	})

	t.Run("partly off canvas clips without panic", func(t *testing.T) {
		t.Parallel()

		canv := mustCanvas(t, side)

		canv.Rect(image.Rect(side-2, side-2, side+4, side+4), canvas.Yellow)

		assertPixel(t, canv, side-2, side-2, canvas.Yellow)
	})
}

// TestSubSharesThePixels checks the point of a sub-canvas: it is a window onto
// the parent in the parent's own coordinates, not a copy and not a translation.
func TestSubSharesThePixels(t *testing.T) {
	t.Parallel()

	const (
		side    = 20
		boxLeft = 5
		boxTop  = 6
		boxSide = 8
	)

	parent, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	parent.Clear(canvas.Black)

	box := image.Rect(boxLeft, boxTop, boxLeft+boxSide, boxTop+boxSide)

	window, ok := parent.Sub(box)
	if !ok {
		t.Fatalf("Sub(%v) reported no room, want a window", box)
	}

	if window.Bounds() != box {
		t.Errorf("Sub(%v).Bounds() = %v, want the box itself", box, window.Bounds())
	}

	window.Set(boxLeft, boxTop, canvas.White)
	assertPixel(t, parent, boxLeft, boxTop, canvas.White)

	// A pixel outside the window is dropped rather than wrapped into it, which
	// is what stops a scene drawing over the block beside its own.
	window.Set(boxLeft-1, boxTop, canvas.Red)
	assertPixel(t, parent, boxLeft-1, boxTop, canvas.Black)

	window.Set(box.Max.X, boxTop, canvas.Red)
	assertPixel(t, parent, box.Max.X, boxTop, canvas.Black)
}

// TestSubClearsOnlyItsOwnBox checks the trap a sub-canvas sets for Clear: the
// stride belongs to the parent, so filling by stride would paint across whole
// parent rows instead of the window.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout this package.
func TestSubClearsOnlyItsOwnBox(t *testing.T) {
	t.Parallel()

	const (
		side    = 16
		boxLeft = 4
		boxTop  = 4
		boxSide = 6
	)

	parent, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	parent.Clear(canvas.Black)

	box := image.Rect(boxLeft, boxTop, boxLeft+boxSide, boxTop+boxSide)

	window, ok := parent.Sub(box)
	if !ok {
		t.Fatalf("Sub(%v) reported no room, want a window", box)
	}

	window.Clear(canvas.White)

	for y := range side {
		for x := range side {
			want := canvas.Black
			if image.Pt(x, y).In(box) {
				want = canvas.White
			}

			assertPixel(t, parent, x, y, want)
		}
	}
}

// TestSubTrimsAndRefuses checks the two edges of Sub's contract: a box hanging
// off the canvas is trimmed to what overlaps, and one that overlaps nothing is
// refused rather than handed back empty.
func TestSubTrimsAndRefuses(t *testing.T) {
	t.Parallel()

	const side = 10

	parent, err := canvas.New(side, side)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	for _, testCase := range []struct {
		name   string
		box    image.Rectangle
		want   image.Rectangle
		wantOK bool
	}{
		{
			name: "a box hanging off two edges is trimmed",
			box:  image.Rect(-4, -4, 4, 4), want: image.Rect(0, 0, 4, 4), wantOK: true,
		},
		{name: "a box entirely outside is refused", box: image.Rect(side*2, side*2, side*3, side*3)},
		{name: "an empty box is refused", box: image.Rectangle{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			window, ok := parent.Sub(testCase.box)
			if ok != testCase.wantOK {
				t.Fatalf("Sub(%v) ok = %v, want %v", testCase.box, ok, testCase.wantOK)
			}

			if !ok {
				return
			}

			if window.Bounds() != testCase.want {
				t.Errorf("Sub(%v).Bounds() = %v, want %v", testCase.box, window.Bounds(), testCase.want)
			}
		})
	}
}
