package radar

import (
	"image"
	"image/color"
	"math"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// The right column's spacing and fixed strings.
const (
	// The card: padding inside the border, the accent bar down its left edge,
	// the scale the callsign is set at, and the gap before a unit suffix.
	cardPadX       = 12
	cardPadY       = 14
	accentWidth    = 3
	cardTitleScale = 2
	unitGap        = 6
	rowGap         = 12
	columnGap      = 12

	// figureCount is how many numbers sit along the bottom of the card. The
	// card is divided into that many equal columns rather than measured,
	// because a measured column would shift every time a value changed width
	// and the numbers would never sit still.
	figureCount = 3

	cardLabelSuffix = " / SELECTED FLIGHT"
	cardNoSelection = "--"
	noContact       = "NO CONTACT"

	squawkPrefix   = "SQ "
	trackPrefix    = "TRACK "
	trackSeparator = " / "

	// trackUnknown stands in when no heading has been decoded. A heading of
	// exactly zero is the undecoded state rather than due north, and printing
	// 000 / N would be the card inventing a course.
	trackUnknown = "---"

	unitFT = "FT"
	unitKT = "KT"
	unitNm = "NM"

	// distanceDecimals is one place. A tenth of a nautical mile is about 180
	// metres, which is finer than the position under it is worth.
	distanceDecimals = 1

	// notSelected is the position drawCardLabel reads as "nothing selected".
	notSelected = -1

	// The caps on what is drawn from a decoded field. A callsign is eight
	// characters by the standard and a squawk four, but the standard is not
	// what arrives when a frame is half corrupt.
	maxCallsign = 8
	maxICAO     = 6
	maxSquawk   = 4
)

// The compact rows. Their columns are sized in characters rather than
// measured from the values in them, for the same reason the card's figures
// are: a measured column jumps about as the numbers change, and a table that
// moves is harder to read than one that wastes a few pixels.
const (
	rowIndexChars    = 2
	rowCallsignChars = maxCallsign
	rowAltitudeChars = 6
	rowDistanceChars = 6
)

// The legend along the bottom of the column.
const (
	swatchSide = 10
	swatchGap  = 6
	legendGap  = 18

	legendLow  = "< 10K FT"
	legendMid  = "10-25K FT"
	legendHigh = "> 25K FT"
)

// The stats line.
const (
	statsAircraft  = " AIRCRAFT / FRAMES "
	statsSeparator = " / "
)

// legendEntry is one altitude band and the colour that means it.
type legendEntry struct {
	col   color.RGBA
	label string
}

// drawColumn fills the right-hand column.
//
// The legend and the stats take their room off the bottom before anything
// else runs, the card takes its room off the top, and the compact rows get
// whatever is left. That ordering is what lets the rows grow on a tall canvas
// and disappear on a short one without pushing the legend off the screen.
func (s *Scene) drawColumn(lay *layout, frame source.Frame) {
	if lay.column.Empty() {
		return
	}

	col := layout{
		dst:    lay.dst,
		left:   lay.column.Min.X,
		right:  lay.column.Max.X,
		top:    lay.column.Min.Y,
		bottom: lay.column.Max.Y,
		labels: lay.labels,
	}

	s.drawStats(&col, frame)
	s.drawLegend(&col)
	s.drawCard(&col, frame)
	s.drawRows(&col, frame)
}

// drawCard is the selected-flight card: the numbered label, the callsign set
// large with its track under it, the ICAO hex and squawk in the top right,
// and the three figures along the bottom.
func (s *Scene) drawCard(col *layout, frame source.Frame) {
	labelHeight := lineHeight(s.faces.Small)
	detailHeight := lineHeight(s.faces.Body)
	figureHeight := lineHeight(s.faces.Large)

	if labelHeight == 0 || detailHeight == 0 || figureHeight == 0 {
		return
	}

	middle := max(figureHeight*cardTitleScale+detailHeight, 2*detailHeight)
	total := 2*cardPadY + labelHeight + rowGap + middle + rowGap + figureHeight

	if !col.fits(total + blockGap) {
		return
	}

	s.cardFrame(col, total)

	left, right := col.left+accentWidth+cardPadX, col.right-cardPadX
	top := col.top + cardPadY

	plane, position := s.selectedPlane(frame)
	s.drawCardLabel(col.dst, left, top, position)
	top += labelHeight + rowGap

	if position < 0 {
		text.Draw(col.dst, s.faces.Large, left, top, noContact, s.pal.Muted)
		col.top += total + blockGap

		return
	}

	s.drawCardIdentity(col.dst, image.Rect(left, top, right, top+middle), plane)
	s.drawCardFigures(col.dst,
		image.Rect(left, top+middle+rowGap, right, top+middle+rowGap+figureHeight),
		frame.Receiver, plane)

	col.top += total + blockGap
}

// selectedPlane is the aircraft the card is about and its place in the list.
// A position of -1 means nothing is selected, which is what an empty sky looks
// like.
func (s *Scene) selectedPlane(frame source.Frame) (airplane.Snapshot, int) {
	if s.selIndex < 0 || s.selIndex >= len(frame.Planes) {
		return airplane.Snapshot{}, notSelected
	}

	return frame.Planes[s.selIndex], s.selIndex
}

// cardFrame draws the hairline border and the accent bar down the left edge.
func (s *Scene) cardFrame(col *layout, total int) {
	box := image.Rect(col.left, col.top, col.right, col.top+total)

	col.dst.Rect(box, s.pal.Rule)
	col.dst.FillRect(image.Rect(box.Min.X, box.Min.Y, box.Min.X+accentWidth, box.Max.Y), s.pal.Accent)
}

// drawCardLabel writes the numbered heading, which ties the card to the row
// of the same number further down the column.
//
// A negative number means nothing is selected and the heading reads "--",
// which is the same shape as a number and so does not move the text beside it.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawCardLabel(dst *canvas.Canvas, x, y, position int) {
	pen := s.drawLabelNumber(dst, x, y, position)
	text.Draw(dst, s.faces.Small, pen, y, cardLabelSuffix, s.pal.Muted)
}

