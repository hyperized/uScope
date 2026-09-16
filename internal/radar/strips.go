package radar

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// The flight strips' spacing, in pixels at the panel's resolution.
//
// The column used to hold a card about the selected flight and a table of every
// other aircraft under it, which meant two grammars for one list: a panel with
// its own layout, and rows repeating three of its figures somewhere else. A
// strip board has one grammar. Every aircraft is a strip, the selected one is a
// full strip and the rest are half strips, and a field is in the same place on
// all of them.
const (
	// stripEdge is the accent bar down the left of the selected strip. Every
	// strip leaves room for it, so the fields line up whether or not the strip
	// they are on is the selected one.
	stripEdge = 3

	// stripPadX is the air between a field's hairline rule and its type, and
	// stripPadY the air above and below the three rows of the selected strip.
	stripPadX = 8
	stripPadY = 4

	// stripHalfPadY is the air above and below the single line a half strip
	// carries.
	//
	// It is wider than the selected strip's padding on purpose. A half strip is
	// one line of type and nothing else, and at four pixels it read as a table
	// row again, which is the thing the strips replaced. Seven puts a half
	// strip at thirty pixels against the selected strip's sixty-four at the
	// panel's faces, so the two are obviously a whole and a half of one object.
	stripHalfPadY = 7

	// stripGap is the air between two fields, with the hairline rule that
	// separates them halfway across it.
	stripGap = 8

	// stripLift is how far the selected strip's fill sits from the field
	// towards the softkeys' grey. Two fifths is enough to read as a card lifted
	// off the page under type that has to stay legible, without competing with
	// the accent edge down its left.
	stripLift = 0.40

	// The "+N MORE" line that closes a board longer than the column. N counts
	// every aircraft with no strip, so the strips plus that number always add
	// up to what the filter is showing.
	morePrefix = "+"
	moreSuffix = " MORE"

	// stripsCountSuffix follows the aircraft count on the line above the
	// strips, and stripsOfInfix joins the two numbers it carries while a filter
	// is on: how many aircraft have strips, then how many are on the field.
	// Without the second number a short board reads as a quiet sky rather than
	// as a filter doing its job.
	stripsCountSuffix = " AIRCRAFT"
	stripsOfInfix     = " OF "

	// attCell is the room the little aeroplane at the right end of the ident
	// field takes. It is the model's fourteen-pixel span plus the room a wing
	// needs to swing into as an aircraft banks through a turn.
	attCell = 24

	// rateUnit follows the climb or descent figure on the selected strip. It is
	// the aviation shorthand rather than FT/MIN because the field's own header
	// already says the level is in feet, and the long form cost the field
	// eighteen pixels of width it could not spare.
	rateUnit = " FPM"

	// emergencyText is what the warning box says, and the two paddings are the
	// air inside it.
	emergencyText = "EMERGENCY"
	emergencyPadX = 3
	emergencyPadY = 2

	// fullChannel is a colour channel at full strength, which is the only value
	// emergencyInk is built out of.
	fullChannel = 0xFF
)

// The fields of a strip, left to right.
const (
	fieldIdent = iota
	fieldLevel
	fieldSpeed
	fieldTrack
	fieldRange
	fieldPos
	fieldSeen

	// fieldCount is how many fields there are, which is what every loop over
	// them runs to.
	fieldCount
)

// stripHeaders is the word over each field, set once on the selected strip and
// carrying the field's unit where it has one.
//
// The units live here and nowhere else. A board that repeated FT and KT beside
// every figure on every strip spent a tenth of the column saying the same two
// words forty times, and a column of figures under a header naming the unit is
// how a flight strip has always been read.
//
//nolint:gochecknoglobals // a header row is data, and an array cannot be const.
var stripHeaders = [fieldCount]string{
	fieldIdent: "CALLSIGN / ICAO",
	fieldLevel: "LEVEL FT",
	fieldSpeed: "GS KT",
	fieldTrack: "TRK",
	fieldRange: "DIST NM / BRG",
	fieldPos:   "POS",
	fieldSeen:  "SEEN",
}

