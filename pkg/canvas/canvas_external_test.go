package canvas_test

import (
	"errors"
	"image"
	"image/color"
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
