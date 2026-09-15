package radar

import (
	"image"
	"math"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airports"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/text"
)

// The scope's furniture, in pixels.
const (
	// cardinalGap is the air between the outer ring and the N E S W letters.
	cardinalGap = 4

	// ringInset keeps the full-range ring a little inside the outer ring, so
	// the two read as a boundary and a range rather than as one thick line.
	ringInset = 4

	// ringCount is how many range rings are drawn, at even fractions of the
	// current range.
	ringCount = 3

	// dashOn and dashOff are the range rings' dash pattern, in samples along
	// the circumference. Roughly one sample per pixel, so this is about four
	// pixels on and five off.
	dashOn  = 4
	dashOff = 5

	// homeRadius is the little ring around the receiver's own position.
	homeRadius = 4

	// rangeLabelGap is the air between a range number and the ring it names.
	rangeLabelGap = 4

	// airportHalf is half the side of an airport's hollow square, and
	// airportLabelGap the air before its ICAO code.
	airportHalf     = 2
	airportLabelGap = 5

	// noHeadingRadius draws an aircraft whose heading nobody has decoded as a
	// small hollow circle. Radius 2 is five pixels across.
	noHeadingRadius = 2

	// selectionRadius is the ring around the selected aircraft, and leaderRun
	// how far up and to the right its label sits.
	selectionRadius = 11
	leaderRun       = 20

	// Trails fade from tail to head. Starting at a quarter rather than at
	// nothing keeps the oldest part of a long trail visible on the panel,
	// where a very dim line disappears into the field.
	trailMinAlpha = 0.25
	trailMaxAlpha = 1.0

	// nmPerDegree is one degree of latitude in nautical miles, which is the
	// definition of the unit.
	nmPerDegree = 60.0

	// rangeUnit is the suffix on every range number. It carries its own
	// leading space so it can be drawn straight after the number.
	rangeUnit = " NM"
)

// projector turns a position into a pixel on the scope.
//
// The projection is a local equirectangular one: latitude scales straight to
// distance, and longitude is squeezed by the cosine of the receiver's
// latitude. Over the tens of nautical miles a scope covers, the error against
// a proper projection is smaller than a pixel, and it costs two multiplies
// where a great-circle projection costs several trigonometric calls per
// aircraft per frame.
type projector struct {
	centerX int
	centerY int
	radius  int
	scopeNm float64
	lat0    float64
	lon0    float64
	cosLat0 float64
	scale   float64
}

// newProjector builds the projection for one frame.
func newProjector(geom scopeGeometry, receiver source.Receiver, scopeNm float64) (projector, bool) {
	if geom.rangeR <= 0 || scopeNm <= 0 || math.IsNaN(scopeNm) {
		return projector{}, false
	}

	// A receiver at exactly (0, 0) is the "no position yet" state rather than
	// a buoy in the Gulf of Guinea, and uAirwaves' distance function treats it
	// the same way. Nothing can be plotted against it.
	if receiver.Latitude == 0 && receiver.Longitude == 0 {
		return projector{}, false
	}

	return projector{
		centerX: geom.centerX,
		centerY: geom.centerY,
		radius:  geom.rangeR,
		scopeNm: scopeNm,
		lat0:    receiver.Latitude,
		lon0:    receiver.Longitude,
		cosLat0: math.Cos(receiver.Latitude * math.Pi / halfCircle),
		scale:   float64(geom.rangeR) / scopeNm,
	}, true
}

// at projects a position, reporting false when it falls outside the scope.
//
// The inside test is done in nautical miles rather than in pixels so it does
// not depend on the rounding, and so an aircraft exactly on the outer ring
// lands on the ring rather than one pixel past it.
func (p projector) at(latitude, longitude float64) (int, int, bool) {
	if latitude == 0 && longitude == 0 {
		return 0, 0, false
	}

	eastNm := (longitude - p.lon0) * nmPerDegree * p.cosLat0
	northNm := (latitude - p.lat0) * nmPerDegree

	if math.IsNaN(eastNm) || math.IsNaN(northNm) || math.Hypot(eastNm, northNm) > p.scopeNm {
		return 0, 0, false
	}

	return p.centerX + int(math.Round(eastNm*p.scale)),
		p.centerY - int(math.Round(northNm*p.scale)),
		true
}

// scopeGeometry is where the rings and the centre go.
type scopeGeometry struct {
	centerX int
	centerY int

	// outer is the faint boundary ring the cardinal letters sit outside of,
	// and rangeR the ring that means the current range.
	outer  int
	rangeR int
}

