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
	// bar and the board when either is on. approachHeightPercent is its
	// height as a share of the box's width: a tenth, half the thickness of
	// the 40 percent wide, 2 to 1 panel it replaced, which the owner found
	// too heavy on the live stream.
	approachLeftPercent   = 3
	approachTopPercent    = 6
	approachWidthPercent  = 65
	approachHeightPercent = 10

	// approachReachNm is the distance at the plot's left edge and
	// approachCeilingFt the height at its top. A three-degree descent is
	// about 318 ft a nautical mile, so from 15 NM it starts at 4,780 ft, just
	// under the top.
	approachReachNm   = 15.0
	approachCeilingFt = 5000.0
	approachGlideDeg  = 3.0

	// approachTickNm is the step of the distance labels along the bottom.
	approachTickNm = 5.0

	// approachPad is the air inside the panel's sides, approachTopPad and
	// approachBottomPad the air over the heading and under the distances,
	// approachGap the air between the gutter and the plot and approachAxisGap
	// between the plot and its distances, all in glyph pixels so they grow
	// with the type. The top and bottom are tighter than the sides because
	// at a tenth of the width every glyph pixel of height is a share of the
	// plot.
	approachPad       = 6
	approachTopPad    = 4
	approachBottomPad = 3
	approachGap       = 4
	approachAxisGap   = 2

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
	// width: a sixteenth, a little under one nautical mile.
	approachGroundShare = 16

	// approachMaxDots bounds the dots kept on one frame. Fifteen nautical
	// miles of final approach holds about six aircraft at the closest spacing
	// a tower runs, so the bound only matters for a feed gone wrong; the
	// nearest are the ones kept.
	approachMaxDots = 32

	// approachFixedObstacles is room for the ground line, the heading, the
	// five axis labels and the "+N" corner in the label table, ahead of the
	// dots and their callsigns.
	approachFixedObstacles = 8

	// approachRows is how many rows over its dot a callsign may climb before
	// it gives up, and approachHeadroomRows how many of them the panel keeps
	// clear above the plot, so a dot at the ceiling still has room for its
	// name. The heading shares that row, in the gutter. approachRowGap is the
	// air between two rows, in glyph pixels. Two rows is what the thinner
	// panel holds: from a dot on the ground the second row reaches the top of
	// the plot.
	approachRows         = 2
	approachHeadroomRows = 1
	approachRowGap       = 1

	// approachSlideSteps bounds how many things a callsign steps past on one
	// row before that row is given up.
	approachSlideSteps = 16

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
	approachHeading = "INBOUND TRAFFIC"
	approachField   = "field"
	approachTopText = "5,000 ft"

	// approachMorePrefix leads the count of dots without a callsign, and
	// approachMoreWidest is the widest count room is set aside for.
	approachMorePrefix = "+"
	approachMoreWidest = "+99"
)

// approachShown reports whether the strip is on screen: the bare 3D view with
// --minimal-approach on.
func (s *Scene) approachShown() bool { return s.shown == ViewMinimal3D && s.approach }

// approachLayout is where the strip's pieces go on this frame: the panel, the
// plot inside it, the gutter left of the plot, and the type's scale.
type approachLayout struct {
	panel  image.Rectangle
	plot   image.Rectangle
	scale  int
	shadow int
	line   int
	pitch  int
	gap    int
}

// newApproachLayout fits the strip into the top left of the scope box, and
// reports false when the box is too small for its heading and a plot, or
// there is no face to set it in.
//
// The heading sits in the gutter left of the plot, on the row above it, with
// the ceiling's height under it and the "+N" at the ground. That gutter is as
// wide as the heading, which costs about a quarter of its width; the other place
// for it, a line of its own over the plot, would cost a fifth of the height,
// and height is what the panel is short of.
func (s *Scene) newApproachLayout(lay *layout) (approachLayout, bool) {
	win := newWindowLayout(lay)
	box := lay.scope
	left := box.Min.X + box.Dx()*approachLeftPercent/percent
	top := box.Min.Y + box.Dy()*approachTopPercent/percent
	right := min(left+box.Dx()*approachWidthPercent/percent, s.approachRightLimit(lay, &win))

	strip := approachLayout{
		panel:  image.Rect(left, top, right, top+box.Dx()*approachHeightPercent/percent),
		scale:  win.scale,
		shadow: win.shadow,
		gap:    win.scale * approachGap,
	}

	pen := s.windowPen(lay, &win, s.faces.Small, 0)
	heading := s.windowPen(lay, &win, s.faces.Small, windowTracking)
	strip.line = pen.height()
	strip.pitch = strip.line + win.scale*approachRowGap
	pad := win.scale * approachPad
	gutter := max(heading.measure(approachHeading), pen.measure(approachTopText), pen.measure(approachMoreWidest))

	strip.plot = image.Rect(
		strip.panel.Min.X+pad+gutter+strip.gap,
		strip.panel.Min.Y+win.scale*approachTopPad+approachHeadroomRows*strip.pitch,
		strip.panel.Max.X-pad,
		strip.panel.Max.Y-win.scale*(approachBottomPad+approachAxisGap)-strip.line,
	)

	// A plot shorter than its rows of callsigns, or narrower than its own
	// heading, has no room for a queue.
	tall := strip.plot.Dy() >= approachRows*strip.pitch
	wide := strip.plot.Dx() >= gutter

	return strip, strip.line > 0 && tall && wide
}

