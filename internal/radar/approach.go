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
	// 2.2 to 1, tall enough for a queue bunched near the ground to stack its
	// callsigns in rows above it.
	approachAspectTenths = 22

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

	// approachFixedObstacles is room for the heading, the six axis labels and
	// the "+N" corner in the label table, ahead of the dots and their
	// callsigns.
	approachFixedObstacles = 8

	// approachRows is how many rows above the dot, and as many below, a
	// callsign may move out to before it gives up; approachRowGap is the air
	// between two rows, in glyph pixels.
	approachRows   = 4
	approachRowGap = 1

	// approachThinDivisor turns the glyph scale into the width of the
	// strip's hairlines: one pixel up to 1440p, two at 2160p.
	approachThinDivisor = 3
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
	approachLeaderAlpha = 0.7
)

// The strip's words. Distance is to the field rather than to a runway,
// because the scope does not know which runway is in use.
const (
	approachHeading = "COMING IN, SEEN FROM THE SIDE"
	approachField   = "field"
	approachLowText = "750 m"
	approachTopText = "1,500 m"

	// approachMorePrefix leads the count of dots without a callsign, and
	// approachMoreWidest is the widest count room is set aside for.
	approachMorePrefix = "+"
	approachMoreWidest = "+99"
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

	thin := max(1, strip.scale/approachThinDivisor)
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
// focus takes its label and the rest take theirs nearest the field first.
// Every dot gets a callsign while there is room for one in any of its rows;
// the furthest out that find none are counted in a "+N" at the left end of
// the plot instead.
func (s *Scene) drawApproachDots(lay *layout, strip *approachLayout) {
	arrival := s.classColour(source.TrackInbound)
	dots := s.approachDots.dots[:s.approachDots.count]

	// The corner the "+N" goes in is kept clear before any callsign is set,
	// because how many go without is only known once they all have tried.
	more := s.approachMoreBox(lay, strip)
	s.approachLabels.add(more)
	s.approachLabels.dotsFrom = s.approachLabels.count

	for index := range dots {
		radius := approachRadius(strip, &dots[index])
		lay.dst.BlendCircle(dots[index].x, dots[index].y, radius, arrival, dots[index].alpha)
		s.approachLabels.add(image.Rect(dots[index].x-radius, dots[index].y-radius,
			dots[index].x+radius+1, dots[index].y+radius+1))
	}

	win := windowLayout{scale: strip.scale, shadow: strip.shadow}
	pen := s.windowPen(lay, &win, s.faces.Small, 0)
	labels := &s.approachLabels
	labels.unlabelled = 0

	for pass := range 2 {
		for index := range dots {
			if dots[index].focus == (pass == 0) && !s.labelApproachDot(lay, &pen, strip, &dots[index], index) {
				labels.unlabelled++
			}
		}
	}

	if labels.unlabelled > 0 {
		s.drawApproachMore(&pen, more, labels.unlabelled)
	}
}

// approachRadius is how large a dot is drawn.
func approachRadius(strip *approachLayout, dot *approachDot) int {
	if dot.focus {
		return strip.scale * approachFocusRadius
	}

	return strip.scale * approachDotRadius
}

// approachSpot is where a callsign goes and which row that is: row 0 sits
// beside the dot, and any further row is joined to it by a leader.
type approachSpot struct {
	at  image.Point
	row int
}

// labelApproachDot sets one dot's callsign in the first free spot
// findApproachSpot finds, with a leader back to the dot when it had to move
// a row or more away, and reports false when there is no free spot.
//
// The focus is set in the palette's ink and every other callsign in its
// muted ink, so the one the caption is about stands out from the queue.
func (s *Scene) labelApproachDot(
	lay *layout, pen *windowPen, strip *approachLayout, dot *approachDot, index int,
) bool {
	width, height := pen.measure(dot.callsign), pen.height()

	spot, found := s.findApproachSpot(strip, dot, width, height, s.approachLabels.dotsFrom+index)
	if !found {
		return false
	}

	ink := s.pal.Muted
	if dot.focus {
		ink = s.pal.Ink
	}

	box := image.Rect(spot.at.X, spot.at.Y, spot.at.X+width, spot.at.Y+height)
	if spot.row > 0 {
		s.drawApproachLeader(lay, strip, dot, box)
	}

	s.approachLabels.add(box)

	pen.x, pen.y = spot.at.X, spot.at.Y
	pen.draw(dot.callsign, ink, dot.alpha)

	return true
}

// findApproachSpot is the first spot for a callsign width by height that
// stays in the panel and clears every entry in the label table but own, the
// dot's own disc. It tries row 0 above the dot, then row 0 below it, then
// each further row above and below in turn, out to approachRows. On each row
// it tries the right of the dot before the left, because the queue runs down
// to the right and that side of a dot above the line is most often free.
func (s *Scene) findApproachSpot(
	strip *approachLayout, dot *approachDot, width, height, own int,
) (approachSpot, bool) {
	gap := approachRadius(strip, dot) + strip.scale*approachLabelGap
	pitch := height + strip.scale*approachRowGap
	sides := [...]int{dot.x + gap, dot.x - gap - width}

	for step := range 2 * approachRows {
		row := step / 2

		top := dot.y - height - row*pitch
		if step%2 == 1 {
			top = dot.y + row*pitch
		}

		for _, left := range sides {
			box := image.Rect(left, top, left+width, top+height)
			if box.In(strip.panel) && !s.approachLabels.hits(box, own) {
				return approachSpot{at: box.Min, row: row}, true
			}
		}
	}

	return approachSpot{}, false
}

// drawApproachLeader draws a hairline from the dot to the corner of its
// callsign's box that faces it.
func (s *Scene) drawApproachLeader(lay *layout, strip *approachLayout, dot *approachDot, box image.Rectangle) {
	endX, endY := box.Min.X, box.Max.Y
	if box.Min.X < dot.x {
		endX = box.Max.X
	}

	if box.Min.Y > dot.y {
		endY = box.Min.Y
	}

	lay.dst.BlendLineAA(float64(dot.x), float64(dot.y), float64(endX), float64(endY),
		max(1, strip.scale/approachThinDivisor), s.pal.Muted, approachLeaderAlpha)
}

// approachMoreBox is where the "+N" goes: in the gutter left of the plot,
// level with the ground, which nothing else uses.
func (s *Scene) approachMoreBox(lay *layout, strip *approachLayout) image.Rectangle {
	win := windowLayout{scale: strip.scale, shadow: strip.shadow}
	pen := s.windowPen(lay, &win, s.faces.Small, 0)
	right := strip.plot.Min.X - strip.gap
	top := strip.plot.Max.Y - strip.line

	return image.Rect(right-pen.measure(approachMoreWidest), top, right, top+strip.line)
}

// drawApproachMore sets "+N" right-aligned in box: how many dots went
// without a callsign.
func (s *Scene) drawApproachMore(pen *windowPen, box image.Rectangle, count int) {
	figure := s.count(count)
	pen.x = box.Max.X - pen.measure(approachMorePrefix) - pen.measureBytes(figure)
	pen.y = box.Min.Y
	pen.draw(approachMorePrefix, s.pal.Muted, 1)
	pen.drawBytes(figure, s.pal.Muted, 1)
}

// approachLabels is the places taken on the strip this frame, which a
// callsign may not overlap: the heading, the axis labels and the "+N"
// corner, then one disc per dot from dotsFrom on, then the callsigns already
// set. unlabelled is how many dots found no room for a callsign this frame.
type approachLabels struct {
	boxes      [approachFixedObstacles + 2*approachMaxDots]image.Rectangle
	count      int
	dotsFrom   int
	unlabelled int
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
