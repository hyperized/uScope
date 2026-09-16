package radar

import (
	"image"
	"math"
	"time"
)

// The world the 3D view is built in: x east, y north and z up, all in nautical
// miles from the receiver at ground level.
const (
	// ftPerNm is one nautical mile in feet, which is the definition of the
	// unit. Altitudes arrive in feet and the world is measured in nautical
	// miles, so every height goes through it.
	ftPerNm = 6076.12

	// MinExaggerate is life size, where a nautical mile of height draws as
	// long as a nautical mile of ground. It is the floor rather than the
	// default because at life size the 3D view is the scope view with a tilt
	// on it: forty thousand feet is six and a half nautical miles against a
	// scope tens of miles across, so the whole fleet lies in a film on the
	// floor.
	MinExaggerate = 1.0

	// MaxExaggerate is as far as the stretch goes. Past twenty the bowl is
	// taller than the range is wide and the picture is a column rather than a
	// scope.
	MaxExaggerate = 20.0

	// DefaultExaggerate puts an airliner about as far above the ground as the
	// outer ring is wide, which is where two aircraft a flight level apart are
	// visibly a flight level apart.
	DefaultExaggerate = 8.0
)

// The camera and what moves it.
const (
	// The elevation the brackets tilt between, and where it starts. Below ten
	// degrees the ground rings collapse into a line and the view says nothing
	// about where anything is; above eighty it is the scope view again with
	// the heights hidden behind the aircraft that carry them.
	minElevation     = 10.0
	maxElevation     = 80.0
	defaultElevation = 35.0

	// tiltStep is how far one press of [ or ] moves the elevation, and
	// azimuthStep how far Left or Right turns the camera.
	tiltStep    = 5.0
	azimuthStep = 15.0

	// orbitPeriod is one revolution of the automatic orbit. Two minutes is
	// slow enough that the picture reads as still while the eye is on one
	// aircraft, and quick enough that waiting for the other side of the
	// envelope is not a thing anyone has to decide to do.
	orbitPeriod = 2 * time.Minute

	// ringSpan is how much of the scope box's width the outer range ring is
	// framed to take. Leaving a sixth of the box empty around it is what keeps
	// the cardinal letters and a trail that runs to the edge of the range on
	// the picture rather than half off it.
	ringSpan = 0.85

	// minDepth is how close in front of the camera a point may be and still be
	// drawn, in nautical miles. A point behind the camera projects to a
	// mirrored ghost of itself and one just in front projects to an enormous
	// coordinate, so both are refused here rather than left to the canvas to
	// reject a pixel at a time.
	minDepth = 0.25

	// guardBoxes is how many scope boxes away from the centre a projected
	// point may land and still be drawn. Perspective puts the far corner of a
	// long trail well outside the box, and the canvas clips it for nothing,
	// but a line running to a point a million pixels away is a million
	// rejected Set calls. Three boxes is past anything the camera frames.
	guardBoxes = 3.0

	// minOrbitNm is how close the camera may get to its target, in range
	// radii. It only ever bites at the top of the exaggeration range with the
	// elevation right up, where the height of the envelope is worth more
	// distance than framing the ring asks for. See newCamera3.
	minOrbitNm = 1.5
)

// point3 is a point in the 3D view's world: east and north in nautical miles
// from the receiver, up in nautical miles after the exaggeration.
//
// It is a value everywhere. A frame projects a few thousand of these and a
// pointer would be the only reason for any of them to reach the heap.
type point3 struct {
	east  float64
	north float64
	up    float64
}

// minus is the vector from other to this point.
func (p point3) minus(other point3) point3 {
	return point3{east: p.east - other.east, north: p.north - other.north, up: p.up - other.up}
}

// dot is the scalar product, which is how a world offset is resolved onto one
// of the camera's three axes.
func (p point3) dot(other point3) float64 {
	return p.east*other.east + p.north*other.north + p.up*other.up
}

// camera3 is the perspective camera the 3D view looks through: where it sits,
// the three axes it sees along, and the lens.
//
// It is built once per frame and copied by value into everything that
// projects, which is what keeps the draw path free of allocations.
type camera3 struct {
	eye     point3
	right   point3
	above   point3
	forward point3

	focal   float64
	centerX int
	centerY int

	// limitX and limitY are how far from the centre of the box a projected
	// point may land before it is culled. See guardBoxes.
	limitX float64
	limitY float64
}

