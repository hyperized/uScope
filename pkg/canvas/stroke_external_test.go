package canvas_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/pkg/canvas"
)

// lit reports whether the pixel at (x, y) has been drawn on. A fresh canvas is
// all zero bytes, and every drawing call here writes an opaque colour.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout this package.
func lit(canv *canvas.Canvas, x, y int) bool {
	return canv.Image().RGBAAt(x, y) != color.RGBA{}
}

// countLit returns how many pixels of canv have been drawn on.
func countLit(canv *canvas.Canvas) int {
	bounds := canv.Bounds()
	count := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if lit(canv, x, y) {
				count++
			}
		}
	}

	return count
}

// drawOn runs draw against a fresh side by side canvas and returns it.
func drawOn(t *testing.T, side int, draw func(canv *canvas.Canvas)) *canvas.Canvas {
	t.Helper()

	canv := mustCanvas(t, side)
	draw(canv)

	return canv
}

// assertSamePixels fails the test unless both canvases hold the same bytes.
func assertSamePixels(t *testing.T, got, want *canvas.Canvas) {
	t.Helper()

	if !bytes.Equal(got.Image().Pix, want.Image().Pix) {
		t.Errorf("canvases differ: got %d lit pixels, want %d", countLit(got), countLit(want))
	}
}

// Case names shared by the width-one identity tables.
const (
	nameShallow        = "shallow"
	nameSteep          = "steep"
	nameHorizontal     = "horizontal"
	nameVertical       = "vertical"
	namePoint          = "point"
	nameClipped        = "clipped"
	nameNothing        = "nothing"
	nameWidthTwo       = "width 2"
	nameNegativeRadius = "negative radius"
)

// strokeIdentityCase pairs a one-pixel drawing with the Stroke call that has
// to reproduce it at a width of one or less.
type strokeIdentityCase struct {
	name   string
	thin   func(canv *canvas.Canvas)
	stroke func(canv *canvas.Canvas, width int)
	// blank marks a case whose reference drawing is meant to be empty.
	blank bool
}

// runIdentityCases checks every case at each width that must fall back to the
// one-pixel method.
func runIdentityCases(t *testing.T, cases []strokeIdentityCase) {
	t.Helper()

	const side = 48

	for _, testCase := range cases {
		for _, width := range []int{1, 0, -4} {
			t.Run(fmt.Sprintf("%s width %d", testCase.name, width), func(t *testing.T) {
				t.Parallel()

				want := drawOn(t, side, testCase.thin)
				got := drawOn(t, side, func(canv *canvas.Canvas) { testCase.stroke(canv, width) })

				assertSamePixels(t, got, want)

				if countLit(want) == 0 && !testCase.blank {
					t.Error("reference drawing is empty, the case proves nothing")
				}
			})
		}
	}
}

// linePath is a line between whole-pixel endpoints.
type linePath struct {
	name           string
	x0, y0, x1, y1 int
}

// wholeLines are the paths both line identity tests draw: shallow, steep,
// reversed, axis-aligned, a single pixel and one running well off the canvas.
func wholeLines() []linePath {
	return []linePath{
		{name: nameShallow, x0: 2, y0: 3, x1: 40, y1: 9},
		{name: nameSteep, x0: 5, y0: 2, x1: 12, y1: 40},
		{name: "reversed", x0: 40, y0: 9, x1: 2, y1: 3},
		{name: nameHorizontal, x0: 3, y0: 10, x1: 30, y1: 10},
		{name: nameVertical, x0: 10, y0: 4, x1: 10, y1: 30},
		{name: "diagonal", x0: 4, y0: 4, x1: 30, y1: 30},
		{name: namePoint, x0: 10, y0: 10, x1: 10, y1: 10},
		{name: "off canvas", x0: -20, y0: -5, x1: 70, y1: 60},
	}
}

