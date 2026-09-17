package radar

import (
	"image"
	"math"

	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
)

// groundPoint is one point on the floor of the world with how far in front of
// the camera it sits worked out at the same time.
//
// The depth is carried rather than asked for twice because every point of a
// ring is measured against the near plane once as the far end of one edge and
// once as the near end of the next.
type groundPoint struct {
	at    point3
	depth float64
}

// inFront reports whether the camera can see this point at all. A NaN depth
// comes out false, which cuts the point away rather than letting it reach a
// conversion the spec does not define.
func (p groundPoint) inFront() bool { return p.depth >= minDepth }

// ringRun is one ring's run into the Scene's point scratch: where in the
// scratch it started, and how far the part of it that survived reaches on the
// canvas.
type ringRun struct {
	start int
	reach image.Rectangle
}

// discPoint is one of the points the ground's edge is drawn as: step out of
// ringSegments the way round the circle the view reaches to.
func (v scene3) discPoint(step int) groundPoint {
	sin, cos := math.Sincos(2 * math.Pi * float64(step) / ringSegments)

	return v.groundPoint(point3{east: v.reachNm * sin, north: v.reachNm * cos})
}

// groundPoint carries a point on the floor together with its depth.
func (v scene3) groundPoint(at point3) groundPoint {
	return groundPoint{at: at, depth: v.cam.depth(at)}
}

// groundPointAt is groundPoint for a position rather than for a world offset.
func (v scene3) groundPointAt(latitude, longitude float64) groundPoint {
	return v.groundPoint(v.ground(latitude, longitude))
}

// fillGround3 tints the ground the tilted view stands on and paints the land
// back out of it, under the outlines drawn over the join.
//
// It is fillGround in perspective and the argument is the same one: nothing in
// a coastline says which side of it is sea, so the ground is flooded and the
// land rings take it back in a single even-odd pass. A lake arrives as a ring
// inside the ring around it, comes out enclosed twice, and is left as the
// water it was flooded with. What changes here is the shape of the ground and
// how a ring reaches the canvas.
//
// The ground is the disc the view reaches to, projected onto the floor. In the
// full view that is the outer range ring, which is the flat scope's rule word
// for word; in the bare one it is the two range radii minimalReach3 draws to.
// Either way it is the same circle drawShoreLine3 clips the outlines against
// and inRange judges the traffic by, so the sea stops exactly where the view
// stops saying anything, rather than running on over ground that has no
// coastline drawn on it.
//
// Filling the whole visible ground instead was the other option and it is not
// affordable. In perspective the floor runs to a horizon, and at a shallow
// tilt that horizon is inside the box: covering the picture up to it would
// need land rings out to a dozen range radii, which at 200 nautical miles is a
// quarter of the world queried on every frame. What the plate costs instead is
// a visible edge at the far side of the ground, and that is the edge the
// outlines and the traffic already stop at.
//
// Both halves run through the Scene's ring scratch one after the other: the
// disc goes into it, is filled, and the land rings take the buffer back. That
// is what keeps a fill on a per-frame path free of allocations once the
// scratch has grown, which is what both tilted views need it to be.
func (s *Scene) fillGround3(dst *canvas.Canvas, view scene3, latSpan, lonSpan float64) {
	box := dst.Bounds()

	s.floodGround3(dst, view, box)
	s.fillLand3(dst, view, box, latSpan, lonSpan)
}

// floodGround3 paints the ground disc in the water tint.
//
// The disc is projected as ringSegments straight pieces, the same sixty-four
// drawCircle3 draws a range ring as, so the fill's edge and the outer ring
// drawn over it later are the same polygon. There is no FillCircle for this:
// on the floor of a perspective scene a circle is an ellipse the camera sees
// from inside its own plane, and nothing on the canvas draws one of those.
func (s *Scene) floodGround3(dst *canvas.Canvas, view scene3, box image.Rectangle) {
	s.landPoints = s.landPoints[:0]

	run := ringRun{}
	last := view.discPoint(ringSegments - 1)

	for step := range ringSegments {
		next := view.discPoint(step)
		s.addRingStep(view, &run, last, next)
		last = next
	}

	s.landRings = append(s.landRings[:0], s.landPoints)

	dst.FillPolygon(s.landRings, box, s.waterInk())
}

