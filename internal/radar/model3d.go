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
	// is the only cue the dart has for which way up it is, and forty per cent
	// is far enough from full strength to read as shadow without the wing
	// disappearing on the paper palette. The twin jet fades its whole far
	// side by the same share before shading it, so a wing seen past the body
	// reads as further away at 32 pixels, where the light alone barely shows.
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

// The dart's vertices, by name. The twin jet's are numbered by its build
// instead; posedVertexCount sizes the arrays either is projected into.
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

	// vertBeacon is not a corner of any face. It is where the red
	// anti-collision beacon sits, on top of the fuselage over the wing root,
	// and it is projected with the rest so the light costs no second pass.
	vertBeacon

	modelVertexCount
)

// modelVertices is the dart, the model drawn below jetMinSpanPx, in units of
// its own wingspan.
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
	vertBeacon:        {north: -0.02, up: 0.04},
}

// modelTriangle is one filled face, as three indices into modelVertices.
type modelTriangle struct {
	first  int
	second int
	third  int
}

// The dart's faces, grouped by the order they are painted in.
//
// There is no depth buffer and no per-face sort. The grouping is the sort: the
// dart is flat enough that the far wing is behind the body and the near wing
// in front of it from every angle the camera can reach, so painting far wing,
// fin, fuselage, near wing gives the same picture a sorted renderer would, for
// four fills instead of a comparison per face per aircraft. The twin jet has
// a body with a top and a bottom and pods under its wings, and needs the
// three questions jetOrder asks.
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
	atX  [posedVertexCount]int
	atY  [posedVertexCount]int
	seen [posedVertexCount]bool

	// lamps says which of the projected vertices the lights sit on, since
	// the dart and the twin jet number their wingtips differently.
	lamps modelLamps

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
//
// scale is the layout's, which the cell and the model in it are multiplied by.
func newCellCamera3(centre image.Point, scale int) camera3 {
	size := max(scale, 1)

	sinEl, cosEl := math.Sincos(defaultElevation * math.Pi / halfCircle)

	return camera3{
		eye:       point3{north: -cosEl * attDepthNm, up: sinEl * attDepthNm},
		forward:   point3{north: cosEl, up: -sinEl},
		right:     point3{east: 1},
		above:     point3{north: sinEl, up: cosEl},
		focal:     attDepthNm,
		centerX:   centre.X,
		centerY:   centre.Y,
		limitX:    float64(attCell * size),
		limitY:    float64(attCell * size),
		modelSpan: attSpanPx * float64(size),
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
	weight int,
) {
	if !knownHeading(plane.Heading) {
		dst.FillCircle(at.X, at.Y, modelDiscRadius*max(weight, 1), col)

		return
	}

	s.drawModel3(dst, cam, plane, centre, col, weight)
}

// drawModel3 paints one aircraft as a solid shape, picking the model by the
// span the camera draws it at: the twin jet from jetMinSpanPx up, the dart
// below it.
//
// centre is where the aircraft sits in whatever world cam looks at, and the
// caller has already had that camera accept the point, which is what spanScale
// relies on.
func (s *Scene) drawModel3(
	dst *canvas.Canvas, cam camera3, plane airplane.Snapshot, centre point3, col color.RGBA, weight int,
) {
	scale := cam.spanScale(centre)
	orient := newOrient3(
		plane.Heading,
		modelPitch(plane.VertRate),
		modelBank(plane.PositionHistory),
		scale,
	)

	if cam.modelSpan >= jetMinSpanPx {
		s.drawJet3(dst, cam, centre, orient, scale, col)

		return
	}

	s.drawDart3(dst, cam, centre, orient, col, weight)
}

// drawDart3 paints one aircraft as the dart, the model for sixteen pixels:
// the far wing shaded, then the fin, the fuselage and the near wing at full
// strength, then the lines that hold the whole thing together at that size.
func (s *Scene) drawDart3(dst *canvas.Canvas, cam camera3, centre point3, orient orient3, col color.RGBA, weight int) {
	s.projectModel3(cam, centre, orient)

	far, near := &modelLeftWing, &modelRightWing
	if s.posed.farRight {
		far, near = &modelRightWing, &modelLeftWing
	}

	s.fillFaces3(dst, far[:], s.fade(col, modelFarAlpha))
	s.fillFaces3(dst, modelFin[:], col)
	s.fillFaces3(dst, modelFuselage[:], col)
	s.fillFaces3(dst, near[:], col)
	s.strokeEdges3(dst, col, weight)
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
	s.projectVertices3(cam, centre, orient, modelVertices[:])
	s.posed.lamps = dartLamps

	// The wings are symmetric about the fuselage, so whichever of them points
	// away from the camera is the far one, and the sign of the dot product
	// says which without projecting anything twice. Scaling by spanScale
	// cannot flip it: the scale is a depth over a focal length and both are
	// positive.
	s.posed.farRight = orient.right.dot(cam.forward) > 0
}

// projectVertices3 puts one model's vertices on the canvas in s.posed, in
// the order the model lists them.
func (s *Scene) projectVertices3(cam camera3, centre point3, orient orient3, vertices []point3) {
	for index, vertex := range vertices {
		atX, atY, seen := cam.at(orient.place(centre, vertex))
		s.posed.atX[index], s.posed.atY[index], s.posed.seen[index] = atX, atY, seen
	}
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
func (s *Scene) strokeEdges3(dst *canvas.Canvas, col color.RGBA, weight int) {
	for _, edge := range modelEdges {
		if !s.posed.seen[edge.from] || !s.posed.seen[edge.to] {
			continue
		}

		dst.StrokeLineAA(
			float64(s.posed.atX[edge.from]), float64(s.posed.atY[edge.from]),
			float64(s.posed.atX[edge.to]), float64(s.posed.atY[edge.to]),
			weight, col,
		)
	}
}

// modelLamps names the vertices of one model the lights sit on.
type modelLamps struct {
	port      int
	starboard int
	tail      int
	beacon    int
}

// dartLamps are the dart's wingtips, tail and beacon.
//
//nolint:gochecknoglobals // scene content, read-only.
var dartLamps = modelLamps{
	port: vertWingTipLeft, starboard: vertWingTipRight, tail: vertTail, beacon: vertBeacon,
}

// The twin jet's size, light and tones.
const (
	// jetMinSpanPx is the smallest span on screen the twin jet is drawn at;
	// anything narrower gets the dart. Rendered side by side at sixteen
	// pixels the two are the same grey blobs, the body two pixels thick and
	// the pods one, and the dart's stroked centreline reads the heading
	// better; at twenty-four the twin jet is already an airliner and the
	// dart a pair of needles. Twenty splits the two, and since every span
	// drawn is sixteen or fourteen times the layout's scale, it leaves the
	// panel's picture and its strips the dart's to the byte.
	jetMinSpanPx = 20.0

	// fuselageSides and engineSides are how many flat faces go round the
	// body and round a pod. Eight is the fewest that still shades as a tube
	// at 128 pixels; a pod is four pixels across at 64 and six sides are
	// already more than it shows.
	fuselageSides = 8
	engineSides   = 6

	// jetFaceCorners is the most corners one face has: a cap of the body.
	jetFaceCorners = fuselageSides

	// jetAmbient is how bright a face turned away from the light still is,
	// as a share of a face square on to it, before its tone is applied.
	jetAmbient = 0.62

	// The tones of the three kinds of part, the body lightest. They are
	// what tells a wing from the fuselage behind it where the light alone
	// would draw both the same.
	jetBodyTone   = 1.0
	jetFinTone    = 0.9
	jetWingTone   = 0.82
	jetEngineTone = 0.72

	// jetDarkest is the dimmest a face can come out: the engine's tone in
	// the ambient light. jetInk spreads the day palettes' shades from it.
	jetDarkest = jetEngineTone * jetAmbient

	// jetEdgeOnSine is how close to edge on a plate has to be seen, as the
	// sine of the angle between it and the line of sight, before
	// fillJetFace paints the pixels on its edges as well. A wing chord is
	// about a fifth of the span, so at 32 pixels this is a plate a pixel
	// or so wide.
	jetEdgeOnSine = 0.2

	// jetDayFloor is the least ink a face keeps on a day palette, where the
	// lit faces are the ones that go towards the paper.
	jetDayFloor = 0.75
)

// jetWingTip is the corner of jetWingOutline at the wingtip.
const jetWingTip = 4

// The twin jet's parts. A part is a run of faces painted together, and the
// order of the parts is the whole of the painter's sort.
const (
	jetFuselage = iota
	jetFin
	jetWingLeft
	jetWingRight
	jetTailLeft
	jetTailRight
	jetEngineLeft
	jetEngineRight

	jetPartCount
)

// The twin jet's size in vertices and faces. They are worked out by hand from
// the tables below because they size the fixed arrays the draw path uses, and
// TestJetMeshFillsItsArrays fails if the tables and these disagree.
//
// The body is a point at each end and seven rings of eight between, a pod
// four rings of six, a wing nine corners, a tailplane and the fin six each,
// and one vertex for the beacon. The body has eight triangles at each end and
// six bands of eight quads, a pod three bands of six and two caps, and every
// plate is one face except the wing, which is two because its trailing edge
// kinks inwards and a face has to be convex to be filled as a fan.
const (
	jetVertexCount = 2 + 7*fuselageSides + 2*4*engineSides + 2*9 + 2*6 + 6 + 1
	jetFaceCount   = 2*fuselageSides + 6*fuselageSides + 2*(3*engineSides+2) + 2*2 + 2 + 1

	// posedVertexCount sizes Scene.posed for whichever model is bigger.
	posedVertexCount = max(modelVertexCount, jetVertexCount)
)

// jetStation is one ring of a lofted part: how far along it is, how fat, and
// how far its centre is lifted off the part's axis. A radius of zero closes
// the loft to a point.
type jetStation struct {
	north  float64
	radius float64
	lift   float64
}

// The twin jet, in the dart's axes and units: east is the right wing, north
// the nose, up is up, and the span runs from -0.5 to 0.5. These are the
// coordinates of the approved mock-up (jet(1) with one engine a side), with
// two changes that do not show at 128 pixels: the tail closes to a point
// rather than a hole 0.012 across, and the pods lose the lip in front of the
// intake.
//
//nolint:gochecknoglobals,mnd // the coordinates are the drawing; naming each would hide the shape.
var (
	// jetFuselageStations run from a blunt nose through a constant tube to
	// a tail that tapers and rises to meet the fin.
	jetFuselageStations = [...]jetStation{
		{north: 0.53},
		{north: 0.515, radius: 0.02},
		{north: 0.48, radius: 0.038},
		{north: 0.42, radius: 0.05},
		{north: 0.32, radius: 0.055},
		{north: -0.2, radius: 0.055},
		{north: -0.34, radius: 0.044, lift: 0.012},
		{north: -0.45, radius: 0.024, lift: 0.03},
		{north: -0.51, lift: 0.042},
	}

	// jetEngineStations are one pod, north of its own centre, which
	// jetEnginePod puts under the right wing and its mirror under the left.
	jetEngineStations = [...]jetStation{
		{north: 0.1, radius: 0.028},
		{north: 0.02, radius: 0.031},
		{north: -0.03, radius: 0.024},
		{north: -0.05, radius: 0.012},
	}
	jetEnginePod = point3{east: 0.17, north: 0.04, up: -0.045}

	// jetWingOutline is the right wing, east and north, from the leading
	// edge at the root round the rounded tip and back along the trailing
	// edge. Index 4 is the tip, where the navigation light goes.
	jetWingOutline = [...][2]float64{
		{0.05, 0.15}, {0.2, 0.05}, {0.46, -0.14}, {0.495, -0.175}, {0.5, -0.2},
		{0.485, -0.225}, {0.45, -0.225}, {0.2, -0.12}, {0.05, -0.09},
	}
	jetWingFaces = [...][]int{{0, 1, 7, 8}, {1, 2, 3, 4, 5, 6, 7}}

	// jetTailOutline is the right half of the tailplane, the same way round.
	jetTailOutline = [...][2]float64{
		{0.03, -0.35}, {0.17, -0.45}, {0.19, -0.48}, {0.18, -0.5}, {0.15, -0.5}, {0.03, -0.46},
	}

	// jetFinOutline is the fin, north and up, with a rounded top.
	jetFinOutline = [...][2]float64{
		{-0.27, 0.05}, {-0.4, 0.2}, {-0.43, 0.225}, {-0.47, 0.225}, {-0.49, 0.2}, {-0.5, 0.045},
	}

	// jetBeaconAt is on top of the body over the wing root, as on the dart.
	jetBeaconAt = point3{north: -0.02, up: 0.06}
)

// jetWingUp and jetTailUp are the heights of the two horizontal plates at a
// distance out from the centreline: the wing sits low on the body with a
// little dihedral, the tailplane higher with less.
//
//nolint:mnd // the mock-up's own numbers.
func jetWingUp(east float64) float64 { return -0.02 + math.Abs(east)*0.09 }

//nolint:mnd // the mock-up's own numbers.
func jetTailUp(east float64) float64 { return 0.035 + math.Abs(east)*0.05 }

// jetFace is one flat face of the twin jet: a convex polygon as indices into
// the vertices, its unit normal in the model's axes, and its tone.
type jetFace struct {
	corners [jetFaceCorners]int
	count   int
	normal  point3
	tone    float64
}

// jetPart is a run of faces in jetMesh.faces. A closed part is a lofted solid
// whose faces turned away from the camera are never drawn; an open one is a
// plate with two sides.
type jetPart struct {
	first  int
	end    int
	closed bool
}

// jetMesh is the twin jet, built once from the tables above.
type jetMesh struct {
	vertices [jetVertexCount]point3
	faces    [jetFaceCount]jetFace
	parts    [jetPartCount]jetPart
	lamps    modelLamps

	vertexCount int
	faceCount   int
}

// jet is the one twin jet every aircraft is posed from.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var jet = newJetMesh()

// newJetMesh builds the twin jet. It runs once, at init, so it is free to
// allocate the few slices the loft works in.
func newJetMesh() *jetMesh {
	mesh := &jetMesh{}

	mesh.part(jetFuselage, true, func() {
		mesh.loft(jetFuselageStations[:], fuselageSides, point3{}, jetBodyTone)

		// The loft's last vertex is the point the tail closes to, where the
		// white tail light goes.
		mesh.lamps.tail = mesh.vertexCount - 1
	})
	mesh.part(jetFin, false, func() {
		mesh.plate(len(jetFinOutline), func(index int) point3 {
			return point3{north: jetFinOutline[index][0], up: jetFinOutline[index][1]}
		}, [][]int{{0, 1, 2, 3, 4, 5}}, jetFinTone)
	})

	for _, side := range [...]struct {
		mirror             float64
		wing, tail, engine int
		lamp               *int
	}{
		{mirror: -1, wing: jetWingLeft, tail: jetTailLeft, engine: jetEngineLeft, lamp: &mesh.lamps.port},
		{mirror: 1, wing: jetWingRight, tail: jetTailRight, engine: jetEngineRight, lamp: &mesh.lamps.starboard},
	} {
		mesh.part(side.wing, false, func() {
			*side.lamp = mesh.vertexCount + jetWingTip
			mesh.plate(len(jetWingOutline), func(index int) point3 {
				east := side.mirror * jetWingOutline[index][0]

				return point3{east: east, north: jetWingOutline[index][1], up: jetWingUp(east)}
			}, jetWingFaces[:], jetWingTone)
		})
		mesh.part(side.tail, false, func() {
			mesh.plate(len(jetTailOutline), func(index int) point3 {
				east := side.mirror * jetTailOutline[index][0]

				return point3{east: east, north: jetTailOutline[index][1], up: jetTailUp(east)}
			}, [][]int{{0, 1, 2, 3, 4, 5}}, jetWingTone)
		})
		mesh.part(side.engine, true, func() {
			pod := point3{east: side.mirror * jetEnginePod.east, north: jetEnginePod.north, up: jetEnginePod.up}
			mesh.loft(jetEngineStations[:], engineSides, pod, jetEngineTone)
		})
	}

	mesh.lamps.beacon = mesh.vertex(jetBeaconAt)

	return mesh
}

// part records the faces build adds as one part of the mesh, and for a closed
// part turns their normals outwards.
//
//nolint:revive // flag-parameter: closed is what the part is, stored on it, not a mode of this call.
func (m *jetMesh) part(index int, closed bool, build func()) {
	first := m.faceCount

	build()

	m.parts[index] = jetPart{first: first, end: m.faceCount, closed: closed}

	if closed {
		m.faceOutwards(first)
	}
}

// vertex adds one vertex and says where it went.
func (m *jetMesh) vertex(at point3) int {
	m.vertices[m.vertexCount] = at
	m.vertexCount++

	return m.vertexCount - 1
}

// face adds one face over the given vertices, skipping a corner that repeats
// the one before it, which is how a band of quads meeting a point becomes a
// fan of triangles. The loft only ever repeats a point in the middle of a
// band's corners or at its end, never across the wrap from last to first.
func (m *jetMesh) face(corners []int, tone float64) {
	face := jetFace{tone: tone}

	for _, corner := range corners {
		if face.count > 0 && face.corners[face.count-1] == corner {
			continue
		}

		face.corners[face.count] = corner
		face.count++
	}

	face.normal = m.newell(face.corners[:face.count])
	m.faces[m.faceCount] = face
	m.faceCount++
}

// newell is the unit normal of a polygon by Newell's method, which gives the
// plane of the whole polygon rather than of whichever three corners were
// picked, so a band of a loft that is not quite flat still gets the normal
// of its middle.
func (m *jetMesh) newell(corners []int) point3 {
	var normal point3

	for index, corner := range corners {
		here, next := m.vertices[corner], m.vertices[corners[(index+1)%len(corners)]]
		normal.east += (here.north - next.north) * (here.up + next.up)
		normal.north += (here.up - next.up) * (here.east + next.east)
		normal.up += (here.east - next.east) * (here.north + next.north)
	}

	return normal.unit()
}

// faceOutwards turns every normal of a closed part from first onwards to point
// away from the middle of the part. The loft winds its bands and its caps in
// whatever order was easiest to write, and culling needs to know which side is
// the outside.
//
// The middle of the vertices is inside a convex solid, and every lofted part
// of the jet is convex, so a face whose normal points towards it is facing in.
func (m *jetMesh) faceOutwards(first int) {
	var middle point3

	used := 0

	for index := first; index < m.faceCount; index++ {
		face := &m.faces[index]
		for _, corner := range face.corners[:face.count] {
			middle = point3{
				east:  middle.east + m.vertices[corner].east,
				north: middle.north + m.vertices[corner].north,
				up:    middle.up + m.vertices[corner].up,
			}
			used++
		}
	}

	share := 1 / float64(used)
	middle = point3{east: middle.east * share, north: middle.north * share, up: middle.up * share}

	for index := first; index < m.faceCount; index++ {
		face := &m.faces[index]
		if face.normal.dot(m.vertices[face.corners[0]].minus(middle)) < 0 {
			face.normal = point3{east: -face.normal.east, north: -face.normal.north, up: -face.normal.up}
		}
	}
}

// loft adds a solid of revolution through stations around an axis running
// north through at, with sides flat faces round it. A station of radius zero
// is a single point the faces either side of it close to; an end that is a
// ring rather than a point gets a flat cap.
func (m *jetMesh) loft(stations []jetStation, sides int, at point3, tone float64) {
	rings := make([][]int, len(stations))

	for index, station := range stations {
		rings[index] = m.ring(station, sides, at)
	}

	for index := range len(stations) - 1 {
		here, next := rings[index], rings[index+1]

		for side := range sides {
			turn := (side + 1) % sides
			m.face([]int{here[side], here[turn], next[turn], next[side]}, tone)
		}
	}

	for _, end := range [...]int{0, len(stations) - 1} {
		if stations[end].radius > 0 {
			m.face(rings[end], tone)
		}
	}
}

// ring adds one station's vertices and returns them in order round the axis
// through axis:
// sides of them, or the one point repeated sides times when the radius is
// zero, so the loft can treat both the same and face drops the repeats.
func (m *jetMesh) ring(station jetStation, sides int, axis point3) []int {
	ring := make([]int, sides)

	if station.radius == 0 {
		point := m.vertex(point3{east: axis.east, north: axis.north + station.north, up: axis.up + station.lift})
		for side := range ring {
			ring[side] = point
		}

		return ring
	}

	for side := range ring {
		sin, cos := math.Sincos(float64(side) / float64(sides) * 2 * math.Pi)
		ring[side] = m.vertex(point3{
			east:  axis.east + cos*station.radius,
			north: axis.north + station.north,
			up:    axis.up + station.lift + sin*station.radius,
		})
	}

	return ring
}

// plate adds a flat part: count outline corners placed by place, and faces
// over them as indices into the outline.
func (m *jetMesh) plate(count int, place func(index int) point3, faces [][]int, tone float64) {
	first := m.vertexCount

	for index := range count {
		m.vertex(place(index))
	}

	for _, outline := range faces {
		corners := make([]int, len(outline))
		for index, at := range outline {
			corners[index] = first + at
		}

		m.face(corners, tone)
	}
}

// body resolves a vector in the world onto the aircraft's own axes, in the
// model's units: orient's axes are scale long, so each dot product is divided
// by it once.
func (o orient3) body(world point3, scale float64) point3 {
	return point3{
		east:  world.dot(o.right) / scale,
		north: world.dot(o.nose) / scale,
		up:    world.dot(o.up) / scale,
	}
}

// jetLight is where the light on the twin jet comes from, in the world: above
// the camera, a little to its left and mostly behind it, so the faces square
// on to the viewer are the bright ones and the tones fall off towards the
// silhouette. It follows the camera rather than the sun because a model a few
// pixels across lit from behind would be a black shape on every frame where
// the orbit had the sun in front of it.
//
//nolint:mnd // the mock-up's light.
func jetLight(cam camera3) point3 {
	const (
		left   = -0.4
		above  = 0.3
		toward = -0.85
	)

	light := point3{
		east:  left*cam.right.east + above*cam.above.east + toward*cam.forward.east,
		north: left*cam.right.north + above*cam.above.north + toward*cam.forward.north,
		up:    left*cam.right.up + above*cam.above.up + toward*cam.forward.up,
	}

	return light.unit()
}

// unit is the vector scaled to a length of one.
func (p point3) unit() point3 {
	length := math.Sqrt(p.dot(p))

	return point3{east: p.east / length, north: p.north / length, up: p.up / length}
}

// drawJet3 paints one aircraft as the twin jet: its vertices projected into
// s.posed for the lights to find, then its parts in the order jetOrder gives
// for where the camera is, each face in a shade of col.
func (s *Scene) drawJet3(
	dst *canvas.Canvas, cam camera3, centre point3, orient orient3, scale float64, col color.RGBA,
) {
	s.projectVertices3(cam, centre, orient, jet.vertices[:])
	s.posed.lamps = jet.lamps

	// The ray from the eye to this aircraft rather than the camera's own
	// forward axis: at the edge of a wide picture the two are thirty degrees
	// apart, which is enough to cull a face the viewer is looking at.
	look := orient.body(centre.minus(cam.eye), scale)
	look = look.unit()
	light := orient.body(jetLight(cam), scale)

	s.posed.farRight = look.east > 0

	order, far := jetOrder(look)

	// The far side is faded by modelFarAlpha before it is shaded, the cue the
	// dart gives with its far wing.
	ink := s.fade(col, modelFarAlpha)

	for step, part := range order {
		if step == far {
			ink = col
		}

		s.paintJetPart(dst, jet.parts[part], look, light, ink)
	}
}

// jetOrder is the order the twin jet's parts are painted in, seen along look,
// and how many of the first of them are on the far side.
//
// There is no depth buffer and no sort. The body and the pods are convex and
// their back faces are culled, so inside each of them no two drawn faces
// overlap, and the plates are flat; what is left is the order of the parts,
// and three questions about the camera settle it. The side away from the
// camera goes first and the near side last, with the body and fin between.
// Seen from above, the pod goes before the wing it hangs under and the fin
// after the body it stands on; from below both swap. And on each side the
// wing and the tailplane go furthest first, which is the tailplane when the
// nose points at the camera.
//
// Each answer only flips where the two parts it orders are seen edge on to
// each other, so the order cannot flicker between two frames that look the
// same.
func jetOrder(look point3) ([jetPartCount]int, int) {
	type side struct{ wing, tail, engine int }

	far, near := side{jetWingLeft, jetTailLeft, jetEngineLeft}, side{jetWingRight, jetTailRight, jetEngineRight}
	if look.east > 0 {
		far, near = near, far
	}

	above := look.up < 0
	noseOn := look.north < 0

	half := func(sides side) [3]int {
		under, over := sides.engine, sides.wing
		if !above {
			under, over = over, under
		}

		if noseOn {
			return [3]int{sides.tail, under, over}
		}

		return [3]int{under, over, sides.tail}
	}

	body, fin := jetFuselage, jetFin
	if !above {
		body, fin = fin, body
	}

	farHalf, nearHalf := half(far), half(near)

	return [jetPartCount]int{
		farHalf[0], farHalf[1], farHalf[2], body, fin, nearHalf[0], nearHalf[1], nearHalf[2],
	}, len(farHalf)
}

// paintJetPart fills one part's faces, each in col shaded by how squarely it
// faces the light. A closed part's faces turned away from the camera are
// skipped, and so is any face with a corner the camera refused.
func (s *Scene) paintJetPart(dst *canvas.Canvas, part jetPart, look, light point3, col color.RGBA) {
	for index := part.first; index < part.end; index++ {
		face := &jet.faces[index]

		facing := face.normal.dot(look)
		if part.closed && facing >= 0 {
			continue
		}

		if !s.posedFace(face) {
			continue
		}

		// A plate is seen from either side, and the side seen is the one
		// facing the camera, so its normal is turned towards the eye first.
		lit := face.normal.dot(light)
		if facing > 0 {
			lit = -lit
		}

		edgeOn := !part.closed && math.Abs(facing) < jetEdgeOnSine

		shade := s.jetInk(col, face.tone*(jetAmbient+(1-jetAmbient)*max(lit, 0)))
		s.fillJetFace(dst, face, shade)

		if edgeOn {
			s.outlineJetFace(dst, face, shade)
		}
	}
}

// posedFace reports whether the camera took every corner of face.
func (s *Scene) posedFace(face *jetFace) bool {
	for _, corner := range face.corners[:face.count] {
		if !s.posed.seen[corner] {
			return false
		}
	}

	return true
}

// fillJetFace fills one convex face as a fan of quadrilaterals from its first
// corner, the last of them a triangle when the corners run out.
//
// A quad goes through BlendQuad, which fills by spans along each row. A fan of
// FillTriangle tests every pixel of each triangle's bounding box, and a band
// of the tube seen at an angle is a thin diagonal whose box is the size of the
// whole aeroplane: 120 jets at 32 pixels took 1.26 ms that way and take 0.8 ms
// this way (BenchmarkModels3). Spans cover the pixels whose centres are
// inside, so neighbouring faces meet without a seam and without painting their
// shared edge twice.
func (s *Scene) fillJetFace(dst *canvas.Canvas, face *jetFace, col color.RGBA) {
	bounds := dst.Bounds()

	for index := 1; index+1 < face.count; index += 2 {
		last := min(index+2, face.count-1)
		dst.BlendQuad([4]image.Point{
			s.posedPoint(face.corners[0]), s.posedPoint(face.corners[index]),
			s.posedPoint(face.corners[index+1]), s.posedPoint(face.corners[last]),
		}, bounds, col, 1)
	}
}

// outlineJetFace draws a face's edges as plain lines over its fill. It is for
// a plate seen nearly edge on: its width on screen is under a pixel, and a
// fill by pixel centres, or by triangles between whole-pixel corners, breaks
// it into a dotted line or drops it. The outline keeps the wing or the fin a
// line, as the dart's strokes do.
func (s *Scene) outlineJetFace(dst *canvas.Canvas, face *jetFace, col color.RGBA) {
	for index := range face.count {
		from, to := s.posedPoint(face.corners[index]), s.posedPoint(face.corners[(index+1)%face.count])
		dst.Line(from.X, from.Y, to.X, to.Y, col)
	}
}

// posedPoint is where vertex was projected, as a pixel.
func (s *Scene) posedPoint(vertex int) image.Point {
	return image.Pt(s.posed.atX[vertex], s.posed.atY[vertex])
}

// jetInk is the shade of col a face of brightness shade is filled with, where
// one is a face of the body square on to the light and jetDarkest the dimmest
// face there is.
//
// On a night palette the ink is lighter than the field, so a dimmer face is
// col faded towards the field by its brightness. On a day palette the ink is
// darker than the field, and the same fade would put the shadows on the paper
// and leave the lit faces darkest, which reads as a negative. There the scale
// runs the other way: the dimmest face keeps all of col and the brightest
// keeps jetDayFloor of it, so the lit side of a body is the light one in
// either.
func (s *Scene) jetInk(col color.RGBA, shade float64) color.RGBA {
	if !s.light {
		return s.fade(col, shade)
	}

	return s.fade(col, 1-(shade-jetDarkest)/(1-jetDarkest)*(1-jetDayFloor))
}