// The widest thing each field can be asked to hold.
//
// They are strings rather than character counts because a field's three rows
// are set in three different faces, so a count would have to say which face it
// was counting in. M is the widest glyph a proportional face has and the same
// width as every other in these, which keeps the measurement honest if the
// scene is ever handed a face that is not fixed pitch.
const (
	widestCallsign = "MMMMMMMM"
	widestICAO     = "MMMMMM"
	widestSquawk   = squawkPrefix + "7777"
	widestLevel    = "999,999"
	widestRate     = "99,999" + rateUnit
	widestSpeed    = "999"
	widestTrack    = "359"
	widestDistance = "999.9"
	widestBearing  = "359"
	widestPlace    = "179.77 E"

	// widestCount and widestFiltered are what the count line above the strips
	// reserves room for, whichever of the two it is drawing.
	widestCount    = "9999" + stripsCountSuffix
	widestFiltered = "9999" + stripsOfInfix + widestCount
)

// emergencyInk is the text inside the warning box.
//
// It is white on both themes rather than the palette's own ink, because the box
// is filled with the warning red on both and the day theme's ink is near-black:
// set in that, the one word on the board that has to be read from across a room
// would be the one word nobody could read. A warning is the single place in the
// scene where a colour is fixed rather than themed.
//
//nolint:gochecknoglobals // a colour is data, and color.RGBA cannot be const.
var emergencyInk = color.RGBA{R: fullChannel, G: fullChannel, B: fullChannel, A: opaque}

// stripDrop is one thing the plan gives up as the column narrows: usually a
// whole field, and once the little aeroplane inside the ident field.
type stripDrop struct {
	field int
	att   bool
}

// stripDropOrder is the order things are given up as the column narrows.
//
// Position goes first. It is the widest field on the board and the scope beside
// it already answers the question, to a pixel rather than to two decimals. Then
// how long ago the contact was heard, which only matters when it stops being
// heard at all. Then the track, because the silhouette on the scope is already
// pointing that way, and then the little aeroplane, which is that same fact a
// third time. Speed is last of the five: it is the only one of them nothing
// else on screen carries.
//
// The identity, the level and the range are not in the list. They are what a
// strip is for, and a board that had given those up would be a column of blanks.
//
//nolint:gochecknoglobals // an order is data, and an array cannot be const.
var stripDropOrder = [...]stripDrop{
	{field: fieldPos},
	{field: fieldSeen},
	{field: fieldTrack},
	{att: true},
	{field: fieldSpeed},
}

// stripPlan is which fields are drawn and where each one goes.
type stripPlan struct {
	// left is a field's own left edge and width how much room it has. Values
	// are set from the left edge rather than aligned on the right, because a
	// field carries a header above it and a figure starting somewhere else from
	// the word naming it would read as belonging to the field before.
	left  [fieldCount]int
	width [fieldCount]int
	on    [fieldCount]bool

	// rule is the x of the hairline before each field. Every field on the plan
	// has one except the ident, which is always the first drawn: it is never in
	// the drop order, so nothing else can be.
	rule [fieldCount]int

	// att is whether the little aeroplane is drawn at the right end of the
	// ident field. It is the one thing dropped without a field going with it.
	att bool
}

// count is how many fields are still on the plan.
func (p *stripPlan) count() int {
	total := 0

	for index := range fieldCount {
		if p.on[index] {
			total++
		}
	}

	return total
}

// total is the least room the fields on the plan need, the gaps between them
// included.
//
// The gaps are part of the total rather than spread on afterwards, for the
// reason the compact rows counted theirs: a board whose fields only stop
// touching when the column is wide is unreadable exactly when it is fullest.
func (p *stripPlan) total() int {
	sum := 0

	for index := range fieldCount {
		if p.on[index] {
			sum += p.width[index]
		}
	}

	return sum + max(p.count()-1, 0)*stripGap
}

// size copies the measured widths onto the plan, adding the little aeroplane's
// cell to the ident field while that is still being drawn.
func (p *stripPlan) size(widths [fieldCount]int) {
	p.width = widths

	if p.att {
		p.width[fieldIdent] += stripGap + attCell
	}
}

// give drops one entry of the drop order and measures the plan again.
func (p *stripPlan) give(entry stripDrop, widths [fieldCount]int) {
	if entry.att {
		p.att = false
	} else {
		p.on[entry.field] = false
	}

	p.size(widths)
}

// place spreads the fields across the width, so the first starts on the left
// edge and the last finishes on the right one.
//
// The slack is shared by interpolating on the field's position rather than by
// adding a fixed gap, which is how the compact rows shared theirs: an integer
// gap leaves a remainder, and the last field would then stop a few pixels short
// of the edge it is meant to meet.
func (p *stripPlan) place(left, available int) {
	slack := max(available-p.total(), 0)
	gaps := max(p.count()-1, 1)
	used, seen, prevRight := 0, 0, left

	for index := range fieldCount {
		if !p.on[index] {
			continue
		}

		p.left[index] = left + used + seen*stripGap + slack*seen/gaps

		if seen > 0 {
			p.rule[index] = (prevRight + p.left[index]) / 2
		}

		used += p.width[index]
		prevRight = p.left[index] + p.width[index]
		seen++
	}
}