func TestStrokeLineAAWidthOneIdentity(t *testing.T) {
	t.Parallel()

	nan := math.NaN()
	lines := wholeLines()
	cases := make([]strokeIdentityCase, 0, len(lines)+2)

	for _, line := range lines {
		cases = append(cases, strokeIdentityCase{
			name: line.name,
			thin: func(canv *canvas.Canvas) {
				canv.LineAA(float64(line.x0), float64(line.y0), float64(line.x1), float64(line.y1), canvas.White)
			},
			stroke: func(canv *canvas.Canvas, width int) {
				canv.StrokeLineAA(float64(line.x0), float64(line.y0), float64(line.x1), float64(line.y1),
					width, canvas.White)
			},
			blank: false,
		})
	}

	cases = append(cases,
		strokeIdentityCase{
			name: "fractional",
			thin: func(canv *canvas.Canvas) { canv.LineAA(3.3, 4.7, 25.2, 18.9, canvas.White) },
			stroke: func(canv *canvas.Canvas, width int) {
				canv.StrokeLineAA(3.3, 4.7, 25.2, 18.9, width, canvas.White)
			},
			blank: false,
		},
		strokeIdentityCase{
			name:   nameNothing,
			thin:   func(canv *canvas.Canvas) { canv.LineAA(nan, 0, 10, 10, canvas.White) },
			stroke: func(canv *canvas.Canvas, width int) { canv.StrokeLineAA(nan, 0, 10, 10, width, canvas.White) },
			blank:  true,
		},
	)

	runIdentityCases(t, cases)
}

func TestStrokeDashedCircleWidthOneIdentity(t *testing.T) {
	t.Parallel()

	rings := []struct {
		name                  string
		centerX, centerY, rad int
		dash, gap             int
		blank                 bool
	}{
		{name: "dashed", centerX: 24, centerY: 24, rad: 15, dash: 3, gap: 2, blank: false},
		{name: "long dashes", centerX: 24, centerY: 24, rad: 20, dash: 7, gap: 1, blank: false},
		{name: "small", centerX: 24, centerY: 24, rad: 3, dash: 1, gap: 1, blank: false},
		{name: "zero gap", centerX: 24, centerY: 24, rad: 12, dash: 3, gap: 0, blank: false},
		{name: "negative gap", centerX: 24, centerY: 24, rad: 12, dash: 3, gap: -2, blank: false},
		{name: nameClipped, centerX: 2, centerY: 40, rad: 12, dash: 4, gap: 3, blank: false},
		{name: "zero dash", centerX: 24, centerY: 24, rad: 12, dash: 0, gap: 2, blank: true},
		{name: nameNegativeRadius, centerX: 24, centerY: 24, rad: -5, dash: 3, gap: 2, blank: true},
	}
	cases := make([]strokeIdentityCase, 0, len(rings))

	for _, ring := range rings {
		cases = append(cases, strokeIdentityCase{
			name: ring.name,
			thin: func(canv *canvas.Canvas) {
				canv.DashedCircle(ring.centerX, ring.centerY, ring.rad, ring.dash, ring.gap, canvas.White)
			},
			stroke: func(canv *canvas.Canvas, width int) {
				canv.StrokeDashedCircle(ring.centerX, ring.centerY, ring.rad, ring.dash, ring.gap, width, canvas.White)
			},
			blank: ring.blank,
		})
	}

	runIdentityCases(t, cases)
}

func TestStrokeCircleWidthOneIdentity(t *testing.T) {
	t.Parallel()

	rings := []struct {
		name                  string
		centerX, centerY, rad int
		blank                 bool
	}{
		{name: "large", centerX: 24, centerY: 24, rad: 20, blank: false},
		{name: "small", centerX: 24, centerY: 24, rad: 2, blank: false},
		{name: "radius one", centerX: 24, centerY: 24, rad: 1, blank: false},
		{name: "radius zero", centerX: 24, centerY: 24, rad: 0, blank: false},
		{name: nameClipped, centerX: 3, centerY: 44, rad: 15, blank: false},
		{name: nameNegativeRadius, centerX: 24, centerY: 24, rad: -3, blank: true},
	}
	cases := make([]strokeIdentityCase, 0, len(rings))

	for _, ring := range rings {
		cases = append(cases, strokeIdentityCase{
			name: ring.name,
			thin: func(canv *canvas.Canvas) { canv.Circle(ring.centerX, ring.centerY, ring.rad, canvas.White) },
			stroke: func(canv *canvas.Canvas, width int) {
				canv.StrokeCircle(ring.centerX, ring.centerY, ring.rad, width, canvas.White)
			},
			blank: ring.blank,
		})
	}

	runIdentityCases(t, cases)
}

