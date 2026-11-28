package radar

import (
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
)

// The approach strip: a see-through side view in the top-left corner of the
// bare 3D picture, one dot per arriving aircraft by its distance from the
// corridor airfield and its height. The caption names one arrival; the strip
// shows the whole queue behind it.
//
// Like the window overlay it is drawn on every frame after the picture and
// reads nothing the picture is drawn from, so it stays out of layerKey: the
// bare 3D view has no background layer for it to go stale in. Its type is
// sized by the overlay's rule, a glyph scale per 360 rows.
const (
	// approachLeftPercent, approachTopPercent and approachWidthPercent place
	// the panel as shares of the scope box, which is the picture less the
	// bar and the board when either is on.
	approachLeftPercent  = 3
	approachTopPercent   = 6
	approachWidthPercent = 40

	// approachAspectTenths is the panel's width over its height, in tenths:
	// 2.6 to 1.
	approachAspectTenths = 26

	// approachReachKm is the distance at the plot's left edge and
	// approachCeilingM the height at its top. A three-degree descent from
	// 30 km passes 1,572 m, so the reference line starts just under the top.
	approachReachKm  = 30.0
	approachCeilingM = 1600.0
	approachGlideDeg = 3.0

	// approachLowM and approachHighM are the heights the two quiet labels on
	// the left stand at; approachTickKm is the step of the labels along the
	// bottom.
	approachLowM   = 750.0
	approachHighM  = 1500.0
	approachTickKm = 10.0

	// approachPad is the air inside the panel's edge and approachGap the air
	// between its rows, in glyph pixels so both grow with the type.
	approachPad = 6
	approachGap = 4

	// approachDotRadius and approachFocusRadius are the dots' radii in glyph pixels:
	// the aircraft the caption is on is drawn half again as large.
	approachDotRadius   = 2
	approachFocusRadius = 3

	// approachDash and approachDashGap are the reference line's dashes, in
	// glyph pixels.
	approachDash    = 4
	approachDashGap = 3

	// approachLabelGap is the air between a dot and its callsign, in glyph
	// pixels.
	approachLabelGap = 2

	// approachGroundShare is the ground mark's length as a share of the plot
	// width: a sixteenth, a little under two kilometres.
	approachGroundShare = 16

	// approachMaxDots bounds the dots kept on one frame. Thirty kilometres of
	// final approach holds about seven aircraft at the closest spacing a
	// tower runs, so the bound only matters for a feed gone wrong; the
	// nearest are the ones kept.
	approachMaxDots = 32

	// approachFixedObstacles is room for the heading and the six axis labels
	// in the label table, ahead of the dots and their callsigns.
	approachFixedObstacles = 8
)

// How see-through the strip's pieces are, from zero to one. The panel is a
// third of the field colour on a dark palette, enough to settle the type but
// not to hide the map and the tubes under it. A light palette's field is
// close to its map already, so it takes more of it to make a ground the dark
// type reads on.
const (
	approachDarkPanel   = 0.34
	approachLightPanel  = 0.55
	approachRuleAlpha   = 0.7
	approachBaseAlpha   = 0.6
	approachGlideAlpha  = 0.55
	approachAboveAlpha  = 0.45
	approachOthersAlpha = 0.8
)

// The strip's words. Distance is to the field rather than to a runway,
// because the scope does not know which runway is in use.
const (
	approachHeading = "COMING IN, SEEN FROM THE SIDE"
	approachField   = "field"
	approachLowText = "750 m"
	approachTopText = "1,500 m"
)

// approachShown reports whether the strip is on screen: the bare 3D view with
// --minimal-approach on.
func (s *Scene) approachShown() bool { return s.shown == ViewMinimal3D && s.approach }

// approachLayout is where the strip's pieces go on this frame: the panel, the
// plot inside it, and the type's scale.
type approachLayout struct {
	panel  image.Rectangle
	plot   image.Rectangle
	scale  int
	shadow int
	line   int
	gap    int
}