// stripWidths is the room each field needs, taken from the widest thing that
// can land in it rather than from what is on screen.
//
// Sizing from the worst case is what keeps the board still, which is the rule
// the compact rows were laid out by and the reason a strip is readable at all:
// a field measured from its own contents would move every time an aircraft
// climbed through ten thousand feet, and a column whose rules walk sideways is
// harder to read than one wasting a few pixels.
//
// Three of the fields are measured against more than one worst case, because
// the selected strip and a half strip put different things in them. The ident
// field is the extreme: a callsign set large on one, the codes and a warning
// box under it, and the callsign with its hex beside it on the other.
func (s *Scene) stripWidths() [fieldCount]int {
	small, bold, large := s.faces.Small, s.faces.BodyBold, s.faces.Large

	icao := measure(small, widestICAO)
	squawk := measure(small, widestSquawk)
	warning := measure(small, emergencyText) + 2*emergencyPadX

	var widths [fieldCount]int

	widths[fieldIdent] = max(
		measure(large, widestCallsign),
		icao+stripGap+squawk+stripGap+warning,
		measure(bold, widestCallsign)+stripGap+max(icao, warning),
	)

	widths[fieldLevel] = max(
		vertMarker+vertGap+measure(bold, widestLevel),
		measure(small, widestRate),
	)

	widths[fieldSpeed] = measure(bold, widestSpeed)
	widths[fieldTrack] = measure(bold, widestTrack) + s.arrowWidth()
	widths[fieldRange] = measure(bold, widestDistance) + stripGap + measure(bold, widestBearing) + s.arrowWidth()
	widths[fieldPos] = measure(small, widestPlace+" "+widestPlace)
	widths[fieldSeen] = measure(small, seenNowText)

	// Every field is at least as wide as the word over it. TRK and SEEN are the
	// two that would otherwise come out narrower than their own headers, and a
	// header running into the rule beside it says less than a wider field does.
	for index := range fieldCount {
		widths[index] = max(widths[index], measureTracked(small, stripHeaders[index]))
	}

	return widths
}

// planStrips works out which fields fit between left and right.
//
// Fields are dropped whole rather than squeezed, for the reason every other
// block in this scene is: the values are set in bitmap faces with one design
// size each, so a narrower field means fewer characters, not smaller ones. A
// column too narrow even for the identity, the level and the range draws no
// board at all rather than running it past its own right edge.
//
// All four faces have to be there. The selected strip sets its callsign in the
// large one and its codes in the small one, and a board missing either would be
// a strip with a hole in it rather than a shorter strip.
func (s *Scene) planStrips(left, right int) (stripPlan, bool) {
	var plan stripPlan

	if s.faces.Small == nil || s.faces.Body == nil || s.faces.BodyBold == nil || s.faces.Large == nil {
		return plan, false
	}

	widths := s.stripWidths()
	available := right - left

	for index := range fieldCount {
		plan.on[index] = true
	}

	plan.att = true
	plan.size(widths)

	for _, entry := range stripDropOrder {
		if plan.total() <= available {
			break
		}

		plan.give(entry, widths)
	}

	if plan.total() > available {
		return plan, false
	}

	plan.place(left, available)

	return plan, true
}

// stripPen is everything the fields of one strip share: where to draw, where
// the plan put each field, and the rows the type sits on.
//
// A half strip has one row, so big, value and sub all name it there and only
// the drawers that ask full ever put anything on a second line.
type stripPen struct {
	dst  *canvas.Canvas
	plan stripPlan

	// big is the top of the row the selected strip sets its callsign on, in the
	// large face. value is where a figure in the bold face rides in that row,
	// small where a small line rides in it, and sub the top of the second line,
	// which the selected strip alone has.
	big   int
	value int
	small int
	sub   int

	full bool
}

// figureFace is the face a strip sets its figures in: the bold one on the
// selected strip, where they share a row with a callsign twice their size, and
// the plain one on a half strip, where nothing is competing with them.
func (s *Scene) figureFace(pen stripPen) *psf.Font {
	if pen.full {
		return s.faces.BodyBold
	}

	return s.faces.Body
}