// fillLand3 paints every land ring in view back out of the flooded ground, in
// one even-odd pass.
//
// The box is the outline pass's own, so every ring that gets a coastline drawn
// on it gets filled and nothing else does. Rings the box catches outside the
// ground disc cost nothing that shows: they are painted in the field colour
// over a frame the background already cleared to it, which is what lets the
// fill be clipped to the window while the ground is round.
func (s *Scene) fillLand3(dst *canvas.Canvas, view scene3, box image.Rectangle, latSpan, lonSpan float64) {
	s.landPoints = s.landPoints[:0]
	s.landSpans = s.landSpans[:0]

	s.shoreSet.LandWithin(
		view.origin.lat-latSpan, view.origin.lat+latSpan,
		view.origin.lon-lonSpan, view.origin.lon+lonSpan,
		func(line shore.Polyline) { s.collectLand3(view, box, line) },
	)

	s.landRings = s.landRings[:0]
	for _, span := range s.landSpans {
		s.landRings = append(s.landRings, s.landPoints[span.start:span.end:span.end])
	}

	dst.FillPolygon(s.landRings, box, s.pal.Field)
}

// collectLand3 projects one land ring onto the floor of the world and records
// it for the fill, unless nothing of it lands near the picture.
//
// It is collectLand in perspective and it throws a ring away on the same rule:
// one whose reach on the canvas misses the clip box goes whole. That is safe
// rather than merely close enough, because a closed ring crosses any row an
// even number of times and crossings that pair off outside the box are clipped
// away without moving the parity inside it.
//
// What it adds is the near-plane clip. See addRingStep.
func (s *Scene) collectLand3(view scene3, box image.Rectangle, line shore.Polyline) {
	if len(line) < minFillRing {
		return
	}

	run := ringRun{start: len(s.landPoints)}
	last := view.groundPointAt(line[len(line)-1].Lat, line[len(line)-1].Lon)

	for _, point := range line {
		next := view.groundPointAt(point.Lat, point.Lon)
		s.addRingStep(view, &run, last, next)
		last = next
	}

	if len(s.landPoints)-run.start < minFillRing || !run.reach.Overlaps(box) {
		s.landPoints = s.landPoints[:run.start]

		return
	}

	s.landSpans = append(s.landSpans, landSpan{start: run.start, end: len(s.landPoints)})
}

// addRingStep adds one edge of a ring: where it crosses the plane the camera
// sees past, and then its far end when that end is in front of the lens.
//
// It is one Sutherland-Hodgman pass against a single half-plane, run in the
// ground's own nautical miles rather than on the canvas, and it is what makes
// the fill's projection safe without a guard. Dropping a ring that reaches
// behind the camera, the way a coastline segment is dropped, is not an option
// here: a land polygon arrives cut to a five degree cell, which is three
// hundred nautical miles across, and the camera orbits a couple of range radii
// out, so at anything under about a hundred miles of range the ring around the
// receiver's own cell has points behind the lens. Dropping it would take the
// country out of the fill at exactly the ranges the view is used at.
//
// Cutting leaves a ring that runs along the near plane where the original ran
// behind the lens. That plane sits a quarter of a nautical mile in front of a
// camera tens of miles up, so it projects hundreds of screen heights below the
// picture and the bridge contributes nothing to any row the fill paints.
func (s *Scene) addRingStep(view scene3, run *ringRun, last, next groundPoint) {
	if last.inFront() != next.inFront() {
		s.addRingPoint(view, run, crossNear(last, next))
	}

	if next.inFront() {
		s.addRingPoint(view, run, next.at)
	}
}

// crossNear is where the edge between two points meets the plane the camera
// sees past.
//
// It is only ever asked about an edge with one end on each side, so the two
// depths differ and the denominator cannot be zero.
func crossNear(last, next groundPoint) point3 {
	fraction := (minDepth - last.depth) / (next.depth - last.depth)

	return point3{
		east:  last.at.east + fraction*(next.at.east-last.at.east),
		north: last.at.north + fraction*(next.at.north-last.at.north),
	}
}

// addRingPoint projects one vertex and records it, unless it lands on the
// pixel the vertex before it took.
//
// The collapse is collectLand's and it is the only thinning the tilted fill
// does either. Two neighbouring cells hold pieces cut from the same ring and
// they meet without a seam only because both carry the crossing points
// exactly; a point that rounds onto a pixel already taken cannot move an edge
// off that pixel, so dropping it cannot open one.
func (s *Scene) addRingPoint(view scene3, run *ringRun, at point3) {
	pixel := view.cam.pixel(at)

	if len(s.landPoints) > run.start && s.landPoints[len(s.landPoints)-1] == pixel {
		return
	}

	s.landPoints = append(s.landPoints, pixel)
	run.reach = grow(run.reach, pixel)
}
