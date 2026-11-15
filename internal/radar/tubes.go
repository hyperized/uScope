package radar

import (
	"cmp"
	"image"
	"image/color"
	"math"
	"slices"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
)

// How a route is drawn as a tube.
//
// The source hands over each route as a mean path with the spread of its
// tracks measured at every station. The tube is that path with the spread as
// its width, so an arrival route reads as a cone narrowing onto its runway
// and an airway as a pipe. The figures below are what the mock-up the look
// was judged on used.
const (
	// tubeSigmas is how many standard deviations of spread the tube is drawn
	// at. One keeps the tube to the core of the bundle, so two routes that
	// share a fix stay apart either side of it.
	tubeSigmas = 1.0

	// tubeFloorNm and tubeFloorFt are added to the spread, so a station where
	// every track flew the same line, which every arrival's last miles are,
	// still has a width to draw and a height to skin.
	tubeFloorNm = 0.4
	tubeFloorFt = 400.0

	// tubeFillFlat is the fill's opacity on the flat views at full strength.
	// The flat band is one layer of skin, so it can be stronger than the 3D
	// one without hiding the coast under it.
	tubeFillFlat = 0.16

	// tubeFill3 is the near wall's base opacity in the tilted views, before
	// the rim shading in rimAlpha turns it into a face's own alpha. A tube
	// seen side on is two walls, and where two routes cross it is four, so
	// the base is kept faint.
	tubeFill3 = 0.075

	// tubeLineStrength is the centreline's strength at full route strength.
	// The line is what reads as the route; the skin says how wide it is.
	tubeLineStrength = 0.75

	// tubeFarWall is the far wall's base opacity as a share of the near
	// wall's. Seen through the near wall it would otherwise read as the
	// nearer of the two.
	tubeFarWall = 0.75

	// tubeNearFloor and tubeFarFloor are the share of a wall's base that a
	// face looking straight at the camera keeps. A face seen edge on gets the
	// whole base; the floor is what stops the middle of a tube vanishing.
	tubeNearFloor = 0.5
	tubeFarFloor  = 0.35

	// tubeRimExponent, tubeRimGain and tubeRimCap are the rim shading's
	// look, the figures the mock-up was judged on: how quickly the density
	// falls away from the silhouette, how strong the result is, and where it
	// stops, so a tube seen end on never paints a solid disc.
	tubeRimExponent = 1.4
	tubeRimGain     = 2.2
	tubeRimCap      = 0.6

	// tubeEdgeStrength is the silhouette hairline's strength at full route
	// strength. It is the line that says where the tube turns away.
	tubeEdgeStrength = 0.55

	// tubeRingNear and tubeRingFar are a cross-section ring's strength on
	// the side facing the camera and on the side behind, at full route
	// strength.
	tubeRingNear = 0.5
	tubeRingFar  = 0.22

	// tubeTaper is how many stations a run fades out over where it ends away
	// from the field, whether the history ran out there or the filter, the
	// reach or a lost position cut it. A mouth drawn at a cut claims the
	// route stops in mid-air; a taper says only that the picture does.
	tubeTaper = 4

	// tubeRimSteps is how finely rimTable samples the rim shading. A face's
	// alpha ends up in eight bits on the canvas, so a step this fine cannot be
	// seen, and math.Pow once a face was 8% of BenchmarkDraw/minimal3d-tube-cap.
	tubeRimSteps = 256

	// halfway is the fraction along a line its middle is at.
	halfway = 0.5

	// tubeSides is how many sides a cross-section has in the tilted views.
	// Twelve is round at the size a tube is on screen, and a route of fifty
	// stations is six hundred quads.
	tubeSides = 12
)

// tubeAround is the cosine and sine of each cross-section vertex's angle
// round the tube, worked out once rather than per station.
//
//nolint:gochecknoglobals // a table, read only after init.
var tubeAround = func() [tubeSides][2]float64 {
	var around [tubeSides][2]float64

	for side := range around {
		sin, cos := math.Sincos(float64(side) * 2 * math.Pi / tubeSides)
		around[side] = [2]float64{cos, sin}
	}

	return around
}()

// rimTable is (1-|cos|)^tubeRimExponent at |cos| = step/tubeRimSteps.
//
//nolint:gochecknoglobals // a table, read only after init.
var rimTable = func() [tubeRimSteps + 1]float64 {
	var table [tubeRimSteps + 1]float64

	for step := range table {
		table[step] = math.Pow(1-float64(step)/tubeRimSteps, tubeRimExponent)
	}

	return table
}()