func TestStrokeRectWidthOneIdentity(t *testing.T) {
	t.Parallel()

	boxes := []struct {
		name  string
		rect  image.Rectangle
		blank bool
	}{
		{name: "wide", rect: image.Rect(5, 4, 40, 20), blank: false},
		{name: "tall", rect: image.Rect(5, 4, 12, 44), blank: false},
		{name: namePoint, rect: image.Rect(7, 7, 8, 8), blank: false},
		{name: "one row", rect: image.Rect(7, 7, 30, 8), blank: false},
		{name: nameClipped, rect: image.Rect(-5, -5, 20, 20), blank: false},
		{name: "empty", rect: image.Rect(10, 10, 10, 20), blank: true},
	}
	cases := make([]strokeIdentityCase, 0, len(boxes))

	for _, box := range boxes {
		cases = append(cases, strokeIdentityCase{
			name:   box.name,
			thin:   func(canv *canvas.Canvas) { canv.Rect(box.rect, canvas.White) },
			stroke: func(canv *canvas.Canvas, width int) { canv.StrokeRect(box.rect, width, canvas.White) },
			blank:  box.blank,
		})
	}

	runIdentityCases(t, cases)
}

func TestStrokeLineWidthOneIdentity(t *testing.T) {
	t.Parallel()

	lines := wholeLines()
	cases := make([]strokeIdentityCase, 0, len(lines))

	for _, line := range lines {
		cases = append(cases, strokeIdentityCase{
			name: line.name,
			thin: func(canv *canvas.Canvas) { canv.Line(line.x0, line.y0, line.x1, line.y1, canvas.White) },
			stroke: func(canv *canvas.Canvas, width int) {
				canv.StrokeLine(line.x0, line.y0, line.x1, line.y1, width, canvas.White)
			},
			blank: false,
		})
	}

	runIdentityCases(t, cases)
}

// channelFor is the red channel Blend leaves on a clear pixel when it blends
// white in at the given coverage.
func channelFor(cover float64) uint8 {
	return uint8(math.Round(255 * cover))
}

