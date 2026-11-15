package radar

import (
	"image"
	"math"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/text"
)

// The selected aircraft's panel at the top of the column: the radar data block
// set large.
//
// It is the same object as the tag hanging off the ring on the scope. The tag
// is three lines in the body face because it has to sit on a field of traffic
// without covering it; the panel is the block a controller would have in front
// of them, with the abbreviations spelled out underneath. Reading the two is
// one habit rather than two.
const (
	// cardLabel names the block, and cardFiltered follows it while the filter
	// is hiding the aeroplane the block is about. The pin survives the filter,
	// so the panel keeps drawing the aircraft and says why it has no strip in
	// the list under it.
	cardLabel    = "SELECTED"
	cardFiltered = " · FILTERED"

	// cardRule is the accent hairline down the left edge of the block and
	// cardPadX the air between it and the type.
	//
	// One pixel, where the strip below it wears three. The rule here is a
	// margin mark on a block that is already the only thing at the top of the
	// column; the strip's edge has to be found in a list of twenty.
	cardRule = 1
	cardPadX = 12

	// cardLead is the air between two rows of the block and cardCellGap the
	// air between two figures on one row.
	cardLead    = 6
	cardCellGap = 14

	// cardScale is how many times over the large face is drawn for the
	// callsign, and cardPlainScale the scale it drops to on a short column.
	// Two is the one place in the scene where a face is scaled at all: the
	// callsign is what the panel is about, and at arm's length on a five-inch
	// panel thirty-two pixels is not enough to read it across a cockpit.
	cardScale      = 2
	cardPlainScale = 1

	// cardRows is how many rows of figures the block carries, cardOneRow how
	// many are left when the column is too short for both, and cardTopRow the
	// first of them.
	cardRows   = 2
	cardOneRow = 1
	cardTopRow = 0

	// The words under the five figures. They are the tag's own abbreviations
	// rather than full names, because the figures over them are the tag's
	// figures: a block reading LEVEL over 024 says the same thing twice.
	cardLevelLabel = "LEVEL"
	cardSpeedLabel = "GS"
	cardTrackLabel = "TRK"
	cardRangeLabel = "DIST"
	cardBearLabel  = "BRG"

	// The plain-language line under the block, in the order it reads: the
	// level and what it is doing, then where the aeroplane is, then how long
	// ago it was last heard.
	//
	// It exists because everything above it is controller shorthand. 024 is
	// two thousand four hundred feet to anyone who has worked a radar and a
	// three-digit number to everyone else, and a panel that cannot be read
	// cold is a panel with a manual.
	plainFeet       = " FT "
	plainLevelWord  = "LEVEL"
	plainClimbing   = "CLIMBING "
	plainDescending = "DESCENDING "
	plainRate       = " FT/MIN"
	plainSeparator  = " · "
	plainSeen       = "SEEN "
	plainSlash      = " / "
)

// The figures on the block, in the order they are read.
const (
	cardLevel = iota
	cardSpeed
	cardTrack
	cardRange
	cardBearing

	// cardFigures is how many there are, which is what every loop over them
	// runs to. The first three are the top row and the last two the second,
	// which is the row the column gives up first.
	cardFigures
)

// cardRowStart is the first figure of each row, with the end of the last row
// on the tail so a row's figures are always cardRowStart[row] to
// cardRowStart[row+1].
//
//nolint:gochecknoglobals // a row split is data, and an array cannot be const.
var cardRowStart = [cardRows + 1]int{0, cardRange, cardFigures}

// cardLabels is the word under each figure.
//
//nolint:gochecknoglobals // a label row is data, and an array cannot be const.
var cardLabels = [cardFigures]string{
	cardLevel:   cardLevelLabel,
	cardSpeed:   cardSpeedLabel,
	cardTrack:   cardTrackLabel,
	cardRange:   cardRangeLabel,
	cardBearing: cardBearLabel,
}