// tubePatch is one face of a tube's near wall, at its alpha with the far
// wall's behind it compounded in. Its outline is
// Scene.tubePoints[from:to].
type tubePatch struct {
	from, to int
	ink      color.RGBA
	alpha    float64
}

// tubeGroup is everything between two neighbouring cross-sections, painted
// as a unit: the lines behind the tube, the faces, and the lines in front of
// it, in that order. The faces are Scene.tubePatches[faces:facesEnd], the
// lines Scene.tubeUnder[under:underEnd] and Scene.tubeOver[over:overEnd].
//
// Groups are depth sorted rather than faces, because faces of one ink blended
// over each other come to the same colour in any order: a route is one ink,
// so sorting its faces among themselves would buy nothing. What does depend
// on the order is a solid line against a translucent wall, and that is the
// order inside a group. Across groups the sort puts a far tube's edges and
// rings under a nearer tube's walls.
type tubeGroup struct {
	faces, facesEnd int
	under, underEnd int
	over, overEnd   int
}

// tubeOrder is a group's place in the painting order: its mean depth and
// where it sits in Scene.tubeGroups. Sorting these rather than the groups
// moves sixteen bytes a swap instead of forty-eight.
type tubeOrder struct {
	depth float64
	index int
}

// tubeLine is one stroke of a tilted view's tubes: a stretch of centreline,
// a silhouette edge or a ring segment.
type tubeLine struct {
	x0, y0, x1, y1 float64
	ink            color.RGBA
}

// tubeStyle is one route's inks and wall opacities, worked out once per
// route rather than once per face.
type tubeStyle struct {
	ink, line, edge   color.RGBA
	ringNear, ringFar color.RGBA
	nearBase, farBase float64

	// strength is the route's own, kept so taper can fade the lines again
	// from the ink rather than fading an already faded colour.
	strength float64
}

// tubeSection is one station's cross-section in the tilted views: the
// vertices round it in the world and on the canvas, how deep each one is,
// whether the camera placed all of them, and where the centre landed.
type tubeSection struct {
	vertices [tubeSides]point3
	corners  [tubeSides]image.Point
	depths   [tubeSides]float64

	centre           point3
	centreX, centreY int
	centred          bool

	// whole is whether the camera placed every vertex. A section that is not
	// whole ends the run: a face cannot be drawn to a corner nobody could
	// place, and a mouth cannot be stroked round a ring with a gap in it.
	whole bool
}

// tubeDrawn reports whether a route gets a tube: an arrival or a departure
// strong enough to see. Under the exposure's own threshold the centreline
// would round to the field.
//
// Transit routes get none. A transit bundle is through traffic at 17,000 to
// 25,000 ft and up to nine miles wide, and at the 3D view's exaggeration it
// floats thirty miles above its ground track, where it drew as a large grey
// surface that a band filter then cut off in mid-air. The tubes are
// there to show the field's arrivals and departures; through traffic keeps
// its hairlines on the flat views and is left off the tilted ones.
func tubeDrawn(route *source.Route) bool {
	return route.Class != source.TrackTransit && tubeLineStrength*route.Strength >= corridorMinStrength
}

// stationShown reports whether a station is on the picture the filter asks
// for. An undecoded altitude is in no band, so it is never drawn.
func stationShown(station *source.Station, bands bandMask) bool {
	return bands.has(bandOf(station.Altitude))
}

// tubeHeading is the direction of a route's mean path at one station, on the
// ground, taken across its two neighbours so the cross-section stands square
// to the path through a turn rather than to one leg of it. The first and the
// last station use themselves as the missing neighbour.
//
//nolint:varnamelen // dx, dy is a direction on the ground, spelled as usual.
func tubeHeading(
	stations []source.Station, index int, place func(*source.Station) (float64, float64),
) (float64, float64) {
	behindX, behindY := place(&stations[max(index-1, 0)])
	aheadX, aheadY := place(&stations[min(index+1, len(stations)-1)])

	dx, dy := aheadX-behindX, aheadY-behindY

	length := math.Hypot(dx, dy)
	if length == 0 {
		return 0, 0
	}

	return dx / length, dy / length
}