// assertBand checks every pixel of canv against the band a thick line is
// meant to leave: cover maps a minor-axis index to its coverage, and only the
// majors from start to end inclusive are drawn on.
//
//nolint:varnamelen,revive // x, y is the pixel-addressing idiom; steep picks which axis the band runs along.
func assertBand(t *testing.T, canv *canvas.Canvas, steep bool, start, end int, cover map[int]float64) {
	t.Helper()

	bounds := canv.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			major, minor := x, y
			if steep {
				major, minor = y, x
			}

			want := color.RGBA{}

			if frac, ok := cover[minor]; ok && major >= start && major <= end {
				want = color.RGBA{R: channelFor(frac), G: channelFor(frac), B: channelFor(frac), A: 255}
			}

			if got := canv.Image().RGBAAt(x, y); got != want {
				t.Fatalf("pixel (%d, %d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

func TestStrokeLineAABands(t *testing.T) {
	t.Parallel()

	const (
		side  = 32
		start = 5
		end   = 20
	)

	for _, testCase := range []struct {
		name  string
		steep bool
		// across is the line's coordinate on the minor axis.
		across float64
		width  int
		// cover maps a row (or column, when steep) to its expected coverage.
		cover map[int]float64
	}{
		{
			name: "horizontal width 2 on pixel edge", steep: false, across: 10.5, width: 2,
			cover: map[int]float64{10: 1, 11: 1},
		},
		{
			name: "horizontal width 3 on pixel centre", steep: false, across: 10, width: 3,
			cover: map[int]float64{9: 1, 10: 1, 11: 1},
		},
		{
			name: "horizontal width 2 on pixel centre", steep: false, across: 10, width: 2,
			cover: map[int]float64{9: 0.5, 10: 1, 11: 0.5},
		},
		{
			name: "horizontal width 3 on pixel edge", steep: false, across: 10.5, width: 3,
			cover: map[int]float64{9: 0.5, 10: 1, 11: 1, 12: 0.5},
		},
		{
			name: "vertical width 2", steep: true, across: 10.5, width: 2,
			cover: map[int]float64{10: 1, 11: 1},
		},
		{
			name: "vertical width 3", steep: true, across: 10, width: 3,
			cover: map[int]float64{9: 1, 10: 1, 11: 1},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv := mustCanvas(t, side)

			if testCase.steep {
				canv.StrokeLineAA(testCase.across, start, testCase.across, end, testCase.width, canvas.White)
			} else {
				canv.StrokeLineAA(start, testCase.across, end, testCase.across, testCase.width, canvas.White)
			}

			assertBand(t, canv, testCase.steep, start, end, testCase.cover)
		})
	}
}

func TestStrokeLineAAInvalidCoordinates(t *testing.T) {
	t.Parallel()

	inf := math.Inf(1)
	nan := math.NaN()

	for _, coords := range [][4]float64{
		{nan, 1, 10, 10}, {1, nan, 10, 10}, {1, 1, nan, 10}, {1, 1, 10, nan},
		{inf, 1, 10, 10}, {1, -inf, 10, 10}, {1, 1, inf, 10}, {1, 1, 10, -inf},
	} {
		t.Run(fmt.Sprint(coords), func(t *testing.T) {
			t.Parallel()

			canv := mustCanvas(t, 48)
			canv.StrokeLineAA(coords[0], coords[1], coords[2], coords[3], 3, canvas.White)

			if got := countLit(canv); got != 0 {
				t.Errorf("lit %d pixels, want 0", got)
			}
		})
	}
}

func TestStrokeLineAAThickLine(t *testing.T) {
	t.Parallel()

	const side = 48

	for _, testCase := range []struct {
		name           string
		x0, y0, x1, y1 float64
		width          int
	}{
		{name: nameShallow, x0: 3, y0: 4, x1: 40, y1: 20, width: 2},
		{name: nameSteep, x0: 6, y0: 3, x1: 18, y1: 44, width: 3},
		{name: "fractional", x0: 3.4, y0: 8.7, x1: 33.1, y1: 12.2, width: 4},
		{name: namePoint, x0: 9, y0: 9, x1: 9, y1: 9, width: 3},
	} {
		forward := func(canv *canvas.Canvas) {
			canv.StrokeLineAA(testCase.x0, testCase.y0, testCase.x1, testCase.y1, testCase.width, canvas.White)
		}

		t.Run(testCase.name+" reversed endpoints", func(t *testing.T) {
			t.Parallel()

			backward := drawOn(t, side, func(canv *canvas.Canvas) {
				canv.StrokeLineAA(testCase.x1, testCase.y1, testCase.x0, testCase.y0, testCase.width, canvas.White)
			})

			assertSamePixels(t, backward, drawOn(t, side, forward))

			if countLit(backward) == 0 {
				t.Error("nothing drawn")
			}
		})

		t.Run(testCase.name+" is heavier than width one", func(t *testing.T) {
			t.Parallel()

			thin := drawOn(t, side, func(canv *canvas.Canvas) {
				canv.StrokeLineAA(testCase.x0, testCase.y0, testCase.x1, testCase.y1, 1, canvas.White)
			})
			thick := drawOn(t, side, forward)

			if countLit(thick) <= countLit(thin) {
				t.Errorf("width %d lit %d pixels, width 1 lit %d", testCase.width, countLit(thick), countLit(thin))
			}
		})
	}

	t.Run("off canvas does not panic", func(t *testing.T) {
		t.Parallel()

		canv := mustCanvas(t, side)
		canv.StrokeLineAA(-30, -30, 80, 90, 5, canvas.White)

		if countLit(canv) == 0 {
			t.Error("a line crossing the canvas drew nothing")
		}
	})
}

// annulusLit checks canv against the annulus StrokeCircle documents: a pixel
// is set when its centre lies in [inner-0.5, outer+0.5) from the centre of the
// ring.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout this package.
func annulusLit(t *testing.T, canv *canvas.Canvas, centerX, centerY, radius, width int) {
	t.Helper()

	inner := max(radius-width/2, 0)
	low := float64(inner) - 0.5
	high := float64(inner+width-1) + 0.5
	bounds := canv.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dist := math.Hypot(float64(x-centerX), float64(y-centerY))
			want := dist >= low && dist < high

			if got := lit(canv, x, y); got != want {
				t.Errorf("pixel (%d, %d) at distance %.3f: lit = %v, want %v (annulus [%.1f, %.1f))",
					x, y, dist, got, want, low, high)
			}
		}
	}
}

