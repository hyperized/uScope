package radar

import (
	"image"
	"image/color"
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

// The haze the tilted ground fades into past the reach.
const (
	// hazeBands is how many steps the tint takes on its way down to the
	// horizon colour. At night that is twenty levels of blue over black and
	// twelve steps were plenty, but a dusk horizon is a couple of hundred
	// levels off the water tint, and at twelve every step was a visible arc.
	// Thirty-two keeps a step to six or seven levels at the worst, which
	// reads as a gradient. The bands cost a few crossings per row each, not a
	// fill each (see projectHaze3); a blend per pixel would be smoother again
	// and would cost a read-modify-write over half a 4K frame on every frame.
	hazeBands = 32

	// hazeDepth is how far the haze reaches past the ground at full tint, as
	// a multiple of that ground's own radius around the camera. Four puts the
	// last band a quarter of the way from the full tint to the horizon on the
	// screen, which is far enough that the fade reads as distance rather than
	// as the edge of a plate.
	hazeDepth = 4.0

	// maxHazeNm caps how far the haze runs past the full tint, in nautical
	// miles. The land the haze needs is queried out to its far edge, and at
	// two hundred miles of range four times the ground would be a box across
	// half the northern hemisphere, where the local projection the view uses
	// is no longer anything like the map. Nine hundred miles is fifteen
	// degrees of latitude; past it the fade is merely steeper.
	maxHazeNm = 900.0

	// hazePast is the band past the far edge of the haze, where nothing
	// the ground carries is drawn at all.
	hazePast = hazeBands + 1
)

// haze3 is the ground the tilted view stands on, measured from the point on
// the floor under the camera: the disc that carries the full water tint, and
// the distance the tint has faded to nothing at.
//
// It is centred on the camera rather than on the origin because haze is a
// matter of distance from the eye. Centred on the origin, the ground between
// the camera and the reach would fade as well and the bottom of the picture
// would go dark in front of the eye.
type haze3 struct {
	foot point3
	near float64
	far  float64
}

// haze measures this frame's haze. The full tint reaches from the camera's
// foot to the far side of the reach, so everything the view draws traffic and
// coastline over is at full strength and only the ground beyond it fades.
func (v scene3) haze() haze3 {
	foot := point3{east: v.cam.eye.east, north: v.cam.eye.north}
	near := math.Hypot(foot.east, foot.north) + v.reachNm

	return haze3{foot: foot, near: near, far: near + min((hazeDepth-1)*near, maxHazeNm)}
}

// past reports whether every point of a line's box on the floor is further
// from the camera's foot than the haze reaches, which is when nothing inside
// a land ring is ground the flood tinted and no piece of a coastline is drawn.
func (h haze3) past(ring boxedLine3) bool {
	return h.nearest(ring) > h.far
}

// nearest is how far the closest point of a line's box on the floor is from
// the camera's foot, which is zero when the foot is inside the box.
func (h haze3) nearest(ring boxedLine3) float64 {
	east := max(ring.low.east-h.foot.east, 0, h.foot.east-ring.high.east)
	north := max(ring.low.north-h.foot.north, 0, h.foot.north-ring.high.north)

	return math.Hypot(east, north)
}

// reachNm is how far from the origin the haze can reach, which is what the
// two shore caches are asked to hold.
func (h haze3) reachNm() float64 {
	return math.Hypot(h.foot.east, h.foot.north) + h.far
}

// band is which band of the haze a point on the floor lies in: zero inside
// the full tint, one to hazeBands across the fade, and hazePast beyond it.
//
// It is edge run backwards. The radii are even steps in one over the
// distance, so the step a distance falls in is read straight off its
// reciprocal rather than searched for. A distance that is not a number comes
// out past, so the point is never drawn rather than drawn in a colour picked
// off a conversion the spec does not define.
func (h haze3) band(at point3) int {
	distance := math.Hypot(at.east-h.foot.east, at.north-h.foot.north)

	switch {
	case !(distance < h.far):
		return hazePast
	case distance <= h.near:
		return 0
	}

	along := (1/h.near - 1/distance) / (1/h.near - 1/h.far)

	return min(int(along*hazeBands)+1, hazeBands)
}

// edge is the radius of the boundary between two bands, from zero at the full
// tint's own edge to hazeBands at the far edge of the haze.
//
// The radii are spaced evenly in the reciprocal of the distance rather than in
// the distance itself. On the floor of a perspective scene a point's height
// below the horizon goes as one over its distance, so this is what gives the
// bands the same height on the screen and the fade an even slope down it.
func (h haze3) edge(band int) float64 {
	along := float64(band) / hazeBands

	return 1 / ((1-along)/h.near + along/h.far)
}

// hazeInk is the colour of one band, numbered from one beyond the full tint
// out to hazeBands, for ground whose full tint is col: the palette's Water or
// Land. Each band takes the strength the middle of its own step would have,
// so the first is a shade under the full tint and the last a shade over the
// horizon colour, and neither end of the fade lands on a visible edge.
//
// col is already mixed into the field, so it is let off the field by the
// band's whole strength. On the handheld palettes that is the picture the old
// fade of Data or OK drew, because strength times an 8 per cent mix of the ink
// is strength of the mix itself; only the tint's own rounding can move a
// channel. Land and sea go through the same call with only the colour
// changed, so both lose the same share of their tint in a band and a coast
// reads at the same strength on either side of it all the way out.
func (s *Scene) hazeInk(col color.RGBA, band int) color.RGBA {
	strength := hazeStrength(band)

	return s.hazed(col, strength, strength)
}

// landInks3 is the land's colour for every count of haze circles around a
// pixel, which is how FillLayersZoned hands the bands over: none is past the
// haze and takes the horizon colour, all of them is the full tint's own disc
// and takes landInk, and each count between is a band of the fade.
//
// The land fades by the bands the water does, from the very same circles, so
// a coast never shows the two at different strengths. Without a sky the
// horizon is the field, and each band is then landInk's own fade at that
// band's share, the way the water's is.
func (s *Scene) landInks3() [hazeBands + 2]color.RGBA {
	var inks [hazeBands + 2]color.RGBA

	for circles := range inks {
		band := hazePast - circles

		switch band {
		case 0:
			inks[circles] = s.landInk()
		case hazePast:
			inks[circles] = s.skyHorizon()
		default:
			inks[circles] = s.hazeInk(s.landInk(), band)
		}
	}

	return inks
}

// hazed is col let off the field by alpha, the way fade mixes it, and then
// carried towards the horizon colour by however much of strength is missing.
//
// It is one weighted sum of three colours rather than two mixes one after the
// other, so nothing is rounded halfway. With no sky the horizon is the field,
// its term is exactly zero, and the answer is fade's to the bit, which is
// what keeps a night frame identical to the one drawn before the sky existed.
// alpha is never more than strength, which keeps the weights a mix.
func (s *Scene) hazed(col color.RGBA, alpha, strength float64) color.RGBA {
	far := s.skyHorizon()

	return color.RGBA{
		R: hazeChannel(s.pal.Field.R, col.R, far.R, alpha, strength),
		G: hazeChannel(s.pal.Field.G, col.G, far.G, alpha, strength),
		B: hazeChannel(s.pal.Field.B, col.B, far.B, alpha, strength),
		A: opaque,
	}
}

// hazeChannel is one channel of hazed.
func hazeChannel(field, ink, far uint8, alpha, strength float64) uint8 {
	near := float64(field) + (float64(ink)-float64(field))*alpha

	return uint8(math.Round(near + (float64(far)-float64(field))*(1-strength))) //nolint:gosec // a mix of bytes.
}

// hazeStrength is how much of the full tint is left in one band, from a shade
// under one in the first to a shade over nothing in the last.
func hazeStrength(band int) float64 {
	return 1 - (float64(band)-sectorCentre)/hazeBands
}

// circlePoint is one of the points a circle on the floor is drawn as: step
// out of ringSegments the way round it.
func (v scene3) circlePoint(centre point3, radius float64, step int) groundPoint {
	sin, cos := math.Sincos(2 * math.Pi * float64(step) / ringSegments)

	return v.groundPoint(point3{east: centre.east + radius*sin, north: centre.north + radius*cos})
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
// The ground runs on past the reach and fades, in bands, into the colour of
// the sky at the horizon: the sky's horizon colour when the sun is known, the
// field when it is not. The land fades with it; see collectLandRings3. It
// used to stop at the reach, which left the sea ending on a hard ellipse with black beyond it, a horizon line
// the bare view exists not to have. The tint is full out to the far side of
// the reach, so the traffic, the coastline and the rings all sit on the ground
// they always did; see haze3 for where the fade is measured from.
//
// The land is filled out to the far edge of the haze, so a country beyond the
// reach stays land rather than turning into sea. The cost of that was the
// reason the plate used to stop at the reach, and measuring it moved the
// argument: walking the cells of a box a thousand miles across takes
// microseconds, and what costs is projecting the seventy-odd thousand points
// in it. collectLandRings3 skips the rings the camera cannot see before
// projecting any of them, which leaves the whole tilted ground at about two milliseconds
// a 4K frame over the real coastline. The coastline outlines run out into
// the haze as well and fade with it; see drawCoast3.
//
// Both halves run through the Scene's ring scratch one after the other: the
// bands go into it and are copied out, and the land rings take the buffer
// back. That
// is what keeps a fill on a per-frame path free of allocations once the
// scratch has grown, which is what both tilted views need it to be.
func (s *Scene) fillGround3(dst *canvas.Canvas, view scene3, haze haze3) {
	box := dst.Bounds()

	s.projectHaze3(view, haze)
	s.collectLandRings3(view, haze, box)

	// The water is the lower layer and the land the one over it, filled in
	// one sweep, so a pixel of land is written once rather than flooded
	// first and painted back.
	water, land := s.waterInks3(), s.landInks3()
	layers := [...]canvas.ZonedLayer{
		{Rings: s.hazeRings[hazeBands:], Inks: water[:]},
		{Rings: s.landRings, Inks: land[:]},
	}

	dst.FillLayersZoned(layers[:], s.hazeRings, box)
}

// projectHaze3 projects the haze's circles into hazeRings: the water is the
// outermost of them filled, coloured by how many of the circles enclose each
// pixel, and the land is coloured by the same count.
//
// The flood is one fill of the outermost circle rather than one per band, so
// the bands cost a few crossings per row rather than a sweep each. The bands
// used to be one even-odd annulus per fill, which was the same picture at
// twelve bands and more than twice the frame time at thirty-two.
//
// The circles are projected as ringSegments straight pieces, the same
// sixty-four drawCircle3 draws a range ring as. There is no FillCircle for
// this: on the floor of a perspective scene a circle is an ellipse the camera
// sees from inside its own plane, and nothing on the canvas draws one of
// those. Every circle surrounds the camera's foot, so every one is cut by
// addRingStep where it passes behind the lens.
//
// All the circles go into the scratch before any of them is sliced, for the
// reason the land rings are recorded as spans: see landPoints. They are
// projected through the land scratch, the one addRingStep
// writes, and copied out of it before the land takes it back: a couple of
// thousand points, against tens of thousands of land points that would
// otherwise grow two buffers to the land's size in turn.
func (s *Scene) projectHaze3(view scene3, haze haze3) {
	var starts [hazeBands + 2]int

	s.landPoints = s.landPoints[:0]

	for band := 0; band <= hazeBands; band++ {
		starts[band] = len(s.landPoints)
		s.addCircle3(view, haze.foot, haze.edge(band))
	}

	starts[hazeBands+1] = len(s.landPoints)
	s.hazePoints = append(s.hazePoints[:0], s.landPoints...)

	s.hazeRings = s.hazeRings[:0]
	for band := 0; band <= hazeBands; band++ {
		s.hazeRings = append(s.hazeRings, s.hazePoints[starts[band]:starts[band+1]:starts[band+1]])
	}
}

// waterInks3 is the water's colour for every count of haze circles around a
// pixel, the way landInks3 is the land's: the full tint inside them all, each
// band's tint between, and nothing that is ever painted outside the last,
// since that circle is the ring the water is filled inside.
func (s *Scene) waterInks3() [hazeBands + 2]color.RGBA {
	var inks [hazeBands + 2]color.RGBA

	for circles := 1; circles < len(inks); circles++ {
		band := hazePast - circles
		if band == 0 {
			inks[circles] = s.waterInk()

			continue
		}

		inks[circles] = s.hazeInk(s.waterInk(), band)
	}

	return inks
}

// addCircle3 projects one circle on the floor onto the end of the point
// scratch.
func (s *Scene) addCircle3(view scene3, centre point3, radius float64) {
	run := ringRun{start: len(s.landPoints)}
	last := view.circlePoint(centre, radius, ringSegments-1)

	for step := range ringSegments {
		next := view.circlePoint(centre, radius, step)
		s.addRingStep(view, &run, last, next)
		last = next
	}
}

// collectLandRings3 projects every land ring the ground paints back out of
// the water into landRings. They are filled in one even-odd pass, coloured
// band by band by the haze's circles: landInk inside the full tint, fading
// out to the horizon colour past the haze.
//
// The rings come from the cache rather than from the shore set, and two kinds
// are skipped before a point of them is projected. A ring whose box on the
// floor is wholly outside what the camera sees of the window is the one
// collectLand3 would drop anyway, a ring that misses the clip box goes whole,
// asked of four corners instead of every vertex. A ring wholly past the haze
// is the other: everything inside it is past the haze, which the clear
// already painted the horizon colour, so filling it would paint that over
// itself. Most of
// the land the cache holds is behind the camera, off to one side of it or out
// beyond the haze, so most of the projection never happens.
//
// Rings the cull keeps that run on past the haze cost nothing that shows:
// the part past it is painted the horizon colour over a clear already that
// colour, which is what lets the fill be clipped to the window while the
// ground is round.
func (s *Scene) collectLandRings3(view scene3, haze haze3, box image.Rectangle) {
	s.landPoints = s.landPoints[:0]
	s.landSpans = s.landSpans[:0]

	s.cacheLines3(&s.land3, landRings, view, haze.reachNm())

	sight := view.cam.frustum(box.Inset(-cullMarginPx))

	for _, ring := range s.land3.lines {
		if !haze.past(ring) && !sight.misses(view.cam, ring.low, ring.high) {
			s.collectLand3(view, box, ring.line)
		}
	}

	s.landRings = s.landRings[:0]
	for _, span := range s.landSpans {
		s.landRings = append(s.landRings, s.landPoints[span.start:span.end:span.end])
	}
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
//
// The arithmetic always starts from the end in front of the lens, whichever
// way round the ring walks the edge. Two neighbouring land cells walk their
// shared boundary in opposite directions, and their edges cancel in the fill
// only if both land on the same pixels; interpolating from opposite ends can
// differ in the last bit, and that bit can round onto the next pixel.
func crossNear(last, next groundPoint) point3 {
	if last.depth < next.depth {
		last, next = next, last
	}

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
