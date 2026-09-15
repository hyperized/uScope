package pattern

import (
	"image/color"
	"testing"
	"time"

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

func TestDrawBorder(t *testing.T) {
	t.Parallel()

	const (
		width  = 64
		height = 40
	)

	canv, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	drawBorder(canv, width, height)

	for _, testCase := range []struct {
		name string
		x    int
		y    int
		want color.RGBA
	}{
		{name: "top-left corner", x: 0, y: 0, want: canvas.White},
		{name: "top-right corner", x: width - 1, y: 0, want: canvas.White},
		{name: "bottom-left corner", x: 0, y: height - 1, want: canvas.White},
		{name: "bottom-right corner", x: width - 1, y: height - 1, want: canvas.White},
		{name: "top edge midpoint", x: width / 2, y: 0, want: canvas.White},
		{name: "bottom edge midpoint", x: width / 2, y: height - 1, want: canvas.White},
		{name: "left edge midpoint", x: 0, y: height / 2, want: canvas.White},
		{name: "right edge midpoint", x: width - 1, y: height / 2, want: canvas.White},
		{name: "interior stays untouched", x: width / 2, y: height / 2, want: color.RGBA{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assertPixel(t, canv, testCase.x, testCase.y, testCase.want, testCase.name)
		})
	}
}

// TestDrawCorners checks drawCorners on its own, without the diagonal that
// Scene.Draw paints afterwards. In isolation all four corners keep their
// true square colour; it is only in the full scene that the diagonal's two
// endpoints repaint the top-left and bottom-right corners white.
func TestDrawCorners(t *testing.T) {
	t.Parallel()

	const (
		width  = 300
		height = 250
	)

	sizes := metricsFor(width, height)
	insideOffset := sizes.corner / 2

	canv, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	drawCorners(canv, width, height, sizes)

	for _, testCase := range []struct {
		name string
		x    int
		y    int
		want color.RGBA
	}{
		{name: "top-left corner pixel", x: 0, y: 0, want: canvas.Red},
		{name: "top-right corner pixel", x: width - 1, y: 0, want: canvas.Green},
		{name: "bottom-left corner pixel", x: 0, y: height - 1, want: canvas.Blue},
		{name: "bottom-right corner pixel", x: width - 1, y: height - 1, want: canvas.Yellow},
		{name: "top-left square interior", x: insideOffset, y: insideOffset, want: canvas.Red},
		{name: "top-right square interior", x: width - insideOffset, y: insideOffset, want: canvas.Green},
		{name: "bottom-left square interior", x: insideOffset, y: height - insideOffset, want: canvas.Blue},
		{name: "bottom-right square interior", x: width - insideOffset, y: height - insideOffset, want: canvas.Yellow},
		{name: "top-left square just inside the edge", x: sizes.corner - 1, y: sizes.corner - 1, want: canvas.Red},
		{name: "top-left square just outside the edge", x: sizes.corner, y: sizes.corner, want: color.RGBA{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assertPixel(t, canv, testCase.x, testCase.y, testCase.want, testCase.name)
		})
	}
}

func TestDrawTriangle(t *testing.T) {
	t.Parallel()

	const (
		width   = 250
		height  = 100
		centerX = 100
	)

	sizes := metricsFor(width, height)
	baseY := sizes.apexInset + sizes.triangleHeight

	canv, err := canvas.New(width, height)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	drawTriangle(canv, centerX, sizes)

	for _, testCase := range []struct {
		name string
		x    int
		y    int
		want color.RGBA
	}{
		{name: "apex", x: centerX, y: sizes.apexInset, want: canvas.Cyan},
		{name: "row above apex is untouched", x: centerX, y: sizes.apexInset - 1, want: color.RGBA{}},
		{name: "base row left edge", x: centerX - sizes.triangleHalfBase, y: baseY, want: canvas.Cyan},
		{name: "base row center", x: centerX, y: baseY, want: canvas.Cyan},
		{name: "base row right edge", x: centerX + sizes.triangleHalfBase, y: baseY, want: canvas.Cyan},
		{
			name: "base row just outside left edge",
			x:    centerX - sizes.triangleHalfBase - 1, y: baseY, want: color.RGBA{},
		},
		{
			name: "base row just outside right edge",
			x:    centerX + sizes.triangleHalfBase + 1, y: baseY, want: color.RGBA{},
		},
		{name: "row below base is untouched", x: centerX, y: baseY + 1, want: color.RGBA{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assertPixel(t, canv, testCase.x, testCase.y, testCase.want, testCase.name)
		})
	}
}

func TestDrawSweep(t *testing.T) {
	t.Parallel()

	const (
		width   = 100
		height  = 100
		centerX = 50
		centerY = 50
		radius  = 30
	)

	for _, testCase := range []struct {
		name    string
		elapsed time.Duration
		endX    int
		endY    int
	}{
		{name: "zero elapsed points straight up", elapsed: 0, endX: centerX, endY: centerY - radius},
		{name: "quarter period points right", elapsed: sweepPeriod / 4, endX: centerX + radius, endY: centerY},
		{name: "half period points straight down", elapsed: sweepPeriod / 2, endX: centerX, endY: centerY + radius},
		{name: "three quarter period points left", elapsed: 3 * sweepPeriod / 4, endX: centerX - radius, endY: centerY},
		// A negative elapsed must fold into the period rather than mirror
		// the sweep, so it lands on the same point as the three-quarter
		// case above. This is the phase < 0 branch.
		{
			name:    "negative quarter period folds to the three quarter point",
			elapsed: -sweepPeriod / 4, endX: centerX - radius, endY: centerY,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := canvas.New(width, height)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			drawSweep(canv, centerX, centerY, radius, testCase.elapsed)

			assertPixel(t, canv, centerX, centerY, canvas.White, "sweep origin")
			assertPixel(t, canv, testCase.endX, testCase.endY, canvas.White, testCase.name)
		})
	}
}