// newApproachLayout fits the strip into the top left of the scope box, and
// reports false when the box is too narrow for its heading or there is no
// face to set it in.
func (s *Scene) newApproachLayout(lay *layout) (approachLayout, bool) {
	win := newWindowLayout(lay)
	box := lay.scope
	left := box.Min.X + box.Dx()*approachLeftPercent/percent
	top := box.Min.Y + box.Dy()*approachTopPercent/percent
	width := box.Dx() * approachWidthPercent / percent

	strip := approachLayout{
		panel:  image.Rect(left, top, left+width, top+width*tenths/approachAspectTenths),
		scale:  win.scale,
		shadow: win.shadow,
		gap:    win.scale * approachGap,
	}

	pen := s.windowPen(lay, &win, s.faces.Small, 0)
	strip.line = pen.height()
	pad := win.scale * approachPad
	gutter := pen.measure(approachTopText) + strip.gap

	strip.plot = image.Rect(
		strip.panel.Min.X+pad+gutter, strip.panel.Min.Y+pad+strip.line+strip.gap+strip.line/2,
		strip.panel.Max.X-pad, strip.panel.Max.Y-pad-strip.line-strip.gap,
	)

	heading := s.windowPen(lay, &win, s.faces.Small, windowTracking)
	fits := heading.measure(approachHeading) <= strip.panel.Dx()-2*pad

	// A panel wide enough for the heading is always tall enough for a plot:
	// the heading needs about 540 glyph pixels of width, which gives the
	// panel some 80 of height against the 50 its rows and padding take.
	return strip, strip.line > 0 && fits
}

// percent and tenths are the denominators of the layout's shares, and
// metresPerKm turns the reach into the reference line's starting height.
const (
	percent     = 100
	tenths      = 10
	metresPerKm = 1000
)

// drawApproach draws the strip over the picture, or nothing while it is off.
func (s *Scene) drawApproach(lay *layout, frame source.Frame) {
	if !s.approachShown() {
		return
	}

	strip, ok := s.newApproachLayout(lay)
	if !ok {
		return
	}

	s.approachLabels.count = 0
	s.drawApproachPanel(lay, &strip)
	s.collectApproach(frame, &strip)
	s.drawApproachDots(lay, &strip)
}

// drawApproachPanel draws everything that does not move: the wash, the line
// along the top, the heading, the axes and their labels, the ground at the
// field and the reference descent. Every label it sets is entered in the
// label table, so a callsign never lands on one.
func (s *Scene) drawApproachPanel(lay *layout, strip *approachLayout) {
	panelAlpha := approachDarkPanel
	if s.light {
		panelAlpha = approachLightPanel
	}

	arrival := s.classColour(source.TrackInbound)
	panel, plot := strip.panel, strip.plot

	lay.dst.BlendRect(panel, s.pal.Field, panelAlpha)
	lay.dst.BlendRect(image.Rect(panel.Min.X, panel.Min.Y, panel.Max.X, panel.Min.Y+strip.shadow), arrival,
		approachRuleAlpha)

	win := windowLayout{scale: strip.scale, shadow: strip.shadow}
	heading := s.windowPen(lay, &win, s.faces.Small, windowTracking)
	heading.x, heading.y = panel.Min.X+strip.scale*approachPad, panel.Min.Y+strip.scale*approachPad
	s.approachText(&heading, approachHeading, s.pal.Muted)

	thin := max(1, strip.scale/3)
	lay.dst.BlendRect(image.Rect(plot.Min.X, plot.Max.Y, plot.Max.X, plot.Max.Y+thin), s.pal.Muted, approachBaseAlpha)
	lay.dst.BlendRect(image.Rect(plot.Max.X-plot.Dx()/approachGroundShare, plot.Max.Y-strip.shadow,
		plot.Max.X, plot.Max.Y+strip.shadow), s.pal.Ink, 1)

	drawApproachGlide(lay, strip, arrival, thin)
	s.drawApproachAxes(lay, strip, &win)
}

