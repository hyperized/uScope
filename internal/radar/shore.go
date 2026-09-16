package radar

import (
	"math"

	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// The shore overlay's measurements.
const (
	// shoreMinSegment is how far a segment has to run, in pixels, before it is
	// worth drawing. Natural Earth carries a point every few hundred metres,
	// which at any range uScope shows is dozens of points inside one pixel.
	//
	// Points closer together than this are folded into the next segment rather
	// than dropped, so the coastline thins out as the range widens instead of
	// disappearing. Dropping them one at a time would erase the Dutch coast at
	// 200 nautical miles, which is exactly the view the overlay is for.
	shoreMinSegment = 0.7

	// minCosLat floors the cosine the longitude span is divided by. A receiver
	// at the pole has a cosine of zero and would ask for a box infinitely
	// wide; a hundredth is a little under 89.5 degrees, past which the box is
	// the whole world anyway.
	minCosLat = 0.01
)

// circle is the scope's outer ring, which shore segments are clipped against.
type circle struct {
	centerX float64
	centerY float64
	radius  float64
}

// segment is a straight line on the canvas, in pixels.
type segment struct {
	fromX float64
	fromY float64
	toX   float64
	toY   float64
}

// drawShore paints the coastlines and lake shores that fall on the scope.
//
// Both the box and the clipping circle are taken off the projection rather
// than off the receiver and the range ring. The two are the same thing in the
// ordinary view, where limitNm is the range and the origin is the receiver.
// They are not the same in minimal mode, which projects around the traffic's
// own centre and reaches out to the canvas corner: a box around the receiver
// would fetch the wrong stretch of coast, and a circle at the range radius
// would leave a ring-shaped edge on a view that has no rings.
//
// The box is the origin plus and minus that reach in degrees, with longitude
// widened by the cosine of the latitude, because a degree of longitude is
// shorter than a degree of latitude everywhere except the equator. It is a
// box around a circle, so it asks for a little more than the scope shows and
// the clipping throws the rest away.
//
// A nil set draws nothing. That is the normal state when uScope was started
// without the data rather than a failure, so there is nothing to report.
func (s *Scene) drawShore(dst *canvas.Canvas, view scopeFrame) {
	if s.shoreSet == nil {
		return
	}

	proj := view.proj

	latSpan := proj.limitNm / nmPerDegree
	lonSpan := latSpan / max(proj.cosLat0, minCosLat)

	ring := circle{
		centerX: float64(view.geom.centerX),
		centerY: float64(view.geom.centerY),
		radius:  proj.limitNm * proj.scale,
	}

	s.shoreSet.Within(
		proj.lat0-latSpan, proj.lat0+latSpan,
		proj.lon0-lonSpan, proj.lon0+lonSpan,
		func(line shore.Polyline) { s.drawShoreLine(dst, proj, ring, line) },
	)
}

// drawShoreLine projects one polyline and draws whatever part of it lands on
// the scope.
//
// The last point is always drawn even when it is closer than shoreMinSegment,
// so a short line still reaches its own end rather than stopping one segment
// early. The distance test is per axis rather than a hypotenuse: it is off by
// at most a factor of the square root of two on a diagonal, which for deciding
// whether a segment is worth a pixel is not worth a square root.
func (s *Scene) drawShoreLine(dst *canvas.Canvas, proj projector, ring circle, line shore.Polyline) {
	if len(line) < 2 {
		return
	}

	fromX, fromY := proj.offset(line[0].Lat, line[0].Lon)
	last := len(line) - 1

	for index := 1; index <= last; index++ {
		toX, toY := proj.offset(line[index].Lat, line[index].Lon)

		if index < last && math.Abs(toX-fromX) < shoreMinSegment && math.Abs(toY-fromY) < shoreMinSegment {
			continue
		}

		if piece, inside := ring.clip(segment{fromX: fromX, fromY: fromY, toX: toX, toY: toY}); inside {
			dst.LineAA(piece.fromX, piece.fromY, piece.toX, piece.toY, s.pal.Shore)
		}

		fromX, fromY = toX, toY
	}
}

// clip trims a segment to the ring, reporting false when none of it is inside.
//
// The scope shares its canvas with the right column, so a coastline that runs
// off the ring has to be cut rather than left to the canvas bounds: an uncut
// segment would draw straight across the flight list. Dropping the whole
// segment instead would leave the coastline ending at whichever point happened
// to fall inside, a ragged edge that moves as the range changes.
//
// The cut is the segment's parametric form substituted into the circle
// equation, which is a quadratic in t. Halving the usual b takes the 2 and the
// 4 out of the formula and leaves the same roots. Whatever part of [0, 1] sits
// between them is the piece that is inside.
func (c circle) clip(seg segment) (segment, bool) {
	runX, runY := seg.toX-seg.fromX, seg.toY-seg.fromY
	offX, offY := seg.fromX-c.centerX, seg.fromY-c.centerY

	span := runX*runX + runY*runY
	outside := offX*offX + offY*offY - c.radius*c.radius

	// A segment of no length is one point, so it is in or out as a whole.
	if span == 0 {
		return seg, outside <= 0
	}

	half := offX*runX + offY*runY

	discriminant := half*half - span*outside
	if discriminant < 0 {
		return segment{}, false
	}

	root := math.Sqrt(discriminant)
	enter := max((-half-root)/span, 0)
	leave := min((-half+root)/span, 1)

	if enter >= leave {
		return segment{}, false
	}

	return segment{
		fromX: seg.fromX + enter*runX,
		fromY: seg.fromY + enter*runY,
		toX:   seg.fromX + leave*runX,
		toY:   seg.fromY + leave*runY,
	}, true
}
