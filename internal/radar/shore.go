package radar

import (
	"image"
	"image/color"
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

	// waterFade is how far the water tint is pulled off the field towards the
	// data colour.
	//
	// Eight per cent is the whole argument. The tint has one job, which is to
	// say at a glance which side of a coastline is sea, and it has to do that
	// without becoming something anybody reads: an aircraft two pixels wide
	// has to stand off it, the range rings have to stay the loudest lines on
	// the scope, and a shoreline drawn in the quietest colour in the palette
	// still has to be visible against it. Anything stronger turns the scope
	// into a map that happens to have aircraft on it, which is the line the
	// shore colour was picked to stay on the right side of.
	//
	// Fading towards Data rather than towards a colour of its own is what
	// makes it work in all six palettes without a table: Data is the cyan a
	// glass panel sets a reading in, the pale green of the phosphor look and
	// the plain ink of mono, so the tint comes out cyan-black on Glass night,
	// a deeper green on Phosphor, a lighter grey on Mono and a faint
	// blue-grey on the three day palettes.
	waterFade = 0.08

	// minFillRing is the fewest points that enclose an area worth filling.
	minFillRing = 3
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

// landSpan is one projected ring's window on the Scene's point buffer.
type landSpan struct {
	start int
	end   int
}

// waterInk is the tint the sea is filled with.
//
// It is worked out here rather than carried on the palette for the reason
// envelope.go works its two out here: internal/theme is the colours somebody
// picked, and this is one of them mixed with the field for a job only the
// scene knows about. A palette that had to carry every derived shade would
// have to be re-picked every time a scene found a new use for one.
func (s *Scene) waterInk() color.RGBA { return s.fade(s.pal.Data, waterFade) }

// drawShore paints the water, the land and the coastlines that fall on the
// scope, in that order.
//
// The fill goes down first and the outlines over it, so the join between sea
// and land is covered by the line that describes it and the jagged edge the
// fill leaves never shows. fillGround is the first half; everything below the
// call to it is the second.
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

	s.fillGround(dst, proj, ring, latSpan, lonSpan)

	s.shoreSet.Within(
		proj.lat0-latSpan, proj.lat0+latSpan,
		proj.lon0-lonSpan, proj.lon0+lonSpan,
		func(line shore.Polyline) { s.drawShoreLine(dst, proj, ring, line) },
	)
}

// fillGround tints the sea and paints the land back out of it, under the
// outlines drawn on top.
//
// The order is water first and land second rather than the other way round
// because a coastline is a line: nothing in the data says which side of it is
// sea, which is the whole reason the land polygons are carried at all. So the
// ground is flooded, and the land rings take it back.
//
// One even-odd pass does the taking. A lake arrives as another ring inside the
// ring around it, so a pixel in the IJsselmeer is enclosed twice, comes out
// even, and is left as the water it was flooded with. Nothing here has to know
// which rings are lakes, and neither does the file they came out of.
//
// Outside the flooded ground nothing is painted that would show: the land fill
// is the field colour, which is what the layer was cleared to, and the sea and
// the lakes are left alone. That is what lets the fill be clipped to a
// rectangle while the scope is a disc.
func (s *Scene) fillGround(dst *canvas.Canvas, proj projector, ring circle, latSpan, lonSpan float64) {
	box := s.floodWater(dst, ring)

	s.landPoints = s.landPoints[:0]
	s.landSpans = s.landSpans[:0]

	s.shoreSet.LandWithin(
		proj.lat0-latSpan, proj.lat0+latSpan,
		proj.lon0-lonSpan, proj.lon0+lonSpan,
		func(line shore.Polyline) { s.collectLand(proj, box, line) },
	)

	s.landRings = s.landRings[:0]
	for _, span := range s.landSpans {
		s.landRings = append(s.landRings, s.landPoints[span.start:span.end:span.end])
	}

	dst.FillPolygon(s.landRings, box, s.pal.Field)
}

// floodWater paints the ground the scope covers in the water tint and returns
// the rectangle the land fill is clipped to.
//
// The ground is the range ring's own disc in the scope view, because that is
// where the picture is: a square of sea behind a round scope would put tint
// under the flight strips. The bare view has no ring and reaches to the
// corners, so there the ground is the whole canvas.
func (s *Scene) floodWater(dst *canvas.Canvas, ring circle) image.Rectangle {
	water := s.waterInk()

	if s.minimal() {
		dst.Clear(water)

		return dst.Bounds()
	}

	centerX, centerY := int(ring.centerX), int(ring.centerY)
	radius := int(math.Round(ring.radius))

	dst.FillCircle(centerX, centerY, radius, water)

	return image.Rect(centerX-radius, centerY-radius, centerX+radius+1, centerY+radius+1)
}

// collectLand projects one land ring onto the canvas and records it for the
// fill, unless it lands nowhere near the picture.
//
// Points that round onto the pixel the last one did are dropped. That is the
// only thinning the fill does, and it is deliberately weaker than the one
// drawShoreLine applies to the outlines: two neighbouring cells hold pieces
// cut from the same ring, and they only meet without a seam because both hold
// the crossing points exactly. Thinning by distance would drop such a point
// from one piece and keep it in the other, leaving a hairline of sea along a
// cell boundary a thousand miles long. Dropping a point that lands on a pixel
// already taken cannot move an edge off that pixel, so it cannot open one.
//
// A ring whose own bounding box misses box is thrown away whole, which at the
// narrow ranges is most of what a five degree cell holds: a cell is a couple
// of thousand pixels across when the scope is five hundred. Throwing it away
// is safe rather than merely close enough, because a closed ring crosses any
// row an even number of times. Those crossings pair off into spans that lie
// entirely outside box and are clipped away, so removing them leaves the
// parity inside box exactly where it was.
func (s *Scene) collectLand(proj projector, box image.Rectangle, line shore.Polyline) {
	if len(line) < minFillRing {
		return
	}

	start := len(s.landPoints)
	reach := image.Rectangle{}

	for _, point := range line {
		pixel := pixelOf(proj.offset(point.Lat, point.Lon))

		if len(s.landPoints) > start && s.landPoints[len(s.landPoints)-1] == pixel {
			continue
		}

		s.landPoints = append(s.landPoints, pixel)
		reach = grow(reach, pixel)
	}

	if len(s.landPoints)-start < minFillRing || !reach.Overlaps(box) {
		s.landPoints = s.landPoints[:start]

		return
	}

	s.landSpans = append(s.landSpans, landSpan{start: start, end: len(s.landPoints)})
}

// grow widens a bounding box to hold one more pixel.
//
// An empty rectangle is the seed rather than a sentinel pair of infinities:
// image.Rectangle.Union treats an empty rectangle as nothing at all, so the
// first point sets the box and every later one widens it.
func grow(box image.Rectangle, point image.Point) image.Rectangle {
	return box.Union(image.Rectangle{Min: point, Max: point.Add(image.Point{X: 1, Y: 1})})
}

// pixelOf rounds a projected position onto the pixel grid.
func pixelOf(x, y float64) image.Point {
	return image.Point{X: int(math.Round(x)), Y: int(math.Round(y))}
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