// newCamera3 frames the outer range ring inside the scope box.
//
// The camera orbits the point directly above the receiver at half the height
// of the envelope, so the bowl sits in the middle of the picture rather than
// above or below it. Azimuth zero puts it due south looking north, which is
// the orientation the scope view has: east is then to the right of the screen
// and the cardinal letters land where the eye expects them. Increasing azimuth
// walks it clockwise around the traffic.
//
// The lens is fixed and the camera moves. focal is the box's own width, a
// horizontal field of view of about 53 degrees, which is wide enough to hold
// the envelope without the fisheye a shorter lens gives a scene this deep.
//
// The distance is solved from that. The ring's widest pair of points sit one
// range radius off the view axis, so half the ring covers focal*scopeNm/depth
// pixels and that has to come to ringSpan of half the box. The depth in
// question is not the orbit distance: the camera looks down at a target half
// the envelope's height up, and the ring is on the ground below it, which puts
// the ring that much further away again. Subtracting it is what makes the ring
// come out at the width asked for rather than at four fifths of it.
//
// minOrbitNm is the floor under the answer. At a high exaggeration and a steep
// elevation the height correction is larger than the whole distance, and
// without the floor the camera would end up beside or behind its own target.
func newCamera3(box image.Rectangle, scopeNm, azimuthDeg, elevationDeg, topNm float64) (camera3, bool) {
	if box.Empty() || scopeNm <= 0 || math.IsNaN(scopeNm) {
		return camera3{}, false
	}

	width := float64(box.Dx())
	focal := width

	sinAz, cosAz := math.Sincos(azimuthDeg * math.Pi / halfCircle)
	sinEl, cosEl := math.Sincos(elevationDeg * math.Pi / halfCircle)

	target := point3{up: topNm / 2}
	framed := focal * scopeNm / (ringSpan / 2 * width)
	distance := max(framed-target.up*sinEl, minOrbitNm*scopeNm)
	away := point3{east: -sinAz * cosEl, north: -cosAz * cosEl, up: sinEl}
	forward := point3{east: -away.east, north: -away.north, up: -away.up}

	return camera3{
		eye:     point3{east: away.east * distance, north: away.north * distance, up: target.up + away.up*distance},
		forward: forward,

		// The camera is never rolled, so its right axis is the forward
		// direction's ground track turned a quarter turn: the cross product
		// with the world's up has no vertical part at all, and dividing out
		// the cosine of the elevation leaves a unit vector without a square
		// root. above is then right crossed with forward, which is a unit
		// vector for the same reason.
		right: point3{east: cosAz, north: -sinAz},
		above: point3{east: sinAz * sinEl, north: cosAz * sinEl, up: cosEl},

		focal:   focal,
		centerX: box.Min.X + box.Dx()/2,
		centerY: box.Min.Y + box.Dy()/2,
		limitX:  guardBoxes * width,
		limitY:  guardBoxes * float64(box.Dy()),
	}, true
}

// at projects a world point onto the canvas, reporting false when the camera
// cannot see it.
//
// Three things make a point invisible: sitting behind the camera or almost on
// its lens, which the depth test catches, and landing so far outside the box
// that drawing a line to it would cost more than the line is worth. A NaN
// anywhere in the point comes out as a NaN depth, because multiplying a NaN by
// a zero axis component still gives a NaN, so the depth test refuses it before
// anything is converted to an integer.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (c camera3) at(world point3) (int, int, bool) {
	rel := world.minus(c.eye)

	depth := rel.dot(c.forward)
	if math.IsNaN(depth) || depth < minDepth {
		return 0, 0, false
	}

	scale := c.focal / depth
	offX, offY := rel.dot(c.right)*scale, rel.dot(c.above)*scale

	if math.Abs(offX) > c.limitX || math.Abs(offY) > c.limitY {
		return 0, 0, false
	}

	return c.centerX + int(math.Round(offX)), c.centerY - int(math.Round(offY)), true
}

// cameraAzimuth is where the camera is pointing on this frame, in degrees.
//
// While the orbit is running it is wound forward from the azimuth the last
// keypress left, at one revolution per orbitPeriod, measured on the render
// clock rather than on the frame's own timestamp: it is an animation and has
// to advance once per drawn frame whatever the feed is doing. Stopped, it is
// that azimuth and nothing else.
func (s *Scene) cameraAzimuth(elapsed time.Duration) float64 {
	if !s.orbiting {
		return s.azimuth
	}

	turns := float64(elapsed-s.azimuthAt) / float64(orbitPeriod)

	return wrapDegrees(s.azimuth + turns*degreesPerCircle)
}

// wrapDegrees brings an angle back into 0 to 360, the way a compass rose does.
func wrapDegrees(degrees float64) float64 {
	wrapped := math.Mod(degrees, degreesPerCircle)
	if wrapped < 0 {
		wrapped += degreesPerCircle
	}

	return wrapped
}