// fullStripHeight is the selected strip's three rows: the headers, the callsign
// in the large face, and the second line under it. halfStripHeight is one row
// of figures. Both come out of the faces rather than being written down, so a
// scene handed different fonts gets strips that fit them.
func (s *Scene) fullStripHeight() int {
	return 2*stripPadY + lineHeight(s.faces.Small) + lineHeight(s.faces.Large) + lineHeight(s.faces.Small)
}

func (s *Scene) halfStripHeight() int {
	return 2*stripHalfPadY + lineHeight(s.faces.Body)
}

// drawStrips fills the column with the board: the count over it, the selected
// aircraft as a full strip, and every other one as a half strip under it.
//
// The selected aircraft is lifted to the top rather than left in its place in
// the list, and that took the scrolling window away with it. The window existed
// to keep the selection on screen; a board that puts it at the top always has it
// there, so n and p move which strip is full rather than which part of a list
// is showing.
func (s *Scene) drawStrips(col *layout, frame source.Frame) {
	plan, drawable := s.planStrips(col.left+stripEdge+stripPadX, col.right-stripPadX)
	if !drawable {
		return
	}

	head := lineHeight(s.faces.Small) + rowLead

	full := s.fullStripHeight()
	if !col.fits(head + full) {
		return
	}

	shown := s.shownPlanes()
	sel := s.selectedPlane(frame)

	s.drawStripCount(col, shown, len(frame.Planes))
	col.top += head

	s.drawFullStrip(col, plan, frame, sel)
	col.top += full

	s.drawHalfStrips(col, plan, frame, sel, shown)
}

// drawStripCount writes how many aircraft are being tracked, on the line above
// the board and right-aligned on it.
//
// It is set in the data colour because it is a reading about the machine rather
// than about any aeroplane, which is the same rule that puts the source label
// and the clocks in cyan up in the header.
//
// With a filter on it reads "12 OF 81 AIRCRAFT" instead. The second number is
// the whole field, so the line says what is being held back as well as what is
// on the board, and a scope that has gone quiet cannot be mistaken for one that
// is filtered.
func (s *Scene) drawStripCount(col *layout, shown, total int) {
	face := s.faces.Small

	widest := widestCount
	if s.filter.active() {
		widest = widestFiltered
	}

	if measureTracked(face, widest) > col.right-col.left {
		return
	}

	suffix := measureTracked(face, stripsCountSuffix)
	value := s.count(total)
	left := col.right - suffix - trackedWidth(face, len(value))

	if s.filter.active() {
		left = s.drawCountLead(col.dst, left, col.top, shown)
	}

	pen := drawBytesTracked(col.dst, face, left, col.top, value, s.pal.Data)
	text.Draw(col.dst, face, pen, col.top, stripsCountSuffix, s.pal.Data, text.WithSpacing(labelTracking))
}

// drawCountLead writes the "12 OF " a filtered count opens with, placed so it
// finishes where the total was going to start, and returns the x the total now
// starts at.
//
// The number goes into its own buffer rather than into the one s.count uses,
// because the total has already been formatted into that and a formatter's
// slice is only good until the next call on the same array.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawCountLead(dst *canvas.Canvas, rightX, y, shown int) int {
	face := s.faces.Small

	lead := s.shownCount(shown)
	infix := measureTracked(face, stripsOfInfix)

	pen := drawBytesTracked(dst, face, rightX-infix-trackedWidth(face, len(lead)), y, lead, s.pal.Data)

	return text.Draw(dst, face, pen, y, stripsOfInfix, s.pal.Data, text.WithSpacing(labelTracking))
}

// drawFullStrip draws the selected aircraft's strip, or the empty one an empty
// sky gets.
//
// The empty strip keeps its headers and its rules and loses its edge and its
// fill, because there is no selection for either to mark. A board that
// collapsed instead would change shape every time the last aircraft left range,
// which is worse to look at than a gap.
func (s *Scene) drawFullStrip(col *layout, plan stripPlan, frame source.Frame, sel selection) {
	box := image.Rect(col.left, col.top, col.right, col.top+s.fullStripHeight())

	if sel.found {
		col.dst.FillRect(box, s.fade(s.pal.Key, stripLift))
		col.dst.FillRect(image.Rect(box.Min.X, box.Min.Y, box.Min.X+stripEdge, box.Max.Y), s.pal.Accent)
	}

	s.drawStripRules(col.dst, plan, box)
	s.drawStripHeaders(col.dst, plan, col.top+stripPadY)

	big := col.top + stripPadY + lineHeight(s.faces.Small)
	large := lineHeight(s.faces.Large)

	pen := stripPen{
		dst:   col.dst,
		plan:  plan,
		big:   big,
		value: big + (large-lineHeight(s.faces.BodyBold))/2,
		small: big + (large-lineHeight(s.faces.Small))/2,
		sub:   big + large,
		full:  true,
	}

	if !sel.found {
		text.Draw(col.dst, s.faces.Large, plan.left[fieldIdent], pen.big, noTraffic, s.pal.Muted)

		return
	}

	s.drawStripFields(pen, sel, frame)
}

