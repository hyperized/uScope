package canvas

import (
	"image"
	"image/color"
	"testing"
)

// assertPixelAt fails the test if the pixel at (x, y) is not want.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout this package.
func assertPixelAt(t *testing.T, canv *Canvas, x, y int, want color.RGBA) {
	t.Helper()

	if got := canv.img.RGBAAt(x, y); got != want {
		t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, want)
	}
}

func TestAbs(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		in   int
		want int
	}{
		{name: "negative", in: -5, want: 5},
		{name: "zero", in: 0, want: 0},
		{name: "positive", in: 5, want: 5},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := abs(testCase.in); got != testCase.want {
				t.Errorf("abs(%d) = %d, want %d", testCase.in, got, testCase.want)
			}
		})
	}
}

func TestStep(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		from int
		to   int
		want int
	}{
		{name: "from greater than to", from: 5, to: 1, want: -1},
		{name: "from less than to", from: 1, to: 5, want: 1},
		// from == to takes the same branch as from < to; Bresenham relies
		// on this to keep walking a perfectly horizontal or vertical line.
		{name: "from equal to destination", from: 3, to: 3, want: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := step(testCase.from, testCase.to); got != testCase.want {
				t.Errorf("step(%d, %d) = %d, want %d", testCase.from, testCase.to, got, testCase.want)
			}
		})
	}
}

func TestHline(t *testing.T) {
	t.Parallel()

	const (
		canvasSide = 10
		lineStart  = 2
		lineEnd    = 6
		lineRow    = 4
	)

	canv, err := New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	canv.hline(lineStart, lineEnd, lineRow, Red)

	for x := lineStart; x <= lineEnd; x++ {
		assertPixelAt(t, canv, x, lineRow, Red)
	}

	assertPixelAt(t, canv, lineStart-1, lineRow, color.RGBA{})
	assertPixelAt(t, canv, lineEnd+1, lineRow, color.RGBA{})
}

func TestOctants(t *testing.T) {
	t.Parallel()

	const (
		canvasSide = 20
		centerX    = 10
		centerY    = 10
		offsetX    = 4
		offsetY    = 2
	)

	canv, err := New(canvasSide, canvasSide)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	canv.octants(centerX, centerY, offsetX, offsetY, Blue)

	for _, point := range []struct{ x, y int }{
		{x: centerX + offsetX, y: centerY + offsetY},
		{x: centerX - offsetX, y: centerY + offsetY},
		{x: centerX + offsetX, y: centerY - offsetY},
		{x: centerX - offsetX, y: centerY - offsetY},
		{x: centerX + offsetY, y: centerY + offsetX},
		{x: centerX - offsetY, y: centerY + offsetX},
		{x: centerX + offsetY, y: centerY - offsetX},
		{x: centerX - offsetY, y: centerY - offsetX},
	} {
		assertPixelAt(t, canv, point.x, point.y, Blue)
	}
}

// TestCanvasClearEmptyPix reaches the early return in Clear for a Canvas
// whose Pix slice is empty. New can never build one this way (width and
// height both have to be positive), so the zero-area image is built by
// hand here instead.
func TestCanvasClearEmptyPix(t *testing.T) {
	t.Parallel()

	canv := Canvas{img: image.NewRGBA(image.Rect(0, 0, 0, 0))}

	canv.Clear(Red)

	if got := len(canv.img.Pix); got != 0 {
		t.Errorf("len(Pix) = %d, want 0", got)
	}
}
