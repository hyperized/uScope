package radar

import (
	"image"
	"image/color"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// The compact row list's bounds.
const (
	// maxRowCount is where the list stops growing on a tall canvas.
	//
	// It was sixteen while a fixed-height details block sat under the list and
	// a longer table would have pushed that block off the bottom. The block is
	// gone, so the rows take the height it had: twenty-four fills the column at
	// 720 pixels without a gap under it, and on a live feed with eighty
	// aircraft in range those eight extra lines are eight fewer in the
	// "+N MORE" tail.
	maxRowCount = 24

	// The "+N MORE" line that closes a list longer than the window. N counts
	// every aircraft not on screen, above the window as well as below it, so
	// the row list plus that number always adds up to the fleet.
	morePrefix = "+"
	moreSuffix = " MORE"

	// rowsCountSuffix follows the aircraft count at the right end of the
	// column-title line. The count used to be a stats line of its own under
	// the legend, next to the source label the header already carried.
	rowsCountSuffix = " AIRCRAFT"

	// rowsCountWidest is the count the title line reserves room for, whatever
	// the count actually is. Sizing from the widest keeps the table's right
	// edge still, the same way every column is sized from its widest value: a
	// right-aligned figure that grew a digit would otherwise pull the whole
	// table left on the frame a hundredth aircraft arrived.
	rowsCountWidest = "9999" + rowsCountSuffix

	// rowsOfInfix joins the two numbers the count carries while a filter is
	// on: how many aircraft the rows are showing, then how many are on the
	// field. Without the second number a short list reads as a quiet sky
	// rather than as a filter doing its job.
	rowsOfInfix = " OF "

	// rowsFilteredWidest is what the title line reserves instead while the
	// filter is on. It is the wider string, so the table gives up about fifty
	// pixels the moment f is pressed and takes them back when it goes to ALL.
	//
	// Reserving the wider one always would be the stiller table, and it would
	// cost every unfiltered scope those pixels for a prefix it never draws. A
	// table that moves when the operator changes what it is a table of is the
	// better trade.
	rowsFilteredWidest = "9999" + rowsOfInfix + rowsCountWidest
)

// Each column's width in characters, taken from the widest value that column
// can hold rather than from the values on screen.
//
// Sizing from the widest plausible value is what keeps the table still. A
// column measured from its contents jumps a character wider the moment an
// aircraft climbs through ten thousand feet, and a table whose columns move is
// harder to read than one that wastes a few pixels.
const (
	rowIndexChars    = 2 // "12"
	rowCallsignChars = maxCallsign
	rowICAOChars     = maxICAO
	rowAltitudeChars = 7 // "999,999"
	rowSpeedChars    = 3 // "999"
	rowDistanceChars = 5 // "999.9"
	rowBearingChars  = 8 // "359" and the arrow, in the room "359 / NW" had

	// attCell is the attitude column's width in pixels. It is the one column
	// measured in pixels rather than in characters, because what goes in it is
	// a shape and not a value: twenty-four is the model's fourteen-pixel span
	// plus the room a wing needs to swing into as an aircraft banks through a
	// turn.
	attCell = 24
)

// The columns, in the order they are drawn.
const (
	colIndex = iota
	colCallsign
	colICAO
	colAltitude
	colSpeed
	colDistance
	colBearing
	colAttitude

	// colCount is the number of columns, which is what every loop over them
	// runs to.
	colCount
)

// rowColumn is one column of the compact row list.
type rowColumn struct {
	title string
	chars int

	// extra is room beside the value that is not part of it. The altitude
	// column uses it for the climb and descent triangle, and the attitude
	// column is nothing but extra: it holds a drawing and no characters at
	// all, so its whole width is here and its chars is zero.
	extra int

	// right says the column's values end on its edge rather than start there.
	// Numbers are right-aligned so their digits line up down the list; text is
	// left-aligned so the first letter does.
	right bool
}

// rowColumns describes the columns left to right.
//
//nolint:gochecknoglobals // a table header is data, and an array cannot be const.
var rowColumns = [colCount]rowColumn{
	colIndex:    {title: "#", chars: rowIndexChars, right: true},
	colCallsign: {title: "CALLSIGN", chars: rowCallsignChars},
	colICAO:     {title: "ICAO", chars: rowICAOChars},
	colAltitude: {title: "ALT", chars: rowAltitudeChars, extra: vertMarker + vertGap, right: true},
	colSpeed:    {title: "SPD", chars: rowSpeedChars, right: true},
	colDistance: {title: "DIST", chars: rowDistanceChars, right: true},
	colBearing:  {title: "BRG", chars: rowBearingChars, right: true},
	colAttitude: {title: "ATT", extra: attCell, right: true},
}

// rowDropOrder is the order columns are given up as the column narrows.
//
// Attitude goes first. It is the newest column and the most decorative: it
// says which way an aeroplane is pointing, which the scope beside it already
// draws, and unlike every other cell it carries no figure anyone could read
// back. Bearing next, for the same reason one step weaker: the scope shows
// where an aircraft is, so the number repeats it. Then speed, then the ICAO
// hex, which only matters when a callsign is missing and the callsign column
// already falls back to it. Altitude and distance are last because they are
// what the list is for, and past those two there is nothing left to drop but
// the identity.
//
//nolint:gochecknoglobals // an order is data, and an array cannot be const.
var rowDropOrder = [...]int{colAttitude, colBearing, colSpeed, colICAO, colAltitude, colDistance}

// rowPlan is which columns are being drawn and where each one ends.
type rowPlan struct {
	// edge is a column's right-hand boundary: right-aligned values finish on
	// it and left-aligned ones start a column width back from it.
	edge [colCount]int
	on   [colCount]bool
}

// count is how many columns are still in the plan.
func (p *rowPlan) count() int {
	total := 0

	for index := range colCount {
		if p.on[index] {
			total++
		}
	}

	return total
}

// width is the least room the columns in the plan need, values plus the
// minimum gap between them.
//
// The gap is part of the width rather than something spread on afterwards. A
// table whose columns only touch when the canvas is wide is a table that is
// unreadable exactly when it is most crowded.
func (p *rowPlan) width(glyph int) int {
	total, columns := 0, 0

	for index := range colCount {
		if p.on[index] {
			total += rowColumns[index].chars*glyph + rowColumns[index].extra
			columns++
		}
	}

	return total + max(columns-1, 0)*columnGap
}

// place spreads the columns across the width, so the first starts on the left
// edge and the last finishes on the right one.
//
// The slack is shared out by interpolating on the column number rather than by
// adding a fixed gap between columns. An integer gap leaves a remainder, and
// the last column would then stop a few pixels short of the edge it is
// supposed to meet.
func (p *rowPlan) place(left, available, glyph int) {
	slack := max(available-p.width(glyph), 0)
	gaps := max(p.count()-1, 1)
	used, seen := 0, 0

	for index := range colCount {
		if !p.on[index] {
			continue
		}

		used += rowColumns[index].chars*glyph + rowColumns[index].extra
		p.edge[index] = left + used + seen*columnGap + slack*seen/gaps
		seen++
	}
}

// planRows works out which columns fit between left and right.
//
// Columns are dropped whole rather than squeezed, for the reason every other
// block in this scene is: the values are set in a bitmap face with one design
// size, so a narrower column means fewer characters, not smaller ones. A
// column too narrow even for the identity draws no rows at all rather than
// running the table past its own right edge.
func (s *Scene) planRows(left, right int) (rowPlan, bool) {
	var plan rowPlan

	glyph := glyphWidth(s.faces.Body)
	if glyph == 0 {
		return plan, false
	}

	for index := range colCount {
		plan.on[index] = true
	}

	available := right - left

	for _, drop := range rowDropOrder {
		if plan.width(glyph) <= available {
			break
		}

		plan.on[drop] = false
	}

	if plan.width(glyph) > available {
		return plan, false
	}

	plan.place(left, available, glyph)

	return plan, true
}

// rowPen is everything one row's cells share, so the cell helpers take a
// column and a value and nothing else.
type rowPen struct {
	dst   *canvas.Canvas
	face  *psf.Font
	plan  rowPlan
	glyph int
	top   int
}

// left sets one left-aligned cell, or nothing when the column was dropped.
func (p rowPen) left(column int, value string, ink color.RGBA) {
	if !p.plan.on[column] {
		return
	}

	text.Draw(p.dst, p.face, p.plan.edge[column]-rowColumns[column].chars*p.glyph, p.top, value, ink)
}

// right sets one right-aligned cell, finishing on the column's own edge.
func (p rowPen) right(column int, value []byte, ink color.RGBA) {
	if !p.plan.on[column] {
		return
	}

	drawBytesRight(p.dst, p.face, p.plan.edge[column], p.top, value, ink)
}

// rowsHeight is the room count rows need, including the header line above
// them.
func (s *Scene) rowsHeight(count int) int {
	step := s.rowStep()
	if step == 0 {
		return 0
	}

	return lineHeight(s.faces.Small) + rowLead + count*step
}

// rowsCountWidth is the room the aircraft count needs at the right end of the
// title line, the gap back to the last column title included, or zero without
// a face to set it in.
func (s *Scene) rowsCountWidth() int {
	face := s.faces.Small
	if face == nil {
		return 0
	}

	widest := rowsCountWidest
	if s.filter.active() {
		widest = rowsFilteredWidest
	}

	width, _ := text.Measure(face, widest, text.WithSpacing(labelTracking))

	return width + columnGap
}

// planRowTable fits the table between left and right, reserving room for the
// aircraft count when the columns still fit without it, and reports how much
// it reserved.
//
// A reserve of zero means the count is not drawn. That is better than dropping
// a column to make room for it: the count is a reading about the list and the
// columns are the list itself.
func (s *Scene) planRowTable(left, right int) (rowPlan, int, bool) {
	reserve := s.rowsCountWidth()
	if plan, fits := s.planRows(left, right-reserve); fits {
		return plan, reserve, true
	}

	plan, fits := s.planRows(left, right)

	return plan, 0, fits
}

// drawRows draws the compact list under the panel: a title line carrying the
// column names and the aircraft count, then one line per aircraft, as many as
// the room left in the column holds.
//
// The list is the aircraft the filter is showing, numbered from one down the
// page rather than by where they sit in the fleet. The count on the title line
// is what says how many were left out; a "#" column skipping from 3 to 17
// would say the same thing far less clearly.
//
// A filter that hides everything still draws the title line and its count. An
// empty table with "0 OF 81 AIRCRAFT" over it explains itself; an empty column
// does not.
func (s *Scene) drawRows(col *layout, frame source.Frame) {
	step := s.rowStep()
	if step == 0 || len(frame.Planes) == 0 {
		return
	}

	right := col.right - cardPadX

	plan, reserved, drawable := s.planRowTable(col.left+accentWidth+cardPadX, right)
	if !drawable {
		return
	}

	head := s.rowsHeight(0)

	capacity := min((col.bottom-col.top-head)/step, maxRowCount)
	if capacity <= 0 {
		return
	}

	shown := s.shownPlanes()

	// The "+N MORE" tail gets a line of the window rather than being squeezed
	// under it, so it cannot end up drawn over the block below. A shorter list
	// with an honest count under it is worth one row.
	window := capacity
	if shown > capacity {
		window = capacity - 1
	}

	s.trackWindow(window, shown)
	s.drawRowHeader(col, plan)

	if reserved > 0 {
		s.drawRowCount(col.dst, right, col.top, shown, len(frame.Planes))
	}

	pen := rowPen{
		dst: col.dst, face: s.faces.Body, plan: plan,
		glyph: glyphWidth(s.faces.Body), top: col.top + head,
	}

	s.drawMoreRow(col, s.drawRowWindow(col, pen, frame, window, step), shown-window)
}

// drawRowWindow draws the slice of the filtered list the window is over.
//
// It walks the whole fleet rather than indexing into it, because the rows are
// the aircraft that passed the filter and nothing holds those in a list of
// their own: the ICAO index has their identities but not their readings. The
// walk stops at the bottom of the window, so a busy scope does not scan eighty
// aircraft to draw twenty rows.
// It returns the top of the line after the last row it drew, which is where
// the "+N MORE" tail goes.
func (s *Scene) drawRowWindow(col *layout, pen rowPen, frame source.Frame, window, step int) int {
	index := 0

	for _, plane := range frame.Planes {
		if !s.visible(plane) {
			continue
		}

		if index >= s.rowStart+window {
			break
		}

		if index >= s.rowStart {
			s.drawRow(col, pen, index, plane, frame.Receiver)
			pen.top += step
		}

		index++
	}

	return pen.top
}

// drawRowHeader names the columns above the first row, in the small face so
// the titles read as chrome rather than as another row of data.
func (s *Scene) drawRowHeader(col *layout, plan rowPlan) {
	face := s.faces.Small
	glyph := glyphWidth(s.faces.Body)

	for index := range colCount {
		if !plan.on[index] {
			continue
		}

		column := rowColumns[index]
		if column.right {
			text.DrawRight(col.dst, face, plan.edge[index], col.top, column.title, s.pal.Muted,
				text.WithSpacing(labelTracking))

			continue
		}

		text.Draw(col.dst, face, plan.edge[index]-column.chars*glyph, col.top, column.title, s.pal.Muted,
			text.WithSpacing(labelTracking))
	}
}

// drawRowCount writes how many aircraft are being tracked, right-aligned on
// the column-title line and set in the same small muted face the titles are,
// so it reads as part of the table's heading rather than as another figure.
//
// With a filter on it reads "12 OF 81 AIRCRAFT" instead. The second number is
// the whole field, so the line says what is being held back as well as what is
// on screen, and a scope that has gone quiet cannot be mistaken for one that
// is filtered.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawRowCount(dst *canvas.Canvas, rightX, y, shown, total int) {
	face := s.faces.Small

	suffix, _ := text.Measure(face, rowsCountSuffix, text.WithSpacing(labelTracking))
	value := s.count(total)
	left := rightX - suffix - trackedWidth(face, len(value))

	if s.filter.active() {
		left = s.drawCountLead(dst, left, y, shown)
	}

	pen := drawBytesTracked(dst, face, left, y, value, s.pal.Muted)
	text.Draw(dst, face, pen, y, rowsCountSuffix, s.pal.Muted, text.WithSpacing(labelTracking))
}