// drawHalfStrips draws every aircraft that is not the selected one, as many as
// the column has room for, and closes the board with the count it had no room
// for.
//
// It walks the whole fleet rather than indexing into it, because the strips are
// the aircraft that passed the filter and nothing holds those in a list of their
// own: the ICAO index has their identities but not their readings. The walk
// stops at the bottom of the column, so a busy scope does not scan eighty
// aircraft to draw seventeen strips.
//
// There is no guard on the strip height. planStrips has already refused the
// whole board unless all four faces loaded, and a half strip is two fixed
// paddings plus a face height, so by the time this runs the answer cannot be
// anything but positive.
func (s *Scene) drawHalfStrips(col *layout, plan stripPlan, frame source.Frame, sel selection, shown int) {
	height := s.halfStripHeight()

	// The selected aircraft has the full strip at the top, so it is one of the
	// filtered list rather than one of the half strips. A selection the filter
	// is hiding was never in that list to begin with.
	rest := shown
	if sel.found && !sel.hidden {
		rest--
	}

	window := s.stripWindow(col, rest, height)
	drawn := 0

	for _, plane := range frame.Planes {
		if drawn >= window {
			break
		}

		// The selected aircraft already has the full strip, so it is skipped
		// here rather than drawn twice. With nothing selected there is no ICAO
		// to match and every visible aircraft gets a half strip.
		if !s.visible(plane) || (sel.found && plane.ICAO == sel.plane.ICAO) {
			continue
		}

		s.drawHalfStrip(col, plan, frame, plane, col.top+drawn*height)
		drawn++
	}

	s.drawMoreStrip(col, plan, col.top+drawn*height, rest-drawn)
}

// stripWindow is how many half strips the room under the full one holds.
//
// A board longer than the column gives one line back for the "+N MORE" tail
// rather than squeezing it under the last strip, where it would be drawn over
// the legend. A shorter board with an honest count under it is worth a strip.
func (s *Scene) stripWindow(col *layout, rest, height int) int {
	room := col.bottom - col.top

	window := room / height
	if rest > window {
		window = (room - lineHeight(s.faces.Small) - rowLead) / height
	}

	return max(window, 0)
}

// drawHalfStrip draws one unselected aircraft: a single row of figures with the
// field rules through it and a hairline under it.
//
// There is no fill and no edge. Both mark the selection, and a board where every
// strip was lifted off the page would be a board with nothing picked out of it.
func (s *Scene) drawHalfStrip(
	col *layout, plan stripPlan, frame source.Frame, plane airplane.Snapshot, top int,
) {
	height := s.halfStripHeight()

	s.drawStripRules(col.dst, plan, image.Rect(col.left, top, col.right, top+height))

	value := top + stripHalfPadY

	s.drawStripFields(stripPen{
		dst:   col.dst,
		plan:  plan,
		big:   value,
		value: value,
		small: top + (height-lineHeight(s.faces.Small))/2,
		sub:   value,
	}, selection{plane: plane, found: true}, frame)
}

// drawStripFields sets one strip's seven fields, whichever of them the plan
// still has on it.
func (s *Scene) drawStripFields(pen stripPen, sel selection, frame source.Frame) {
	plane := sel.plane

	s.drawStripIdent(pen, plane, sel)
	s.drawStripLevel(pen, plane)
	s.drawStripSpeed(pen, plane)
	s.drawStripTrack(pen, plane)
	s.drawStripRange(pen, frame.Receiver, plane)
	s.drawStripPos(pen, plane)
	s.drawStripSeen(pen, frame.Now, plane)
}

// drawStripRules draws the hairline along the bottom of a strip and the
// verticals between its fields.
//
// The verticals run the whole height of the strip rather than stopping at the
// type, which is what makes a field a field: the eye follows a rule down the
// board and finds the same reading on every aircraft in the same place.
func (s *Scene) drawStripRules(dst *canvas.Canvas, plan stripPlan, box image.Rectangle) {
	dst.FillRect(image.Rect(box.Min.X, box.Max.Y-ruleHeight, box.Max.X, box.Max.Y), s.pal.Rule)

	for index := range fieldCount {
		if !plan.on[index] || index == fieldIdent {
			continue
		}

		dst.FillRect(image.Rect(plan.rule[index], box.Min.Y, plan.rule[index]+ruleHeight, box.Max.Y), s.pal.Rule)
	}
}

