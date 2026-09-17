package radar

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airports"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/shore"
	"github.com/hyperized/uScope/pkg/text"
)

// The 3D view's furniture.
const (
	// ringSegments is how many straight pieces a projected circle is drawn as.
	// Sixty-four is smooth at the panel's resolution, and it divides by the
	// dash period so a ring's pattern comes out even all the way round.
	ringSegments = 64

	// sectorCentre places a point in the middle of a bin rather than on its
	// edge, for both the bearing sectors and the altitude bands of the
	// measured envelope.
	sectorCentre = 0.5

	// bowlDashOn and bowlDashPeriod are the theoretical bowl's dash: two
	// segments drawn out of every four, so a ring is half line and half air.
	bowlDashOn     = 2
	bowlDashPeriod = 4

	// minimalReach3 is how far past the range the bare 3D view draws, as a
	// multiple of it.
	//
	// The flat bare view works its own cut-off out from the canvas corner,
	// because there a position either lands on a pixel or it does not, and the
	// corners are meant to show traffic a ring would have cut off. Perspective
	// has no such answer: how far the picture reaches depends on the tilt, and
	// a point behind the camera is not far away at all. So it is a multiple of
	// the range, and the camera's own guard drops whatever still lands off the
	// picture. Two radii is past anything the camera frames at any tilt.
	//
	// The full 3D view keeps the range itself, because there the outer ring is
	// the edge of the world and an aeroplane drawn outside it would sit beyond
	// the only thing that says how far the picture goes.
	minimalReach3 = 2.0
)

// dashPattern breaks a projected circle up: on segments drawn out of every
// period. A solid ring is one on out of every one.
type dashPattern struct {
	on     int
	period int
}

// The two patterns the view draws circles with. The range rings are solid
// because they are the floor the whole picture is measured against; the bowl
// is dashed so a theoretical surface cannot be mistaken for a measured one.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var (
	solidDash = dashPattern{on: 1, period: 1}
	bowlDash  = dashPattern{on: bowlDashOn, period: bowlDashPeriod}
)

// cardinals3 is where the four letters go on the ground, as multiples of the
// range along each axis.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var cardinals3 = [...]struct {
	letter string
	east   float64
	north  float64
}{
	{letter: "N", north: 1},
	{letter: "E", east: 1},
	{letter: "S", north: -1},
	{letter: "W", east: -1},
}

// scene3 is the 3D view measured for one frame: the camera, how a position
// becomes a point in the world, and how far the ground furniture reaches.
//
// plottable is false when nothing can be plotted against the receiver, which
// is the state before any position is known. The rings, the cardinals and the
// envelope are still drawn then, because all four are measured from the
// receiver rather than from a coordinate; the shore, the airfields and the
// aircraft are not, because there is nowhere to put them.
type scene3 struct {
	cam     camera3
	origin  geo
	cosLat0 float64
	scopeNm float64
	azimuth float64

	// reachNm is how far from the origin the ground furniture and the traffic
	// are drawn, which is the range in the full view and further out in the
	// bare one. See minimalReach3.
	reachNm float64

	// upScale is how many nautical miles of world one foot of altitude is
	// worth, which is the exaggeration divided by the feet in a mile.
	upScale float64

	// minSegNm is how far a coastline has to run on the ground, in nautical
	// miles, before it is worth a segment. It is the scope view's pixel
	// threshold converted at the ground's average scale: perspective makes the
	// near ground coarser and the far ground finer than that, which for
	// deciding whether a piece of coast is worth drawing is close enough.
	minSegNm float64

	plottable bool
}

// ground is where a position falls on the floor of the world.
//
// The projection is the same local equirectangular one the scope view uses,
// for the same reason: over the tens of nautical miles a scope covers the
// error is smaller than a pixel, and the two views have to agree about where
// an aeroplane is or switching between them would move it.
func (v scene3) ground(latitude, longitude float64) point3 {
	return point3{
		east:  (longitude - v.origin.lon) * nmPerDegree * v.cosLat0,
		north: (latitude - v.origin.lat) * nmPerDegree,
	}
}

