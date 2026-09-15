package canvas

import (
	"image"
	"image/color"
	"math"
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

func TestBlendChannel(t *testing.T) {
	t.Parallel()

	const (
		low       = 10
		high      = 100
		halfAlpha = 0.5
		oneAlpha  = 1.0
	)

	for _, testCase := range []struct {
		name  string
		prev  uint8
		next  uint8
		alpha float64
		want  uint8
	}{
		{name: "alpha zero keeps prev", prev: low, next: high, alpha: 0, want: low},
		{name: "alpha one takes next", prev: low, next: high, alpha: oneAlpha, want: high},
		{name: "halfway interpolates", prev: 0, next: high, alpha: halfAlpha, want: 50},
		// math.Round rounds a .5 fraction away from zero, so this case pins
		// the midpoint down to a single deterministic byte rather than
		// letting truncation silently round it down instead.
		{name: "midpoint rounds up", prev: 0, next: 1, alpha: halfAlpha, want: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := blendChannel(testCase.prev, testCase.next, testCase.alpha); got != testCase.want {
				t.Errorf("blendChannel(%d, %d, %v) = %d, want %d",
					testCase.prev, testCase.next, testCase.alpha, got, testCase.want)
			}
		})
	}
}

func TestInvalidCoordinate(t *testing.T) {
	t.Parallel()

	const finite = 3.5

	for _, testCase := range []struct {
		name string
		in   float64
		want bool
	}{
		{name: "finite", in: finite, want: false},
		{name: "NaN", in: math.NaN(), want: true},
		{name: "positive infinity", in: math.Inf(1), want: true},
		{name: "negative infinity", in: math.Inf(-1), want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := invalidCoordinate(testCase.in); got != testCase.want {
				t.Errorf("invalidCoordinate(%v) = %v, want %v", testCase.in, got, testCase.want)
			}
		})
	}
}

func TestEdgeFunction(t *testing.T) {
	t.Parallel()

	const (
		legLength = 4
		onEdge    = 2
	)

	for _, testCase := range []struct {
		name                   string
		ax, ay, bx, by, px, py int
		want                   int
	}{
		{name: "point left of a-to-b line", ax: 0, ay: 0, bx: legLength, by: 0, px: 0, py: legLength, want: 16},
		{name: "point right of a-to-b line", ax: 0, ay: 0, bx: legLength, by: 0, px: 0, py: -legLength, want: -16},
		{name: "point on the line", ax: 0, ay: 0, bx: legLength, by: 0, px: onEdge, py: 0, want: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := edgeFunction(testCase.ax, testCase.ay, testCase.bx, testCase.by, testCase.px, testCase.py)
			if got != testCase.want {
				t.Errorf("edgeFunction(...) = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestSameSign(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		area       int
		w0, w1, w2 int
		want       bool
	}{
		{name: "positive area, all inside", area: 1, w0: 1, w1: 2, w2: 3, want: true},
		{name: "positive area, one outside", area: 1, w0: 1, w1: -2, w2: 3, want: false},
		{name: "negative area, all inside", area: -1, w0: -1, w1: -2, w2: -3, want: true},
		{name: "negative area, one outside", area: -1, w0: -1, w1: 2, w2: -3, want: false},
		{name: "zero area takes the non-negative branch", area: 0, w0: 0, w1: 0, w2: 0, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := sameSign(testCase.area, testCase.w0, testCase.w1, testCase.w2); got != testCase.want {
				t.Errorf("sameSign(%d, %d, %d, %d) = %v, want %v",
					testCase.area, testCase.w0, testCase.w1, testCase.w2, got, testCase.want)
			}
		})
	}
}

func TestPointOnCircle(t *testing.T) {
	t.Parallel()

	const (
		centerX = 5
		centerY = 5
		radius  = 3
	)

	for _, testCase := range []struct {
		name   string
		radius int
		angle  float64
		wantX  int
		wantY  int
	}{
		{name: "zero radius collapses to centre", radius: 0, angle: 1, wantX: centerX, wantY: centerY},
		{name: "angle zero is due east", radius: radius, angle: 0, wantX: centerX + radius, wantY: centerY},
		{
			name: "quarter turn is due south", radius: radius, angle: math.Pi / 2,
			wantX: centerX, wantY: centerY + radius,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotX, gotY := pointOnCircle(centerX, centerY, testCase.radius, testCase.angle)
			if gotX != testCase.wantX || gotY != testCase.wantY {
				t.Errorf("pointOnCircle(%d, %d, %d, %v) = (%d, %d), want (%d, %d)",
					centerX, centerY, testCase.radius, testCase.angle, gotX, gotY, testCase.wantX, testCase.wantY)
			}
		})
	}
}