// drawTubes paints every route on the flat views' ground: a translucent band
// the width of the bundle, and the mean path along its middle.
//
// The band is blended rather than faded into the field and painted solid,
// because it lies over the coastline and over any hairline track it crosses,
// and both have to show through it.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawTubes(dst *canvas.Canvas, proj projector, bands bandMask, weight int) {
	radius := proj.limitNm * proj.scale
	clip := image.Rect(
		proj.centerX-int(radius)-1, proj.centerY-int(radius)-1,
		proj.centerX+int(radius)+2, proj.centerY+int(radius)+2,
	)

	place := func(station *source.Station) (float64, float64) {
		return proj.offset(station.Latitude, station.Longitude)
	}

	for index := range s.exposure.routes {
		route := &s.exposure.routes[index]
		if !tubeDrawn(route) {
			continue
		}

		ink := s.classColour(route.Class)
		s.tubeLeft, s.tubeRight = s.tubeLeft[:0], s.tubeRight[:0]

		for station := range route.Stations {
			point := &route.Stations[station]
			x, y := place(point)

			if !stationShown(point, bands) || !positioned(point.Latitude, point.Longitude) ||
				math.Hypot(x-float64(proj.centerX), y-float64(proj.centerY)) > radius {
				s.flushBand(dst, clip, ink, route.Strength, weight)

				continue
			}

			dx, dy := tubeHeading(route.Stations, station, place)
			half := (tubeSigmas*point.SpreadNm + tubeFloorNm) * proj.scale

			s.tubeLeft = append(s.tubeLeft, roundPoint(x-dy*half, y+dx*half))
			s.tubeRight = append(s.tubeRight, roundPoint(x+dy*half, y-dx*half))
			s.tubeCentre = append(s.tubeCentre, [2]float64{x, y})
		}

		s.flushBand(dst, clip, ink, route.Strength, weight)
	}
}

// flushBand draws the run of stations collected so far, if there are two to
// draw between, and starts the next run empty.
//
// The band is one ring: down the left edge and back up the right. A filter or
// the edge of the scope cutting the route in the middle leaves two runs, and
// each is its own band.
func (s *Scene) flushBand(dst *canvas.Canvas, clip image.Rectangle, ink color.RGBA, strength float64, weight int) {
	count := len(s.tubeLeft)

	if count >= 2 {
		dst.BlendPolygon(s.bandRing(), clip, ink, tubeFillFlat*strength)

		line := s.fade(ink, tubeLineStrength*strength)

		for index := 1; index < count; index++ {
			from, to := s.tubeCentre[index-1], s.tubeCentre[index]
			dst.StrokeLineAA(from[0], from[1], to[0], to[1], max(weight, 1), line)
		}
	}

	s.tubeLeft, s.tubeRight, s.tubeCentre = s.tubeLeft[:0], s.tubeRight[:0], s.tubeCentre[:0]
}

// bandRing is the outline of the run in tubeLeft and tubeRight, down the
// left edge and back up the right, as the one-ring list BlendPolygon takes.
func (s *Scene) bandRing() [][]image.Point {
	ring := s.tubeRing[:0]
	ring = append(ring, s.tubeLeft...)

	for _, point := range slices.Backward(s.tubeRight) {
		ring = append(ring, point)
	}

	s.tubeRing = ring
	s.tubeRings[0] = ring

	return s.tubeRings[:]
}

// drawTubes3 skins every route in the tilted views, each station's
// cross-section an ellipse at its mean altitude as wide as the bundle's
// spread on the ground and as tall as its spread in altitude.
//
// Every stretch of tube between two stations is painted far to near, each
// one as the ring segments behind it, its walls, and its edges and the ring
// segments in front, so a near wall tints whatever is behind it rather than
// being tinted by it. The centrelines go on last so
// the skin never dims the line that names the route.
func (s *Scene) drawTubes3(dst *canvas.Canvas, view scene3, bands bandMask) {
	s.tubePoints, s.tubePatches, s.tubeGroups = s.tubePoints[:0], s.tubePatches[:0], s.tubeGroups[:0]
	s.tubeOrders, s.tubeLines = s.tubeOrders[:0], s.tubeLines[:0]
	s.tubeUnder, s.tubeOver = s.tubeUnder[:0], s.tubeOver[:0]
	reach := !s.bare()

	for index := range s.exposure.routes {
		route := &s.exposure.routes[index]
		if tubeDrawn(route) {
			s.skinRoute(view, route, bands, reach)
		}
	}

	slices.SortFunc(s.tubeOrders, farthestFirst)

	weight := max(view.weight, 1)

	for _, order := range s.tubeOrders {
		group := &s.tubeGroups[order.index]
		strokeTubeLines(dst, s.tubeUnder[group.under:group.underEnd], weight)
		s.blendFaces(dst, group.faces, group.facesEnd)
		strokeTubeLines(dst, s.tubeOver[group.over:group.overEnd], weight)
	}

	strokeTubeLines(dst, s.tubeLines, weight)
}