// drawCountLead writes the "12 OF " the filtered count opens with, placed so
// that it finishes where the total was going to start, and returns the x the
// total now starts at.
//
// The number goes into its own buffer rather than into the one s.count uses,
// because the total has already been formatted into that and a formatter's
// slice is only good until the next call on the same array.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawCountLead(dst *canvas.Canvas, rightX, y, shown int) int {
	face := s.faces.Small

	lead := s.shownCount(shown)
	infix, _ := text.Measure(face, rowsOfInfix, text.WithSpacing(labelTracking))

	pen := drawBytesTracked(dst, face, rightX-infix-trackedWidth(face, len(lead)), y, lead, s.pal.Muted)

	return text.Draw(dst, face, pen, y, rowsOfInfix, s.pal.Muted, text.WithSpacing(labelTracking))
}

// trackWindow scrolls the row list so the selected aircraft is always one of
// the rows on screen. Without it, selecting past the bottom of the list would
// move a selection nobody could see.
func (s *Scene) trackWindow(fit, count int) {
	if s.selIndex < 0 {
		s.rowStart = 0

		return
	}

	if s.selIndex < s.rowStart {
		s.rowStart = s.selIndex
	}

	if s.selIndex >= s.rowStart+fit {
		s.rowStart = s.selIndex - fit + 1
	}

	s.rowStart = min(max(s.rowStart, 0), max(count-fit, 0))
}

