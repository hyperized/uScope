package radar

import (
	"image"
	"image/color"
	"math"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/pkg/canvas"
)

// The aircraft model's shape and how it is posed.
const (
	// modelSpanPx is how wide the model is drawn, wingtip to wingtip, in
	// pixels. It is a constant on screen rather than in the world: an
	// aeroplane drawn to scale at forty nautical miles is a fraction of a
	// pixel, and the shape is there to say which way something is pointing,
	// which it can only do at a size the eye can read. Sixteen is a little
	// wider than the flat scope's fifteen-pixel sprite, because a model seen
	// at an angle has less of itself facing the camera than a bitmap does.
	modelSpanPx = 16.0

	// attSpanPx is the same model in a flight strip's attitude cell, a couple
	// of pixels narrower: a wing that swings out as an aircraft turns has to
	// stay inside a 24-pixel column, where in the 3D view it has the whole
	// picture to swing into.
	attSpanPx = 14.0

	// attDepthNm is how far the attitude cell's camera stands off its
	// subject, in the units the model is written in.
	//
	// A thousand spans away a model one span across foreshortens by a tenth of
	// a per cent, which is orthographic as far as a 24-pixel cell can tell.
	// That is the point of the number: every row has to be drawn from the same
	// angle whatever it holds, so the cell cannot borrow the camera that
	// frames the range ring and moves with the orbit.
	attDepthNm = 1000.0

	// modelDiscRadius draws an aircraft whose heading nobody has decoded as a
	// small filled disc, seven pixels across. It is filled where the flat
	// scope's is hollow: on a field with models on it a hollow ring reads as
	// a selection marker, and a solid blob reads as an aeroplane whose
	// attitude is unknown, which is what it is.
	modelDiscRadius = 3

	// modelFarAlpha is how much of the aircraft's own colour is left in the
	// wing on the far side of the camera: the rest is field. Shading one wing
	// is the only cue a wireframe of this size has for which way up it is,
	// and forty per cent is far enough from full strength to read as shadow
	// without the wing disappearing on the paper palette.
	modelFarAlpha = 0.4

	// modelClimbFpm is the vertical rate, in feet per minute, above which the
	// nose is drawn up and below whose negative it is drawn down. Three
	// hundred is uAirwaves' own idea of level flight: below it a rate is
	// barometric noise rather than a climb.
	modelClimbFpm = 300.0

	// modelPitchDeg is how far the nose is tipped when it is tipped at all.
	// The pitch is a state and not a measurement: the shape says climbing,
	// level or descending, and eight degrees is what a sixteen-pixel model
	// can show without the wings turning into a line.
	modelPitchDeg = 8.0

	// modelBankDeg caps the roll, and modelTurnDeg is the heading change
	// across one leg of the trail that reaches it.
	//
	// uAirwaves samples one fix every ten seconds, so a standard-rate turn of
	// three degrees a second moves the track thirty degrees from one leg to
	// the next. An aircraft turning at that rate is drawn at the full fifteen
	// degrees of bank, and anything gentler in proportion.
	modelBankDeg = 15.0
	modelTurnDeg = 30.0
)

// The model's vertices, by name. modelVertexCount is the size of every fixed
// array the draw path projects into.
const (
	vertNose = iota
	vertHullLeft
	vertHullRight
	vertTail
	vertWingTipLeft
	vertWingTipRight
	vertWingAftLeft
	vertWingAftRight
	vertTailRootLeft
	vertTailRootRight
	vertTailTipLeft
	vertTailTipRight
	vertFinFront
	vertFinTop

	modelVertexCount
)