func TestStrokeCircleAnnulus(t *testing.T) {
	t.Parallel()

	const side = 41

	for _, testCase := range []struct {
		name                  string
		centerX, centerY, rad int
		width                 int
	}{
		{name: nameWidthTwo, centerX: 20, centerY: 20, rad: 9, width: 2},
		{name: "width 3", centerX: 20, centerY: 20, rad: 9, width: 3},
		{name: "width 4", centerX: 20, centerY: 20, rad: 12, width: 4},
		{name: "width 5 small radius", centerX: 20, centerY: 20, rad: 5, width: 5},
		{name: "clipped by corner", centerX: 2, centerY: 2, rad: 6, width: 3},
		{name: "centre off canvas", centerX: -3, centerY: 40, rad: 8, width: 4},
		{name: "ring larger than canvas", centerX: 20, centerY: 20, rad: 60, width: 3},
		{name: "radius zero width 3 fills the centre", centerX: 20, centerY: 20, rad: 0, width: 3},
		{name: "radius zero width 2 fills the centre", centerX: 20, centerY: 20, rad: 0, width: 2},
		{name: "radius one width 3 fills the centre", centerX: 20, centerY: 20, rad: 1, width: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv := drawOn(t, side, func(canv *canvas.Canvas) {
				canv.StrokeCircle(testCase.centerX, testCase.centerY, testCase.rad, testCase.width, canvas.White)
			})

			annulusLit(t, canv, testCase.centerX, testCase.centerY, testCase.rad, testCase.width)
		})
	}

	t.Run("negative radius draws nothing", func(t *testing.T) {
		t.Parallel()

		canv := drawOn(t, side, func(canv *canvas.Canvas) { canv.StrokeCircle(20, 20, -4, 3, canvas.White) })

		if got := countLit(canv); got != 0 {
			t.Errorf("lit %d pixels, want 0", got)
		}
	})
}

// assertWithinBand fails the test for every lit pixel of canv whose distance
// from (centreX, centreY) falls outside [low, high].
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout this package.
func assertWithinBand(t *testing.T, canv *canvas.Canvas, centre int, low, high float64) {
	t.Helper()

	bounds := canv.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if !lit(canv, x, y) {
				continue
			}

			if dist := math.Hypot(float64(x-centre), float64(y-centre)); dist < low || dist > high {
				t.Errorf("pixel (%d, %d) at distance %.3f is outside [%.2f, %.2f]", x, y, dist, low, high)
			}
		}
	}
}

func TestStrokeDashedCircleThick(t *testing.T) {
	t.Parallel()

	const (
		side   = 51
		centre = 25
		// slack is how far rounding a point on the ring to a pixel can move it
		// off the ring, half a pixel on each axis.
		slack = 0.71
	)

	for _, testCase := range []struct {
		name  string
		rad   int
		width int
		dash  int
		gap   int
	}{
		{name: nameWidthTwo, rad: 12, width: 2, dash: 3, gap: 2},
		{name: "width 3", rad: 18, width: 3, dash: 4, gap: 3},
		{name: "width 4 long gap", rad: 15, width: 4, dash: 2, gap: 6},
		{name: "radius zero", rad: 0, width: 2, dash: 1, gap: 1},
		{name: "ring wider than radius", rad: 3, width: 8, dash: 2, gap: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv := drawOn(t, side, func(canv *canvas.Canvas) {
				canv.StrokeDashedCircle(centre, centre, testCase.rad, testCase.dash, testCase.gap, testCase.width,
					canvas.White)
			})
			solid := drawOn(t, side, func(canv *canvas.Canvas) {
				canv.StrokeCircle(centre, centre, testCase.rad, testCase.width, canvas.White)
			})

			inner := max(testCase.rad-testCase.width/2, 0)
			assertWithinBand(t, canv, centre, float64(inner)-slack, float64(inner+testCase.width-1)+slack)

			if countLit(canv) == 0 {
				t.Fatal("nothing drawn")
			}

			if countLit(canv) >= countLit(solid) {
				t.Errorf("dashed ring lit %d pixels, the solid one %d: no gaps", countLit(canv), countLit(solid))
			}
		})
	}
}