// height is an altitude in feet as a height in the world, stretched by
// whatever --exaggerate asked for.
//
// A zero altitude is one nobody has decoded rather than sea level, the same
// reading bandColour gives it, so it sits on the ground and has no stalk to
// speak of.
func (v scene3) height(altitudeFt float64) float64 {
	if altitudeFt <= 0 {
		return 0
	}

	return altitudeFt * v.upScale
}

// inRange reports whether a point is inside the reach the ground furniture is
// drawn to, measured on the floor so an aircraft is judged by where it is
// rather than by how high it is.
func (v scene3) inRange(point point3) bool {
	return math.Hypot(point.east, point.north) <= v.reachNm
}

// world is where one aircraft fix sits in the 3D view's world: its shadow on
// the ground, lifted by whatever the altitude is worth after the
// exaggeration.
func (v scene3) world(latitude, longitude, altitudeFt float64) point3 {
	point := v.ground(latitude, longitude)
	point.up = v.height(altitudeFt)

	return point
}

// project is where one aircraft fix lands on the canvas: refused when it has
// no position, when it is outside the range, or when the camera cannot see it.
func (v scene3) project(latitude, longitude, altitudeFt float64) (int, int, bool) {
	if !positioned(latitude, longitude) {
		return 0, 0, false
	}

	point := v.world(latitude, longitude, altitudeFt)
	if !v.inRange(point) {
		return 0, 0, false
	}

	return v.cam.at(point)
}

// measure3D works out the camera and the projection for one frame, reporting
// false when there is no room for the view at all.
func (s *Scene) measure3D(lay *layout, frame source.Frame, elapsed time.Duration) (scene3, bool) {
	scopeNm := s.scopeRange.GetCurrent()
	azimuth := s.cameraAzimuth(elapsed)
	upScale := s.exaggerate / ftPerNm

	cam, drawable := newCamera3(lay.scope, framing3{
		scopeNm:     scopeNm,
		azimuth:     azimuth,
		elevation:   s.elevation,
		topNm:       bowlTopFt * upScale,
		topRadiusNm: bowlRadiusNm(bowlTopFt, scopeNm),
	})
	if !drawable {
		return scene3{}, false
	}

	origin := s.origin3(frame.Receiver)

	return scene3{
		cam:       cam,
		origin:    origin,
		cosLat0:   math.Cos(origin.lat * math.Pi / halfCircle),
		scopeNm:   scopeNm,
		reachNm:   s.reach3(scopeNm),
		azimuth:   azimuth,
		upScale:   upScale,
		minSegNm:  2 * scopeNm * shoreMinSegment / (ringSpan * float64(lay.scope.Dx())),
		plottable: positioned(origin.lat, origin.lon),
	}, true
}

// origin3 is the point the 3D picture is projected from.
//
// The bare view follows the traffic the way the flat bare one does, so its
// origin is the same centre minimal projects around. The full view never
// recentres: its range rings are measured from the receiver and its envelope
// is drawn around the antenna, so a centre that moved would make both lie.
func (s *Scene) origin3(receiver source.Receiver) geo {
	if s.bare() {
		return s.minimalOrigin(receiver)
	}

	return geo{lat: receiver.Latitude, lon: receiver.Longitude}
}

// reach3 is how far from the origin the picture is drawn. See minimalReach3.
func (s *Scene) reach3(scopeNm float64) float64 {
	if s.bare() {
		return scopeNm * minimalReach3
	}

	return scopeNm
}

// envelopeDrawn reports whether the receiving envelope is on screen.
//
// The bare 3D view never draws it, whatever the e key has the flag set to. The
// envelope is the largest piece of furniture in the picture and that view
// exists to have none; the key is refused there rather than silently ignored,
// for the reason toggleEnvelope gives.
func (s *Scene) envelopeDrawn() bool {
	return s.envelope && !s.bare()
}