// drawStripHeaders names the fields over the selected strip, in the small face
// and the data colour, so they read as labels on the board rather than as
// another aircraft's readings.
//
// They are set once, at the top, and every half strip under them is read against
// the same words. Repeated on every strip they spent a third of the board saying
// what the board already showed.
func (s *Scene) drawStripHeaders(dst *canvas.Canvas, plan stripPlan, top int) {
	for index := range fieldCount {
		if !plan.on[index] {
			continue
		}

		text.Draw(dst, s.faces.Small, plan.left[index], top, stripHeaders[index], s.pal.Data,
			text.WithSpacing(labelTracking))
	}
}

// drawMoreStrip writes the muted line saying how many aircraft the board had no
// room for.
func (s *Scene) drawMoreStrip(col *layout, plan stripPlan, top, hidden int) {
	if hidden <= 0 {
		return
	}

	face := s.faces.Small

	pen := text.Draw(col.dst, face, plan.left[fieldIdent], top, morePrefix, s.pal.Muted)
	pen = drawBytes(col.dst, face, pen, top, s.count(hidden), s.pal.Muted)
	text.Draw(col.dst, face, pen, top, moreSuffix, s.pal.Muted)
}

// drawStripIdent sets the identity field: who the aeroplane is, and the little
// model of it at the right end of the field.
//
// The selected strip gives the callsign the large face and puts the hex and the
// squawk on the line under it. A half strip has one line, so the hex sits beside
// the callsign in the small face and there is no room for a squawk, which is the
// right thing to give up: a code nobody is squawking in anger says nothing, and
// one squawked in anger brings the warning box with it.
func (s *Scene) drawStripIdent(pen stripPen, plane airplane.Snapshot, sel selection) {
	left := pen.plan.left[fieldIdent]
	name := clip(callsignOf(plane), maxCallsign)

	if pen.full {
		text.Draw(pen.dst, s.faces.Large, left, pen.big, name, s.callsignInk(plane))
		s.drawStripCodes(pen, left, plane, sel)
	} else {
		mark := text.Draw(pen.dst, s.faces.BodyBold, left, pen.value, name, s.callsignInk(plane))
		s.drawHalfCodes(pen, mark+stripGap, plane)
	}

	s.drawStripAttitude(pen, plane)
}

// drawStripCodes writes the selected strip's second line: the ICAO hex, the
// transponder code, the warning box when one is being squawked, and the tag
// that says the filter is hiding the aircraft this strip is about.
//
// The FILTERED tag is a caution rather than chrome. The strip is about an
// aeroplane that is not on the field, which is a state the operator has to
// notice before wondering where it went, and amber is what a panel says that
// with.
func (s *Scene) drawStripCodes(pen stripPen, left int, plane airplane.Snapshot, sel selection) {
	face := s.faces.Small

	code := clip(plane.Squawk, maxSquawk)
	if code == "" {
		code = noSquawk
	}

	mark := text.Draw(pen.dst, face, left, pen.sub, clip(plane.ICAO, maxICAO), s.pal.Muted)
	mark = text.Draw(pen.dst, face, mark+stripGap, pen.sub, squawkPrefix, s.pal.Muted)
	mark = text.Draw(pen.dst, face, mark, pen.sub, code, s.pal.Ink)
	mark = s.drawEmergency(pen.dst, mark+stripGap, pen.sub, plane)

	if sel.hidden {
		text.Draw(pen.dst, face, mark+stripGap, pen.sub, filteredTag, s.pal.Caution)
	}
}

// drawHalfCodes writes a half strip's hex beside its callsign, with the warning
// box in its place when the aircraft is squawking an emergency.
//
// The box takes the hex's room rather than following it. A half strip has one
// line and the field is sized for the wider of the two, and of the pair the hex
// is the one worth giving up: it is the identity nobody reads while an aircraft
// is declaring one.
func (s *Scene) drawHalfCodes(pen stripPen, left int, plane airplane.Snapshot) {
	if plane.Emergency {
		s.drawEmergency(pen.dst, left, pen.small, plane)

		return
	}

	text.Draw(pen.dst, s.faces.Small, left, pen.small, clip(plane.ICAO, maxICAO), s.pal.Muted)
}