// drawRow draws one aircraft's line.
//
// The selected row carries the accent bar, and in altitude mode its callsign
// is the only one set in ink; the rest are muted, so the eye lands on the
// selection first when scanning down. In airline mode every callsign carries
// its operator's colour instead and the accent bar is what marks the
// selection on its own.
func (s *Scene) drawRow(
	col *layout, pen rowPen, index int, plane airplane.Snapshot, receiver source.Receiver,
) {
	selected := index == s.selIndex
	if selected {
		col.dst.FillRect(
			image.Rect(col.left, pen.top, col.left+accentWidth, pen.top+pen.face.Height()), s.pal.Accent)
	}

	pen.right(colIndex, s.index(index+1), s.pal.Muted)
	pen.left(colCallsign, clip(callsignOf(plane), maxCallsign), s.rowCallsignInk(plane, index))
	pen.left(colICAO, clip(plane.ICAO, maxICAO), s.pal.Muted)

	s.drawRowAltitude(pen, plane)
	pen.right(colSpeed, s.speed(plane.Velocity), s.pal.Muted)

	away := airplanes.HaversineDistance(receiver.Latitude, receiver.Longitude, plane.Latitude, plane.Longitude)
	pen.right(colDistance, s.distance(away), s.pal.Muted)

	s.drawRowBearing(pen, receiver, plane)
	s.drawRowAttitude(pen, plane)
}