// geometry measures the scope box.
//
// The inset is the room the cardinal letters need outside the boundary ring.
// It is passed in rather than worked out here, because the scene decides
// whether there are labels at all and this only has to know how much space
// they take.
func geometry(box image.Rectangle, inset int) (scopeGeometry, bool) {
	side := min(box.Dx(), box.Dy())
	if side <= 0 {
		return scopeGeometry{}, false
	}

	geom := scopeGeometry{
		centerX: box.Min.X + box.Dx()/2,
		centerY: box.Min.Y + box.Dy()/2,
		outer:   side/2 - inset,
	}
	geom.rangeR = geom.outer - ringInset

	return geom, geom.rangeR > 0
}

// drawScope paints the left half: rings, cardinals, the home marker, the
// airports and the aircraft.
func (s *Scene) drawScope(lay *layout, frame source.Frame) {
	if lay.scope.Empty() {
		return
	}

	// The boundary ring has to leave room outside itself for the N E S W
	// letters, or they would be drawn off the edge of the frame.
	inset := cardinalGap
	if lay.labels {
		inset += lineHeight(s.faces.Body)
	}

	geom, drawable := geometry(lay.scope, inset)
	if !drawable {
		return
	}

	scopeNm := s.scopeRange.GetCurrent()

	s.drawRings(lay, geom, scopeNm)
	s.drawCardinals(lay, geom)
	s.drawHome(lay, geom)

	proj, plottable := newProjector(geom, frame.Receiver, scopeNm)
	if !plottable {
		return
	}

	s.drawAirports(lay, proj)
	s.drawAircraft(lay, proj, frame)
}

// drawRings draws the boundary and the range rings, with the range written on
// each one.
func (s *Scene) drawRings(lay *layout, geom scopeGeometry, scopeNm float64) {
	lay.dst.Circle(geom.centerX, geom.centerY, geom.outer, s.pal.Rule)

	spacing := geom.rangeR / ringCount

	for ring := 1; ring <= ringCount; ring++ {
		radius := geom.rangeR * ring / ringCount
		lay.dst.DashedCircle(geom.centerX, geom.centerY, radius, dashOn, dashOff, s.pal.Rule)
		s.drawRangeLabel(lay, geom, radius, spacing, scopeNm*float64(ring)/ringCount)
	}
}

// drawRangeLabel writes one ring's range just inside it, above the three
// o'clock point so the text never sits on the line it names.
//
// A label wider than the gap to the ring inside it is dropped rather than
// drawn. Three numbers running into each other say less than no numbers at
// all, and on a small scope the rings are only a couple of dozen pixels apart.
func (s *Scene) drawRangeLabel(lay *layout, geom scopeGeometry, radius, spacing int, valueNm float64) {
	face := s.faces.Small
	if !lay.labels || face == nil || radius <= 0 {
		return
	}

	value := s.whole(valueNm)
	unit, _ := text.Measure(face, rangeUnit)
	width := measureBytes(face, value) + unit

	if width+2*rangeLabelGap > spacing {
		return
	}

	left := geom.centerX + radius - rangeLabelGap - width
	top := geom.centerY - face.Height() - rangeLabelGap

	pen := drawBytes(lay.dst, face, left, top, value, s.pal.Muted)
	text.Draw(lay.dst, face, pen, top, rangeUnit, s.pal.Muted)
}

// drawCardinals puts N, E, S and W just outside the boundary ring, which is
// the only thing on the scope that says which way up the world is.
func (s *Scene) drawCardinals(lay *layout, geom scopeGeometry) {
	face := s.faces.Body
	if !lay.labels || face == nil {
		return
	}

	height := face.Height()
	edge := geom.outer + cardinalGap

	text.DrawCentered(lay.dst, face, geom.centerX, geom.centerY-edge-height, "N", s.pal.Muted)
	text.DrawCentered(lay.dst, face, geom.centerX, geom.centerY+edge, "S", s.pal.Muted)
	text.Draw(lay.dst, face, geom.centerX+edge, geom.centerY-height/2, "E", s.pal.Muted)
	text.DrawRight(lay.dst, face, geom.centerX-edge, geom.centerY-height/2, "W", s.pal.Muted)
}

