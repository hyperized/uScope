package radar

import (
	"image"
	"image/color"
	"strconv"
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
// Every aircraft the filter is showing gets one strip, all of them the same
// height, in the order the list is sorted in. The selected aeroplane's strip is
// marked where it stands rather than promoted to the top: the board used to
// lift it out of the list as a full-height card, which meant pressing n swapped
// which aeroplane was at the top and nothing on screen said where in the list
// the operator had got to. A cursor that moves down a list is a cursor you can
// follow. The panel above the board is where the selection is read; the board
// is where it is found.
const (
	// stripEdge is the accent bar down the left of the selected strip. Every
	// strip leaves room for it, so the fields line up whether or not the strip
	// they are on is the one the selection is standing on.
	stripEdge = 3

	// stripPadX is the air between a field's hairline rule and its type, and
	// stripPadY the air above and below the row of field names over the board.
	stripPadX = 8
	stripPadY = 4

	// stripHalfPadY is the air above and below the single line a strip carries.
	//
	// It is wider than the padding round the field names on purpose. A strip is
	// one line of type and nothing else, and at four pixels it read as a table
	// row again, which is the thing the strips replaced. Seven puts a strip at
	// thirty pixels at the panel's faces. They are still half strips: there is
	// simply no whole one left on the board for them to be halves of.
	stripHalfPadY = 7

	// stripGap is the air between two fields, with the hairline rule that
	// separates them halfway across it.
	stripGap = 8

	// The "+N ABOVE" and "+N MORE" lines that close a board longer than the
	// column, one at each end the window has cut. The two numbers plus the
	// strips on screen always add up to what the filter is showing, so the
	// board never quietly loses an aeroplane.
	morePrefix  = "+"
	moreSuffix  = " MORE"
	aboveSuffix = " ABOVE"

	// maxStripMarks is how many of those lines a board can carry at once, and
	// stripLeadDivisor is how far down the window the selected strip rides: a
	// third of the way, which is in the upper half.
	//
	// A third rather than the top, because a window pinned to the cursor would
	// hide the aircraft just stepped past. A third rather than the middle,
	// because most of a list read in distance order is still to come.
	maxStripMarks    = 2
	stripLeadDivisor = 3

	// The status line over the panel. It opens with where the selection is
	// sitting, so n and p move a number as well as a mark, and closes with how
	// many aircraft there are.
	//
	// With a filter on it reads "SEL 07 - 12 OF 81 AIRCRAFT". The second number
	// is the whole field, so the line says what is being held back as well as
	// what is on the board, and a scope that has gone quiet cannot be mistaken
	// for one that is filtered.
	selPrefix         = "SEL "
	selNone           = "--"
	selFilteredTag    = " (FILTERED)"
	stripsSeparator   = " · "
	stripsCountSuffix = " AIRCRAFT"
	stripsOfInfix     = " OF "

	// attCell is the room the little aeroplane at the right end of the ident
	// field takes. It is the model's fourteen-pixel span plus the room a wing
	// needs to swing into as an aircraft banks through a turn.
	attCell = 24

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

// stripHeaders is the word over each field, set once in the row above the board
// and carrying the field's unit where it has one.
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
// They are strings rather than character counts because a strip's figures and
// its small readings are set in two different faces, so a count would have to
// say which face it was counting in. M is the widest glyph a proportional face
// has and the same width as every other in these, which keeps the measurement
// honest if the scene is ever handed a face that is not fixed pitch.
const (
	widestCallsign = "MMMMMMMM"
	widestICAO     = "MMMMMM"
	widestSquawk   = squawkPrefix + "7777"
	widestLevel    = "999,999"
	widestSpeed    = "999"
	widestTrack    = "359"
	widestDistance = "999.9"
	widestBearing  = "359"
	widestPlace    = "179.77 E"

	// widestRank is the room the index at the far left of every strip is given.
	//
	// Three digits, where an index is written in two. The index is a place in
	// the whole filtered list rather than a place in the window, so a board
	// scrolled to the eightieth of a hundred and twenty aircraft has to write
	// three; reserving the room means the callsigns beside it stay where they
	// are instead of stepping sideways as the window moves.
	widestRank = "999"

	// widestStatus is what the line over the panel reserves room for, which is
	// every part of it at once: a four-figure index, the filter's tag, and two
	// four-figure counts.
	widestStatus = selPrefix + "9999" + selFilteredTag + stripsSeparator +
		"9999" + stripsOfInfix + "9999" + stripsCountSuffix
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
// The ident field is the crowded one. It holds the index, the callsign, and
// then either the ICAO hex or the warning box that takes the hex's place while
// an aircraft is squawking an emergency, whichever of those two is wider.
func (s *Scene) stripWidths() [fieldCount]int {
	small, body := s.faces.Small, s.faces.Body

	icao := measure(small, widestICAO)
	warning := measure(small, emergencyText) + 2*emergencyPadX

	var widths [fieldCount]int

	widths[fieldIdent] = measure(small, widestRank) + stripGap +
		measure(s.faces.BodyBold, widestCallsign) + stripGap + max(icao, warning)

	widths[fieldLevel] = vertMarker + vertGap + measure(body, widestLevel)
	widths[fieldSpeed] = measure(body, widestSpeed)
	widths[fieldTrack] = measure(body, widestTrack) + s.arrowWidth()
	widths[fieldRange] = measure(body, widestDistance) + stripGap + measure(body, widestBearing) + s.arrowWidth()
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
// All four faces have to be there. A strip sets its callsign in the bold face
// and its codes in the small one, the row of field names over the board is the
// small one again, and the panel over that wants the large one.
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
// the plan put each field, and the two rows the type sits on.
type stripPen struct {
	dst  *canvas.Canvas
	plan stripPlan

	// value is the top of the row a figure in the body face rides on and small
	// the top of the row a small reading rides on. They are the same line of
	// the strip, set in two faces of different heights and centred on each
	// other.
	value int
	small int

	// rank is the aircraft's place in the list, counted from one, and picked
	// says the selection is standing on this strip.
	rank   int
	picked bool
}

// halfStripHeight is one row of figures with its padding, and boardHeadHeight
// the row of field names over the board. Both come out of the faces rather
// than being written down, so a scene handed different fonts gets a board that
// fits them.
func (s *Scene) halfStripHeight() int {
	return 2*stripHalfPadY + lineHeight(s.faces.Body)
}

func (s *Scene) boardHeadHeight() int {
	return 2*stripPadY + lineHeight(s.faces.Small)
}

// stripBoard is the slice of the list that has strips this frame.
type stripBoard struct {
	// first is the list index of the topmost strip and count how many strips
	// are drawn.
	first int
	count int

	// above and below are how many aircraft the window leaves off each end,
	// which is what the two +N lines say. Zero at both ends is a board holding
	// the whole list.
	above int
	below int
}

// marks is how many +N lines the board carries, which is the room the window
// has had to give up to draw them.
func (b stripBoard) marks() int {
	marks := 0

	if b.above > 0 {
		marks++
	}

	if b.below > 0 {
		marks++
	}

	return marks
}

// stripFit is how many strips fit in room once marks of the +N lines have taken
// theirs.
//
// A +N line is charged a whole strip's height rather than its own. It stands in
// the list where a strip would, and one squeezed into the slack under the last
// strip would be drawn over the legend.
func (s *Scene) stripFit(room, marks int) int {
	height := s.halfStripHeight()

	return max((room-marks*height)/height, 0)
}

// stripFirst is the list index the window opens at.
//
// The selected strip rides a third of the way down the window, which is what
// keeps the cursor in the upper half of a long board. With nothing pinned the
// selection is the nearest contact, which is index zero, so the clamp puts the
// window at the top of the list and the board opens where the list does.
//
// A selection the filter is hiding has no index and the window opens at the top
// as well: there is no strip on the board to keep in view.
func stripFirst(total, count, sel int) int {
	if sel < 0 || count >= total {
		return 0
	}

	return min(max(sel-count/stripLeadDivisor, 0), total-count)
}

// boardAt measures the window for a fixed number of +N lines.
func (s *Scene) boardAt(room, total, sel, marks int) stripBoard {
	count := min(s.stripFit(room, marks), total)
	first := stripFirst(total, count, sel)

	return stripBoard{first: first, count: count, above: first, below: total - first - count}
}

// planBoard chooses the window: which strips are drawn, and how many aircraft
// are left off each end.
//
// The arrangement is found by trying the cheapest first, because each +N line
// costs a strip. The whole list, then a line under it, then a line at both
// ends. Taking a strip away can only push the window further down the list and
// never back up it, so three passes settle the answer.
//
// A column with no room for a strip and a line keeps the strip. The line over
// the panel already says how many aircraft there are, so what is lost is a
// figure rather than an aeroplane.
func (s *Scene) planBoard(room, total, sel int) stripBoard {
	board := s.boardAt(room, total, sel, 0)

	for marks := 1; marks <= maxStripMarks; marks++ {
		if board.marks() < marks {
			break
		}

		next := s.boardAt(room, total, sel, marks)
		if next.count == 0 {
			break
		}

		board = next
	}

	return board
}

// boardIndex is the list index of the strip the selection is standing on, or
// notSelected when nothing on the board is picked.
//
// A pinned aircraft the filter is hiding has no strip and so no index. The
// panel above the board draws it and says FILTERED for it.
func (s *Scene) boardIndex() int {
	if s.selHidden || s.selIndex < 0 || s.selIndex >= len(s.icaos) {
		return notSelected
	}

	return s.selIndex
}

// drawStrips fills what is left of the column with the board: the row of field
// names, one strip per aircraft in the window, and a +N line at either end the
// window has cut.
func (s *Scene) drawStrips(col *layout, frame source.Frame) {
	plan, drawable := s.planStrips(col.left+stripEdge+stripPadX, col.right-stripPadX)
	if !drawable {
		return
	}

	head := s.boardHeadHeight()
	if !col.fits(head + s.halfStripHeight()) {
		return
	}

	s.drawBoardHead(col, plan)
	col.top += head

	s.drawBoard(col, plan, frame, s.planBoard(col.bottom-col.top, s.shownPlanes(), s.boardIndex()))
}

// drawBoardHead names the fields once, over the whole board.
//
// They are set at the top and every strip under them is read against the same
// words. Repeated on every strip they spent a third of the board saying what
// the board already showed, and carried on the selected strip alone they walked
// down the list with the cursor.
func (s *Scene) drawBoardHead(col *layout, plan stripPlan) {
	s.drawStripRules(col.dst, plan, image.Rect(col.left, col.top, col.right, col.top+s.boardHeadHeight()))

	for index := range fieldCount {
		if !plan.on[index] {
			continue
		}

		text.Draw(col.dst, s.faces.Small, plan.left[index], col.top+stripPadY, stripHeaders[index], s.pal.Data,
			text.WithSpacing(labelTracking))
	}
}

// drawBoard draws the window and the two lines that say what it cut.
func (s *Scene) drawBoard(col *layout, plan stripPlan, frame source.Frame, board stripBoard) {
	top := col.top

	if board.above > 0 {
		s.drawMoreStrip(col.dst, plan, top, board.above, aboveSuffix)
		top += s.halfStripHeight()
	}

	top = s.drawBoardStrips(col, plan, frame, board, top)

	if board.below > 0 {
		s.drawMoreStrip(col.dst, plan, top, board.below, moreSuffix)
	}
}

// drawBoardStrips draws the window's own strips and returns the y under the
// last of them.
//
// It walks the fleet rather than indexing into it, because the strips are the
// aircraft that passed the filter and nothing holds those in a list of their
// own: the ICAO index has their identities but not their readings. The walk
// stops at the bottom of the window, so a busy scope does not scan eighty
// aircraft to draw seventeen strips.
func (s *Scene) drawBoardStrips(
	col *layout, plan stripPlan, frame source.Frame, board stripBoard, top int,
) int {
	picked := s.boardIndex()
	index := 0

	for _, plane := range frame.Planes {
		if index >= board.first+board.count {
			break
		}

		if !s.visible(plane) {
			continue
		}

		if index >= board.first {
			s.drawHalfStrip(col, plan, frame, plane, top, stripPen{rank: index + 1, picked: index == picked})
			top += s.halfStripHeight()
		}

		index++
	}

	return top
}

// drawMoreStrip writes the muted line saying how many aircraft the window has
// left off one end of the board.
//
// It takes a strip's room and sits centred in it, so the board keeps its pitch
// whether the line at the top of it is a strip or a count of the ones above.
func (s *Scene) drawMoreStrip(dst *canvas.Canvas, plan stripPlan, top, hidden int, suffix string) {
	face := s.faces.Small
	line := top + (s.halfStripHeight()-lineHeight(face))/2

	pen := text.Draw(dst, face, plan.left[fieldIdent], line, morePrefix, s.pal.Muted)
	pen = drawBytes(dst, face, pen, line, s.count(hidden), s.pal.Muted)
	text.Draw(dst, face, pen, line, suffix, s.pal.Muted)
}

// drawHalfStrip draws one aircraft: a single row of figures with the field
// rules through it and a hairline under it.
//
// The selected one gets the accent bar down its left edge and nothing else. The
// board is a list to find a place in, and a strip lifted off the page or set in
// a brighter ink would be a second panel competing with the one above it. The
// index at the far left carries the rest of the mark: it is set in the reading
// ink on this strip and muted on every other.
func (s *Scene) drawHalfStrip(
	col *layout, plan stripPlan, frame source.Frame, plane airplane.Snapshot, top int, pen stripPen,
) {
	height := s.halfStripHeight()

	s.drawStripRules(col.dst, plan, image.Rect(col.left, top, col.right, top+height))

	if pen.picked {
		col.dst.FillRect(image.Rect(col.left, top, col.left+stripEdge, top+height), s.pal.Accent)
	}

	pen.dst = col.dst
	pen.plan = plan
	pen.value = top + stripHalfPadY
	pen.small = top + (height-lineHeight(s.faces.Small))/2

	s.drawStripFields(pen, plane, frame)
}

// drawStripFields sets one strip's seven fields, whichever of them the plan
// still has on it.
func (s *Scene) drawStripFields(pen stripPen, plane airplane.Snapshot, frame source.Frame) {
	s.drawStripIdent(pen, plane)
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

// drawStripIdent sets the identity field: where the aircraft is in the list,
// who it is, and the little model of it at the right end of the field.
//
// The index is the point of the field. A list that marked the selection and
// counted nothing left the operator pressing n with no way of knowing whether
// they were three aircraft in or thirty, which is the whole reason the board
// stopped promoting the selected strip to the top.
func (s *Scene) drawStripIdent(pen stripPen, plane airplane.Snapshot) {
	small := s.faces.Small
	left := pen.plan.left[fieldIdent]

	drawBytes(pen.dst, small, left, pen.small, s.rank(pen.rank), s.rankInk(pen.picked))

	left += measure(small, widestRank) + stripGap

	mark := text.Draw(pen.dst, s.faces.BodyBold, left, pen.value,
		clip(callsignOf(plane), maxCallsign), s.callsignInk(plane))

	s.drawHalfCodes(pen, mark+stripGap, plane)
	s.drawStripAttitude(pen, plane)
}

// rank writes a place in the list, padded to the two digits an index is written
// in. A list longer than ninety-nine runs to three, and the field has the room
// for it: see widestRank.
func (s *Scene) rank(value int) []byte {
	out := s.digits[:0]
	if value < decimalBase {
		out = append(out, '0')
	}

	return strconv.AppendInt(out, int64(value), decimalBase)
}

// rankInk is the colour an index takes: the reading ink on the strip the
// selection is standing on, muted on every other.
//
//nolint:revive // flag-parameter: picked names the strip, not a mode to branch deeper on.
func (s *Scene) rankInk(picked bool) color.RGBA {
	if picked {
		return s.pal.Ink
	}

	return s.pal.Muted
}

// drawHalfCodes writes a strip's hex beside its callsign, with the warning box
// in its place when the aircraft is squawking an emergency.
//
// The box takes the hex's room rather than following it. A strip has one line
// and the field is sized for the wider of the two, and of the pair the hex is
// the one worth giving up: it is the identity nobody reads while an aircraft is
// declaring one.
func (s *Scene) drawHalfCodes(pen stripPen, left int, plane airplane.Snapshot) {
	if plane.Emergency {
		s.drawEmergency(pen.dst, left, pen.small, plane)

		return
	}

	text.Draw(pen.dst, s.faces.Small, left, pen.small, clip(plane.ICAO, maxICAO), s.pal.Muted)
}

// drawEmergency puts the warning box on the line at left, and does nothing at
// all when the aircraft is not squawking one.
//
// It is a filled box rather than a coloured word because it is the one thing on
// the board that has to be seen without being looked for. The word used to be
// set in the accent, which is now the selected aircraft's own colour: an
// emergency on any other strip would have worn the mark of the one the operator
// had picked.
//
// Nothing needs the x past it. Both callers put the box at the end of what they
// are drawing: the panel after the squawk, a strip in the hex's own place.
func (s *Scene) drawEmergency(dst *canvas.Canvas, left, top int, plane airplane.Snapshot) {
	if !plane.Emergency {
		return
	}

	face := s.faces.Small
	width, height := text.Measure(face, emergencyText)
	box := image.Rect(left, top-emergencyPadY, left+width+2*emergencyPadX, top+height+emergencyPadY)

	dst.FillRect(box, s.pal.Warn)
	text.Draw(dst, face, left+emergencyPadX, top, emergencyText, emergencyInk)
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
// The index and the edge beside it already carry the selection, and the point of
// the model is that reading the board and reading the scope are the same act of
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
// with the climb or descent triangle before it.
//
// The triangle is on every strip because it costs no height at all, sitting
// beside the figure rather than under it, and a board scanned for who is coming
// down is a board where every strip has to answer. The rate itself is on the
// panel above, where there is one figure to read rather than twenty.
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

	drawBytes(pen.dst, s.faces.Body, left+vertMarker+vertGap, pen.value, s.thousands(plane.Altitude), band)
}

// drawStripSpeed sets the ground speed, which is the one figure on the board
// nothing else on screen carries.
func (s *Scene) drawStripSpeed(pen stripPen, plane airplane.Snapshot) {
	if !pen.plan.on[fieldSpeed] {
		return
	}

	drawBytes(pen.dst, s.faces.Body, pen.plan.left[fieldSpeed], pen.value, s.speed(plane.Velocity), s.pal.Ink)
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

	face := s.faces.Body
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

	face := s.faces.Body
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
		seenBucket(now.Sub(plane.LastUpdate)), s.pal.Muted)
}

// drawStripStatus writes where the selection is sitting and how many aircraft
// are being tracked, on the line above the panel and right-aligned on it.
//
// It is set in the data colour because it is a reading about the machine rather
// than about any aeroplane, which is the same rule that puts the source label
// and the clocks in cyan up in the header. The filter's own tag is the one part
// that is not: a selection nothing on screen shows is a caution, and amber is
// what a panel says that with.
func (s *Scene) drawStripStatus(col *layout, sel selection, shown, total int) {
	face := s.faces.Small
	if measureTracked(face, widestStatus) > col.right-col.left {
		return
	}

	top := col.top
	pen := col.right - s.statusWidth(sel, shown, total)

	pen = text.Draw(col.dst, face, pen, top, selPrefix, s.pal.Data, text.WithSpacing(labelTracking))
	pen = s.drawStatusRank(col.dst, pen, top, s.selRank(sel))

	if sel.hidden {
		pen = text.Draw(col.dst, face, pen, top, selFilteredTag, s.pal.Caution, text.WithSpacing(labelTracking))
	}

	pen = text.Draw(col.dst, face, pen, top, stripsSeparator, s.pal.Data, text.WithSpacing(labelTracking))

	if s.filter.active() {
		pen = drawBytesTracked(col.dst, face, pen, top, s.shownCount(shown), s.pal.Data)
		pen = text.Draw(col.dst, face, pen, top, stripsOfInfix, s.pal.Data, text.WithSpacing(labelTracking))
	}

	pen = drawBytesTracked(col.dst, face, pen, top, s.count(total), s.pal.Data)
	text.Draw(col.dst, face, pen, top, stripsCountSuffix, s.pal.Data, text.WithSpacing(labelTracking))
}

// selRank is the selection's place in the list, counted from one, or zero when
// no strip on the board is under it.
func (s *Scene) selRank(sel selection) int {
	if !sel.found || sel.hidden {
		return 0
	}

	return s.boardIndex() + 1
}

// drawStatusRank writes the index the line opens with, or the pair of dashes
// that stands for a selection no strip is under.
func (s *Scene) drawStatusRank(dst *canvas.Canvas, left, top, rank int) int {
	face := s.faces.Small

	if rank <= 0 {
		return text.Draw(dst, face, left, top, selNone, s.pal.Data, text.WithSpacing(labelTracking))
	}

	return drawBytesTracked(dst, face, left, top, s.rank(rank), s.pal.Data)
}

// statusWidth is how wide the status line will come out.
//
// The numbers are counted rather than formatted. Three of them have to be
// measured before the first is drawn so the run can be right-aligned, and only
// one of them can be in a scratch buffer at a time.
func (s *Scene) statusWidth(sel selection, shown, total int) int {
	face := s.faces.Small

	width := measureTracked(face, selPrefix) +
		trackedWidth(face, max(digitsIn(s.selRank(sel)), len(selNone))) +
		measureTracked(face, stripsSeparator) +
		trackedWidth(face, digitsIn(total)) +
		measureTracked(face, stripsCountSuffix)

	if sel.hidden {
		width += measureTracked(face, selFilteredTag)
	}

	if s.filter.active() {
		width += trackedWidth(face, digitsIn(shown)) + measureTracked(face, stripsOfInfix)
	}

	return width
}

// digitsIn is how many characters the decimal form of a count takes, counted
// rather than formatted so a run of numbers can be measured without any of them
// passing through the scene's scratch buffers.
func digitsIn(value int) int {
	digits := 1

	for value >= decimalBase {
		value /= decimalBase
		digits++
	}

	return digits
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