// drawRowAttitude draws the little aeroplane at the right end of the row,
// posed by the same rules the perspective view poses the large one with.
//
// It goes through a camera of its own rather than the view's. The 3D view's
// camera orbits, so an aircraft in it is seen from wherever the orbit has got
// to; a column of these has to be readable down the page, which means every
// row seen from the same angle whatever the picture beside it is doing. The
// model and the projection are shared, the camera is not: see newCellCamera3.
//
// The cell is centred on its own width rather than hung off the column edge
// the way a figure is. A drawing has no baseline and no last digit to line up,
// and a shape that leaned on one side of its cell would read as an aircraft
// drifting rather than as one pointing.
//
// It takes the aircraft's own colour in both colour modes, and the selected
// row is no exception: the callsign beside it already carries the selection,
// and the point of the cell is that reading the table and reading the scope
// are the same act of recognition. A row whose aeroplane changed colour when
// it was selected would break that for the one row it matters most on.
func (s *Scene) drawRowAttitude(pen rowPen, plane airplane.Snapshot) {
	if !pen.plan.on[colAttitude] {
		return
	}

	centre := image.Pt(
		pen.plan.edge[colAttitude]-attCell/2,
		pen.top+pen.face.Height()/2,
	)

	s.drawShape3(pen.dst, newCellCamera3(centre), plane, point3{}, centre, s.aircraftColour(plane))
}

