package radar

import (
	"image"
	"image/color"
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

	// limitNm is how far from the centre a position may be and still be
	// drawn. It is the range itself in the ordinary view, where the outer
	// ring is the edge of the world. Minimal mode widens it to the canvas
	// corner, because it has no ring and the corners are meant to show
	// traffic a ring would have cut off.
	limitNm float64

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
		limitNm: scopeNm,
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
// lands on the ring rather than one pixel past it. What counts as outside is
// limitNm, which is the range ring in the ordinary view and the canvas corner
// in minimal mode.
func (p projector) at(latitude, longitude float64) (int, int, bool) {
	if latitude == 0 && longitude == 0 {
		return 0, 0, false
	}

	eastNm := (longitude - p.lon0) * nmPerDegree * p.cosLat0
	northNm := (latitude - p.lat0) * nmPerDegree

	if math.IsNaN(eastNm) || math.IsNaN(northNm) || math.Hypot(eastNm, northNm) > p.limitNm {
		return 0, 0, false
	}

	return p.centerX + int(math.Round(eastNm*p.scale)),
		p.centerY - int(math.Round(northNm*p.scale)),
		true
}

// offset projects a position to a pixel without the inside test at does.
//
// The shore needs it. A coastline is clipped to the ring by distance rather
// than dropped point by point, so a line that left the scope between two of
// its points has to be cut where it crossed rather than at the last point that
// happened to be inside. at cannot say where that is, because it reports only
// that the point is out.
func (p projector) offset(latitude, longitude float64) (float64, float64) {
	eastNm := (longitude - p.lon0) * nmPerDegree * p.cosLat0
	northNm := (latitude - p.lat0) * nmPerDegree

	return float64(p.centerX) + eastNm*p.scale, float64(p.centerY) - northNm*p.scale
}

// reaching returns the same projection with a wider cut-off.
//
// The copy is taken rather than the receiver written to: a projector is passed
// around by value everywhere else in here, and a method that quietly edited
// the one it was called on would be the one exception.
func (p projector) reaching(limitNm float64) projector {
	wider := p
	wider.limitNm = limitNm

	return wider
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

// scopeFrame is the scope measured for one frame: where the rings go, how a
// position becomes a pixel, and how far the outer ring reaches.
//
// plottable is false when nothing can be plotted against the receiver, which
// is the state before any position is known. The rings and the cardinals are
// still drawn then, because they say what the scope would show; the shore,
// the airports and the aircraft are not, because there is nowhere to put them.
type scopeFrame struct {
	geom      scopeGeometry
	proj      projector
	scopeNm   float64
	plottable bool
}

// measureScope works out the scope's geometry and projection, reporting false
// when there is no room for a scope at all.
//
// It is measured twice per frame, once for the background layer and once for
// the traffic drawn over it. The two have to agree to the pixel or an
// aeroplane would sit beside its rings rather than on them, so they share this
// function rather than a cached answer that could go stale between them.
func (s *Scene) measureScope(lay *layout, receiver source.Receiver) (scopeFrame, bool) {
	if s.minimal {
		return s.measureMinimal(lay.dst, receiver)
	}

	if lay.scope.Empty() {
		return scopeFrame{}, false
	}

	// The boundary ring has to leave room outside itself for the N E S W
	// letters, or they would be drawn off the edge of the frame.
	inset := cardinalGap
	if lay.labels {
		inset += lineHeight(s.faces.Body)
	}

	geom, drawable := geometry(lay.scope, inset)
	if !drawable {
		return scopeFrame{}, false
	}

	scopeNm := s.scopeRange.GetCurrent()
	proj, plottable := newProjector(geom, receiver, scopeNm)

	return scopeFrame{geom: geom, proj: proj, scopeNm: scopeNm, plottable: plottable}, true
}

// measureMinimal is the scope minimal mode projects with.
//
// The scope is the whole canvas rather than a box beside a column, so the
// centre is the centre of the frame and the range maps to half the short edge.
// Nothing is clipped to that radius: the cut-off is pushed out to the furthest
// corner, which is the last place a position can land on a pixel, so the
// corners show traffic the range ring would have hidden.
func (s *Scene) measureMinimal(dst *canvas.Canvas, receiver source.Receiver) (scopeFrame, bool) {
	geom, drawable := minimalGeometry(dst.Bounds())
	if !drawable {
		return scopeFrame{}, false
	}

	scopeNm := s.scopeRange.GetCurrent()

	proj, plottable := newProjector(geom, receiver, scopeNm)
	if plottable {
		proj = proj.reaching(scopeNm * cornerReach(dst.Bounds(), geom))
	}

	return scopeFrame{geom: geom, proj: proj, scopeNm: scopeNm, plottable: plottable}, true
}

// minimalGeometry centres the scope on the canvas with the range at half the
// short edge. There are no rings to draw, so the boundary and the range radius
// are the same number.
func minimalGeometry(bounds image.Rectangle) (scopeGeometry, bool) {
	half := min(bounds.Dx(), bounds.Dy()) / 2

	return scopeGeometry{
		centerX: bounds.Min.X + bounds.Dx()/2,
		centerY: bounds.Min.Y + bounds.Dy()/2,
		outer:   half,
		rangeR:  half,
	}, half > 0
}

// cornerReach is how much further than the range radius the furthest corner of
// the canvas sits, as a multiple of that radius.
func cornerReach(bounds image.Rectangle, geom scopeGeometry) float64 {
	wide := float64(max(geom.centerX-bounds.Min.X, bounds.Max.X-geom.centerX))
	tall := float64(max(geom.centerY-bounds.Min.Y, bounds.Max.Y-geom.centerY))

	return math.Hypot(wide, tall) / float64(geom.rangeR)
}

// drawField paints everything on the scope that does not move between frames:
// the shore, the rings, the cardinals, the range labels, the home marker and
// the airports.
//
// This is the whole of the background layer's contents. The shore goes down
// first so the rings and the markers read above it, which is the point of
// giving it the quietest colour in the palette.
func (s *Scene) drawField(lay *layout, frame source.Frame) {
	view, drawable := s.measureScope(lay, frame.Receiver)
	if !drawable {
		return
	}

	if s.shoreOn && view.plottable {
		s.drawShore(lay.dst, view, frame.Receiver)
	}

	s.drawRings(lay, view.geom, view.scopeNm)
	s.drawCardinals(lay, view.geom)
	s.drawHome(lay, view.geom, frame.Receiver.Mode)

	if s.airports && view.plottable {
		s.drawAirports(lay, view.proj)
	}
}

// drawTraffic paints the part of the scope that changes every frame: the
// trails, the aircraft and the selection marker.
func (s *Scene) drawTraffic(lay *layout, frame source.Frame) {
	view, drawable := s.measureScope(lay, frame.Receiver)
	if !drawable || !view.plottable {
		return
	}

	s.drawAircraft(lay, view.proj, frame)
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

// drawHome marks the receiver's own position, with the ring coloured by where
// that position came from.
//
// The ring carries the fix state and the centre dot stays ink, so the marker
// is in the same place and the same size whatever is known: it is one glance
// for "am I where I think I am", not a second thing to find on the field. The
// header's mode word takes the same colour, so the two read as one signal
// rather than as two facts to reconcile.
func (s *Scene) drawHome(lay *layout, geom scopeGeometry, mode source.FixMode) {
	lay.dst.Circle(geom.centerX, geom.centerY, homeRadius, s.fixColour(mode, s.pal.Ink))
	lay.dst.FillCircle(geom.centerX, geom.centerY, 1, s.pal.Ink)
}

// fixColour says how much the receiver's position is worth.
//
// Muted for nothing known, the reading colour for a position the operator
// typed in, the accent for an estimate with a radius on it, and the altitude
// ramp for a GPS: red while it is searching, amber for a fix without altitude,
// green for a full one. The altitude bands are reused rather than given three
// colours of their own, because they are already the palette's "getting
// better" ramp and a second set would be three more colours to keep in step
// across two themes.
//
// The reading colour is passed in rather than taken from the palette, because
// the header band has its own. The palette's Ink is a dark navy and paper's
// band is a dark navy, so an Ink word on that band would be a word nobody can
// read.
func (s *Scene) fixColour(mode source.FixMode, ink color.RGBA) color.RGBA {
	switch mode {
	case source.FixManual:
		return ink
	case source.FixEstimated:
		return s.pal.Accent
	case source.FixGPSNoFix:
		return s.pal.AltHigh
	case source.FixGPS2D:
		return s.pal.AltMid
	case source.FixGPS3D:
		return s.pal.AltLow
	case source.FixNone:
		fallthrough
	default:
		return s.pal.Muted
	}
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

	col := s.aircraftColour(plane)
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
	col := s.aircraftColour(plane)

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

	// Minimal keeps the ring and drops the rest. The leader line exists to
	// carry a callsign out to where it can be read, and minimal sets no type
	// on screen at all, so the line would be a tick pointing at nothing.
	if s.minimal {
		return
	}

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