// blendFaces paints tubePatches[from:to].
func (s *Scene) blendFaces(dst *canvas.Canvas, from, to int) {
	clip := dst.Bounds()

	for index := from; index < to; index++ {
		patch := &s.tubePatches[index]
		s.tubeRings[0] = s.tubePoints[patch.from:patch.to]
		dst.BlendPolygon(s.tubeRings[:], clip, patch.ink, patch.alpha)
	}
}

// strokeTubeLines draws a run of the tubes' strokes.
func strokeTubeLines(dst *canvas.Canvas, lines []tubeLine, weight int) {
	for index := range lines {
		line := &lines[index]
		dst.StrokeLineAA(line.x0, line.y0, line.x1, line.y1, weight, line.ink)
	}
}

// tubeWalk is where skinRoute has got to along a route: the last two
// cross-sections, how many stations the unbroken run holds so far and the
// index of the last of them.
type tubeWalk struct {
	sections     [2]tubeSection
	length, last int
}

// tubeStyleOf is a route's inks and wall opacities.
func (s *Scene) tubeStyleOf(route *source.Route) tubeStyle {
	ink, strength := s.classColour(route.Class), route.Strength
	wall := tubeFill3 * strength

	return tubeStyle{
		ink:      ink,
		line:     s.fade(ink, tubeLineStrength*strength),
		edge:     s.fade(ink, tubeEdgeStrength*strength),
		ringNear: s.fade(ink, tubeRingNear*strength),
		ringFar:  s.fade(ink, tubeRingFar*strength),
		nearBase: wall,
		farBase:  wall * tubeFarWall,
		strength: strength,
	}
}

// taper is the style scaled down for a station near the end of a run: the
// walls' bases times scale, and the edges and the centreline faded again from
// the ink at scale times their strength. The rings are left alone, since the
// only ring a run keeps is the mouth at the field, which never tapers.
func (s *Scene) taper(style *tubeStyle, scale float64) tubeStyle {
	if scale >= 1 {
		return *style
	}

	scaled := *style
	scaled.nearBase *= scale
	scaled.farBase *= scale
	scaled.edge = s.fade(style.ink, tubeEdgeStrength*style.strength*scale)
	scaled.line = s.fade(style.ink, tubeLineStrength*style.strength*scale)

	return scaled
}

// tubeScale is how much of its wall a station keeps: step/tubeTaper for the
// nearest step under tubeTaper at which the station ahead is past the route's
// end or not shown, or the station behind is not shown, and 1 when there is
// none. Station 0 has nothing behind it and is not tapered for that: it is
// the field end, where the route really starts.
func tubeScale(shown []bool, station int) float64 {
	for step := 1; step < tubeTaper; step++ {
		ahead, behind := station+step, station-step

		if ahead >= len(shown) || !shown[ahead] || (behind >= 0 && !shown[behind]) {
			return float64(step) / tubeTaper
		}
	}

	return 1
}

// skinRoute collects one route's walls, edges, mouth and centreline.
//
// A run of stations breaks wherever the filter, a missing position or the
// full view's reach drops one, and at a cross-section the camera could not
// place whole. Only a run that starts at the field gets a mouth there; every
// run fades out over its last tubeTaper stations instead of ending in a ring,
// and a run cut on its field side fades in the same way.
//
// Which stations are shown is worked out before the walk, because a
// station's taper depends on the stations ahead of it.
//
//nolint:revive // flag-parameter: reach is one test skipped, not a second mode.
func (s *Scene) skinRoute(view scene3, route *source.Route, bands bandMask, reach bool) {
	style := s.tubeStyleOf(route)

	place := func(station *source.Station) (float64, float64) {
		ground := view.ground(station.Latitude, station.Longitude)

		return ground.east, ground.north
	}

	var (
		walk  tubeWalk
		shown [source.RouteMaxStations]bool
	)

	count := min(len(route.Stations), len(shown))

	for station := range count {
		point := &route.Stations[station]
		shown[station] = stationShown(point, bands) && positioned(point.Latitude, point.Longitude) &&
			(!reach || view.inRange(view.world(point.Latitude, point.Longitude, point.Altitude)))
	}

	for station := range count {
		if !shown[station] {
			walk.length = 0

			continue
		}

		point := &route.Stations[station]
		centre := view.world(point.Latitude, point.Longitude, point.Altitude)
		dx, dy := tubeHeading(route.Stations[:count], station, place)
		scaled := s.taper(&style, tubeScale(shown[:count], station))
		s.skinStation(&walk, view, station, centre, dx, dy, point, &scaled)
	}
}