// drawApproachAxes sets the distance labels under the plot and the two
// height labels left of it.
func (s *Scene) drawApproachAxes(lay *layout, strip *approachLayout, win *windowLayout) {
	plot := strip.plot
	pen := s.windowPen(lay, win, s.faces.Small, 0)
	pen.y = plot.Max.Y + strip.gap

	// The labels at 30, 20 and 10 km, far to near; the field's own closes
	// the row.
	for index, word := range [...]string{"30 km", "20 km", "10 km"} {
		x := approachX(plot, approachReachKm-float64(index)*approachTickKm)

		pen.x = x - pen.measure(word)/2
		if index == 0 {
			pen.x = x
		}

		s.approachText(&pen, word, s.pal.Muted)
	}

	pen.x = plot.Max.X - pen.measure(approachField)
	s.approachText(&pen, approachField, s.pal.Muted)

	for _, mark := range [...]struct {
		metres float64
		word   string
	}{{approachLowM, approachLowText}, {approachHighM, approachTopText}} {
		pen.x = plot.Min.X - strip.gap - pen.measure(mark.word)
		pen.y = approachY(plot, mark.metres) - strip.line/2
		s.approachText(&pen, mark.word, s.pal.Muted)
	}
}

// drawApproachGlide draws the faint dashed line a three-degree descent to
// the field would follow, from the plot's left edge down to the field.
func drawApproachGlide(lay *layout, strip *approachLayout, ink color.RGBA, width int) {
	plot := strip.plot
	startM := approachReachKm * metresPerKm * math.Tan(approachGlideDeg*math.Pi/halfCircle)
	fromX, fromY := float64(plot.Min.X), float64(plot.Max.Y)-startM/approachCeilingM*float64(plot.Dy())
	runX, runY := float64(plot.Max.X)-fromX, float64(plot.Max.Y)-fromY

	length := math.Hypot(runX, runY)
	dash, step := float64(strip.scale*approachDash), float64(strip.scale*(approachDash+approachDashGap))

	for along := 0.0; along < length; along += step {
		end := min(along+dash, length)
		lay.dst.BlendLineAA(
			fromX+runX*along/length, fromY+runY*along/length,
			fromX+runX*end/length, fromY+runY*end/length,
			width, ink, approachGlideAlpha,
		)
	}
}

// approachText sets value at the pen and enters the place it took in the
// label table.
func (s *Scene) approachText(pen *windowPen, value string, ink color.RGBA) {
	s.approachLabels.add(image.Rect(pen.x, pen.y, pen.x+pen.measure(value), pen.y+pen.height()))
	pen.draw(value, ink, 1)
}

// approachX is the column a distance from the field falls on: the reach at
// the plot's left edge, the field at its right.
func approachX(plot image.Rectangle, km float64) int {
	return plot.Max.X - int(math.Round(km/approachReachKm*float64(plot.Dx())))
}

// approachY is the row a height falls on: the ground at the plot's bottom,
// the ceiling at its top.
func approachY(plot image.Rectangle, metres float64) int {
	return plot.Max.Y - int(math.Round(metres/approachCeilingM*float64(plot.Dy())))
}

// approachDot is one arrival on the strip: where it is drawn, how far out it
// is, what it is called and how strongly it is drawn. callsign points into
// the frame's snapshot, so keeping it copies nothing.
type approachDot struct {
	x, y     int
	km       float64
	callsign string
	alpha    float64
	focus    bool
}

// approachDots is the frame's dots, nearest the field first.
type approachDots struct {
	dots  [approachMaxDots]approachDot
	count int
}