// modelVertices is the aeroplane, in units of its own wingspan.
//
// It is written in the world's own axes as a northbound, wings-level aircraft
// would sit in them: east is the right wing, north is the nose and up is up.
// That is not a coincidence to be tidied away, it is what makes newOrient3's
// yaw of zero the identity and lets the model be read off the page without
// working a transform out in your head first.
//
// The span is one unit, from -0.5 to 0.5, so scaling the whole thing by one
// number puts the wingtips exactly modelSpanPx apart on screen. Everything
// else is proportioned against that: a fuselage a tenth of the span wide is
// under two pixels at sixteen, which is why the centreline is stroked as well
// as filled.
//
//nolint:gochecknoglobals,mnd // the coordinates are the drawing; naming each would hide the shape.
var modelVertices = [modelVertexCount]point3{
	vertNose:          {north: 0.55},
	vertHullLeft:      {east: -0.05, north: 0.12},
	vertHullRight:     {east: 0.05, north: 0.12},
	vertTail:          {north: -0.45},
	vertWingTipLeft:   {east: -0.50, north: -0.20},
	vertWingTipRight:  {east: 0.50, north: -0.20},
	vertWingAftLeft:   {east: -0.05, north: -0.14},
	vertWingAftRight:  {east: 0.05, north: -0.14},
	vertTailRootLeft:  {east: -0.03, north: -0.30},
	vertTailRootRight: {east: 0.03, north: -0.30},
	vertTailTipLeft:   {east: -0.20, north: -0.44},
	vertTailTipRight:  {east: 0.20, north: -0.44},
	vertFinFront:      {north: -0.26},
	vertFinTop:        {north: -0.38, up: 0.22},
}

// modelTriangle is one filled face, as three indices into modelVertices.
type modelTriangle struct {
	first  int
	second int
	third  int
}

// The model's faces, grouped by the order they are painted in.
//
// There is no depth buffer and no per-face sort. The grouping is the sort: an
// aeroplane is convex enough that the far wing is behind the body and the near
// wing in front of it from every angle the camera can reach, so painting far
// wing, fin, fuselage, near wing gives the same picture a sorted renderer
// would, for four fills instead of a comparison per face per aircraft.
//
// Each wing group carries its own half of the tailplane, because the two are
// always on the same side of the aircraft and so always on the same side of
// the camera.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var (
	modelFuselage = [...]modelTriangle{
		{first: vertNose, second: vertHullRight, third: vertHullLeft},
		{first: vertHullLeft, second: vertHullRight, third: vertTail},
	}

	modelLeftWing = [...]modelTriangle{
		{first: vertHullLeft, second: vertWingTipLeft, third: vertWingAftLeft},
		{first: vertTailRootLeft, second: vertTailTipLeft, third: vertTail},
	}

	modelRightWing = [...]modelTriangle{
		{first: vertHullRight, second: vertWingTipRight, third: vertWingAftRight},
		{first: vertTailRootRight, second: vertTailTipRight, third: vertTail},
	}

	modelFin = [...]modelTriangle{
		{first: vertFinFront, second: vertFinTop, third: vertTail},
	}
)

// modelEdge is one stroked line, as two indices into modelVertices.
type modelEdge struct {
	from int
	to   int
}

// modelEdges are the lines drawn over the fills: the fuselage centreline from
// nose to tail, and the fin's leading edge.
//
// They are what makes the shape survive at sixteen pixels. The fuselage is a
// tenth of the span wide, so its fill is one or two pixels and rounds away
// from some angles entirely; a stroked centreline is always there, and it is
// the line the eye reads the heading off.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var modelEdges = [...]modelEdge{
	{from: vertNose, to: vertTail},
	{from: vertFinFront, to: vertFinTop},
}

// orient3 is an aircraft's own axes expressed in the world, with the model's
// scale already folded into their length.
//
// Holding the pose as three vectors rather than as three angles is what keeps
// the per-vertex work to three multiplies and three adds. The angles are
// resolved once per aircraft in newOrient3 and never looked at again.
type orient3 struct {
	right point3
	nose  point3
	up    point3
}

