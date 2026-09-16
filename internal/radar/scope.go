package radar

import (
	"image"
	"image/color"
	"math"
	"slices"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airports"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
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

	// receiverRadius is the same marker in minimal mode, one pixel wider.
	// The ordinary view puts the receiver dead centre where the eye already
	// is; minimal mode following the traffic puts it wherever it happens to
	// fall, on a field with no other furniture to find it against.
	receiverRadius = 5

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

	// autoPrefix opens the outer ring's label while auto range is on, so the
	// scope says for itself why it is as wide as it is. Without it the only
	// evidence was the key cap, and a range that had climbed to 180 nautical
	// miles on its own looked exactly like one somebody had typed in. It
	// carries its own trailing space, the way rangeUnit carries a leading one.
	autoPrefix = "AUTO "
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

// newProjector builds the projection for one frame around the point the scope
// is centred on.
//
// That point is the receiver in the ordinary view, and in minimal mode it is
// the traffic's own centroid as soon as --recenter has picked one. Taking a
// bare position rather than a source.Receiver is what lets the second case
// exist: the origin is a place, not a fix, and a parameter typed as a
// receiver would be claiming otherwise.
func newProjector(geom scopeGeometry, origin geo, scopeNm float64) (projector, bool) {
	if geom.rangeR <= 0 || scopeNm <= 0 || math.IsNaN(scopeNm) {
		return projector{}, false
	}

	// An origin at exactly (0, 0) is the "no position yet" state rather than a
	// buoy in the Gulf of Guinea, and uAirwaves' distance function treats it
	// the same way. Nothing can be plotted against it.
	if !positioned(origin.lat, origin.lon) {
		return projector{}, false
	}

	return projector{
		centerX: geom.centerX,
		centerY: geom.centerY,
		radius:  geom.rangeR,
		scopeNm: scopeNm,
		limitNm: scopeNm,
		lat0:    origin.lat,
		lon0:    origin.lon,
		cosLat0: math.Cos(origin.lat * math.Pi / halfCircle),
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
	if s.minimal() {
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
	proj, plottable := newProjector(geom, geo{lat: receiver.Latitude, lon: receiver.Longitude}, scopeNm)

	return scopeFrame{geom: geom, proj: proj, scopeNm: scopeNm, plottable: plottable}, true
}

// measureMinimal is the scope minimal mode projects with.
//
// The scope is the whole canvas rather than a box beside a column, so the
// centre of the frame is the centre of the picture and the range maps to half
// the short edge. What sits at that centre is the receiver with --recenter
// off and the traffic's own centre with it on, which is minimalOrigin's
// answer rather than this function's.
//
// Nothing is clipped to that radius: the cut-off is pushed out to the
// furthest corner, which is the last place a position can land on a pixel, so
// the corners show traffic the range ring would have hidden.
func (s *Scene) measureMinimal(dst *canvas.Canvas, receiver source.Receiver) (scopeFrame, bool) {
	geom, drawable := minimalGeometry(dst.Bounds())
	if !drawable {
		return scopeFrame{}, false
	}

	scopeNm := s.scopeRange.GetCurrent()

	proj, plottable := newProjector(geom, s.minimalOrigin(receiver), scopeNm)
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

	if s.minimal() {
		s.drawMinimalField(lay, view)

		return
	}

	if s.shoreOn && view.plottable {
		s.drawShore(lay.dst, view)
	}

	s.drawRings(lay, view.geom, view.scopeNm)
	s.drawCardinals(lay, view.geom)
	s.drawHome(lay, view.geom, frame.Receiver.Mode)

	if s.airports && view.plottable {
		s.drawAirports(lay, view.proj, airports.All())
	}
}

// drawMinimalField is the background minimal mode draws: its own two overlays
// and nothing else.
//
// No rings, no cardinal letters, no range labels and no home marker. The
// receiver gets a marker of its own over the traffic instead, because minimal
// mode following the traffic is not centred on it and a marker in the middle
// of the canvas would be pointing at the wrong place.
func (s *Scene) drawMinimalField(lay *layout, view scopeFrame) {
	if !view.plottable {
		return
	}

	// drawRings is what clears this in the full scope, and minimal never
	// calls it. Left over from the last full-scope render it would drop
	// airports sitting nowhere near a label this view does not draw.
	s.rangeLabelCount = 0

	if s.minimalShore {
		s.drawShore(lay.dst, view)
	}

	if s.minimalAirports {
		s.drawAirports(lay, view.proj, airports.All())
	}
}

// drawTraffic paints the part of the scope that changes every frame: the
// trails, the aircraft and the selection marker.
//
// Minimal mode picks up the receiver marker here rather than on the
// background layer, because minimal mode has no background layer: the field
// is cleared straight into the frame, and the marker moves under the
// projection like everything else that is plotted.
func (s *Scene) drawTraffic(lay *layout, frame source.Frame) {
	view, drawable := s.measureScope(lay, frame.Receiver)
	if !drawable || !view.plottable {
		return
	}

	if s.minimal() {
		s.drawReceiver(lay.dst, view.proj, frame.Receiver)
	}

	s.drawAircraft(lay, view.proj, frame)
}

// drawReceiver marks the receiver's own position on a scope that is no longer
// centred on it.
//
// The ring follows the same fix-mode rule the ordinary view's home marker
// does, with Muted standing in for Ink as the colour a known position gets.
// On an otherwise bare field a marker set in the reading colour competes with
// the aircraft, and the one thing this marker must not do is read as a
// contact.
//
// The position is projected with offset rather than at, because at reports
// only that something is outside the cut-off and this one is allowed to be:
// following the traffic can put the receiver well off the canvas, and then
// there is nothing to draw and nothing wrong.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawReceiver(dst *canvas.Canvas, proj projector, receiver source.Receiver) {
	if !positioned(receiver.Latitude, receiver.Longitude) {
		return
	}

	// No NaN guard: positioned above has already refused a NaN coordinate,
	// and every other term in the projection is finite by construction, so
	// there is nothing left here that could produce one.
	offX, offY := proj.offset(receiver.Latitude, receiver.Longitude)
	x, y := int(math.Round(offX)), int(math.Round(offY))

	// The canvas clips a shape that runs off the edge, so this only has to
	// catch the marker that is entirely outside it. Drawing one of those
	// costs a few dozen rejected Set calls and says nothing.
	marker := image.Rect(x-receiverRadius, y-receiverRadius, x+receiverRadius+1, y+receiverRadius+1)
	if !marker.Overlaps(dst.Bounds()) {
		return
	}

	dst.Circle(x, y, receiverRadius, s.fixColour(receiver.Mode, s.pal.Muted))
	dst.FillCircle(x, y, 1, s.pal.Muted)
}

// drawRings draws the boundary and the range rings, with the range written on
// each one.
func (s *Scene) drawRings(lay *layout, geom scopeGeometry, scopeNm float64) {
	lay.dst.Circle(geom.centerX, geom.centerY, geom.outer, s.pal.Rule)

	// Reset here rather than after the loop: drawRangeLabel below fills the
	// array back in as it goes, and a label dropped for want of room simply
	// leaves the array shorter than three rather than stale from last time.
	s.rangeLabelCount = 0

	spacing := geom.rangeR / ringCount

	for ring := 1; ring <= ringCount; ring++ {
		radius := geom.rangeR * ring / ringCount
		lay.dst.DashedCircle(geom.centerX, geom.centerY, radius, dashOn, dashOff, s.pal.Rule)

		// Only the outer ring carries the AUTO word. It is the one that says
		// how far the scope reaches, so it is the one the mode belongs to, and
		// three copies of the same word would be furniture rather than a
		// reading.
		prefix := ""
		if ring == ringCount && s.autoRange {
			prefix = autoPrefix
		}

		s.drawRangeLabel(lay, geom, radius, spacing, scopeNm*float64(ring)/ringCount, prefix)
	}
}

// drawRangeLabel writes one ring's range just inside it, above the three
// o'clock point so the text never sits on the line it names.
//
// A label wider than the gap to the ring inside it is dropped rather than
// drawn. Three numbers running into each other say less than no numbers at
// all, and on a small scope the rings are only a couple of dozen pixels apart.
func (s *Scene) drawRangeLabel(
	lay *layout, geom scopeGeometry, radius, spacing int, valueNm float64, prefix string,
) {
	face := s.faces.Small
	if !lay.labels || face == nil || radius <= 0 {
		return
	}

	value := s.whole(valueNm)
	unit, _ := text.Measure(face, rangeUnit)
	lead, _ := text.Measure(face, prefix)
	width := lead + measureBytes(face, value) + unit

	if width+2*rangeLabelGap > spacing {
		return
	}

	left := geom.centerX + radius - rangeLabelGap - width
	top := geom.centerY - face.Height() - rangeLabelGap

	pen := text.Draw(lay.dst, face, left, top, prefix, s.pal.Muted)
	pen = drawBytes(lay.dst, face, pen, top, value, s.pal.Muted)
	text.Draw(lay.dst, face, pen, top, rangeUnit, s.pal.Muted)

	s.recordRangeLabel(image.Rect(left, top, left+width, top+face.Height()))
}

// recordRangeLabel keeps a range label's box so drawAirports can drop a
// marker that would land on top of it instead of overlapping it, which is
// what a wide range label and an airport near the three o'clock point used
// to do. A label past the fixed three slots is dropped rather than grown
// into; ringCount never draws more than three, so a fourth would mean
// something upstream had already gone wrong.
func (s *Scene) recordRangeLabel(box image.Rectangle) {
	if s.rangeLabelCount >= len(s.rangeLabelRects) {
		return
	}

	s.rangeLabelRects[s.rangeLabelCount] = box
	s.rangeLabelCount++
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
// hidden underneath the field it is landing at, and skipped rather than drawn
// through when the label they would carry would land on the range labels
// drawRings just put down: that furniture is the scope's own, and it reads
// worse overlapped by an airfield than the airfield reads absent near the
// edge.
//
// fields is passed in rather than read from airports.All() here so a test can
// hand it a synthetic set instead of the whole embedded database.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawAirports(lay *layout, proj projector, fields []airports.Airport) {
	face := s.faces.Small

	for _, field := range fields {
		x, y, inside := proj.at(field.Latitude, field.Longitude)
		if !inside {
			continue
		}

		box := s.airportBox(x, y, lay.labels, face, field.ICAO)
		if s.overlapsRangeLabel(box) {
			continue
		}

		lay.dst.Rect(image.Rect(x-airportHalf, y-airportHalf, x+airportHalf+1, y+airportHalf+1), s.pal.Rule)

		if lay.labels && face != nil {
			text.Draw(lay.dst, face, x+airportLabelGap, y-face.Height()/2, field.ICAO, s.pal.Muted)
		}
	}
}

// airportBox is the rectangle an airport's marker occupies, extended to
// include its ICAO label when one would be drawn beside it. It is what gets
// checked against the range labels, so an airport is only dropped when the
// part of it that would actually collide is under threat.
//
//nolint:varnamelen,revive // x, y is uScope's pixel idiom; labels picks which of two boxes to measure, not a mode.
func (s *Scene) airportBox(x, y int, labels bool, face *psf.Font, icao string) image.Rectangle {
	box := image.Rect(x-airportHalf, y-airportHalf, x+airportHalf+1, y+airportHalf+1)
	if !labels || face == nil {
		return box
	}

	width, height := text.Measure(face, icao)
	top := y - face.Height()/2
	label := image.Rect(x+airportLabelGap, top, x+airportLabelGap+width, top+height)

	return box.Union(label)
}

// overlapsRangeLabel reports whether box lands on one of the range labels
// drawRings recorded for this render of the background layer.
func (s *Scene) overlapsRangeLabel(box image.Rectangle) bool {
	return slices.ContainsFunc(s.rangeLabelRects[:s.rangeLabelCount], box.Overlaps)
}

// drawAircraft paints every aircraft that is inside the range: the ghosts of
// the ones that have gone, then the trails of the ones still flying, then the
// silhouettes on top of them, then the selection marker on top of everything.
//
// Ghosts go under the live trails so an aeroplane is never hidden by the
// track of one that is no longer there. They are drawn only while trails are
// drawn at all: a ghost is a trail, so the t key that turns trails off has to
// take them with it, or the key would be lying about what it does.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawAircraft(lay *layout, proj projector, frame source.Frame) {
	if s.trails {
		for _, ghost := range frame.Ghosts {
			s.drawGhost(lay.dst, proj, ghost)
		}

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

		// Minimal is the traffic and nothing else, the selection included.
		// The ring used to survive so n and p could show they had done
		// something, but minimal sets no type on screen at all, so the only
		// thing the ring could refer to was a panel that is not there.
		if !s.minimal() && plane.ICAO == s.selICAO {
			s.drawSelection(lay, x, y, plane)
		}
	}
}

// drawTrail draws one aircraft's history as a polyline.
func (s *Scene) drawTrail(dst *canvas.Canvas, proj projector, plane airplane.Snapshot) {
	s.drawPath(dst, proj, plane.PositionHistory, s.aircraftColour(plane), s.trailFloor())
}

// drawGhost draws the track of an aircraft that has stopped transmitting.
//
// It is the same polyline as a live trail, at full strength the whole way
// along and with nothing at the head of it: no silhouette, no label, no
// selection ring. There is no aeroplane to mark and no heading to turn one
// to, and a marker on the end of a ghost would read as a contact.
//
// The fade is never applied, whatever --no-decay says. A ghost is only ever
// drawn because --no-decay is on, and a track with no aircraft on it has no
// head for a fade to point at.
func (s *Scene) drawGhost(dst *canvas.Canvas, proj projector, ghost source.Trail) {
	s.drawPath(dst, proj, ghost.Points, s.ghostColour(ghost), trailMaxAlpha)
}

// drawPath draws a run of fixes as a polyline, oldest to newest, brightening
// from floor at the tail to full strength at the head.
//
// A segment with either end outside the range is dropped rather than clipped.
// Clipping would be more correct, but a dropped segment leaves a gap at the
// edge of the scope where a clipped one would draw a line to a place the
// aircraft never was.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawPath(
	dst *canvas.Canvas, proj projector, fixes []airplane.PositionEntry, col color.RGBA, floor float64,
) {
	if len(fixes) < 2 {
		return
	}

	span := float64(len(fixes) - 1)

	prevX, prevY, prevInside := proj.at(fixes[0].Latitude, fixes[0].Longitude)

	for index := 1; index < len(fixes); index++ {
		x, y, inside := proj.at(fixes[index].Latitude, fixes[index].Longitude)

		if inside && prevInside {
			dst.LineAA(float64(prevX), float64(prevY), float64(x), float64(y),
				s.fade(col, segmentAlpha(floor, index, span)))
		}

		prevX, prevY, prevInside = x, y, inside
	}
}