// approachRightLimit is the x the strip has to end at to stay clear of the
// window overlay's clock, with the overlay's usual gap, or no limit while
// the overlay is off. The clock face has fixed-width digits, so measuring
// the format itself gives the width of every time it can show.
func (s *Scene) approachRightLimit(lay *layout, win *windowLayout) int {
	if !s.windowShown() {
		return math.MaxInt
	}

	clock := s.windowPen(lay, win, s.faces.Large, 0)

	return win.box.Max.X - clock.measure(clockFormat) - win.scale*windowCountGap
}

// percent and tenths are the denominators of the layout's shares, and
// feetPerNm turns the reach into the reference line's starting height.
const (
	percent   = 100
	tenths    = 10
	feetPerNm = 6076.12
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
	heading.x, heading.y = panel.Min.X+strip.scale*approachPad, panel.Min.Y+strip.scale*approachTopPad
	s.approachText(&heading, approachHeading, s.pal.Muted)

	thin := max(1, strip.scale/approachThinDivisor)
	ground := image.Rect(plot.Min.X, plot.Max.Y-strip.shadow, plot.Max.X, plot.Max.Y+strip.shadow)
	lay.dst.BlendRect(image.Rect(plot.Min.X, plot.Max.Y, plot.Max.X, plot.Max.Y+thin), s.pal.Muted, approachBaseAlpha)
	lay.dst.BlendRect(image.Rect(plot.Max.X-plot.Dx()/approachGroundShare, ground.Min.Y, plot.Max.X, ground.Max.Y),
		s.pal.Ink, 1)

	// A callsign set across the ground line reads as struck through.
	s.approachLabels.add(ground)

	drawApproachGlide(lay, strip, arrival, thin)
	s.drawApproachAxes(lay, strip, &win)
}

// drawApproachAxes sets the distance labels under the plot and the
// ceiling's height in the gutter under the heading.
//
// The ceiling's label hangs from the top of the plot rather than being
// centred on it, which would put it half over the heading. The panel used
// to mark 2,500 ft as well; at half the height that label sat on top of
// both the ceiling's and the "+N", and the reference line already shows
// how the height falls.
func (s *Scene) drawApproachAxes(lay *layout, strip *approachLayout, win *windowLayout) {
	plot := strip.plot
	pen := s.windowPen(lay, win, s.faces.Small, 0)
	pen.y = plot.Max.Y + strip.scale*approachAxisGap

	// The labels at 15, 10 and 5 NM, far to near; the field's own closes
	// the row.
	for index, word := range [...]string{"15 NM", "10 NM", "5 NM"} {
		x := approachX(plot, approachReachNm-float64(index)*approachTickNm)

		pen.x = x - pen.measure(word)/2
		if index == 0 {
			pen.x = x
		}

		s.approachText(&pen, word, s.pal.Muted)
	}

	pen.x = plot.Max.X - pen.measure(approachField)
	s.approachText(&pen, approachField, s.pal.Muted)

	pen.x, pen.y = plot.Min.X-strip.gap-pen.measure(approachTopText), plot.Min.Y
	s.approachText(&pen, approachTopText, s.pal.Muted)
}