// newOrient3 resolves a heading, a pitch and a bank into the aircraft's axes
// in the world, scaled so one model unit comes out scale nautical miles long.
//
// The composition is the usual aerospace one applied to a body vector: roll
// about the nose first, then pitch about the wing axis, then yaw about the
// vertical. It is written out as the finished matrix rather than as three
// rotations in sequence because the closed form is six sines and cosines and
// nine products, where the sequence is three passes over three basis vectors.
//
// Heading follows the compass the rest of uScope does: zero is north and
// increasing turns clockwise, which is why the north component of the right
// wing carries the minus sign. Pitch is positive nose-up and bank is positive
// right-wing-down, so a right turn drops the wing on the inside of it.
func newOrient3(headingDeg, pitchDeg, bankDeg, scale float64) orient3 {
	sinHeading, cosHeading := math.Sincos(headingDeg * math.Pi / halfCircle)
	sinPitch, cosPitch := math.Sincos(pitchDeg * math.Pi / halfCircle)
	sinBank, cosBank := math.Sincos(bankDeg * math.Pi / halfCircle)

	return orient3{
		right: point3{
			east:  scale * (cosBank*cosHeading + sinBank*sinPitch*sinHeading),
			north: scale * (sinBank*sinPitch*cosHeading - cosBank*sinHeading),
			up:    scale * -sinBank * cosPitch,
		},
		nose: point3{
			east:  scale * cosPitch * sinHeading,
			north: scale * cosPitch * cosHeading,
			up:    scale * sinPitch,
		},
		up: point3{
			east:  scale * (sinBank*cosHeading - cosBank*sinPitch*sinHeading),
			north: scale * -(sinBank*sinHeading + cosBank*sinPitch*cosHeading),
			up:    scale * cosBank * cosPitch,
		},
	}
}

// place is where one model vertex lands in the world, given the aircraft's
// position and pose.
func (o orient3) place(centre, vertex point3) point3 {
	return point3{
		east:  centre.east + o.right.east*vertex.east + o.nose.east*vertex.north + o.up.east*vertex.up,
		north: centre.north + o.right.north*vertex.east + o.nose.north*vertex.north + o.up.north*vertex.up,
		up:    centre.up + o.right.up*vertex.east + o.nose.up*vertex.north + o.up.up*vertex.up,
	}
}

// posed is one aircraft's model projected onto the canvas for one frame.
//
// It lives on the Scene rather than in a local so the draw path never puts a
// fixed array on the heap, which is the same reason the format scratch
// buffers and the range-label rectangles are fields. One is enough: an
// aircraft is projected, drawn and finished with before the next is started.
type posed struct {
	atX  [modelVertexCount]int
	atY  [modelVertexCount]int
	seen [modelVertexCount]bool

	// farRight says the right wing is the one further from the camera, so the
	// two wing groups know which of them is painted first and which is shaded.
	farRight bool
}

// spanScale is how many units of world one unit of the model is worth at this
// point's depth, so a model of unit span always comes out modelSpan pixels
// across whatever the range or the exaggeration.
//
// There is no guard on the depth because there is nothing here to guard: every
// caller reaches this after at() has accepted the same point, and at() refuses
// anything closer than minDepth. A second test would be a branch no frame can
// take.
func (c camera3) spanScale(world point3) float64 {
	return c.modelSpan * world.minus(c.eye).dot(c.forward) / c.focal
}

// newCellCamera3 is the fixed camera a flight strip's attitude cell is drawn
// through: north up, due south of its subject, looking down at the elevation
// the 3D view starts on.
//
// It is a camera of its own rather than the view's because the two answer
// different questions. The 3D view's camera orbits and frames a range ring, so
// an aircraft in it is seen from wherever the camera has got to; a column of
// cells has to be readable down the page, which means every row seen from the
// same angle. Sharing the projection routine and the model while keeping the
// cameras apart is what gets both.
//
// The lens is set to the standoff distance, so one model unit projects to one
// pixel before spanScale is applied and the arithmetic in the cell comes out
// in pixels throughout. The cull limits are a cell wide because a model that
// landed further out than that has nothing to do with this row.
func newCellCamera3(centre image.Point) camera3 {
	sinEl, cosEl := math.Sincos(defaultElevation * math.Pi / halfCircle)

	return camera3{
		eye:       point3{north: -cosEl * attDepthNm, up: sinEl * attDepthNm},
		forward:   point3{north: cosEl, up: -sinEl},
		right:     point3{east: 1},
		above:     point3{north: sinEl, up: cosEl},
		focal:     attDepthNm,
		centerX:   centre.X,
		centerY:   centre.Y,
		limitX:    attCell,
		limitY:    attCell,
		modelSpan: attSpanPx,
	}
}