// draw3D paints the perspective view: the ground, the envelope around it, and
// the traffic inside it.
//
// The order is back to front by a cheap rule rather than by a depth sort. The
// ground is under everything, the envelope is a wireframe around the outside
// of it, the trails sit behind the aircraft that made them, and the aircraft
// are what the eye is meant to land on. A wireframe has almost nothing to hide
// behind it, so sorting several thousand segments per frame would buy a
// picture nobody could tell from this one.
func (s *Scene) draw3D(lay *layout, frame source.Frame, elapsed time.Duration) {
	view, drawable := s.measure3D(lay, frame, elapsed)
	if !drawable {
		return
	}

	// Everything below draws through a window onto the scope box rather than
	// onto the whole frame. The picture is framed so the outer range ring fills
	// most of the box, and the envelope around it is taller than the range is
	// wide, so a good part of the scene genuinely falls outside: unclipped, the
	// bowl's meridians run straight across the flight list.
	clipped := *lay

	window, room := s.window(lay.dst, lay.scope)
	if !room {
		return
	}

	clipped.dst = window

	s.drawGround3(&clipped, view, frame.Receiver)

	if s.envelopeDrawn() {
		s.drawBowl3(window, view)
		s.drawMeasured3(window, view, frame.Coverage)
	}

	if view.plottable {
		s.drawTraffic3(&clipped, view, frame)
	}
}

// window is the canvas the 3D view draws through: dst seen through the scope
// box, kept between frames so the header it costs is paid when the canvas or
// the box moves rather than thirty times a second.
//
// The parent is compared by pointer rather than by its bounds, because the run
// loop throws a canvas away and allocates another of the same size when a
// terminal is resized to the same shape, and the window has to follow it. The
// old one stays reachable through clipOf until the next 3D frame replaces it,
// which is one canvas at most and only after a resize.
func (s *Scene) window(dst *canvas.Canvas, box image.Rectangle) (*canvas.Canvas, bool) {
	if s.clip != nil && s.clipOf == dst && s.clipBox == box {
		return s.clip, true
	}

	sub, room := dst.Sub(box)
	if !room {
		return nil, false
	}

	s.clip, s.clipOf, s.clipBox = sub, dst, box

	return sub, true
}

// drawGround3 paints the floor of the world: the water, the land and the
// coastline under everything, then the centre marks and the airfields.
//
// The two overlays ask shoreDrawn and airportsDrawn rather than reading a
// field, so the full view gets the scope's pair and the bare one gets the pair
// the bare views keep between them, which starts off.
func (s *Scene) drawGround3(lay *layout, view scene3, receiver source.Receiver) {
	if view.plottable && s.shoreDrawn() {
		s.drawShore3(lay.dst, view)
	}

	s.drawGroundMarks3(lay, view, receiver)

	if view.plottable && s.airportsDrawn() {
		s.drawAirports3(lay, view, airports.All())
	}
}

// drawGroundMarks3 puts down whichever mark the view on screen keeps at the
// middle of the world.
//
// The full view has the range rings and the four cardinal letters, which are
// what say how far the picture reaches and which way round it is. The bare one
// has neither and carries the receiver's own marker instead, for the reason
// the flat bare view carries one: it follows the traffic, so the antenna ends
// up wherever it happens to fall on a field with nothing else to find it
// against.
func (s *Scene) drawGroundMarks3(lay *layout, view scene3, receiver source.Receiver) {
	if s.bare() {
		s.drawReceiver3(lay.dst, view, receiver)

		return
	}

	s.drawRings3(lay.dst, view)
	s.drawCardinals3(lay, view)
}

// drawReceiver3 marks the receiver's own position on the ground of a picture
// that is no longer centred on it.
//
// It is drawReceiver's rule in perspective, down to the radius and the muted
// ring coloured by the fix mode, so the antenna reads the same in both bare
// views and never reads as a contact.
//
// There is no bounding test of its own. The camera refuses a point behind the
// lens or far outside the box, which is exactly the case the flat version has
// to check for by hand.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawReceiver3(dst *canvas.Canvas, view scene3, receiver source.Receiver) {
	if !positioned(receiver.Latitude, receiver.Longitude) {
		return
	}

	x, y, ok := view.cam.at(view.ground(receiver.Latitude, receiver.Longitude))
	if !ok {
		return
	}

	dst.Circle(x, y, receiverRadius, s.fixColour(receiver.Mode, s.pal.Muted))
	dst.FillCircle(x, y, 1, s.pal.Muted)
}