func TestStrokeDashedCircleFallbacks(t *testing.T) {
	t.Parallel()

	const (
		side   = 51
		centre = 25
	)

	for _, gap := range []int{0, -3} {
		t.Run(fmt.Sprintf("gap %d is a solid ring", gap), func(t *testing.T) {
			t.Parallel()

			got := drawOn(t, side, func(canv *canvas.Canvas) {
				canv.StrokeDashedCircle(centre, centre, 12, 3, gap, 3, canvas.White)
			})
			want := drawOn(t, side, func(canv *canvas.Canvas) {
				canv.StrokeCircle(centre, centre, 12, 3, canvas.White)
			})

			assertSamePixels(t, got, want)
		})
	}

	for _, testCase := range []struct {
		name string
		rad  int
		dash int
	}{
		{name: "zero dash", rad: 12, dash: 0},
		{name: "negative dash", rad: 12, dash: -2},
		{name: nameNegativeRadius, rad: -12, dash: 3},
	} {
		t.Run(testCase.name+" draws nothing", func(t *testing.T) {
			t.Parallel()

			canv := drawOn(t, side, func(canv *canvas.Canvas) {
				canv.StrokeDashedCircle(centre, centre, testCase.rad, testCase.dash, 2, 3, canvas.White)
			})

			if got := countLit(canv); got != 0 {
				t.Errorf("lit %d pixels, want 0", got)
			}
		})
	}
}

func TestStrokeRectFrames(t *testing.T) {
	t.Parallel()

	const side = 24

	for _, testCase := range []struct {
		name  string
		rect  image.Rectangle
		width int
		// hollow is the box inside the frame, empty when the width fills the rect.
		hollow image.Rectangle
	}{
		{
			name: "width 2 on 10x8", rect: image.Rect(5, 4, 15, 12), width: 2,
			hollow: image.Rect(7, 6, 13, 10),
		},
		{
			name: "width 3 on 10x8", rect: image.Rect(5, 4, 15, 12), width: 3,
			hollow: image.Rect(8, 7, 12, 9),
		},
		{
			name: "width 3 on 9x9", rect: image.Rect(2, 2, 11, 11), width: 3,
			hollow: image.Rect(5, 5, 8, 8),
		},
		{
			name: "clipped by the corner", rect: image.Rect(-4, -4, 10, 8), width: 2,
			hollow: image.Rect(-2, -2, 8, 6),
		},
		{name: "width meets in the middle", rect: image.Rect(5, 4, 15, 12), width: 4, hollow: image.Rectangle{}},
		{name: "width past the middle", rect: image.Rect(5, 4, 15, 12), width: 9, hollow: image.Rectangle{}},
		{name: "wide rect, tight height", rect: image.Rect(1, 1, 20, 5), width: 2, hollow: image.Rectangle{}},
		{name: "tall rect, tight width", rect: image.Rect(1, 1, 5, 20), width: 2, hollow: image.Rectangle{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv := drawOn(t, side, func(canv *canvas.Canvas) {
				canv.StrokeRect(testCase.rect, testCase.width, canvas.White)
			})

			assertRectFrame(t, canv, testCase.rect, testCase.hollow)
		})
	}
}

// assertRectFrame fails the test unless canv holds exactly rect minus hollow.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout this package.
func assertRectFrame(t *testing.T, canv *canvas.Canvas, rect, hollow image.Rectangle) {
	t.Helper()

	bounds := canv.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			point := image.Pt(x, y)
			want := point.In(rect) && !point.In(hollow)

			if got := lit(canv, x, y); got != want {
				t.Fatalf("pixel (%d, %d): lit = %v, want %v", x, y, got, want)
			}
		}
	}
}

func TestStrokeRectEmpty(t *testing.T) {
	t.Parallel()

	for _, rect := range []image.Rectangle{image.Rect(5, 5, 5, 15), image.Rect(5, 5, 15, 5)} {
		t.Run(rect.String(), func(t *testing.T) {
			t.Parallel()

			canv := drawOn(t, 24, func(canv *canvas.Canvas) { canv.StrokeRect(rect, 3, canvas.White) })

			if got := countLit(canv); got != 0 {
				t.Errorf("lit %d pixels, want 0", got)
			}
		})
	}
}