// drawApproachGlide draws the faint dashed line a three-degree descent to
// the field would follow, from the plot's left edge down to the field.
func drawApproachGlide(lay *layout, strip *approachLayout, ink color.RGBA, width int) {
	plot := strip.plot
	startFt := approachReachNm * feetPerNm * math.Tan(approachGlideDeg*math.Pi/halfCircle)
	fromX, fromY := float64(plot.Min.X), float64(plot.Max.Y)-startFt/approachCeilingFt*float64(plot.Dy())
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
// label table, widened by the air a callsign keeps from another, so a
// callsign slid up against the heading does not run on into it.
func (s *Scene) approachText(pen *windowPen, value string, ink color.RGBA) {
	air := pen.scale * approachLabelGap
	s.approachLabels.add(image.Rect(pen.x-air, pen.y, pen.x+pen.measure(value)+air, pen.y+pen.height()))
	pen.draw(value, ink, 1)
}

// approachX is the column a distance from the field falls on: the reach at
// the plot's left edge, the field at its right.
func approachX(plot image.Rectangle, nm float64) int {
	return plot.Max.X - int(math.Round(nm/approachReachNm*float64(plot.Dx())))
}

// approachY is the row a height falls on: the ground at the plot's bottom,
// the ceiling at its top.
func approachY(plot image.Rectangle, feet float64) int {
	return plot.Max.Y - int(math.Round(feet/approachCeilingFt*float64(plot.Dy())))
}

// approachDot is one arrival on the strip: where it is drawn, how far out it
// is, what it is called and how strongly it is drawn. callsign points into
// the frame's snapshot, so keeping it copies nothing.
type approachDot struct {
	x, y     int
	nm       float64
	callsign string
	alpha    float64
	focus    bool

	// label is where its callsign goes this frame, when labelled.
	label    approachSpot
	labelled bool
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
		if dot.nm >= d.dots[slot-1].nm {
			return
		}

		slot--
	} else {
		d.count++
	}

	for slot > 0 && d.dots[slot-1].nm > dot.nm {
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
	class, distanceNm, shown := s.fromField(*plane, field)
	if !shown || class != source.TrackInbound || distanceNm > approachReachNm {
		return approachDot{}, false
	}

	feet, known := s.captionHeight(plane.Altitude)
	if !known {
		return approachDot{}, false
	}

	alpha := 1.0
	if feet > approachCeilingFt {
		feet, alpha = approachCeilingFt, approachAboveAlpha
	}

	callsign := strings.TrimSpace(plane.Callsign)
	if callsign == "" {
		callsign = plane.ICAO
	}

	return approachDot{
		x:        approachX(strip.plot, distanceNm),
		y:        approachY(strip.plot, max(feet, 0)),
		nm:       distanceNm,
		callsign: callsign,
		alpha:    alpha,
	}, true
}

// drawApproachDots draws the dots and their callsigns. The dots go into the
// label table first, so no callsign covers another aircraft's dot; then the
// callsigns are placed over their dots in order, and drawn once all have a
// place.
//
// Every dot gets a callsign while there is room for one in any of its rows.
// The first try leaves the corner the "+N" would take open to callsigns; if
// that leaves any dot bare, the places are worked out again with the corner
// kept clear, and the furthest that still find none are counted there.
func (s *Scene) drawApproachDots(lay *layout, strip *approachLayout) {
	arrival := s.classColour(source.TrackInbound)
	dots := s.approachDots.dots[:s.approachDots.count]
	labels := &s.approachLabels

	moreSlot := labels.count
	labels.add(image.Rectangle{})
	labels.dotsFrom = labels.count

	for index := range dots {
		radius := approachRadius(strip, &dots[index])
		lay.dst.BlendCircle(dots[index].x, dots[index].y, radius, arrival, dots[index].alpha)

		labels.add(image.Rect(dots[index].x-radius, dots[index].y-radius,
			dots[index].x+radius+1, dots[index].y+radius+1))
	}

	win := windowLayout{scale: strip.scale, shadow: strip.shadow}
	pen := s.windowPen(lay, &win, s.faces.Small, 0)
	more := approachMoreBox(&pen, strip)
	placed := labels.count

	if s.placeApproachLabels(&pen, strip, dots) > 0 {
		labels.count = placed
		labels.boxes[moreSlot] = more

		s.placeApproachLabels(&pen, strip, dots)
	}

	for index := range dots {
		s.drawApproachLabel(lay, &pen, strip, &dots[index])
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

// approachSpot is the box a callsign goes in and which row that is: row 0
// sits just over the dot, and any higher row is joined to it by a leader.
type approachSpot struct {
	box image.Rectangle
	row int
}

// approachLean is where a callsign would rather sit over its dot when
// nothing is in the way: centred on it, or with its left edge on it.
type approachLean int

const (
	// leanCentre centres every callsign on its dot, staggering rows before
	// sliding anything sideways. It is what a spread-out queue gets.
	leanCentre approachLean = iota

	// leanRight puts every callsign's left edge on its dot. A tight bunch
	// needs it: a centred callsign over the nearest dot covers the spot
	// the next dot's leader has to come down through.
	leanRight

	// leanStagger leans right and also keeps a callsign on any row but the
	// top one off the next dot's column. With two rows a bunch only fits by
	// alternating: a lower callsign over the next dot shuts that dot out of
	// its own row and puts the lower row across its leader.
	leanStagger
)

// placeApproachLabels gives each dot its callsign's place and returns how
// many found none, keeping that count on the label table. It tries each
// lean in the order they are declared, stopping at the first that labels
// every dot; when none does it keeps the earliest that left the fewest bare.
func (s *Scene) placeApproachLabels(pen *windowPen, strip *approachLayout, dots []approachDot) int {
	base := s.approachLabels.count
	best, fewest := leanCentre, math.MaxInt

	for lean := leanCentre; lean <= leanStagger; lean++ {
		s.approachLabels.count = base

		bare := s.layApproachLabels(pen, strip, dots, lean)
		if bare == 0 {
			return 0
		}

		if bare < fewest {
			best, fewest = lean, bare
		}
	}

	if best != leanStagger {
		s.approachLabels.count = base
		s.layApproachLabels(pen, strip, dots, best)
	}

	return s.approachLabels.unlabelled
}

// approachRun is the state of one lay-out, worked from the field outwards.
// barriers holds, for each row, the x a callsign on that row must end left
// of: the left edge of the last callsign set on it, or a leader that comes
// down through it from a row above, whichever is further left. rightmost is
// the centre of the last callsign set, which the next must stay left of.
type approachRun struct {
	barriers  [approachRows]int
	rightmost int
	gap       int
}

// layApproachLabels places the callsigns nearest the field first, which is
// right to left, so the names keep the order of their dots and the ones
// that go without, when any must, are the furthest out. Every callsign sits
// above its dot, on the lowest row that has room, with its leader dropping
// straight down to the dot; the barriers keep any callsign off a leader
// already placed and any leader off a callsign already placed.
func (s *Scene) layApproachLabels(pen *windowPen, strip *approachLayout, dots []approachDot, lean approachLean) int {
	labels := &s.approachLabels
	labels.unlabelled = 0

	run := approachRun{rightmost: math.MaxInt, gap: strip.scale * approachLabelGap}
	for row := range run.barriers {
		run.barriers[row] = strip.panel.Max.X + run.gap
	}

	for index := range dots {
		dot := &dots[index]
		probe := approachProbe{
			strip: strip, dot: dot, width: pen.measure(dot.callsign), height: pen.height(),
			own: labels.dotsFrom + index, next: math.MinInt, stagger: lean == leanStagger,
		}

		if index+1 < len(dots) {
			probe.next = dots[index+1].x
		}

		dot.label, dot.labelled = s.findApproachSpot(&probe, &run, lean)
		if !dot.labelled {
			labels.unlabelled++

			continue
		}

		box := dot.label.box
		labels.add(box)
		run.barriers[dot.label.row] = box.Min.X
		run.rightmost = (box.Min.X + box.Max.X) / 2

		for below := range dot.label.row {
			run.barriers[below] = min(run.barriers[below], dot.x)
		}
	}

	return labels.unlabelled
}

// approachProbe is one callsign being placed: its dot, its size, where its
// own disc is in the label table, and the column of the dot placed after it,
// which a staggered lay-out keeps its lower rows off.
type approachProbe struct {
	strip         *approachLayout
	dot           *approachDot
	width, height int
	own           int
	next          int
	stagger       bool
}

// coversNext reports whether box, on row, would sit over the next dot's
// column on a row a staggered lay-out keeps clear for it.
func (p *approachProbe) coversNext(box image.Rectangle, row int) bool {
	return p.stagger && row < approachRows-1 && box.Min.X <= p.next+p.strip.scale
}

// box is the callsign's box with its left edge at x, on row.
func (p *approachProbe) box(x, row int) image.Rectangle {
	bottom := p.dot.y - approachRadius(p.strip, p.dot) - p.strip.scale*approachLabelGap - row*p.strip.pitch

	return image.Rect(x, bottom-p.height, x+p.width, bottom)
}

// findApproachSpot is the place for one callsign. It tries the preferred
// place for the lean on each row from the lowest up, so a queue staggers
// before anything slides; then on each row again the furthest right the
// barriers allow, sliding left past whatever is in the way. A callsign
// always covers its own dot's column, which bounds the slide to half its
// width either side of centre.
func (s *Scene) findApproachSpot(probe *approachProbe, run *approachRun, lean approachLean) (approachSpot, bool) {
	preferred := probe.dot.x - probe.width/2
	if lean != leanCentre {
		preferred = probe.dot.x
	}

	for row := range approachRows {
		if box := probe.box(preferred, row); s.spotFree(probe, run, box, row) {
			return approachSpot{box: box, row: row}, true
		}
	}

	for row := range approachRows {
		if spot, found := s.slideApproachSpot(probe, run, row); found {
			return spot, true
		}
	}

	return approachSpot{}, false
}

// slideApproachSpot sets a callsign on row as far right as the barriers
// allow and slides it left past anything in the label table, until it is
// free or would no longer cover its dot.
func (s *Scene) slideApproachSpot(probe *approachProbe, run *approachRun, row int) (approachSpot, bool) {
	start := min(probe.dot.x, run.limit(probe, row)-probe.width)

	for range approachSlideSteps {
		box := probe.box(start, row)
		if s.spotFree(probe, run, box, row) {
			return approachSpot{box: box, row: row}, true
		}

		blocker, hit := s.approachLabels.firstHit(box, probe.own, 0)
		if !hit {
			break
		}

		start = blocker.Min.X - run.gap - probe.width
	}

	return approachSpot{}, false
}

// limit is the x a callsign on row must end at or left of: the row's
// barrier, the panel's edge, and the order of the callsigns.
func (r *approachRun) limit(probe *approachProbe, row int) int {
	byOrder := math.MaxInt
	if r.rightmost != math.MaxInt {
		byOrder = r.rightmost - 1 + probe.width - probe.width/2
	}

	return min(r.barriers[row]-r.gap, probe.strip.panel.Max.X, byOrder)
}

// spotFree reports whether a callsign may go in box on row: inside the
// panel, over its own dot's column, left of the row's barrier and of the
// last callsign's centre, off the next dot's column when the lay-out is
// staggered, with every row under it open at the dot's column for the
// leader, and clear of everything in the label table but its own disc.
func (s *Scene) spotFree(probe *approachProbe, run *approachRun, box image.Rectangle, row int) bool {
	dotX := probe.dot.x

	if !box.In(probe.strip.panel) || box.Min.X > dotX || box.Max.X < dotX || box.Max.X > run.limit(probe, row) ||
		probe.coversNext(box, row) {
		return false
	}

	// A hairline needs only a glyph pixel of air beside a callsign.
	for below := range row {
		if run.barriers[below] < dotX+probe.strip.scale {
			return false
		}
	}

	_, hit := s.approachLabels.firstHit(box, probe.own, 0)

	return !hit
}

// drawApproachLeader draws the hairline straight down from a callsign's box
// to the top of its dot.
func (s *Scene) drawApproachLeader(lay *layout, strip *approachLayout, dot *approachDot, box image.Rectangle) {
	top := float64(dot.y - approachRadius(strip, dot))
	lay.dst.BlendLineAA(float64(dot.x), float64(box.Max.Y), float64(dot.x), top,
		max(1, strip.scale/approachThinDivisor), s.pal.Muted, approachLeaderAlpha)
}

// drawApproachLabel sets one placed callsign, with a leader down to the dot
// when it sits a row or more up. A dot with no place gets nothing.
//
// The focus is set in the palette's ink and every other callsign in its
// muted ink, so the one the caption is about stands out from the queue.
func (s *Scene) drawApproachLabel(lay *layout, pen *windowPen, strip *approachLayout, dot *approachDot) {
	if !dot.labelled {
		return
	}

	ink := s.pal.Muted
	if dot.focus {
		ink = s.pal.Ink
	}

	if dot.label.row > 0 {
		s.drawApproachLeader(lay, strip, dot, dot.label.box)
	}

	pen.x, pen.y = dot.label.box.Min.X, dot.label.box.Min.Y
	pen.draw(dot.callsign, ink, dot.alpha)
}

// approachMoreBox is where the "+N" goes: in the gutter left of the plot,
// level with the ground, which nothing else uses.
func approachMoreBox(pen *windowPen, strip *approachLayout) image.Rectangle {
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
// callsign may not overlap: the ground line, the heading, the axis labels
// and the "+N" corner, then one disc per dot from dotsFrom on, then the
// callsigns already set. unlabelled is how many dots found no room for a
// callsign this frame.
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

// firstHit is the first entry from index from on, other than skip, that box
// overlaps.
func (l *approachLabels) firstHit(box image.Rectangle, skip, from int) (image.Rectangle, bool) {
	for index := from; index < l.count; index++ {
		if index != skip && l.boxes[index].Overlaps(box) {
			return l.boxes[index], true
		}
	}

	return image.Rectangle{}, false
}