// drawRings3 draws the same range rings the scope view does, projected onto
// the ground as polylines. There are no range numbers on them: in perspective
// a ring is an ellipse, and a label pinned to one point of it would say what
// the ring measures only from one side of the orbit.
func (s *Scene) drawRings3(dst *canvas.Canvas, view scene3) {
	for ring := 1; ring <= ringCount; ring++ {
		radius := view.scopeNm * float64(ring) / ringCount
		s.drawCircle3(dst, view, radius, 0, s.pal.Rule, solidDash)
	}
}

// drawCircle3 draws a horizontal circle of radiusNm at heightNm, as
// ringSegments straight pieces with dash applied along them.
//
// A segment with either end the camera cannot see is dropped rather than
// clipped, which is the rule every other polyline in uScope follows: a dropped
// segment leaves a gap where a clipped one would draw a line to a place
// nothing ever was.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (*Scene) drawCircle3(
	dst *canvas.Canvas, view scene3, radiusNm, heightNm float64, col color.RGBA, dash dashPattern,
) {
	if radiusNm <= 0 {
		return
	}

	prevX, prevY, prevOK := 0, 0, false

	for step := 0; step <= ringSegments; step++ {
		sin, cos := math.Sincos(2 * math.Pi * float64(step) / ringSegments)

		x, y, ok := view.cam.at(point3{east: radiusNm * sin, north: radiusNm * cos, up: heightNm})

		if ok && prevOK && (step-1)%dash.period < dash.on {
			dst.LineAA(float64(prevX), float64(prevY), float64(x), float64(y), col)
		}

		prevX, prevY, prevOK = x, y, ok
	}
}

// drawCardinals3 puts N, E, S and W on the ground at the outer ring, which is
// the only thing in the picture that says which way the camera is round.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawCardinals3(lay *layout, view scene3) {
	face := s.faces.Body
	if !lay.labels || face == nil {
		return
	}

	for _, mark := range cardinals3 {
		x, y, ok := view.cam.at(point3{east: view.scopeNm * mark.east, north: view.scopeNm * mark.north})
		if !ok {
			continue
		}

		text.DrawCentered(lay.dst, face, x, y-face.Height()/2, mark.letter, s.pal.Muted)
	}
}

// drawShore3 paints the water, the land and the coastlines on the floor of the
// world, in that order.
//
// It is drawShore in perspective, down to the order: the fill goes down first
// and the outlines over it, so the join between sea and land is covered by the
// line that describes it and the jagged edge the fill leaves never shows.
// fillGround3 is the first half and the call to Within below is the second.
//
// The box handed to the shore set is the same one both halves use, around the
// origin at whatever the view reaches to. The full view never recentres, so
// that origin is the receiver; the bare one follows the traffic and reaches
// two range radii, which is why the span is measured off the view rather than
// off the range.
func (s *Scene) drawShore3(dst *canvas.Canvas, view scene3) {
	if s.shoreSet == nil {
		return
	}

	latSpan := view.reachNm / nmPerDegree
	lonSpan := latSpan / max(view.cosLat0, minCosLat)

	s.fillGround3(dst, view, latSpan, lonSpan)

	s.shoreSet.Within(
		view.origin.lat-latSpan, view.origin.lat+latSpan,
		view.origin.lon-lonSpan, view.origin.lon+lonSpan,
		func(line shore.Polyline) { s.drawShoreLine3(dst, view, line) },
	)
}