func TestConstants(t *testing.T) {
	t.Parallel()

	const (
		wantBaseCornerSize       = 80
		wantBaseApexInset        = 20
		wantBaseTriangleHeight   = 60
		wantBaseTriangleHalfBase = 40
		wantCircleDivisor        = 3
		wantReferenceShortEdge   = 720
	)

	for _, testCase := range []struct {
		name string
		got  int
		want int
	}{
		{name: "baseCornerSize", got: baseCornerSize, want: wantBaseCornerSize},
		{name: "baseApexInset", got: baseApexInset, want: wantBaseApexInset},
		{name: "baseTriangleHeight", got: baseTriangleHeight, want: wantBaseTriangleHeight},
		{name: "baseTriangleHalfBase", got: baseTriangleHalfBase, want: wantBaseTriangleHalfBase},
		{name: "circleDivisor", got: circleDivisor, want: wantCircleDivisor},
		{name: "referenceShortEdge", got: referenceShortEdge, want: wantReferenceShortEdge},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.got != testCase.want {
				t.Errorf("%s = %d, want %d", testCase.name, testCase.got, testCase.want)
			}
		})
	}

	t.Run("sweepPeriod", func(t *testing.T) {
		t.Parallel()

		const wantSweepPeriod = 4 * time.Second

		if sweepPeriod != wantSweepPeriod {
			t.Errorf("sweepPeriod = %v, want %v", sweepPeriod, wantSweepPeriod)
		}
	})
}

// TestMetricsFor pins the scaling formula: base sizes at or above the
// reference short edge, and the same rounded, floored scale below it.
func TestMetricsFor(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name            string
		width           int
		height          int
		wantCorner      int
		wantApexInset   int
		wantTriHeight   int
		wantTriHalfBase int
	}{
		{
			name:  "panel landscape, exactly the reference short edge",
			width: 1280, height: 720,
			wantCorner: 80, wantApexInset: 20, wantTriHeight: 60, wantTriHalfBase: 40,
		},
		{
			name:  "panel portrait, exactly the reference short edge",
			width: 720, height: 1280,
			wantCorner: 80, wantApexInset: 20, wantTriHeight: 60, wantTriHalfBase: 40,
		},
		{
			name:  "300x250 scales down",
			width: 300, height: 250,
			wantCorner: 28, wantApexInset: 7, wantTriHeight: 21, wantTriHalfBase: 14,
		},
		{
			name:  "250x100 scales down further",
			width: 250, height: 100,
			wantCorner: 11, wantApexInset: 3, wantTriHeight: 8, wantTriHalfBase: 6,
		},
		{
			name:  "80x48, the blocks backend's smallest common canvas",
			width: 80, height: 48,
			wantCorner: 5, wantApexInset: 1, wantTriHeight: 4, wantTriHalfBase: 3,
		},
		{
			name:  "4x4 hits every floor",
			width: 4, height: 4,
			wantCorner: 2, wantApexInset: 1, wantTriHeight: 3, wantTriHalfBase: 2,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := metricsFor(testCase.width, testCase.height)
			want := metrics{
				corner:           testCase.wantCorner,
				apexInset:        testCase.wantApexInset,
				triangleHeight:   testCase.wantTriHeight,
				triangleHalfBase: testCase.wantTriHalfBase,
			}

			if got != want {
				t.Errorf("metricsFor(%d, %d) = %+v, want %+v", testCase.width, testCase.height, got, want)
			}
		})
	}
}