// modelPitch is how far the nose is tipped, from the vertical rate.
//
// It is three states rather than a proportion. A sixteen-pixel model cannot
// show the difference between five hundred feet a minute and two thousand, so
// reading the shape as a measurement would be reading something that is not
// there; climbing, level and descending is what it can say and all it says.
func modelPitch(vertRateFpm float64) float64 {
	switch {
	case vertRateFpm > modelClimbFpm:
		return modelPitchDeg
	case vertRateFpm < -modelClimbFpm:
		return -modelPitchDeg
	default:
		return 0
	}
}

// modelBank is how far the aircraft is rolled, worked out from how much its
// track turned across the last two legs of its trail.
//
// A fix carries no heading, only a position, so the turn has to come from
// three of them: the track of the leg into the middle fix against the track of
// the leg out of it. Comparing two legs rather than one leg against the
// aircraft's reported heading is deliberate. Heading and track differ by the
// drift angle the wind puts on them, which on a windy day is five or ten
// degrees of nothing happening at all; between two legs flown minutes apart
// the drift is the same in both and cancels, and what is left is the turn.
//
// Anything short of three fixes, or a pair of legs with no length to take a
// bearing from, is a bank of zero. That is the honest answer rather than a
// safe one: an aircraft nobody can see turning is drawn wings level.
func modelBank(fixes []airplane.PositionEntry) float64 {
	const needed = 3

	if len(fixes) < needed {
		return 0
	}

	last := len(fixes) - 1

	into, haveInto := legTrack(fixes[last-2], fixes[last-1])
	outOf, haveOut := legTrack(fixes[last-1], fixes[last])

	if !haveInto || !haveOut {
		return 0
	}

	turn := signedTurn(outOf - into)

	return min(max(turn/modelTurnDeg, -1), 1) * modelBankDeg
}

// legTrack is the compass bearing one leg of a trail was flown on, reporting
// false for a leg the aircraft did not move along.
//
// The longitude is squeezed by the cosine of the latitude, the same local
// equirectangular approximation everything else in the scene projects
// through: over one ten-second leg the error is far below the angle the bank
// is quantised to anyway.
func legTrack(from, to airplane.PositionEntry) (float64, bool) {
	northNm := (to.Latitude - from.Latitude) * nmPerDegree
	eastNm := (to.Longitude - from.Longitude) * nmPerDegree * math.Cos(from.Latitude*math.Pi/halfCircle)

	if eastNm == 0 && northNm == 0 {
		return 0, false
	}

	return math.Atan2(eastNm, northNm) * halfCircle / math.Pi, true
}

// signedTurn folds a difference of two bearings into -180 to 180, so an
// aircraft crossing north turns by a few degrees rather than by most of a
// circle.
func signedTurn(degrees float64) float64 {
	wrapped := wrapDegrees(degrees)
	if wrapped > halfCircle {
		wrapped -= degreesPerCircle
	}

	return wrapped
}

