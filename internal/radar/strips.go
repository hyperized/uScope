package radar

import (
	"image"
	"image/color"
	"strconv"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uScope/internal/source"
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
	emergencyText  = "EMERGENCY"
	emergencyShort = "EMG"
	emergencyPadX  = 3
	emergencyPadY  = 2

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

	// fieldDir is the inbound or outbound call, which only the bare 3D view's
	// board carries. It is last so the full scope's fields keep the indices
	// and the order they had before it existed.
	fieldDir

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
	fieldDir:   "DIR",
}

// The two headers a board narrowed past its codes or its bearing uses instead
// of the full ones. A header naming a reading the strips no longer carry would
// send the eye looking for it.
const (
	identHeadShort = "CALLSIGN"
	rangeHeadShort = "DIST NM"
)

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
	widestDir      = dirOutText

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
// whole field, and otherwise one part of a field that stays: the little
// aeroplane or the hex beside the callsign, or the bearing beside the
// distance.
type stripDrop struct {
	field   int
	att     bool
	codes   bool
	bearing bool
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

// boardDropOrder is stripDropOrder for the bare 3D view's board, which is a
// quarter of a 16:9 canvas wide and has DIR to fit besides.
//
// It gives up two half fields the scope's column never has to: the hex beside
// the callsign, and the bearing beside the distance. Both go before the
// speed. The hex is an identity for whoever already has a callsign to read,
// and the bearing is what the picture beside the board shows by where the
// aeroplane is, while the speed is on nothing else in the picture. DIR is not
// in the list: it is the one reading the board exists to add.
//
//nolint:gochecknoglobals // an order is data, and an array cannot be const.
var boardDropOrder = [...]stripDrop{
	{field: fieldPos},
	{field: fieldSeen},
	{field: fieldTrack},
	{att: true},
	{codes: true},
	{bearing: true},
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
	// ident field, codes whether the hex or the warning box follows the
	// callsign, and bearing whether the range field carries the bearing after
	// the distance. They are the things dropped without a field going with
	// them, and the full scope's column only ever drops the first.
	att     bool
	codes   bool
	bearing bool

	// board is whether this is the bare 3D view's board rather than the
	// scope's column: DIR is on it unless the list is grouped, and no strip
	// is marked as the selection.
	board bool

	// scale is the layout's scale the plan was made at, which the gaps between
	// fields and the attitude cell are multiplied by. Zero reads as one, the
	// way it does on a layout.
	scale int
}

// gap is the room between two fields, and cell the attitude cell's width, at
// the plan's scale.
func (p *stripPlan) gap() int  { return stripGap * max(p.scale, 1) }
func (p *stripPlan) cell() int { return attCell * max(p.scale, 1) }

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

	return sum + max(p.count()-1, 0)*p.gap()
}

// stripMeasure is the room each field needs, with the two half fields a board
// can give up measured apart so the plan can add them back while it has them.
type stripMeasure struct {
	// widths is each field at its narrowest: the ident with no hex after the
	// callsign and the range with no bearing after the distance.
	widths [fieldCount]int

	// codes is what the hex or the warning box adds to the ident, and bearing
	// what the bearing adds to the range, gaps included.
	codes   int
	bearing int

	// heads is each field's full header, and identShort and rangeShort the two
	// it is swapped for once the half field it names has gone.
	heads      [fieldCount]int
	identShort int
	rangeShort int
}

// header is the word over a field on this plan.
func (p *stripPlan) header(index int) string {
	switch {
	case index == fieldIdent && !p.codes:
		return identHeadShort
	case index == fieldRange && !p.bearing:
		return rangeHeadShort
	default:
		return stripHeaders[index]
	}
}

// headWidth is how wide header(index) comes out.
func (p *stripPlan) headWidth(sizes *stripMeasure, index int) int {
	switch {
	case index == fieldIdent && !p.codes:
		return sizes.identShort
	case index == fieldRange && !p.bearing:
		return sizes.rangeShort
	default:
		return sizes.heads[index]
	}
}

