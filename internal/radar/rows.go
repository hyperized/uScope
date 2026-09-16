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
	rowBearingChars  = 8 // "359 / NW"
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

	// colCount is the number of columns, which is what every loop over them
	// runs to.
	colCount
)

// rowColumn is one column of the compact row list.
type rowColumn struct {
	title string
	chars int

	// extra is room ahead of the value that is not part of it. Only the
	// altitude column has any: it is where the climb and descent triangle
	// goes.
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
}

// rowDropOrder is the order columns are given up as the column narrows.
//
// Bearing goes first because the scope itself shows which way an aircraft is,
// so the figure is the one that repeats something already on screen. Speed
// next, then the ICAO hex, which only matters when a callsign is missing and
// the callsign column already falls back to it. Altitude and distance are last
// because they are what the list is for, and past those two there is nothing
// left to drop but the identity.
//
//nolint:gochecknoglobals // an order is data, and an array cannot be const.
var rowDropOrder = [...]int{colBearing, colSpeed, colICAO, colAltitude, colDistance}

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

	width, _ := text.Measure(face, rowsCountWidest, text.WithSpacing(labelTracking))

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

	// The "+N MORE" tail gets a line of the window rather than being squeezed
	// under it, so it cannot end up drawn over the block below. A shorter list
	// with an honest count under it is worth one row.
	visible := capacity
	if len(frame.Planes) > capacity {
		visible = capacity - 1
	}

	s.trackWindow(visible, len(frame.Planes))
	s.drawRowHeader(col, plan)

	if reserved > 0 {
		s.drawRowCount(col.dst, right, col.top, len(frame.Planes))
	}

	pen := rowPen{
		dst: col.dst, face: s.faces.Body, plan: plan,
		glyph: glyphWidth(s.faces.Body), top: col.top + head,
	}

	for offset := range visible {
		index := s.rowStart + offset
		if index >= len(frame.Planes) {
			break
		}

		s.drawRow(col, pen, index, frame.Planes[index], frame.Receiver)
		pen.top += step
	}

	s.drawMoreRow(col, pen.top, len(frame.Planes)-visible)
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
//nolint:varnamelen // y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawRowCount(dst *canvas.Canvas, rightX, y, count int) {
	face := s.faces.Small

	suffix, _ := text.Measure(face, rowsCountSuffix, text.WithSpacing(labelTracking))
	value := s.count(count)
	left := rightX - suffix - trackedWidth(face, len(value))

	pen := drawBytesTracked(dst, face, left, y, value, s.pal.Muted)
	text.Draw(dst, face, pen, y, rowsCountSuffix, s.pal.Muted, text.WithSpacing(labelTracking))
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
func (s *Scene) drawRowBearing(pen rowPen, receiver source.Receiver, plane airplane.Snapshot) {
	if !pen.plan.on[colBearing] {
		return
	}

	bearing, known := bearingTo(receiver, plane)
	if !known {
		text.DrawRight(pen.dst, pen.face, pen.plan.edge[colBearing], pen.top, detailUnknown, s.pal.Muted)

		return
	}

	pen.right(colBearing, s.bearing(bearing), s.pal.Muted)
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