// drawShape3 draws one aircraft's attitude through cam: the model when its
// heading is known, and a disc at when it is not.
//
// An aircraft nobody has decoded a heading for has no attitude to pose a model
// in, and a model drawn pointing north anyway would be the one thing in the
// picture stating a fact nobody has. The disc says there is an aeroplane here
// and nothing else, which is the whole of what is known.
//
// The pixel is passed in rather than projected again because both callers
// already have it: the perspective view from the fix it drew the stalk to, and
// the row cell from the middle of the cell itself.
func (s *Scene) drawShape3(
	dst *canvas.Canvas, cam camera3, plane airplane.Snapshot, centre point3, at image.Point, col color.RGBA,
) {
	if !knownHeading(plane.Heading) {
		dst.FillCircle(at.X, at.Y, modelDiscRadius, col)

		return
	}

	s.drawModel3(dst, cam, plane, centre, col)
}

// drawModel3 paints one aircraft as a solid shape: the far wing shaded, then
// the fin, the fuselage and the near wing at full strength, then the lines
// that hold the whole thing together at this size.
//
// centre is where the aircraft sits in whatever world cam looks at, and the
// caller has already had that camera accept the point, which is what spanScale
// relies on.
func (s *Scene) drawModel3(
	dst *canvas.Canvas, cam camera3, plane airplane.Snapshot, centre point3, col color.RGBA,
) {
	orient := newOrient3(
		plane.Heading,
		modelPitch(plane.VertRate),
		modelBank(plane.PositionHistory),
		cam.spanScale(centre),
	)

	s.projectModel3(cam, centre, orient)

	far, near := &modelLeftWing, &modelRightWing
	if s.posed.farRight {
		far, near = &modelRightWing, &modelLeftWing
	}

	s.fillFaces3(dst, far[:], s.fade(col, modelFarAlpha))
	s.fillFaces3(dst, modelFin[:], col)
	s.fillFaces3(dst, modelFuselage[:], col)
	s.fillFaces3(dst, near[:], col)
	s.strokeEdges3(dst, col)
}

// projectModel3 puts every vertex of the model on the canvas and works out
// which way round the aircraft is to the camera.
//
// A vertex the camera refuses is marked unseen rather than clamped, and every
// face or edge touching it is dropped. That is the rule every other polyline
// in the view follows, and the reason is the same: a dropped face leaves a
// gap where a clamped one would draw a shape stretching off to a place no part
// of the aeroplane is.
func (s *Scene) projectModel3(cam camera3, centre point3, orient orient3) {
	for index, vertex := range modelVertices {
		atX, atY, seen := cam.at(orient.place(centre, vertex))
		s.posed.atX[index], s.posed.atY[index], s.posed.seen[index] = atX, atY, seen
	}

	// The wings are symmetric about the fuselage, so whichever of them points
	// away from the camera is the far one, and the sign of the dot product
	// says which without projecting anything twice. Scaling by spanScale
	// cannot flip it: the scale is a depth over a focal length and both are
	// positive.
	s.posed.farRight = orient.right.dot(cam.forward) > 0
}

// fillFaces3 fills one group of the model's faces, skipping any whose corners
// are not all on the canvas.
func (s *Scene) fillFaces3(dst *canvas.Canvas, faces []modelTriangle, col color.RGBA) {
	for _, face := range faces {
		if !s.posed.seen[face.first] || !s.posed.seen[face.second] || !s.posed.seen[face.third] {
			continue
		}

		dst.FillTriangle(
			s.posed.atX[face.first], s.posed.atY[face.first],
			s.posed.atX[face.second], s.posed.atY[face.second],
			s.posed.atX[face.third], s.posed.atY[face.third],
			col,
		)
	}
}

// strokeEdges3 draws the model's lines over the fills that are already down.
func (s *Scene) strokeEdges3(dst *canvas.Canvas, col color.RGBA) {
	for _, edge := range modelEdges {
		if !s.posed.seen[edge.from] || !s.posed.seen[edge.to] {
			continue
		}

		dst.LineAA(
			float64(s.posed.atX[edge.from]), float64(s.posed.atY[edge.from]),
			float64(s.posed.atX[edge.to]), float64(s.posed.atY[edge.to]),
			col,
		)
	}
}