// The widest thing each figure can be asked to hold, and the widest each
// clause of the plain line can be.
//
// Sizing from the worst case is what keeps the block still, the same rule the
// flight strips are laid out by: a figure measured from its own contents would
// move every time an aircraft climbed through ten thousand feet, and a panel
// whose words walk sideways is harder to read than one wasting a few pixels.
const (
	widestCardLevel = "999" + tagClimb
	widestCardSpeed = "999"
	widestCardTrack = "359"
	widestCardRange = "999.9"
	widestCardBear  = "359"

	widestPlainLevel = "999,999" + plainFeet + plainDescending + "99,999" + plainRate
	widestPlainPlace = plainSeparator + widestPlace + plainSlash + widestPlace
	widestPlainSeen  = plainSeparator + plainSeen + seenNowText
)

// cardPlan is the shape the block takes in the room the column has.
type cardPlan struct {
	// scale is the callsign's scale in the large face, rows how many rows of
	// figures are drawn, and plain whether the sentence under the block is.
	scale int
	rows  int
	plain bool

	// left is each figure's own left edge and width how much room it has. The
	// figures are placed from the left rather than spread to the block's right
	// edge, because each carries a word under it and a figure starting
	// somewhere else from the word naming it would read as belonging to the
	// one before.
	left  [cardFigures]int
	width [cardFigures]int
}

// cardShapes is every shape the block can take, biggest first.
//
// The order is what the panel gives up as the column shortens. The plain line
// goes first: it is the one part of the block that repeats what is already
// above it, so losing it costs a reading nobody who can read the shorthand
// needs. Then the callsign's second scale, because a callsign at thirty-two
// pixels is still the largest thing in the column. The second row of figures
// is last, and losing it is what finally costs the panel a fact: the range and
// the bearing are then only on the aircraft's own strip.
//
//nolint:gochecknoglobals // a shrink order is data, and an array cannot be const.
var cardShapes = [...]cardPlan{
	{scale: cardScale, rows: cardRows, plain: true},
	{scale: cardScale, rows: cardRows},
	{scale: cardPlainScale, rows: cardRows},
	{scale: cardPlainScale, rows: cardOneRow},
}

// cardWidths is the room each figure needs, which is the widest thing that can
// land in it or the word under it, whichever is wider.
//
// The bearing carries the needle as well as the figure. TRK does not, although
// the strips' own track field does: the block already has one needle on it, a
// second beside a figure eight pixels away would read as a pair of directions
// to reconcile rather than as one bearing to fly.
func (s *Scene) cardWidths(col *layout) [cardFigures]int {
	large, small := s.faces.Large, s.faces.Small

	widths := [cardFigures]int{
		cardLevel:   col.measure(large, widestCardLevel),
		cardSpeed:   col.measure(large, widestCardSpeed),
		cardTrack:   col.measure(large, widestCardTrack),
		cardRange:   col.measure(large, widestCardRange),
		cardBearing: col.measure(large, widestCardBear) + s.arrowWidth(col),
	}

	for index := range cardFigures {
		widths[index] = max(widths[index], col.measureTracked(small, cardLabels[index]))
	}

	return widths
}

// cardIdentWidth is the room the callsign and the codes beside it need at this
// scale.
//
// The codes are two lines rather than one because the callsign is two lines
// tall, and a hex and a squawk stacked beside it fill the block's first row
// instead of leaving a hole under them. The warning box takes the squawk's own
// line, after the code: an aircraft squawking 7700 is squawking a code, and
// the box is what that code means.
func (s *Scene) cardIdentWidth(col *layout, scale int) int {
	body, small := s.faces.Body, s.faces.Small

	warning := col.measure(small, emergencyText) + 2*col.px(emergencyPadX)
	codes := max(
		col.measure(body, widestICAO),
		col.measure(body, widestSquawk)+col.px(stripGap)+warning,
	)

	return scale*col.measure(s.faces.Large, widestCallsign) + col.px(cardCellGap) + codes
}