// drawEmergency puts the warning box after whatever is on the line and returns
// the x just past it, or the pen it was handed when nothing is wrong.
//
// It is a filled box rather than a coloured word because it is the one thing on
// the board that has to be seen without being looked for. The word used to be
// set in the accent, which is now the selected aircraft's own colour: an
// emergency on any other strip would have worn the mark of the one the operator
// had picked.
func (s *Scene) drawEmergency(dst *canvas.Canvas, left, top int, plane airplane.Snapshot) int {
	if !plane.Emergency {
		return left
	}

	face := s.faces.Small
	width, height := text.Measure(face, emergencyText)
	box := image.Rect(left, top-emergencyPadY, left+width+2*emergencyPadX, top+height+emergencyPadY)

	dst.FillRect(box, s.pal.Warn)
	text.Draw(dst, face, left+emergencyPadX, top, emergencyText, emergencyInk)

	return box.Max.X
}

// drawStripAttitude draws the little aeroplane at the right end of the ident
// field, posed by the rules the perspective view poses the large one with.
//
// It goes through a camera of its own rather than the 3D view's. That camera
// orbits, so an aircraft in it is seen from wherever the orbit has got to; a
// column of these has to be readable down the board, which means every strip
// seen from the same angle whatever the picture beside it is doing. The model
// and the projection are shared, the camera is not: see newCellCamera3.
//
// It takes the aircraft's own colour on every strip, the selected one included.
// The callsign beside it already carries the selection, and the point of the
// model is that reading the board and reading the scope are the same act of
// recognition. A shape that changed colour when it was selected would break that
// on the one strip it matters most on.
func (s *Scene) drawStripAttitude(pen stripPen, plane airplane.Snapshot) {
	if !pen.plan.att {
		return
	}

	centre := image.Pt(
		pen.plan.left[fieldIdent]+pen.plan.width[fieldIdent]-attCell/2,
		pen.value+lineHeight(s.faces.Body)/2,
	)

	s.drawShape3(pen.dst, newCellCamera3(centre), plane, point3{}, centre, s.aircraftColour(plane))
}

// drawStripLevel sets the level field: the altitude in the colour of its band,
// with the climb or descent triangle before it and, on the selected strip, the
// rate itself underneath.
//
// The triangle is on every strip because it costs no height at all, sitting
// beside the figure rather than under it, and a board scanned for who is coming
// down is a board where every strip has to answer. The rate is the selected
// strip's alone: a column of four-digit figures nobody is reading is noise, and
// the aircraft whose rate is worth a number is the one that has been picked.
//
// The triangle's room is reserved whether or not there is one to put in it, so
// the figures line up down the board rather than stepping sideways on the strips
// that happen to be level.
func (s *Scene) drawStripLevel(pen stripPen, plane airplane.Snapshot) {
	if !pen.plan.on[fieldLevel] {
		return
	}

	left := pen.plan.left[fieldLevel]
	band := s.bandColour(plane.Altitude)

	if !level(plane.VertRate) {
		s.drawVertMarker(pen.dst, left, pen.value, plane.VertRate, band)
	}

	drawBytes(pen.dst, s.figureFace(pen), left+vertMarker+vertGap, pen.value, s.thousands(plane.Altitude), band)

	if !pen.full || level(plane.VertRate) {
		return
	}

	mark := drawBytes(pen.dst, s.faces.Small, left, pen.sub, s.thousands(math.Abs(plane.VertRate)), s.pal.Muted)
	text.Draw(pen.dst, s.faces.Small, mark, pen.sub, rateUnit, s.pal.Muted)
}

// drawStripSpeed sets the ground speed, which is the one figure on the board
// nothing else on screen carries.
func (s *Scene) drawStripSpeed(pen stripPen, plane airplane.Snapshot) {
	if !pen.plan.on[fieldSpeed] {
		return
	}

	drawBytes(pen.dst, s.figureFace(pen), pen.plan.left[fieldSpeed], pen.value, s.speed(plane.Velocity), s.pal.Ink)
}