// add puts dot in its place by distance. With the table full a dot further
// out than every kept one is dropped, and otherwise the furthest kept one
// makes room for it.
func (d *approachDots) add(dot approachDot) {
	slot := d.count
	if slot == len(d.dots) {
		if dot.km >= d.dots[slot-1].km {
			return
		}

		slot--
	} else {
		d.count++
	}

	for slot > 0 && d.dots[slot-1].km > dot.km {
		d.dots[slot] = d.dots[slot-1]
		slot--
	}

	d.dots[slot] = dot
}

// collectApproach fills the dot table from the frame: every arrival the
// caption could name, out to the strip's reach, with a known height.
func (s *Scene) collectApproach(frame source.Frame, strip *approachLayout) {
	s.approachDots.count = 0

	if !frame.Corridors.Placed() {
		return
	}

	focus := s.approachFocus(frame)

	for index := range frame.Planes {
		dot, ok := s.approachDotFor(&frame.Planes[index], frame.Corridors, strip)
		if !ok {
			continue
		}

		dot.focus = focus != "" && frame.Planes[index].ICAO == focus
		s.approachDots.add(dot)
	}
}

// approachFocus is the address of the aircraft the strip draws large: the
// one the caption is on while the window overlay shows an arrival, and with
// the overlay off the one it would pick. Empty is none.
func (s *Scene) approachFocus(frame source.Frame) string {
	if s.windowShown() {
		if s.caption.class == source.TrackInbound {
			return s.caption.icao
		}

		return ""
	}

	pick := s.captionCandidate(frame)
	if pick.found && pick.class == source.TrackInbound {
		return pick.plane.ICAO
	}

	return ""
}

// approachDotFor places one aircraft on the strip, or reports false when it
// does not belong there. An aircraft above the ceiling is pinned to the top
// edge and drawn fainter, so it is seen to be coming without being drawn at
// a height it is not at.
func (s *Scene) approachDotFor(plane *airplane.Snapshot, field source.Corridors, strip *approachLayout) (
	approachDot, bool,
) {
	class, distanceKm, shown := s.fromField(*plane, field)
	if !shown || class != source.TrackInbound || distanceKm > approachReachKm {
		return approachDot{}, false
	}

	metres, known := s.overlayHeight(*plane, field.Field)
	if !known {
		return approachDot{}, false
	}

	alpha := 1.0
	if metres > approachCeilingM {
		metres, alpha = approachCeilingM, approachAboveAlpha
	}

	callsign := strings.TrimSpace(plane.Callsign)
	if callsign == "" {
		callsign = plane.ICAO
	}

	return approachDot{
		x:        approachX(strip.plot, distanceKm),
		y:        approachY(strip.plot, max(metres, 0)),
		km:       distanceKm,
		callsign: callsign,
		alpha:    alpha,
	}, true
}

// drawApproachDots draws the dots and their callsigns. The dots go into the
// label table first, so no callsign covers another aircraft's dot; then the
// focus takes its label, which it always gets, and the rest take theirs
// nearest the field first, each only where it fits.
func (s *Scene) drawApproachDots(lay *layout, strip *approachLayout) {
	arrival := s.classColour(source.TrackInbound)
	dots := s.approachDots.dots[:s.approachDots.count]
	s.approachLabels.dotsFrom = s.approachLabels.count

	for index := range dots {
		radius := approachRadius(strip, &dots[index])
		lay.dst.BlendCircle(dots[index].x, dots[index].y, radius, arrival, dots[index].alpha)

		// The disc is entered with a margin, so a callsign set beside
		// another aircraft's dot does not read as that aircraft's.
		margin := radius + strip.scale*approachLabelGap
		s.approachLabels.add(image.Rect(dots[index].x-margin, dots[index].y-margin,
			dots[index].x+margin+1, dots[index].y+margin+1))
	}

	win := windowLayout{scale: strip.scale, shadow: strip.shadow}
	pen := s.windowPen(lay, &win, s.faces.Small, 0)

	for index := range dots {
		if dots[index].focus {
			s.labelApproachDot(&pen, strip, &dots[index], index)
		}
	}

	for index := range dots {
		if !dots[index].focus {
			s.labelApproachDot(&pen, strip, &dots[index], index)
		}
	}
}