// cardRowWidth is how wide one row of figures is, the gaps between them
// included.
func cardRowWidth(row, gap int, widths [cardFigures]int) int {
	sum := 0

	for index := cardRowStart[row]; index < cardRowStart[row+1]; index++ {
		sum += widths[index]
	}

	return sum + (cardRowStart[row+1]-cardRowStart[row]-1)*gap
}

// place puts each figure of each drawn row at its own left edge, gap pixels
// apart.
func (p *cardPlan) place(left, gap int, widths [cardFigures]int) {
	p.width = widths

	for row := range p.rows {
		pen := left

		for index := cardRowStart[row]; index < cardRowStart[row+1]; index++ {
			p.left[index] = pen
			pen += widths[index] + gap
		}
	}
}

// width is the least room the block needs: the widest of its identity row and
// the rows of figures under it, with the rule and its margin in front.
func (s *Scene) cardWidth(col *layout, plan cardPlan, widths [cardFigures]int) int {
	need := s.cardIdentWidth(col, plan.scale)

	for row := range plan.rows {
		need = max(need, cardRowWidth(row, col.px(cardCellGap), widths))
	}

	return col.px(cardRule) + col.px(cardPadX) + need
}

// figureRowHeight is one row of figures and the word under it, and
// identHeight the callsign's own row at this scale.
func (s *Scene) figureRowHeight(col *layout) int {
	return col.lineHeight(s.faces.Large) + col.lineHeight(s.faces.Small)
}

func (s *Scene) identHeight(col *layout, scale int) int {
	return scale * col.lineHeight(s.faces.Large)
}

// blockHeight is the accent rule's own length: the identity row and the rows
// of figures under it.
func (s *Scene) blockHeight(col *layout, plan cardPlan) int {
	return s.identHeight(col, plan.scale) + plan.rows*(col.px(cardLead)+s.figureRowHeight(col))
}

// cardHeight is the whole panel: the word over the block, the block, the
// sentence under it when there is one, and the lead that separates the lot
// from the row of field names the board opens with.
//
// The lead is there because the sentence and the field names are both small
// type in a quiet colour, and without it the last line of the panel read as the
// first line of the list. Four pixels rather than the sixteen the legend is
// held off by: sixteen is half a strip, and a board is worth more than the air
// around it.
func (s *Scene) cardHeight(col *layout, plan cardPlan) int {
	lead := col.px(rowLead)
	height := col.lineHeight(s.faces.Small) + lead + s.blockHeight(col, plan) + lead

	if plan.plain {
		height += lead + col.lineHeight(s.faces.Small)
	}

	return height
}

// planCard picks the largest shape of the block that fits the room the column
// has, and reports whether any of them did.
//
// A column too narrow or too short even for the smallest draws no panel at all
// rather than a squeezed one. The strips under it still draw, and every figure
// the panel would have carried is on the selected aircraft's own strip: the
// panel is the one block in the column that repeats what is already there.
func (s *Scene) planCard(col *layout, width, room int) (cardPlan, bool) {
	if s.faces.Small == nil || s.faces.Body == nil || s.faces.Large == nil {
		return cardPlan{}, false
	}

	widths := s.cardWidths(col)

	for _, plan := range cardShapes {
		if s.cardWidth(col, plan, widths) > width || s.cardHeight(col, plan) > room {
			continue
		}

		plan.place(col.px(cardRule)+col.px(cardPadX), col.px(cardCellGap), widths)

		return plan, true
	}

	return cardPlan{}, false
}