// skinStation adds one station to the run: its cross-section, the stretch of
// tube back to the station before and the centreline between them.
//
// The mouth goes in with the run's first stretch, and only when the run
// starts at station 0, the field end: a station standing alone gets no mouth
// with no tube behind it, and a run that starts at a cut fades in instead.
//
//nolint:varnamelen // dx, dy is a direction on the ground, spelled as usual.
func (s *Scene) skinStation(
	walk *tubeWalk, view scene3, station int, centre point3, dx, dy float64, point *source.Station, style *tubeStyle,
) {
	current := &walk.sections[station%2]
	s.section(current, view, centre, dx, dy, point)

	eye := view.cam.eye

	if !current.whole {
		walk.length = 0

		return
	}

	if walk.length > 0 {
		last := &walk.sections[(station+1)%2]
		s.collectStrips(last, current, eye, style)

		if walk.length == 1 && walk.last == 0 {
			s.collectRing(last, eye, style)
		}

		if last.centred && current.centred {
			s.tubeLines = append(s.tubeLines, tubeLine{
				x0: float64(last.centreX), y0: float64(last.centreY),
				x1: float64(current.centreX), y1: float64(current.centreY),
				ink: style.line,
			})
		}
	}

	walk.length++
	walk.last = station
}

// section projects one station's cross-section. The ring stands square to
// the path: across it on the ground and straight up, so a level route is a
// pipe and a descending one is a pipe cut on the slant.
//
//nolint:varnamelen // dx, dy is a direction on the ground, spelled as usual.
func (*Scene) section(out *tubeSection, view scene3, centre point3, dx, dy float64, point *source.Station) {
	across := tubeSigmas*point.SpreadNm + tubeFloorNm
	upward := view.height(tubeSigmas*point.SpreadFt + tubeFloorFt)
	out.whole = true

	for side, turn := range tubeAround {
		vertex := point3{
			east:  centre.east - dy*across*turn[0],
			north: centre.north + dx*across*turn[0],
			up:    centre.up + upward*turn[1],
		}

		x, y, placed := view.cam.at(vertex)
		out.vertices[side] = vertex
		out.corners[side] = image.Point{X: x, Y: y}
		out.depths[side] = view.cam.depth(vertex)
		out.whole = out.whole && placed
	}

	out.centre = centre
	out.centreX, out.centreY, out.centred = view.cam.at(centre)
}

// collectStrips opens the group for the stretch of tube between two
// neighbouring cross-sections: its walls, one face at a time, and the
// silhouette edges where the ring turns from the near wall to the far one.
//
// Each face carries its own alpha from rimAlpha, so the tube is dense where
// it turns away from the eye and thin where it looks at it, which is what
// makes twelve flat faces read as a round tube.
//
// Both walls are drawn, but only the near wall's faces are filled. A stretch
// of tube is convex, so the far wall covers exactly the pixels the near one
// does, and the far face behind a pixel sits at the same angle to the eye as
// the near face in front of it, mirrored across the tube's axis. One ink
// blended twice is one blend at the two alphas compounded, so each near face
// is filled at its own alpha compounded with its mirror's: the picture of
// both walls at the cost in pixels of one. A face seen exactly edge on covers
// no pixels and is drawn as neither.
func (s *Scene) collectStrips(start, end *tubeSection, eye point3, style *tubeStyle) {
	group := tubeGroup{faces: len(s.tubePatches), under: len(s.tubeUnder), over: len(s.tubeOver)}
	depth, wasFront := 0.0, false

	for step := range tubeSides + 1 {
		side := step % tubeSides
		next := (side + 1) % tubeSides
		cos := faceCos(start.vertices[side], start.vertices[next], end.vertices[side], end.vertices[next], eye)
		front := cos > 0

		if step > 0 && front != wasFront {
			s.tubeOver = appendTubeLine(s.tubeOver, start.corners[side], end.corners[side], style.edge)
		}

		wasFront = front

		if step == tubeSides {
			break
		}

		depth += start.depths[side] + end.depths[side]

		if front {
			near, far := rimAlpha(style.nearBase, tubeNearFloor, cos), rimAlpha(style.farBase, tubeFarFloor, cos)
			s.addFace(start, end, side, style.ink, 1-(1-near)*(1-far))
		}
	}

	group.facesEnd, group.underEnd, group.overEnd = len(s.tubePatches), len(s.tubeUnder), len(s.tubeOver)
	s.tubeOrders = append(s.tubeOrders, tubeOrder{depth: depth / (2 * tubeSides), index: len(s.tubeGroups)})
	s.tubeGroups = append(s.tubeGroups, group)
}