// stamped reports whether any pixel of thin lies in the width by width block
// whose top-left corner is on it and that covers (x, y).
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout this package.
func stamped(thin *canvas.Canvas, x, y, width int) bool {
	for blockY := y - width + 1; blockY <= y; blockY++ {
		for blockX := x - width + 1; blockX <= x; blockX++ {
			if lit(thin, blockX, blockY) {
				return true
			}
		}
	}

	return false
}

func TestStrokeLineBlocks(t *testing.T) {
	t.Parallel()

	const side = 40

	for _, testCase := range []struct {
		name           string
		x0, y0, x1, y1 int
		width          int
	}{
		{name: "diagonal width 2", x0: 3, y0: 3, x1: 25, y1: 20, width: 2},
		{name: "diagonal width 3", x0: 3, y0: 3, x1: 25, y1: 20, width: 3},
		{name: "reversed width 2", x0: 25, y0: 20, x1: 3, y1: 3, width: 2},
		{name: "reversed width 3", x0: 25, y0: 20, x1: 3, y1: 3, width: 3},
		{name: "horizontal width 3", x0: 4, y0: 10, x1: 30, y1: 10, width: 3},
		{name: "vertical width 2", x0: 10, y0: 4, x1: 10, y1: 30, width: 2},
		{name: "steep width 3", x0: 6, y0: 2, x1: 14, y1: 30, width: 3},
		{name: "single pixel width 3", x0: 8, y0: 8, x1: 8, y1: 8, width: 3},
		{name: "blocks run off the edge", x0: 30, y0: 30, x1: 39, y1: 39, width: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv := drawOn(t, side, func(canv *canvas.Canvas) {
				canv.StrokeLine(testCase.x0, testCase.y0, testCase.x1, testCase.y1, testCase.width, canvas.White)
			})
			thin := drawOn(t, side, func(canv *canvas.Canvas) {
				canv.Line(testCase.x0, testCase.y0, testCase.x1, testCase.y1, canvas.White)
			})

			for y := range side {
				for x := range side {
					if got, want := lit(canv, x, y), stamped(thin, x, y, testCase.width); got != want {
						t.Fatalf("pixel (%d, %d): lit = %v, want %v", x, y, got, want)
					}
				}
			}
		})
	}
}

// TestStrokeAllocs does not call t.Parallel: testing.AllocsPerRun panics if it
// runs while the test is marked parallel.
//
//nolint:paralleltest // AllocsPerRun forbids running as a parallel test; see comment above.
func TestStrokeAllocs(t *testing.T) {
	const (
		side  = 64
		width = 2
		runs  = 100
	)

	canv := mustCanvas(t, side)
	rect := image.Rect(8, 8, 50, 40)

	for _, testCase := range []struct {
		name string
		draw func()
	}{
		{name: "StrokeLineAA", draw: func() { canv.StrokeLineAA(2.5, 3.5, 60.5, 41.5, width, canvas.White) }},
		{name: "StrokeDashedCircle", draw: func() { canv.StrokeDashedCircle(32, 32, 20, 3, 2, width, canvas.White) }},
		{name: "StrokeCircle", draw: func() { canv.StrokeCircle(32, 32, 20, width, canvas.White) }},
		{name: "StrokeRect", draw: func() { canv.StrokeRect(rect, width, canvas.White) }},
		{name: "StrokeLine", draw: func() { canv.StrokeLine(2, 3, 60, 41, width, canvas.White) }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if allocs := testing.AllocsPerRun(runs, testCase.draw); allocs != 0 {
				t.Errorf("%s allocated %v per run, want 0", testCase.name, allocs)
			}
		})
	}
}

func BenchmarkStrokeLineAA(b *testing.B) {
	const side = 256

	canv, err := canvas.New(side, side)
	if err != nil {
		b.Fatalf("New: %v", err)
	}

	for _, width := range []int{1, 2} {
		b.Run(map[int]string{1: "width 1", 2: "width 2"}[width], func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				canv.StrokeLineAA(0, 0, side-1, side-1, width, canvas.White)
			}
		})
	}
}
