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
const statsAircraft = " AIRCRAFT / "

// legendEntry is one altitude band and the colour that means it.
type legendEntry struct {
	col   color.RGBA
	label string
}

// drawColumn fills the right-hand column.
//
// Everything with a fixed height takes its room first: the stats line, the
// legend and the details block off the bottom, the card off the top. The
// compact rows get what is left, which is what fills the column at any height
// rather than leaving the hole the first version of this layout had under the
// list.
//
// The order the blocks are called in is the order they claim space, not the
// order they appear on screen. Reading down the frame it is card, rows,
// details, legend, stats.
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
	s.drawLegend(&col, frame)
	s.drawCard(&col, frame)
	s.drawDetails(&col, frame)
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

	text.Draw(dst, large, box.Min.X, box.Min.Y, clip(callsignOf(plane), maxCallsign), s.callsignInk(plane),
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

	s.drawFigure(dst, box.Min.X, box.Min.Y, unitTop,
		figure{value: s.distance(away), unit: unitNm, ink: s.pal.Ink})
	s.drawFigure(dst, box.Min.X+column, box.Min.Y, unitTop,
		figure{value: s.thousands(plane.Altitude), unit: unitFT, ink: s.bandColour(plane.Altitude)})
	s.drawFigure(dst, box.Min.X+2*column, box.Min.Y, unitTop,
		figure{value: s.whole(plane.Velocity), unit: unitKT, ink: s.pal.Ink})
}

// figure is one of the three numbers along the bottom of the card.
//
// The three parts travel together because they belong to one cell, and because
// the altitude figure takes its own ink: passing a sixth loose argument to
// drawFigure would have made its signature the longest in the package for no
// gain in clarity.
type figure struct {
	value []byte
	unit  string
	ink   color.RGBA
}

// drawFigure draws one number with its unit.
//
// Each figure is formatted immediately before it is drawn because they all
// share the scene's one scratch buffer; formatting all three first would
// leave three slices of the same bytes. The unit stays muted whatever the
// number is set in: the number is the reading and the unit is the label.
func (s *Scene) drawFigure(dst *canvas.Canvas, left, top, unitTop int, fig figure) {
	pen := drawBytes(dst, s.faces.Large, left, top, fig.value, fig.ink)
	text.Draw(dst, s.faces.Small, pen+unitGap, unitTop, fig.unit, s.pal.Muted)
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

// drawLegend explains what the colours on the scope mean, which is the one
// thing there that cannot be worked out by looking at it.
//
// What it explains depends on the colour mode, so the two versions are two
// functions rather than one with a branch in the middle of it: the altitude
// legend is a fixed list of three and the airline legend is counted off the
// frame.
func (s *Scene) drawLegend(col *layout, frame source.Frame) {
	face := s.faces.Small

	height := lineHeight(face)
	if height == 0 || !col.fits(height) {
		return
	}

	top := col.bottom - height

	if s.colour == ColourAirline {
		s.drawAirlineLegend(col, face, top, frame)
	} else {
		s.drawBandLegend(col, face, top)
	}

	col.bottom -= height + blockGap
}

// drawBandLegend names the three altitude bands.
//
// An entry that will not fit whole is dropped rather than half drawn. The test
// measures the label as well as the swatch, because a swatch that fits with a
// label that does not is the case a narrow column actually produces, and the
// label is the part that would have run into the margin.
func (s *Scene) drawBandLegend(col *layout, face *psf.Font, top int) {
	pen := col.left

	for _, entry := range s.legendEntries() {
		width, _ := text.Measure(face, entry.label, text.WithSpacing(labelTracking))
		if pen+swatchSide+swatchGap+width > col.right {
			break
		}

		box := image.Rect(pen, top, pen+swatchSide, top+swatchSide)
		col.dst.FillRect(box, entry.col)

		pen = text.Draw(col.dst, face, box.Max.X+swatchGap, top, entry.label, s.pal.Muted,
			text.WithSpacing(labelTracking))
		pen += legendGap
	}
}

// drawAirlineLegend names the operators with the most aircraft on the scope,
// then OTHER when anything on the field has no colour of its own.
//
// Every entry gets an equal slice of the column and its name is cut to what is
// left of that slice. A legend measured from the names instead would put the
// swatches in a different place on every frame, since the names change as
// aircraft come and go.
func (s *Scene) drawAirlineLegend(col *layout, face *psf.Font, top int, frame source.Frame) {
	s.counts.reset()

	for _, plane := range frame.Planes {
		s.counts.add(plane.Callsign)
	}

	s.counts.rank()

	slots := s.counts.shown
	if s.counts.other {
		slots++
	}

	if slots == 0 {
		return
	}

	width := (col.right - col.left) / slots

	for index := range s.counts.shown {
		s.drawOperatorEntry(col.dst, face, image.Pt(col.left+index*width, top), width, s.counts.seen[index])
	}

	if s.counts.other {
		s.drawOtherEntry(col.dst, face, image.Pt(col.left+s.counts.shown*width, top))
	}
}

// drawOperatorEntry sets one airline's swatch, designator and name.
func (s *Scene) drawOperatorEntry(
	dst *canvas.Canvas, face *psf.Font, origin image.Point, width int, entry operatorCount,
) {
	box := image.Rect(origin.X, origin.Y, origin.X+swatchSide, origin.Y+swatchSide)
	dst.FillRect(box, s.operatorColour(entry.airline))

	pen := text.Draw(dst, face, box.Max.X+swatchGap, origin.Y, entry.airline.ICAO, s.pal.Ink,
		text.WithSpacing(labelTracking))
	pen += swatchGap

	room := origin.X + width - legendGap - pen
	text.Draw(dst, face, pen, origin.Y, clip(entry.airline.Name, fitRunes(face, room, labelTracking)), s.pal.Muted,
		text.WithSpacing(labelTracking))
}

// drawOtherEntry closes the airline legend, standing for every aircraft the
// database has no colour for.
func (s *Scene) drawOtherEntry(dst *canvas.Canvas, face *psf.Font, origin image.Point) {
	box := image.Rect(origin.X, origin.Y, origin.X+swatchSide, origin.Y+swatchSide)
	dst.FillRect(box, s.pal.Muted)

	text.Draw(dst, face, box.Max.X+swatchGap, origin.Y, legendOther, s.pal.Muted, text.WithSpacing(labelTracking))
}

// fitRunes is how many glyphs of face fit in width pixels at this tracking.
// The last glyph carries no gap after it, which is why the tracking is added
// back before the divide.
func fitRunes(face *psf.Font, width, tracking int) int {
	step := glyphWidth(face) + tracking
	if step <= 0 {
		return 0
	}

	return max((width+tracking)/step, 0)
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
// many aircraft are being tracked and where from.
//
// The frame count used to sit in this line too, but it changes every tick and
// was more distracting than informative next to numbers that only change when
// something in the sky does. It is still in source.Frame.Stats for whatever
// wants it; it just does not go on screen any more.
func (s *Scene) drawStats(col *layout, frame source.Frame) {
	face := s.faces.Small

	height := lineHeight(face)
	if height == 0 || !col.fits(height) {
		return
	}

	top := col.bottom - height

	pen := drawBytes(col.dst, face, col.left, top, s.count(len(frame.Planes)), s.pal.Ink)
	pen = text.Draw(col.dst, face, pen, top, statsAircraft, s.pal.Muted)
	text.Draw(col.dst, face, pen, top, clip(frame.Source.Label, maxSourceLabel), s.pal.Muted)

	col.bottom -= height + blockGap
}