// drawCard draws the selected aircraft's panel and takes its room off the top
// of the column.
//
// An empty sky keeps the block's shape and loses its accent rule and its
// figures. There is no aeroplane for the rule to mark, and a column that
// changed height every time the last contact left range would be worse to look
// at than one with a gap in it.
func (s *Scene) drawCard(col *layout, frame source.Frame, sel selection) {
	plan, drawable := s.planCard(col, col.right-col.left, col.bottom-col.top)
	if !drawable {
		return
	}

	s.drawCardLabel(col, col.left, col.top, sel)

	top := col.top + col.lineHeight(s.faces.Small) + col.px(rowLead)
	left := col.left + col.px(cardRule) + col.px(cardPadX)

	if sel.found {
		col.dst.FillRect(image.Rect(col.left, top, col.left+col.px(cardRule), top+s.blockHeight(col, plan)),
			s.pal.Accent)
		s.drawCardIdent(col, plan, left, top, sel.plane)
	} else {
		col.text(s.faces.Large, left, top, noTraffic, s.pal.Muted)
	}

	s.drawCardFigures(col, plan, frame, sel)
	s.drawCardPlain(col, plan, frame, sel)

	col.top += s.cardHeight(col, plan)
}

// drawCardLabel names the block, and says when the filter is hiding the
// aeroplane it is about.
//
// The tag is amber rather than the data colour the label itself takes, because
// it is a caution and not a heading: the panel is about an aeroplane that is
// not on the field, which is a state the operator has to notice before
// wondering where the strip went.
func (s *Scene) drawCardLabel(col *layout, left, top int, sel selection) {
	face := s.faces.Small

	pen := col.tracked(face, left, top, cardLabel, s.pal.Data)

	if sel.hidden {
		col.tracked(face, pen, top, cardFiltered, s.pal.Caution)
	}
}

// drawCardIdent sets the block's first row: who the aeroplane is, large, with
// its codes stacked beside it.
//
// The callsign takes the operator's colour in airline mode, the same as it does
// on the strip and on the tag. A name in three places in three colours would be
// three aeroplanes to reconcile.
func (s *Scene) drawCardIdent(col *layout, plan cardPlan, left, top int, plane airplane.Snapshot) {
	body := s.faces.Body

	text.Draw(col.dst, s.faces.Large, left, top, clip(callsignOf(plane), maxCallsign), s.callsignInk(plane),
		col.glyphsAt(plan.scale))

	codes := left + plan.scale*col.measure(s.faces.Large, widestCallsign) + col.px(cardCellGap)
	first := top + (s.identHeight(col, plan.scale)-2*col.lineHeight(body))/2

	col.text(body, codes, first, clip(plane.ICAO, maxICAO), s.pal.Muted)

	code := clip(plane.Squawk, maxSquawk)
	if code == "" {
		code = noSquawk
	}

	// The prefix and the code are two draws rather than one concatenation: a
	// string built on the draw path is an allocation per frame, which is the
	// one thing this scene promises never to make.
	second := first + col.lineHeight(body)
	pen := col.text(body, codes, second, squawkPrefix, s.pal.Muted)
	pen = col.text(body, pen, second, code, s.pal.Muted)

	s.drawEmergency(col, pen+col.px(stripGap), second+(col.lineHeight(body)-col.lineHeight(s.faces.Small))/2, plane)
}

// drawCardFigures sets the block's rows of big figures and the words under
// them.
//
// The words are drawn whether or not there is an aeroplane to put figures over
// them. They are what the block is, and a panel that lost its own structure
// when the sky went quiet would read as a panel that had broken.
func (s *Scene) drawCardFigures(col *layout, plan cardPlan, frame source.Frame, sel selection) {
	top := col.top + col.lineHeight(s.faces.Small) + col.px(rowLead) + s.identHeight(col, plan.scale)

	for row := range plan.rows {
		top += col.px(cardLead)

		if sel.found {
			s.drawCardValues(col, plan, row, top, frame, sel.plane)
		}

		s.drawCardLabels(col, plan, row, top+col.lineHeight(s.faces.Large))
		top += s.figureRowHeight(col)
	}
}