// drawStripTrack sets the course the aircraft is flying, as degrees with a
// needle turned to them.
//
// A number alone takes a moment to place and a picture does not, which is why
// both are here rather than one. The needle takes the figure's own ink: it is
// the reading, not a label on one.
func (s *Scene) drawStripTrack(pen stripPen, plane airplane.Snapshot) {
	if !pen.plan.on[fieldTrack] {
		return
	}

	face := s.figureFace(pen)
	left := pen.plan.left[fieldTrack]

	if !knownHeading(plane.Heading) {
		text.Draw(pen.dst, face, left, pen.value, trackUnknown, s.pal.Muted)

		return
	}

	mark := drawBytes(pen.dst, face, left, pen.value, s.degrees(plane.Heading), s.pal.Ink)
	s.drawArrow(pen.dst, mark+arrowGap, pen.value+lineHeight(face)/2, plane.Heading, s.pal.Ink)
}

// drawStripRange sets how far the aircraft is from the receiver and on what
// bearing, which is where it is rather than where it is going.
//
// The two travel together in one field because neither says much alone: a
// distance with no bearing is a circle, and a bearing with no distance is a line.
func (s *Scene) drawStripRange(pen stripPen, receiver source.Receiver, plane airplane.Snapshot) {
	if !pen.plan.on[fieldRange] {
		return
	}

	face := s.figureFace(pen)
	left := pen.plan.left[fieldRange]

	away := airplanes.HaversineDistance(receiver.Latitude, receiver.Longitude, plane.Latitude, plane.Longitude)
	mark := drawBytes(pen.dst, face, left, pen.value, s.distance(away), s.pal.Ink) + stripGap

	bearing, known := bearingTo(receiver, plane)
	if !known {
		text.Draw(pen.dst, face, mark, pen.value, trackUnknown, s.pal.Muted)

		return
	}

	mark = drawBytes(pen.dst, face, mark, pen.value, s.degrees(bearing), s.pal.Ink)
	s.drawArrow(pen.dst, mark+arrowGap, pen.value+lineHeight(face)/2, bearing, s.pal.Ink)
}

// drawStripPos writes where the aircraft is, to two decimals.
//
// Two places is about a nautical mile, which is as much as a figure read off a
// scope is worth. The receiver's own line in the header carries four, because
// that one is a claim about where the antenna is rather than about where an
// aeroplane was a moment ago.
//
// The selected strip stacks the two halves, one to a row, and a half strip sets
// them side by side, which is the case the field is sized for.
func (s *Scene) drawStripPos(pen stripPen, plane airplane.Snapshot) {
	if !pen.plan.on[fieldPos] {
		return
	}

	face := s.faces.Small
	left := pen.plan.left[fieldPos]

	if !hasPosition(plane) {
		text.Draw(pen.dst, face, left, pen.small, detailUnknown, s.pal.Muted)

		return
	}

	// One half at a time: both share the scene's coordinate buffer, so
	// formatting the second would overwrite the first.
	if pen.full {
		drawBytes(pen.dst, face, left, pen.small, s.place(plane.Latitude, 'N', 'S'), s.pal.Ink)
		drawBytes(pen.dst, face, left, pen.sub, s.place(plane.Longitude, 'E', 'W'), s.pal.Ink)

		return
	}

	mark := drawBytes(pen.dst, face, left, pen.small, s.place(plane.Latitude, 'N', 'S'), s.pal.Muted)
	drawBytes(pen.dst, face, mark+glyphWidth(face), pen.small, s.place(plane.Longitude, 'E', 'W'), s.pal.Muted)
}

// drawStripSeen writes how long ago the aircraft was last heard, in the coarse
// buckets seenBucket names.
func (s *Scene) drawStripSeen(pen stripPen, now time.Time, plane airplane.Snapshot) {
	if !pen.plan.on[fieldSeen] {
		return
	}

	text.Draw(pen.dst, s.faces.Small, pen.plan.left[fieldSeen], pen.small,
		seenBucket(now.Sub(plane.LastUpdate)), s.smallInk(pen))
}

// smallInk is the colour a strip's small readings take: the reading ink on the
// selected strip and the muted one on a half strip.
//
// A board where every strip set its position and its age in full ink would be a
// wall of small type with nothing picked out of it. On the selected strip they
// are what the operator asked for, so there they are read like everything else.
func (s *Scene) smallInk(pen stripPen) color.RGBA {
	if pen.full {
		return s.pal.Ink
	}

	return s.pal.Muted
}

// measure is one string's width in a face, which is the half of text.Measure
// every caller here wants.
func measure(face *psf.Font, value string) int {
	width, _ := text.Measure(face, value)

	return width
}

// measureTracked is measure at the scene's label tracking, which every small
// all-caps label in the scene is set at.
func measureTracked(face *psf.Font, value string) int {
	width, _ := text.Measure(face, value, text.WithSpacing(labelTracking))

	return width
}
