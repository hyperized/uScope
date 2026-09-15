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
	// cornerSize is the side of each corner square. Big enough to read at
	// arm's length on a 5 inch panel, small enough not to crowd a 320px one.
	cornerSize = 80

	// The orientation marker: a triangle whose apex sits apexInset below the
	// top edge. If it is not at the top, the rotation is wrong.
	apexInset        = 20
	triangleHeight   = 60
	triangleHalfBase = 40

	// sweepPeriod is one full turn of the sweep line. Four seconds is slow
	// enough to see motion without the eye having to chase it.
	sweepPeriod = 4 * time.Second

	// circleDivisor sets the ring radius as a fraction of the short edge.
	circleDivisor = 3
)

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

	dst.Clear(canvas.Black)
	drawBorder(dst, width, height)
	drawCorners(dst, width, height)
	dst.Line(0, 0, width-1, height-1, canvas.White)
	dst.Circle(centerX, centerY, radius, canvas.Magenta)
	drawTriangle(dst, centerX)
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
func drawCorners(dst *canvas.Canvas, width, height int) {
	dst.FillRect(image.Rect(0, 0, cornerSize, cornerSize), canvas.Red)
	dst.FillRect(image.Rect(width-cornerSize, 0, width, cornerSize), canvas.Green)
	dst.FillRect(image.Rect(0, height-cornerSize, cornerSize, height), canvas.Blue)
	dst.FillRect(image.Rect(width-cornerSize, height-cornerSize, width, height), canvas.Yellow)
}

// drawTriangle fills the upward triangle a row at a time, widening linearly
// from the apex to the base.
func drawTriangle(dst *canvas.Canvas, centerX int) {
	for row := range triangleHeight + 1 {
		half := triangleHalfBase * row / triangleHeight
		top := apexInset + row
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