// drawShoreLine3 projects one coastline and draws the part of it inside the
// range.
//
// The thinning and the clipping are the scope view's, moved a step earlier in
// the pipeline. Both happen in nautical miles on the ground rather than in
// pixels on the canvas, because in perspective the range is an ellipse and a
// circle of pixels would cut the coast in the wrong place. circle.clip does
// not care which units it is handed, so it is the same function doing the same
// arithmetic.
func (s *Scene) drawShoreLine3(dst *canvas.Canvas, view scene3, line shore.Polyline) {
	if len(line) < 2 {
		return
	}

	ring := circle{radius: view.reachNm}
	from := view.ground(line[0].Lat, line[0].Lon)

	for index := 1; index < len(line); index++ {
		next := view.ground(line[index].Lat, line[index].Lon)

		// The last point is always drawn, however short the run to it, so a
		// coastline reaches its own end rather than stopping one segment early.
		if index < len(line)-1 &&
			math.Abs(next.east-from.east) < view.minSegNm && math.Abs(next.north-from.north) < view.minSegNm {
			continue
		}

		s.drawShoreSegment3(dst, view, ring, from, next)

		from = next
	}
}

// drawShoreSegment3 trims one piece of coast to the range and draws whatever
// is left of it.
func (s *Scene) drawShoreSegment3(dst *canvas.Canvas, view scene3, ring circle, from, to point3) {
	piece, inside := ring.clip(segment{fromX: from.east, fromY: from.north, toX: to.east, toY: to.north})
	if !inside {
		return
	}

	startX, startY, startOK := view.cam.at(point3{east: piece.fromX, north: piece.fromY})
	endX, endY, endOK := view.cam.at(point3{east: piece.toX, north: piece.toY})

	if startOK && endOK {
		dst.LineAA(float64(startX), float64(startY), float64(endX), float64(endY), s.pal.Shore)
	}
}

// drawAirports3 marks the airfields that fall inside the range, with their
// ICAO code beside them.
//
// Nothing is dropped for colliding with a range label the way the scope view
// drops it, because the 3D view draws no range labels for one to land on.
//
// fields is passed in rather than read from airports.All() here, so a test can
// hand it a synthetic set instead of the whole embedded database.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawAirports3(lay *layout, view scene3, fields []airports.Airport) {
	face := s.faces.Small

	for _, field := range fields {
		x, y, ok := view.project(field.Latitude, field.Longitude, 0)
		if !ok {
			continue
		}

		lay.dst.Rect(image.Rect(x-airportHalf, y-airportHalf, x+airportHalf+1, y+airportHalf+1), s.pal.Rule)

		if lay.labels && face != nil {
			text.Draw(lay.dst, face, x+airportLabelGap, y-face.Height()/2, field.ICAO, s.pal.Muted)
		}
	}
}

// drawTraffic3 paints the ghosts, then the live trails, then the aircraft on
// top of them, which is the order the scope view draws them in and for the
// same reason: live traffic is never hidden under the track of something that
// is no longer there.
//
// The selection keeps its ring and its label in the full view, because the
// column beside the picture is still on screen for them to refer to, and loses
// both in the bare one for the reason the flat bare view drops them: there is
// no type anywhere for a ring to point at.
//
// The filter's do apply, and they take the whole aircraft with them: no trail,
// no stalk, no model and no label. It asks the same predicate the flat scope
// asks, which is what stops v from changing which aeroplanes are on screen.
func (s *Scene) drawTraffic3(lay *layout, view scene3, frame source.Frame) {
	if s.trail.ghostsDrawn() {
		for _, ghost := range frame.Ghosts {
			s.drawTrail3(lay.dst, view, ghost.Points, s.ghostColour(ghost))
		}
	}

	for _, plane := range frame.Planes {
		if !s.visible(plane) {
			continue
		}

		s.drawTrail3(lay.dst, view, plane.PositionHistory, s.aircraftColour(plane))
	}

	for _, plane := range frame.Planes {
		if !s.visible(plane) {
			continue
		}

		s.drawContact3(lay, view, plane, frame.Receiver)
	}
}