// drawHome marks the receiver's own position.
func (s *Scene) drawHome(lay *layout, geom scopeGeometry) {
	lay.dst.Circle(geom.centerX, geom.centerY, homeRadius, s.pal.Muted)
	lay.dst.FillCircle(geom.centerX, geom.centerY, 1, s.pal.Ink)
}

// drawAirports overlays the airports that fall inside the current range.
//
// They are drawn before the aircraft so an aeroplane on final approach is not
// hidden underneath the field it is landing at.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawAirports(lay *layout, proj projector) {
	face := s.faces.Small

	for _, field := range airports.All() {
		x, y, inside := proj.at(field.Latitude, field.Longitude)
		if !inside {
			continue
		}

		lay.dst.Rect(image.Rect(x-airportHalf, y-airportHalf, x+airportHalf+1, y+airportHalf+1), s.pal.Rule)

		if lay.labels && face != nil {
			text.Draw(lay.dst, face, x+airportLabelGap, y-face.Height()/2, field.ICAO, s.pal.Muted)
		}
	}
}

// drawAircraft paints every aircraft that is inside the range: trails first,
// then the silhouettes on top of them, then the selection marker on top of
// everything.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawAircraft(lay *layout, proj projector, frame source.Frame) {
	if s.trails {
		for _, plane := range frame.Planes {
			s.drawTrail(lay.dst, proj, plane)
		}
	}

	for _, plane := range frame.Planes {
		x, y, inside := proj.at(plane.Latitude, plane.Longitude)
		if !inside {
			continue
		}

		s.drawContact(lay.dst, x, y, plane)

		if plane.ICAO == s.selICAO {
			s.drawSelection(lay, x, y, plane)
		}
	}
}

// drawTrail draws one aircraft's history as a polyline, oldest to newest,
// brightening towards the head.
//
// A segment with either end outside the range is dropped rather than clipped.
// Clipping would be more correct, but a dropped segment leaves a gap at the
// edge of the scope where a clipped one would draw a line to a place the
// aircraft never was.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawTrail(dst *canvas.Canvas, proj projector, plane airplane.Snapshot) {
	history := plane.PositionHistory
	if len(history) < 2 {
		return
	}

	col := s.bandColour(plane.Altitude)
	span := float64(len(history) - 1)

	prevX, prevY, prevInside := proj.at(history[0].Latitude, history[0].Longitude)

	for index := 1; index < len(history); index++ {
		x, y, inside := proj.at(history[index].Latitude, history[index].Longitude)

		if inside && prevInside {
			alpha := trailMinAlpha + (trailMaxAlpha-trailMinAlpha)*float64(index)/span
			dst.LineAA(float64(prevX), float64(prevY), float64(x), float64(y), s.fade(col, alpha))
		}

		prevX, prevY, prevInside = x, y, inside
	}
}

// drawContact draws one aircraft.
//
// A heading of exactly zero means nobody has decoded one yet rather than due
// north: an aircraft starts at zero and stays there until a velocity message
// arrives, and a real decoded heading lands on an exact zero about never. An
// aircraft without one is a bare circle, because a silhouette would be
// claiming to know which way it is pointing.
func (s *Scene) drawContact(dst *canvas.Canvas, x, y int, plane airplane.Snapshot) { //nolint:varnamelen // pixels.
	col := s.bandColour(plane.Altitude)

	if plane.Heading == 0 {
		dst.Circle(x, y, noHeadingRadius, col)

		return
	}

	s.icon.Draw(dst, x, y, plane.Heading, col)
}

// drawSelection rings the selected aircraft and labels it on a leader line,
// so the card on the right and the dot on the scope are obviously the same
// aeroplane.
func (s *Scene) drawSelection(lay *layout, x, y int, plane airplane.Snapshot) { //nolint:varnamelen // pixels.
	dst := lay.dst
	dst.Circle(x, y, selectionRadius, s.pal.Accent)

	endX, endY := x+leaderRun, y-leaderRun
	dst.Line(x+selectionRadius/2, y-selectionRadius/2, endX, endY, s.pal.Accent)

	face := s.faces.BodyBold
	if !lay.labels || face == nil {
		return
	}

	text.Draw(dst, face, endX+labelTracking, endY-face.Height(), callsignOf(plane), s.pal.Accent)
}

// callsignOf is the callsign, or the ICAO hex when no callsign has been
// decoded. An aircraft is always identified by something.
func callsignOf(plane airplane.Snapshot) string {
	if plane.Callsign == "" {
		return plane.ICAO
	}

	return plane.Callsign
}