// approachRadius is how large a dot is drawn.
func approachRadius(strip *approachLayout, dot *approachDot) int {
	if dot.focus {
		return strip.scale * approachFocusRadius
	}

	return strip.scale * approachDotRadius
}

// labelApproachDot sets one dot's callsign in the first of its places that
// stays in the panel and clears every entry in the label table but the dot's
// own. Any dot but the focus tries only the two places above the line: a
// callsign under the line sits over the dots further down the queue and
// reads as theirs. The focus tries them all, and when it finds no free place is centred over its dot anyway,
// pulled inside the panel, which never covers the dot itself; any other dot
// goes without.
func (s *Scene) labelApproachDot(pen *windowPen, strip *approachLayout, dot *approachDot, index int) {
	width, height := pen.measure(dot.callsign), pen.height()
	places := approachPlaces(dot, width, height, approachRadius(strip, dot)+strip.scale*approachLabelGap)
	own := s.approachLabels.dotsFrom + index

	place, found := places[approachForcedPlace], false

	tried := places[:approachBesidePlaces]
	if dot.focus {
		tried = places[:]
	}

	for _, at := range tried {
		box := image.Rect(at.X, at.Y, at.X+width, at.Y+height)
		if box.In(strip.panel) && !s.approachLabels.hits(box, own) {
			place, found = at, true

			break
		}
	}

	if !found && !dot.focus {
		return
	}

	if !found {
		place.X = min(max(place.X, strip.panel.Min.X), strip.panel.Max.X-width)
		place.Y = min(max(place.Y, strip.panel.Min.Y), strip.panel.Max.Y-height)
	}

	alpha := dot.alpha
	if !dot.focus {
		alpha *= approachOthersAlpha
	}

	pen.x, pen.y = place.X, place.Y
	s.approachLabels.add(image.Rect(place.X, place.Y, place.X+width, place.Y+height))
	pen.draw(dot.callsign, s.pal.Ink, alpha)
}

// approachBesidePlaces is how many of approachPlaces lie beside the dot
// above the line, and approachForcedPlace the one the focus takes when none
// is free: centred over its dot.
const (
	approachBesidePlaces = 2
	approachForcedPlace  = 4
)

// approachPlaces is where a callsign width by height may go around its dot,
// gap clear of it, in the order they are tried: beside it above the line to
// the right and then the left, beside it below the line, then centred over
// it and centred under it. Beside comes first because the queue runs
// diagonally down to the right, so the air beside a dot is the most likely
// to be free.
func approachPlaces(dot *approachDot, width, height, gap int) [6]image.Point {
	right, left, centre := dot.x+gap, dot.x-gap-width, dot.x-width/2

	return [...]image.Point{
		{X: right, Y: dot.y - height},
		{X: left, Y: dot.y - height},
		{X: right, Y: dot.y},
		{X: left, Y: dot.y},
		{X: centre, Y: dot.y - gap - height},
		{X: centre, Y: dot.y + gap},
	}
}

// approachLabels is the places taken on the strip this frame, which a
// callsign may not overlap: the heading and the axis labels, then one disc
// per dot from dotsFrom on, then the callsigns already set.
type approachLabels struct {
	boxes    [approachFixedObstacles + 2*approachMaxDots]image.Rectangle
	count    int
	dotsFrom int
}

// add enters box in the table. The table is sized for every entry a full
// frame can make, so nothing is ever turned away.
func (l *approachLabels) add(box image.Rectangle) {
	if l.count < len(l.boxes) {
		l.boxes[l.count] = box
		l.count++
	}
}

// hits reports whether box overlaps any entry but the one at skip.
func (l *approachLabels) hits(box image.Rectangle, skip int) bool {
	for index := range l.count {
		if index != skip && l.boxes[index].Overlaps(box) {
			return true
		}
	}

	return false
}