// collectRing adds one cross-section to the group collected last, as twelve
// segments: strong on the side facing the camera and drawn over the near
// wall, faint behind and drawn under it. Which side a segment is on is read
// off the ring's own outward direction rather than off a face, so a mouth
// needs no station beyond it.
func (s *Scene) collectRing(section *tubeSection, eye point3, style *tubeStyle) {
	for side := range tubeSides {
		next := (side + 1) % tubeSides
		middle := section.vertices[side].along(section.vertices[next], halfway)

		if middle.minus(section.centre).dot(eye.minus(middle)) > 0 {
			s.tubeOver = appendTubeLine(s.tubeOver, section.corners[side], section.corners[next], style.ringNear)
		} else {
			s.tubeUnder = appendTubeLine(s.tubeUnder, section.corners[side], section.corners[next], style.ringFar)
		}
	}

	group := &s.tubeGroups[len(s.tubeGroups)-1]
	group.underEnd, group.overEnd = len(s.tubeUnder), len(s.tubeOver)
}

// addFace records the outline of one face, from side to the next corner
// round the ring, along the first cross-section and back along the second.
func (s *Scene) addFace(start, end *tubeSection, side int, ink color.RGBA, alpha float64) {
	next := (side + 1) % tubeSides
	begin := len(s.tubePoints)
	s.tubePoints = append(s.tubePoints, start.corners[side], start.corners[next], end.corners[next], end.corners[side])
	s.tubePatches = append(s.tubePatches, tubePatch{from: begin, to: len(s.tubePoints), ink: ink, alpha: alpha})
}

// appendTubeLine is lines with one more stroke from one pixel to another.
func appendTubeLine(lines []tubeLine, from, to image.Point, ink color.RGBA) []tubeLine {
	return append(lines, tubeLine{
		x0: float64(from.X), y0: float64(from.Y), x1: float64(to.X), y1: float64(to.Y), ink: ink,
	})
}

// faceCos is the cosine between a face's outward normal and the direction
// from the face's centre to the eye: positive on the near wall, negative on
// the far one, zero for a face seen edge on or one with no area.
//
// Round the ring and along the tube cross to the outward normal, because the
// ring is laid from the left of the direction of travel up over the top. The
// centre is the middle of the diagonal from at to across.
func faceCos(at, round, along, across, eye point3) float64 {
	ux, uy, uz := round.east-at.east, round.north-at.north, round.up-at.up
	vx, vy, vz := along.east-at.east, along.north-at.north, along.up-at.up
	normal := point3{east: uy*vz - uz*vy, north: uz*vx - ux*vz, up: ux*vy - uy*vx}
	toEye := eye.minus(at.along(across, halfway))

	length := math.Sqrt(normal.dot(normal) * toEye.dot(toEye))
	if length == 0 {
		return 0
	}

	return normal.dot(toEye) / length
}

// rimAlpha is a face's opacity on a wall of opacity base, from the cosine
// between its normal and the eye: the full base and more at the silhouette,
// falling to floor of it on the face looking at the camera, and never past
// tubeRimCap. The rim term is (1-|cos|)^tubeRimExponent, read off rimTable.
func rimAlpha(base, floor, cos float64) float64 {
	rim := rimTable[int(math.Round(min(1, math.Abs(cos))*tubeRimSteps))]

	return min(tubeRimCap, base*(floor+(1-floor)*rim)*tubeRimGain)
}

// farthestFirst orders groups from the back of the picture to the front.
func farthestFirst(left, right tubeOrder) int { return cmp.Compare(right.depth, left.depth) }

// roundPoint is a pixel position rounded to the nearest pixel.
func roundPoint(x, y float64) image.Point {
	return image.Point{X: int(math.Round(x)), Y: int(math.Round(y))}
}