// size copies the measured widths onto the plan, adding the half fields it
// still has and then the little aeroplane's cell to the ident field while that
// is still being drawn.
//
// Every field is at least as wide as the word over it. TRK and SEEN are the
// two that would otherwise come out narrower than their own headers, and a
// header running into the rule beside it says less than a wider field does.
func (p *stripPlan) size(sizes *stripMeasure) {
	p.width = sizes.widths

	if p.codes {
		p.width[fieldIdent] += sizes.codes
	}

	if p.bearing {
		p.width[fieldRange] += sizes.bearing
	}

	for index := range fieldCount {
		p.width[index] = max(p.width[index], p.headWidth(sizes, index))
	}

	if p.att {
		p.width[fieldIdent] += p.gap() + p.cell()
	}
}

// give drops one entry of the drop order and measures the plan again.
func (p *stripPlan) give(entry stripDrop, sizes *stripMeasure) {
	switch {
	case entry.att:
		p.att = false
	case entry.codes:
		p.codes = false
	case entry.bearing:
		p.bearing = false
	default:
		p.on[entry.field] = false
	}

	p.size(sizes)
}

// place spreads the fields across the width, so the first starts on the left
// edge and the last finishes on the right one.
//
// The slack is shared by interpolating on the field's position rather than by
// adding a fixed gap, which is how the compact rows shared theirs: an integer
// gap leaves a remainder, and the last field would then stop a few pixels short
// of the edge it is meant to meet.
//
// Both the share and the hairline between two fields are rounded to whole
// design pixels. A share worked out on the doubled slack rounds differently
// from the same share worked out once and doubled, and on a canvas twice the
// size that put every field after the first a pixel away from where the
// smaller board has it.
func (p *stripPlan) place(left, available int) {
	slack := max(available-p.total(), 0)
	gaps := max(p.count()-1, 1)
	unit := max(p.scale, 1)
	used, seen, prevRight := 0, 0, left

	for index := range fieldCount {
		if !p.on[index] {
			continue
		}

		p.left[index] = left + used + seen*p.gap() + slack*seen/gaps/unit*unit

		if seen > 0 {
			p.rule[index] = (prevRight + p.left[index]) / (2 * unit) * unit
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
func (s *Scene) stripWidths(col *layout) stripMeasure {
	small, body := s.faces.Small, s.faces.Body
	gap := col.px(stripGap)

	icao := col.measure(small, widestICAO)
	warning := col.measure(small, emergencyText) + 2*col.px(emergencyPadX)

	var sizes stripMeasure

	sizes.widths[fieldIdent] = col.measure(small, widestRank) + gap + col.measure(s.faces.BodyBold, widestCallsign)
	sizes.codes = gap + max(icao, warning)

	sizes.widths[fieldLevel] = col.px(vertMarker) + col.px(vertGap) + col.measure(body, widestLevel)
	sizes.widths[fieldSpeed] = col.measure(body, widestSpeed)
	sizes.widths[fieldTrack] = col.measure(body, widestTrack) + s.arrowWidth(col)
	sizes.widths[fieldRange] = col.measure(body, widestDistance)
	sizes.bearing = gap + col.measure(body, widestBearing) + s.arrowWidth(col)
	sizes.widths[fieldPos] = col.measure(small, widestPlace+" "+widestPlace)
	sizes.widths[fieldSeen] = col.measure(small, seenNowText)
	sizes.widths[fieldDir] = col.measure(body, widestDir)

	for index := range fieldCount {
		sizes.heads[index] = col.measureTracked(small, stripHeaders[index])
	}

	sizes.identShort = col.measureTracked(small, identHeadShort)
	sizes.rangeShort = col.measureTracked(small, rangeHeadShort)

	return sizes
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
func (s *Scene) planStrips(col *layout, left, right int) (stripPlan, bool) {
	return s.fitStrips(col, left, right, newStripPlan(col.scale), stripDropOrder[:])
}

// planBoardStrips is planStrips for the bare 3D view's board: DIR on, and the
// board's own drop order. See boardDropOrder.
//
// A grouped board has no DIR. The section heading over every strip already
// says IN or OUT, and the room the column gave up for it goes back to the
// drop order, so a field it had cost the board can come back.
func (s *Scene) planBoardStrips(col *layout, left, right int) (stripPlan, bool) {
	plan := newStripPlan(col.scale)
	plan.on[fieldDir], plan.board = !s.groups.on, true

	return s.fitStrips(col, left, right, plan, boardDropOrder[:])
}

// newStripPlan is a plan with every field the scope's column can carry on it,
// before anything has been measured or given up.
func newStripPlan(scale int) stripPlan {
	plan := stripPlan{scale: scale, att: true, codes: true, bearing: true}

	for index := range fieldCount {
		plan.on[index] = index != fieldDir
	}

	return plan
}

// fitStrips gives up the entries of order one at a time until plan fits
// between left and right, and places what is left.
func (s *Scene) fitStrips(col *layout, left, right int, plan stripPlan, order []stripDrop) (stripPlan, bool) {
	if s.faces.Small == nil || s.faces.Body == nil || s.faces.BodyBold == nil || s.faces.Large == nil {
		return plan, false
	}

	sizes := s.stripWidths(col)
	available := right - left

	plan.size(&sizes)

	for _, entry := range order {
		if plan.total() <= available {
			break
		}

		plan.give(entry, &sizes)
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
	lay  *layout
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
func (s *Scene) halfStripHeight(col *layout) int {
	return 2*col.px(stripHalfPadY) + col.lineHeight(s.faces.Body)
}

func (s *Scene) boardHeadHeight(col *layout) int {
	return 2*col.px(stripPadY) + col.lineHeight(s.faces.Small)
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
func (s *Scene) stripFit(col *layout, room, marks int) int {
	height := s.halfStripHeight(col)

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
func (s *Scene) boardAt(col *layout, room, total, sel, marks int) stripBoard {
	count := min(s.stripFit(col, room, marks), total)
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
func (s *Scene) planBoard(col *layout, room, total, sel int) stripBoard {
	board := s.boardAt(col, room, total, sel, 0)

	for marks := 1; marks <= maxStripMarks; marks++ {
		if board.marks() < marks {
			break
		}

		next := s.boardAt(col, room, total, sel, marks)
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
	plan, drawable := s.planStrips(col, stripsLeft(col), col.right-col.px(stripPadX))
	if !drawable {
		return
	}

	s.drawPlannedStrips(col, frame, plan, s.boardIndex())
}

// stripsLeft is where the fields of a strip start: past the accent bar every
// strip leaves room for, and the air after it.
func stripsLeft(col *layout) int { return col.left + col.px(stripEdge) + col.px(stripPadX) }

// drawPlannedStrips draws a board already planned: the row of field names, then
// the window, kept so sel is in view.
func (s *Scene) drawPlannedStrips(col *layout, frame source.Frame, plan stripPlan, sel int) {
	head := s.boardHeadHeight(col)
	if !col.fits(head + s.halfStripHeight(col)) {
		return
	}

	s.drawBoardHead(col, plan)
	col.top += head

	if s.groups.on {
		s.drawGroups(col, plan, frame, sel)

		return
	}

	s.drawBoard(col, plan, frame, s.planBoard(col, col.bottom-col.top, s.shownPlanes(), sel))
}

// drawBoardHead names the fields once, over the whole board.
//
// They are set at the top and every strip under them is read against the same
// words. Repeated on every strip they spent a third of the board saying what
// the board already showed, and carried on the selected strip alone they walked
// down the list with the cursor.
func (s *Scene) drawBoardHead(col *layout, plan stripPlan) {
	s.drawStripRules(col, plan, image.Rect(col.left, col.top, col.right, col.top+s.boardHeadHeight(col)))

	for index := range fieldCount {
		if !plan.on[index] {
			continue
		}

		col.tracked(s.faces.Small, plan.left[index], col.top+col.px(stripPadY), plan.header(index), s.pal.Data)
	}
}

// drawBoard draws the window and the two lines that say what it cut.
func (s *Scene) drawBoard(col *layout, plan stripPlan, frame source.Frame, board stripBoard) {
	top := col.top

	if board.above > 0 {
		s.drawMoreStrip(col, plan, top, board.above, aboveSuffix)
		top += s.halfStripHeight(col)
	}

	top = s.drawBoardStrips(col, plan, frame, board, top)

	if board.below > 0 {
		s.drawMoreStrip(col, plan, top, board.below, moreSuffix)
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
	// The board in the bare 3D view marks nothing: that view draws no ring and
	// no card, so a mark here would point at a selection nothing else shows.
	picked := notSelected
	if !plan.board {
		picked = s.boardIndex()
	}

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
			top += s.halfStripHeight(col)
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
func (s *Scene) drawMoreStrip(col *layout, plan stripPlan, top, hidden int, suffix string) {
	face := s.faces.Small
	line := top + (s.halfStripHeight(col)-col.lineHeight(face))/2

	pen := col.text(face, plan.left[fieldIdent], line, morePrefix, s.pal.Muted)
	pen = col.drawBytes(face, pen, line, s.count(hidden), s.pal.Muted)
	col.text(face, pen, line, suffix, s.pal.Muted)
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
	height := s.halfStripHeight(col)

	s.drawStripRules(col, plan, image.Rect(col.left, top, col.right, top+height))

	if pen.picked {
		col.dst.FillRect(image.Rect(col.left, top, col.left+col.px(stripEdge), top+height), s.pal.Accent)
	}

	pen.lay = col
	pen.plan = plan
	pen.value = top + col.px(stripHalfPadY)
	pen.small = top + (height-col.lineHeight(s.faces.Small))/2

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
	s.drawStripDir(pen, frame.Corridors, plane)
}

// drawStripRules draws the hairline along the bottom of a strip and the
// verticals between its fields.
//
// The verticals run the whole height of the strip rather than stopping at the
// type, which is what makes a field a field: the eye follows a rule down the
// board and finds the same reading on every aircraft in the same place.
func (s *Scene) drawStripRules(col *layout, plan stripPlan, box image.Rectangle) {
	rule := col.px(ruleHeight)

	col.dst.FillRect(image.Rect(box.Min.X, box.Max.Y-rule, box.Max.X, box.Max.Y), s.pal.Rule)

	for index := range fieldCount {
		if !plan.on[index] || index == fieldIdent {
			continue
		}

		col.dst.FillRect(image.Rect(plan.rule[index], box.Min.Y, plan.rule[index]+rule, box.Max.Y), s.pal.Rule)
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

	pen.lay.drawBytes(small, left, pen.small, s.rank(pen.rank), s.rankInk(pen.picked))

	left += pen.lay.measure(small, widestRank) + pen.lay.px(stripGap)

	mark := pen.lay.text(s.faces.BodyBold, left, pen.value, clip(callsignOf(plane), maxCallsign), s.callsignInk(plane))

	if pen.plan.codes {
		s.drawHalfCodes(pen, mark+pen.lay.px(stripGap), plane)
	} else {
		s.drawShortEmergency(pen, mark+pen.lay.px(stripGap), plane)
	}

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
		s.drawEmergency(pen.lay, left, pen.small, plane)

		return
	}

	pen.lay.text(s.faces.Small, left, pen.small, clip(plane.ICAO, maxICAO), s.pal.Muted)
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
func (s *Scene) drawEmergency(lay *layout, left, top int, plane airplane.Snapshot) {
	if !plane.Emergency {
		return
	}

	s.drawWarningBox(lay, left, top, emergencyText)
}

// drawWarningBox is the filled red box with word knocked out of it, which is
// what an emergency looks like wherever it is drawn.
func (s *Scene) drawWarningBox(lay *layout, left, top int, word string) {
	face := s.faces.Small
	padX, padY := lay.px(emergencyPadX), lay.px(emergencyPadY)
	width, height := text.Measure(face, word, lay.glyphs())
	box := image.Rect(left, top-padY, left+width+2*padX, top+height+padY)

	lay.dst.FillRect(box, s.pal.Warn)
	lay.text(face, left+padX, top, word, emergencyInk)
}

// drawShortEmergency is the warning a strip with no room for its codes still
// carries: EMG in the box, after the callsign, when the callsign leaves room
// for it inside the field.
//
// The field is sized for the widest callsign there is and not for a box after
// it, so an eight-character callsign squawking an emergency loses the box.
// Real ones are seven characters or fewer and keep it. Reserving the room on
// every strip for the one in a thousand that needs it would have cost the
// board its speed column.
func (s *Scene) drawShortEmergency(pen stripPen, left int, plane airplane.Snapshot) {
	if !plane.Emergency {
		return
	}

	width := pen.lay.measure(s.faces.Small, emergencyShort) + 2*pen.lay.px(emergencyPadX)
	if left+width > pen.plan.left[fieldIdent]+pen.plan.width[fieldIdent] {
		return
	}

	s.drawWarningBox(pen.lay, left, pen.small, emergencyShort)
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
		pen.plan.left[fieldIdent]+pen.plan.width[fieldIdent]-pen.plan.cell()/2,
		pen.value+pen.lay.lineHeight(s.faces.Body)/2,
	)

	s.drawShape3(pen.lay.dst, newCellCamera3(centre, pen.lay.unit()), plane, point3{}, centre,
		s.aircraftColour(plane), pen.lay.unit())
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
		s.drawVertMarker(pen.lay, left, pen.value, plane.VertRate, band)
	}

	pen.lay.drawBytes(s.faces.Body, left+pen.lay.px(vertMarker)+pen.lay.px(vertGap), pen.value,
		s.thousands(plane.Altitude), band)
}

// drawStripSpeed sets the ground speed, which is the one figure on the board
// nothing else on screen carries.
func (s *Scene) drawStripSpeed(pen stripPen, plane airplane.Snapshot) {
	if !pen.plan.on[fieldSpeed] {
		return
	}

	pen.lay.drawBytes(s.faces.Body, pen.plan.left[fieldSpeed], pen.value, s.speed(plane.Velocity), s.pal.Ink)
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
		pen.lay.text(face, left, pen.value, trackUnknown, s.pal.Muted)

		return
	}

	mark := pen.lay.drawBytes(face, left, pen.value, s.degrees(plane.Heading), s.pal.Ink)
	s.drawArrow(pen.lay, mark+pen.lay.px(arrowGap), pen.value+pen.lay.lineHeight(face)/2, plane.Heading, s.pal.Ink)
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
	mark := pen.lay.drawBytes(face, left, pen.value, s.distance(away), s.pal.Ink) + pen.lay.px(stripGap)

	if !pen.plan.bearing {
		return
	}

	bearing, known := bearingTo(receiver, plane)
	if !known {
		pen.lay.text(face, mark, pen.value, trackUnknown, s.pal.Muted)

		return
	}

	mark = pen.lay.drawBytes(face, mark, pen.value, s.degrees(bearing), s.pal.Ink)
	s.drawArrow(pen.lay, mark+pen.lay.px(arrowGap), pen.value+pen.lay.lineHeight(face)/2, bearing, s.pal.Ink)
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
		pen.lay.text(face, left, pen.small, detailUnknown, s.pal.Muted)

		return
	}

	// One half at a time: both share the scene's coordinate buffer, so
	// formatting the second would overwrite the first.
	mark := pen.lay.drawBytes(face, left, pen.small, s.place(plane.Latitude, 'N', 'S'), s.pal.Muted)
	pen.lay.drawBytes(face, mark+pen.lay.glyphWidth(face), pen.small, s.place(plane.Longitude, 'E', 'W'), s.pal.Muted)
}

// drawStripSeen writes how long ago the aircraft was last heard, in the coarse
// buckets seenBucket names.
func (s *Scene) drawStripSeen(pen stripPen, now time.Time, plane airplane.Snapshot) {
	if !pen.plan.on[fieldSeen] {
		return
	}

	pen.lay.text(s.faces.Small, pen.plan.left[fieldSeen], pen.small, seenBucket(now.Sub(plane.LastUpdate)), s.pal.Muted)
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
	if col.measureTracked(face, widestStatus) > col.right-col.left {
		return
	}

	top := col.top
	pen := col.right - s.statusWidth(col, sel, shown, total)

	pen = col.tracked(face, pen, top, selPrefix, s.pal.Data)
	pen = s.drawStatusRank(col, pen, top, s.selRank(sel))

	if sel.hidden {
		pen = col.tracked(face, pen, top, selFilteredTag, s.pal.Caution)
	}

	pen = col.tracked(face, pen, top, stripsSeparator, s.pal.Data)

	if s.filter.active() {
		pen = col.drawBytesTracked(face, pen, top, s.shownCount(shown), s.pal.Data)
		pen = col.tracked(face, pen, top, stripsOfInfix, s.pal.Data)
	}

	pen = col.drawBytesTracked(face, pen, top, s.count(total), s.pal.Data)
	col.tracked(face, pen, top, stripsCountSuffix, s.pal.Data)
}

// selRank is the selection's place in the list, counted from one, or zero when
// no strip on the board is under it. On a grouped list it is the place in its
// own section, the number its strip carries, so the line and the strip agree.
func (s *Scene) selRank(sel selection) int {
	if !sel.found || sel.hidden {
		return 0
	}

	return s.rowRank(s.boardIndex())
}

// drawStatusRank writes the index the line opens with, or the pair of dashes
// that stands for a selection no strip is under.
func (s *Scene) drawStatusRank(col *layout, left, top, rank int) int {
	face := s.faces.Small

	if rank <= 0 {
		return col.tracked(face, left, top, selNone, s.pal.Data)
	}

	return col.drawBytesTracked(face, left, top, s.rank(rank), s.pal.Data)
}

// statusWidth is how wide the status line will come out.
//
// The numbers are counted rather than formatted. Three of them have to be
// measured before the first is drawn so the run can be right-aligned, and only
// one of them can be in a scratch buffer at a time.
func (s *Scene) statusWidth(col *layout, sel selection, shown, total int) int {
	face := s.faces.Small

	width := col.measureTracked(face, selPrefix) +
		col.trackedWidth(face, max(digitsIn(s.selRank(sel)), len(selNone))) +
		col.measureTracked(face, stripsSeparator) +
		col.trackedWidth(face, digitsIn(total)) +
		col.measureTracked(face, stripsCountSuffix)

	if sel.hidden {
		width += col.measureTracked(face, selFilteredTag)
	}

	if s.filter.active() {
		width += col.trackedWidth(face, digitsIn(shown)) + col.measureTracked(face, stripsOfInfix)
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

// The words DIR is written in. They are the body face's figures rather than
// arrows, because someone reading the board from across a room has a legend saying
// what IN and OUT mean and no legend for which way an arrow on a strip points.
const (
	dirInText      = "IN"
	dirOutText     = "OUT"
	dirUnknownText = "---"
)

// drawStripDir writes whether the aircraft looks to be arriving at the
// corridor airfield or leaving it, in the colour its tube will be drawn in
// once the track is over.
//
// The colours are the ones the corridors use, not new ones, so the board and
// the tubes in the picture beside it read as one key: a cyan IN on a strip is
// the aircraft that will leave a cyan tube behind it.
func (s *Scene) drawStripDir(pen stripPen, corridors source.Corridors, plane airplane.Snapshot) {
	if !pen.plan.on[fieldDir] {
		return
	}

	class := corridors.Classify(plane)

	pen.lay.text(s.faces.Body, pen.plan.left[fieldDir], pen.value, dirWord(class), s.classColour(class))
}

// dirWord is what DIR says for a class. Transit is the muted dashes, which
// classColour already draws in the muted grey the tubes give it.
func dirWord(class source.TrackClass) string {
	switch class {
	case source.TrackInbound:
		return dirInText
	case source.TrackOutbound:
		return dirOutText
	case source.TrackTransit:
		fallthrough
	default:
		return dirUnknownText
	}
}