// drawCardValues sets one row's figures.
//
// The two rows are written out rather than dispatched through a table indexed
// by the figure. Each of the five is its own reading with its own colour and
// its own way of being unknown, and none of them is interchangeable with the
// one beside it, so a table would be five one-line entries plus the machinery
// to walk them.
func (s *Scene) drawCardValues(
	col *layout, plan cardPlan, row, top int, frame source.Frame, plane airplane.Snapshot,
) {
	if row == cardTopRow {
		s.drawCardLevel(col, col.left+plan.left[cardLevel], top, plane)
		col.drawBytes(s.faces.Large, col.left+plan.left[cardSpeed], top, s.speed(plane.Velocity), s.pal.Ink)
		s.drawCardTrack(col, col.left+plan.left[cardTrack], top, plane)

		return
	}

	s.drawCardRange(col, col.left+plan.left[cardRange], top, frame.Receiver, plane)
	s.drawCardBearing(col, col.left+plan.left[cardBearing], top, frame.Receiver, plane)
}

// drawCardLabels writes the small word under each figure of one row.
func (s *Scene) drawCardLabels(col *layout, plan cardPlan, row, top int) {
	for index := cardRowStart[row]; index < cardRowStart[row+1]; index++ {
		col.tracked(s.faces.Small, col.left+plan.left[index], top, cardLabels[index], s.pal.Data)
	}
}

// drawCardLevel writes the level in hundreds of feet with the trend mark after
// it, both in the colour of the altitude band the aircraft is in.
//
// The band colour rather than the reading ink, because it is the one figure on
// the block the scope beside it also says: the aeroplane out there is that
// colour, and the level is why.
func (s *Scene) drawCardLevel(col *layout, left, top int, plane airplane.Snapshot) {
	band := s.bandColour(plane.Altitude)

	pen := col.drawBytes(s.faces.Large, left, top, s.flightLevel(plane.Altitude), band)
	s.drawTrend(col, s.faces.Large, pen, top, plane.VertRate, band)
}

// drawCardTrack writes the course the aircraft is flying, or dashes when
// nobody has decoded one.
func (s *Scene) drawCardTrack(col *layout, left, top int, plane airplane.Snapshot) {
	if !knownHeading(plane.Heading) {
		col.text(s.faces.Large, left, top, trackUnknown, s.pal.Muted)

		return
	}

	col.drawBytes(s.faces.Large, left, top, s.degrees(plane.Heading), s.pal.Ink)
}

// drawCardRange writes how far the aircraft is from the receiver.
func (s *Scene) drawCardRange(
	col *layout, left, top int, receiver source.Receiver, plane airplane.Snapshot,
) {
	away := airplanes.HaversineDistance(receiver.Latitude, receiver.Longitude, plane.Latitude, plane.Longitude)

	col.drawBytes(s.faces.Large, left, top, s.distance(away), s.pal.Ink)
}

// drawCardBearing writes the bearing from the receiver with the needle turned
// to it, which is the one figure on the block that is a direction to look in
// rather than a reading to note.
func (s *Scene) drawCardBearing(
	col *layout, left, top int, receiver source.Receiver, plane airplane.Snapshot,
) {
	face := s.faces.Large

	bearing, known := bearingTo(receiver, plane)
	if !known {
		col.text(face, left, top, detailUnknown, s.pal.Muted)

		return
	}

	pen := col.drawBytes(face, left, top, s.degrees(bearing), s.pal.Ink)
	s.drawArrow(col, pen+col.px(arrowGap), top+col.lineHeight(face)/2, bearing, s.pal.Ink)
}