// trailFloor is how strongly the oldest segment of a live trail is drawn.
//
// With --no-decay it is full strength, so the whole track reads as one line
// rather than as a line that arrives from nowhere. The fade is the default
// because on a busy field it says which end of a track is the aeroplane;
// turning it off is for looking at the shapes the traffic makes, where an
// even line is easier to follow across the scope.
func (s *Scene) trailFloor() float64 {
	if s.noDecay {
		return trailMaxAlpha
	}

	return trailMinAlpha
}

// segmentAlpha is how strongly one segment is drawn, where 1 is the
// aircraft's own colour and 0 is the field. The head is always the colour
// itself; floor is where the tail starts from.
func segmentAlpha(floor float64, index int, span float64) float64 {
	return floor + (trailMaxAlpha-floor)*float64(index)/span
}

// drawContact draws one aircraft.
//
// A negative heading means nobody has decoded one yet. uAirwaves starts an
// Airplane at airplane's own defaultHeading of -1 and leaves it there until a
// velocity message arrives, which is the sentinel this reads: zero is due
// north and gets a silhouette pointing up the screen. An aircraft without a
// heading is a bare circle, because a silhouette would be claiming to know
// which way it is pointing.
func (s *Scene) drawContact(dst *canvas.Canvas, x, y int, plane airplane.Snapshot) { //nolint:varnamelen // pixels.
	col := s.aircraftColour(plane)

	if !knownHeading(plane.Heading) {
		dst.Circle(x, y, noHeadingRadius, col)

		return
	}

	s.icon.Draw(dst, x, y, plane.Heading, col)
}

// knownHeading reports whether a heading was decoded.
//
// It is one function rather than a comparison at each call site because the
// scope and the panel have to agree: a silhouette pointing north beside a
// panel reading TRACK --- would be the same aircraft contradicting itself on
// one screen. NaN counts as unknown for the same reason every other formatter
// here guards against it: the figure came off the air and is never trusted.
func knownHeading(heading float64) bool {
	return !math.IsNaN(heading) && heading >= 0
}

// drawSelection rings the selected aircraft and labels it on a leader line,
// so the panel on the right and the dot on the scope are obviously the same
// aeroplane. Minimal mode never calls it.
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