// drawRowAltitude sets the altitude cell with the same climb and descent
// triangle the details block uses.
//
// The figure and its triangle are both set in the aircraft's altitude band,
// in either colour mode. That is the one column where the number and a colour
// say the same thing, so the band survives airline mode instead of being the
// price of turning it on: the list still answers "how high" at a glance while
// the scope answers "who".
//
// The triangle is placed against the figure rather than at the column's left
// edge, so the pair reads as one value. At the widest figure the column can
// hold the two land exactly on the column's own boundary, which is what its
// extra width is sized for.
func (s *Scene) drawRowAltitude(pen rowPen, plane airplane.Snapshot) {
	if !pen.plan.on[colAltitude] {
		return
	}

	edge := pen.plan.edge[colAltitude]
	band := s.bandColour(plane.Altitude)
	value := s.thousands(plane.Altitude)
	width := measureBytes(pen.face, value)

	drawBytesRight(pen.dst, pen.face, edge, pen.top, value, band)

	if level(plane.VertRate) {
		return
	}

	s.drawVertMarker(pen.dst, edge-width-vertGap-vertMarker, pen.top, plane.VertRate, band)
}

// drawRowBearing sets the bearing cell, which is where the aircraft is from
// the receiver rather than the course it is flying.
//
// The digits and the arrow are one right-aligned group, so the table's right
// edge stays where the column plan put it. The dashes a row with no bearing
// gets finish where the digits do rather than where the arrow does, so the
// column reads down as a run of numbers with a gap in it rather than as two
// things alternating.
//
// The column is still measured at the eight characters the compass spelling
// took, which is what keeps the table fitting and dropping columns exactly as
// it did. The arrow takes less room than the letters and the slack goes to
// the left of the group, where the gap between columns already is.
func (s *Scene) drawRowBearing(pen rowPen, receiver source.Receiver, plane airplane.Snapshot) {
	if !pen.plan.on[colBearing] {
		return
	}

	digits := pen.plan.edge[colBearing] - s.arrowWidth()

	bearing, known := bearingTo(receiver, plane)
	if !known {
		text.DrawRight(pen.dst, pen.face, digits, pen.top, detailUnknown, s.pal.Muted)

		return
	}

	drawBytesRight(pen.dst, pen.face, digits, pen.top, s.degrees(bearing), s.pal.Muted)
	s.drawArrow(pen.dst, digits+arrowGap, pen.top+pen.face.Height()/2, bearing, s.pal.Muted)
}

// drawMoreRow writes the muted line saying how many aircraft the list had no
// room for.
func (s *Scene) drawMoreRow(col *layout, top, hidden int) {
	if hidden <= 0 {
		return
	}

	face := s.faces.Body
	pen := col.left + accentWidth + cardPadX

	pen = text.Draw(col.dst, face, pen, top, morePrefix, s.pal.Muted)
	pen = drawBytes(col.dst, face, pen, top, s.count(hidden), s.pal.Muted)
	text.Draw(col.dst, face, pen, top, moreSuffix, s.pal.Muted)
}