// drawLabelNumber writes the card's number, or the two dashes that stand in
// for one, and returns the x just past it.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawLabelNumber(dst *canvas.Canvas, x, y, position int) int {
	if position < 0 {
		return text.Draw(dst, s.faces.Small, x, y, cardNoSelection, s.pal.Muted)
	}

	return drawBytes(dst, s.faces.Small, x, y, s.index(position+1), s.pal.Muted)
}

// drawCardIdentity sets the callsign large with the track under it, and the
// two codes right-aligned beside them.
func (s *Scene) drawCardIdentity(dst *canvas.Canvas, box image.Rectangle, plane airplane.Snapshot) {
	large, body := s.faces.Large, s.faces.Body

	text.Draw(dst, large, box.Min.X, box.Min.Y, clip(callsignOf(plane), maxCallsign), s.pal.Ink,
		text.WithScale(cardTitleScale))

	s.drawCardTrack(dst, box.Min.X, box.Min.Y+lineHeight(large)*cardTitleScale, plane.Heading)

	text.DrawRight(dst, body, box.Max.X, box.Min.Y, clip(plane.ICAO, maxICAO), s.pal.Ink)
	s.drawSquawk(dst, box.Max.X, box.Min.Y+lineHeight(body), plane.Squawk)
}

// drawCardTrack writes the course as degrees and the compass point it falls
// in, because a number alone takes a moment to place and a letter does not.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawCardTrack(dst *canvas.Canvas, x, y int, heading float64) {
	face := s.faces.Body

	pen := text.Draw(dst, face, x, y, trackPrefix, s.pal.Muted)

	if heading == 0 {
		text.Draw(dst, face, pen, y, trackUnknown, s.pal.Muted)

		return
	}

	pen = drawBytes(dst, face, pen, y, s.degrees(heading), s.pal.Ink)
	pen = text.Draw(dst, face, pen, y, trackSeparator, s.pal.Muted)
	text.Draw(dst, face, pen, y, compass(heading), s.pal.Ink)
}

// drawSquawk right-aligns the transponder code behind its label.
//
// The two pieces are measured and then drawn left to right rather than drawn
// right to left, so the label stays muted and the code stays ink without the
// pair drifting apart when the code is short.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawSquawk(dst *canvas.Canvas, rightX, y int, squawk string) {
	face := s.faces.Body
	code := clip(squawk, maxSquawk)

	prefixWidth, _ := text.Measure(face, squawkPrefix)
	codeWidth, _ := text.Measure(face, code)

	pen := text.Draw(dst, face, rightX-prefixWidth-codeWidth, y, squawkPrefix, s.pal.Muted)
	text.Draw(dst, face, pen, y, code, s.pal.Ink)
}

// drawCardFigures sets the three numbers along the bottom of the card, each
// with its unit small and muted beside it.
func (s *Scene) drawCardFigures(
	dst *canvas.Canvas, box image.Rectangle, receiver source.Receiver, plane airplane.Snapshot,
) {
	column := box.Dx() / figureCount
	unitTop := box.Min.Y + lineHeight(s.faces.Large) - lineHeight(s.faces.Small)

	away := airplanes.HaversineDistance(receiver.Latitude, receiver.Longitude, plane.Latitude, plane.Longitude)

	s.drawFigure(dst, box.Min.X, box.Min.Y, unitTop, s.distance(away), unitNm)
	s.drawFigure(dst, box.Min.X+column, box.Min.Y, unitTop, s.thousands(plane.Altitude), unitFT)
	s.drawFigure(dst, box.Min.X+2*column, box.Min.Y, unitTop, s.whole(plane.Velocity), unitKT)
}