// drawCardPlain writes the sentence under the block, in three clauses.
//
// Each clause is measured against the widest it can ever be rather than
// against what is in it, and a clause that will not fit whole is dropped along
// with everything after it. A sentence cut mid-word says less than a shorter
// one that finishes, and measuring the worst case is what stops the line
// growing and shrinking under the block as an aircraft climbs.
func (s *Scene) drawCardPlain(col *layout, plan cardPlan, frame source.Frame, sel selection) {
	if !plan.plain || !sel.found {
		return
	}

	face := s.faces.Small
	left := col.left + col.px(cardRule) + col.px(cardPadX)
	top := col.top + col.lineHeight(face) + col.px(rowLead) + s.blockHeight(col, plan) + col.px(rowLead)

	if left+col.measure(face, widestPlainLevel) > col.right {
		return
	}

	pen := s.drawPlainLevel(col, left, top, sel.plane)

	pen, drawn := s.drawPlainPlace(col, pen, top, col.right, sel.plane)
	if !drawn {
		return
	}

	s.drawPlainSeen(col, pen, top, col.right, frame.Now, sel.plane)
}

// drawPlainLevel writes the first clause: the altitude in feet and what the
// aircraft is doing with it.
func (s *Scene) drawPlainLevel(col *layout, left, top int, plane airplane.Snapshot) int {
	face := s.faces.Small

	pen := s.drawPlainFeet(col, left, top, plane.Altitude)
	pen = col.text(face, pen, top, plainFeet, s.pal.Muted)

	if level(plane.VertRate) {
		return col.text(face, pen, top, plainLevelWord, s.pal.Muted)
	}

	word := plainClimbing
	if plane.VertRate < 0 {
		word = plainDescending
	}

	pen = col.text(face, pen, top, word, s.pal.Muted)
	pen = col.drawBytes(face, pen, top, s.thousands(math.Abs(plane.VertRate)), s.pal.Muted)

	return col.text(face, pen, top, plainRate, s.pal.Muted)
}

// drawPlainFeet writes the altitude itself, or dashes for one nobody has
// decoded.
//
// An altitude of zero is undecoded rather than sea level, which is the reading
// flightLevel gives it and the one bandColour and the 3D view's height both
// work from.
func (s *Scene) drawPlainFeet(col *layout, left, top int, altitudeFt float64) int {
	face := s.faces.Small

	if math.IsNaN(altitudeFt) || math.IsInf(altitudeFt, 0) || altitudeFt <= 0 {
		return col.text(face, left, top, detailUnknown, s.pal.Muted)
	}

	return col.drawBytes(face, left, top, s.thousands(altitudeFt), s.pal.Muted)
}

// drawPlainPlace writes the second clause, where the aeroplane is, and reports
// whether the sentence can carry on.
//
// An aircraft with no position drops the clause and keeps the sentence going,
// because the clause after it is about the receiver hearing the aeroplane
// rather than about seeing it. A clause dropped for want of room stops the
// line there instead.
func (s *Scene) drawPlainPlace(
	col *layout, left, top, right int, plane airplane.Snapshot,
) (int, bool) {
	if !hasPosition(plane) {
		return left, true
	}

	face := s.faces.Small
	if left+col.measure(face, widestPlainPlace) > right {
		return left, false
	}

	pen := col.text(face, left, top, plainSeparator, s.pal.Muted)
	pen = col.drawBytes(face, pen, top, s.place(plane.Latitude, 'N', 'S'), s.pal.Muted)
	pen = col.text(face, pen, top, plainSlash, s.pal.Muted)

	return col.drawBytes(face, pen, top, s.place(plane.Longitude, 'E', 'W'), s.pal.Muted), true
}

// drawPlainSeen closes the sentence with how long ago the contact was heard.
func (s *Scene) drawPlainSeen(
	col *layout, left, top, right int, now time.Time, plane airplane.Snapshot,
) {
	face := s.faces.Small
	if left+col.measure(face, widestPlainSeen) > right {
		return
	}

	pen := col.text(face, left, top, plainSeparator+plainSeen, s.pal.Muted)
	col.text(face, pen, top, seenBucket(now.Sub(plane.LastUpdate)), s.pal.Muted)
}
