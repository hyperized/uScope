// Package pattern draws the slice 1 test scene.
//
// The point of the scene is to answer two questions at a glance on a panel
// that has no other diagnostics: is the frame the right way up, and is the
// render loop running. The four corner squares and the triangle answer the
// first, the sweeping line answers the second.
//
// Everything here is pure drawing against a canvas, so the same scene that
// goes to the framebuffer also goes to a PNG on a laptop.
package pattern

import (
	"image"
	"math"
	"time"

	"github.com/hyperized/uScope/pkg/canvas"
)

const (
	// baseCornerSize is the side of each corner square at the panel's
	// resolution. Big enough to read at arm's length on a 5 inch panel,
	// small enough not to crowd a 320px one.
	baseCornerSize = 80

	// The orientation marker: a triangle whose apex sits baseApexInset
	// below the top edge. If it is not at the top, the rotation is wrong.
	baseApexInset        = 20
	baseTriangleHeight   = 60
	baseTriangleHalfBase = 40

	// sweepPeriod is one full turn of the sweep line. Four seconds is slow
	// enough to see motion without the eye having to chase it.
	sweepPeriod = 4 * time.Second

	// circleDivisor sets the ring radius as a fraction of the short edge.
	circleDivisor = 3

	// referenceShortEdge is the short edge, in pixels, the base sizes above
	// are drawn for: the panel's 720 lines. The half-block terminal backend
	// can hand back a canvas of a few dozen pixels, far below that, so every
	// size is scaled down proportionally when the canvas is smaller. A
	// canvas at or above this threshold draws at the base sizes unchanged.
	referenceShortEdge = 720

	// Floors under the scaled sizes, so a corner square or the triangle
	// stays a visible shape instead of scaling away to nothing on the
	// smallest canvases blocks mode can produce.
	minCornerSize       = 2
	minApexInset        = 1
	minTriangleHeight   = 3
	minTriangleHalfBase = 2
)

// metrics holds the pixel sizes the scene draws with for one frame, scaled
// for the canvas at hand.
type metrics struct {
	corner           int
	apexInset        int
	triangleHeight   int
	triangleHalfBase int
}

// metricsFor scales the scene's fixed pixel sizes to the canvas being drawn.
// A canvas at or above referenceShortEdge draws at the base sizes, same as
// the panel always has. Below that, every size shrinks in proportion to the
// short edge, which is what keeps the corner squares from swallowing a
// small blocks-mode canvas whole.
func metricsFor(width, height int) metrics {
	scale := min(1.0, float64(min(width, height))/referenceShortEdge)

	return metrics{
		corner:           scaledSize(baseCornerSize, scale, minCornerSize),
		apexInset:        scaledSize(baseApexInset, scale, minApexInset),
		triangleHeight:   scaledSize(baseTriangleHeight, scale, minTriangleHeight),
		triangleHalfBase: scaledSize(baseTriangleHalfBase, scale, minTriangleHalfBase),
	}
}

// scaledSize rounds base*scale to the nearest pixel, never going below
// floor, so a shape stays visible rather than disappearing.
func scaledSize(base int, scale float64, floor int) int {
	return max(floor, int(math.Round(float64(base)*scale)))
}

// Scene is the test pattern. It holds no state, so one instance can draw
// every frame and is safe to share.
type Scene struct{}

// New returns the test-pattern scene.
func New() *Scene {
	return &Scene{}
}

// Draw paints the whole scene. elapsed is the time since the loop started
// and only drives the sweep line; pass 0 for a still frame.
func (*Scene) Draw(dst *canvas.Canvas, elapsed time.Duration) {
	bounds := dst.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	centerX, centerY := width/2, height/2
	radius := min(width, height) / circleDivisor
	sizes := metricsFor(width, height)

	dst.Clear(canvas.Black)
	drawBorder(dst, width, height)
	drawCorners(dst, width, height, sizes)
	dst.Line(0, 0, width-1, height-1, canvas.White)
	dst.Circle(centerX, centerY, radius, canvas.Magenta)
	drawTriangle(dst, centerX, sizes)
	drawSweep(dst, centerX, centerY, radius, elapsed)
}

// drawBorder outlines the canvas so a frame that is shifted by even one
// pixel shows a gap against the panel edge.
func drawBorder(dst *canvas.Canvas, width, height int) {
	right, bottom := width-1, height-1

	dst.Line(0, 0, right, 0, canvas.White)
	dst.Line(0, bottom, right, bottom, canvas.White)
	dst.Line(0, 0, 0, bottom, canvas.White)
	dst.Line(right, 0, right, bottom, canvas.White)
}

// drawCorners puts a different colour in each corner. Red marks top-left,
// which is the one to look for when checking rotation.
func drawCorners(dst *canvas.Canvas, width, height int, sizes metrics) {
	dst.FillRect(image.Rect(0, 0, sizes.corner, sizes.corner), canvas.Red)
	dst.FillRect(image.Rect(width-sizes.corner, 0, width, sizes.corner), canvas.Green)
	dst.FillRect(image.Rect(0, height-sizes.corner, sizes.corner, height), canvas.Blue)
	dst.FillRect(image.Rect(width-sizes.corner, height-sizes.corner, width, height), canvas.Yellow)
}

// drawTriangle fills the upward triangle a row at a time, widening linearly
// from the apex to the base.
func drawTriangle(dst *canvas.Canvas, centerX int, sizes metrics) {
	for row := range sizes.triangleHeight + 1 {
		half := sizes.triangleHalfBase * row / sizes.triangleHeight
		top := sizes.apexInset + row
		dst.FillRect(image.Rect(centerX-half, top, centerX+half+1, top+1), canvas.Cyan)
	}
}

// drawSweep draws the radial line, starting straight up at elapsed 0 and
// turning clockwise. A negative elapsed is folded back into the period
// rather than mirroring the sweep.
func drawSweep(dst *canvas.Canvas, centerX, centerY, radius int, elapsed time.Duration) {
	phase := elapsed % sweepPeriod
	if phase < 0 {
		phase += sweepPeriod
	}

	angle := 2 * math.Pi * float64(phase) / float64(sweepPeriod)
	endX := centerX + int(math.Round(float64(radius)*math.Sin(angle)))
	endY := centerY - int(math.Round(float64(radius)*math.Cos(angle)))

	dst.Line(centerX, centerY, endX, endY, canvas.White)
}