// drawFigure draws one number with its unit.
//
// Each figure is formatted immediately before it is drawn because they all
// share the scene's one scratch buffer; formatting all three first would
// leave three slices of the same bytes.
func (s *Scene) drawFigure(dst *canvas.Canvas, left, top, unitTop int, value []byte, unit string) {
	pen := drawBytes(dst, s.faces.Large, left, top, value, s.pal.Ink)
	text.Draw(dst, s.faces.Small, pen+unitGap, unitTop, unit, s.pal.Muted)
}

// distance writes a range in nautical miles, or a dash when the aircraft has
// no position.
//
// uAirwaves reports MaxFloat64 rather than an error for an unknown position,
// so that sentinel has to be caught here or the card would claim the aircraft
// is 179769313486231570000... nautical miles away.
func (s *Scene) distance(valueNm float64) []byte {
	if valueNm == math.MaxFloat64 || math.IsNaN(valueNm) {
		return append(s.digits[:0], '-')
	}

	return s.fixed(valueNm, distanceDecimals)
}

// drawRows draws the compact one-line rows under the card, one per aircraft,
// as many as fit.
func (s *Scene) drawRows(col *layout, frame source.Frame) {
	face := s.faces.Body

	line := lineHeight(face)
	if line == 0 || len(frame.Planes) == 0 {
		return
	}

	step := line + rowLead

	fit := (col.bottom - col.top) / step
	if fit <= 0 {
		return
	}

	s.trackWindow(fit, len(frame.Planes))

	top := col.top

	for offset := range fit {
		index := s.rowStart + offset
		if index >= len(frame.Planes) {
			break
		}

		s.drawRow(col, face, top, index, frame.Planes[index], frame.Receiver)
		top += step
	}
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

// drawRow draws one compact row: number, callsign, altitude, distance.
//
// The selected row carries the accent bar and is set in ink; the rest are
// muted, so the eye lands on the selection first when scanning down.
func (s *Scene) drawRow(
	col *layout, face *psf.Font, top, index int, plane airplane.Snapshot, receiver source.Receiver,
) {
	ink := s.pal.Muted

	if index == s.selIndex {
		col.dst.FillRect(image.Rect(col.left, top, col.left+accentWidth, top+face.Height()), s.pal.Accent)

		ink = s.pal.Ink
	}

	glyph := glyphWidth(face)
	pen := col.left + accentWidth + cardPadX

	drawBytes(col.dst, face, pen, top, s.index(index+1), s.pal.Muted)
	pen += rowIndexChars*glyph + columnGap

	text.Draw(col.dst, face, pen, top, clip(callsignOf(plane), maxCallsign), ink)
	pen += rowCallsignChars*glyph + columnGap

	drawBytesRight(col.dst, face, pen+rowAltitudeChars*glyph, top, s.thousands(plane.Altitude), s.pal.Muted)
	pen += rowAltitudeChars*glyph + columnGap

	away := airplanes.HaversineDistance(receiver.Latitude, receiver.Longitude, plane.Latitude, plane.Longitude)
	drawBytesRight(col.dst, face, pen+rowDistanceChars*glyph, top, s.distance(away), s.pal.Muted)
}

// drawLegend explains the three altitude colours, which is the only thing on
// the scope that cannot be worked out by looking at it.
func (s *Scene) drawLegend(col *layout) {
	face := s.faces.Small

	height := lineHeight(face)
	if height == 0 || !col.fits(height) {
		return
	}

	top := col.bottom - height
	pen := col.left

	for _, entry := range s.legendEntries() {
		box := image.Rect(pen, top, pen+swatchSide, top+swatchSide)
		if box.Max.X > col.right {
			break
		}

		col.dst.FillRect(box, entry.col)
		pen = text.Draw(col.dst, face, box.Max.X+swatchGap, top, entry.label, s.pal.Muted,
			text.WithSpacing(labelTracking))
		pen += legendGap
	}

	col.bottom -= height + blockGap
}

// legendEntries pairs each band with its colour, in the order the bands go up.
func (s *Scene) legendEntries() [figureCount]legendEntry {
	return [figureCount]legendEntry{
		{col: s.pal.AltLow, label: legendLow},
		{col: s.pal.AltMid, label: legendMid},
		{col: s.pal.AltHigh, label: legendHigh},
	}
}

// drawStats writes the one line that says whether anything is working: how
// many aircraft are being tracked, how many frames have come in, and where
// from.
func (s *Scene) drawStats(col *layout, frame source.Frame) {
	face := s.faces.Small

	height := lineHeight(face)
	if height == 0 || !col.fits(height) {
		return
	}

	top := col.bottom - height

	pen := drawBytes(col.dst, face, col.left, top, s.count(len(frame.Planes)), s.pal.Ink)
	pen = text.Draw(col.dst, face, pen, top, statsAircraft, s.pal.Muted)
	pen = drawBytes(col.dst, face, pen, top, s.counter(frame.Stats.TotalFrames), s.pal.Ink)
	pen = text.Draw(col.dst, face, pen, top, statsSeparator, s.pal.Muted)
	text.Draw(col.dst, face, pen, top, clip(frame.Source.Label, maxSourceLabel), s.pal.Muted)

	col.bottom -= height + blockGap
}