// drawTrail3 draws one run of fixes under whatever the t key has the trails
// set to, or nothing at all when that mode has no trail for it.
//
// It takes the fixes rather than the aircraft so a ghost and a live trail go
// through the same door: the mode decides how far back the track runs and how
// faint it starts, and neither answer depends on whether there is still an
// aeroplane on the end of it.
func (s *Scene) drawTrail3(dst *canvas.Canvas, view scene3, fixes []airplane.PositionEntry, col color.RGBA) {
	plan, drawn := s.trail.plan(len(fixes))
	if !drawn {
		return
	}

	s.drawPath3(dst, view, fixes, col, plan)
}

// drawPath3 draws the part of a run of fixes the plan asks for as a polyline
// in the air, each fix at the altitude it was reported at, brightening from
// the plan's floor at the tail to full strength at the head.
//
// It is drawPath with the third dimension and nothing else changed: the same
// fade, and the same rule that a segment with either end off the picture is
// dropped rather than clipped.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawPath3(
	dst *canvas.Canvas, view scene3, fixes []airplane.PositionEntry, col color.RGBA, plan trailPlan,
) {
	// plan reported the run drawable, so at least two fixes survive the cut.
	fixes = fixes[plan.from:]
	span := float64(len(fixes) - 1)
	prevX, prevY, prevOK := view.project(fixes[0].Latitude, fixes[0].Longitude, fixes[0].Altitude)

	for index := 1; index < len(fixes); index++ {
		x, y, ok := view.project(fixes[index].Latitude, fixes[index].Longitude, fixes[index].Altitude)

		if ok && prevOK {
			dst.LineAA(float64(prevX), float64(prevY), float64(x), float64(y),
				s.fade(col, segmentAlpha(plan.floor, index, span)))
		}

		prevX, prevY, prevOK = x, y, ok
	}
}

// drawContact3 draws one aircraft: in the 3D view the stalk from its shadow on
// the ground up to where it is flying, the shape on the end of it, and the
// selection marker when it is the one the panel is about.
//
// The shape is a model rather than the flat scope's rotated bitmap. A sprite
// is a picture of an aeroplane seen from directly above, and this camera is
// never directly above anything: turning it to a heading put a plan view in a
// perspective scene, which read as a sticker on the glass rather than as
// something flying in the picture. The model is posed in the world and goes
// through the same camera as the rings under it, so it foreshortens with
// everything else.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawContact3(lay *layout, view scene3, plane airplane.Snapshot, receiver source.Receiver) {
	x, y, ok := view.project(plane.Latitude, plane.Longitude, plane.Altitude)
	if !ok {
		return
	}

	// The bare 3D view draws no stalks: with nothing else on screen the trails
	// already say where each aircraft has been, and a forest of verticals under
	// them was the one thing the user asked to have taken away.
	if !s.bare() {
		s.drawStalk3(lay.dst, view, plane, x, y)
	}

	col := s.aircraftColour(plane)

	centre := view.world(plane.Latitude, plane.Longitude, plane.Altitude)
	s.drawShape3(lay.dst, view.cam, plane, centre, image.Pt(x, y), col)

	if !s.bare() && plane.ICAO == s.selICAO {
		s.drawSelection(lay, x, y, plane, receiver)
	}
}

// drawStalk3 draws the thin line from an aircraft's position on the ground up
// to the aircraft itself.
//
// It is the one thing in the picture that says how high an aeroplane is. A
// sprite on its own floats at a height the eye cannot measure against
// anything, and two aircraft at different altitudes on the same bearing draw
// at nearly the same place; with a stalk each, the ground tells you which is
// which.
//
//nolint:varnamelen // topX, topY name a pixel, the idiom used throughout uScope.
func (s *Scene) drawStalk3(dst *canvas.Canvas, view scene3, plane airplane.Snapshot, topX, topY int) {
	baseX, baseY, ok := view.cam.at(view.ground(plane.Latitude, plane.Longitude))
	if !ok {
		return
	}

	dst.LineAA(float64(baseX), float64(baseY), float64(topX), float64(topY), s.pal.Muted)
}
