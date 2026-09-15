package specimen

import (
	"image"
	"image/color"

	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// drawKeyBar puts the key legend on the bottom edge and takes the room it
// used out of the layout, so everything above flows into what is left.
//
// It runs first for that reason: the bar belongs to the bottom of the screen
// whatever else is on it, and a bar pushed down by a block above it would end
// up somewhere in the middle.
func (s *Scene) drawKeyBar(lay *layout) {
	capHeight := lineHeight(s.faces.Small)
	if capHeight == 0 {
		return
	}

	height := capHeight + 2*capPadY
	if !lay.fits(height) {
		return
	}

	top := lay.bottom - height
	pen := lay.left

	for _, entry := range keyCaps {
		pen = s.drawCap(lay.dst, pen, top, entry.key)
		pen += capGap
		pen = text.Draw(lay.dst, s.faces.Small, pen, top+capPadY, entry.label, s.pal.Muted)
		pen += entryGap
	}

	lay.bottom -= height + blockGap
}

// drawCap draws one key cap, the letter knocked out of a filled box, and
// returns the x just past it.
func (s *Scene) drawCap(dst *canvas.Canvas, left, top int, key string) int {
	width, height := text.Measure(s.faces.Small, key)

	box := image.Rect(left, top, left+width+2*capPadX, top+height+2*capPadY)
	dst.FillRect(box, s.pal.Ink)
	text.Draw(dst, s.faces.Small, left+capPadX, top+capPadY, key, s.pal.Field)

	return box.Max.X
}

// drawHeader draws the band across the top: the wordmark on the left, the two
// clocks on the right, a hairline under both.
//
// The band is filled with pal.Band and both texts are set in pal.BandInk
// rather than the scene's usual muted/ink split, because paper's band is a
// navy strip and needs its own contrast rather than the page's. Night's Band
// and BandInk repeat Field and Ink, so there the band is invisible and the
// text reads exactly as it did before the band existed.
func (s *Scene) drawHeader(lay *layout) {
	titleHeight := lineHeight(s.faces.BodyBold)
	clockHeight := lineHeight(s.faces.Large)

	if titleHeight == 0 || clockHeight == 0 {
		return
	}

	band := max(titleHeight, clockHeight) + 2*headerPadY

	total := band + ruleHeight + blockGap
	if !lay.fits(total) {
		return
	}

	lay.dst.FillRect(image.Rect(lay.left, lay.y, lay.right, lay.y+band), s.pal.Band)

	baseline := lay.y + band - headerPadY

	text.Draw(lay.dst, s.faces.BodyBold, lay.left, baseline-titleHeight, appTitle, s.pal.BandInk,
		text.WithSpacing(headerTracking))
	s.drawClocks(lay, lay.y+band/2)

	rule := lay.y + band
	lay.dst.FillRect(image.Rect(lay.left, rule, lay.right, rule+ruleHeight), s.pal.Rule)

	lay.advance(total)
}

// drawClocks sets the local clock large on the right of the band, with UTC
// beside it in the body face.
//
// UTC comes off the same injected clock rather than off time.Now, so a scene
// rendered with a fixed clock in a test produces the same two strings every
// run.
func (s *Scene) drawClocks(lay *layout, middle int) {
	now := s.now()

	pen := s.drawClock(lay, lay.right, middle, labelLocal, s.faces.Large, now.Format(clockFormat))
	s.drawClock(lay, pen-clockGap, middle, labelUTC, s.faces.Body, now.UTC().Format(clockFormat)+utcSuffix)
}

// drawClock sets one labelled clock ending at rightX and returns the x its
// label starts at.
//
// The pair is measured and then drawn left to right rather than drawn right to
// left, so the label and the clock keep their own colours without the two
// drifting apart as the minutes change width.
func (s *Scene) drawClock(
	lay *layout, rightX, middle int, label string, face *psf.Font, value string,
) int {
	small := s.faces.Small

	labelWidth, labelHeight := text.Measure(small, label, text.WithSpacing(labelTracking))
	valueWidth, valueHeight := text.Measure(face, value)
	left := rightX - labelWidth - clockLabelGap - valueWidth

	text.Draw(lay.dst, small, left, middle-labelHeight/2, label, s.pal.BandInk, text.WithSpacing(labelTracking))
	text.Draw(lay.dst, face, left+labelWidth+clockLabelGap, middle-valueHeight/2, value, s.pal.BandInk)

	return left
}

// drawCard draws the selected-flight card: the numbered label, the callsign
// set large, the aircraft type and track right-aligned beside it, and the
// three figures along the bottom, all inside a hairline border with an accent
// bar down the left edge.
func (s *Scene) drawCard(lay *layout) {
	labelHeight := lineHeight(s.faces.Small)
	detailHeight := lineHeight(s.faces.Body)
	figureHeight := lineHeight(s.faces.Large)

	if labelHeight == 0 || detailHeight == 0 || figureHeight == 0 {
		return
	}

	titleHeight := figureHeight * cardTitleScale
	middle := max(titleHeight, 2*detailHeight)
	inner := labelHeight + rowGap + middle + rowGap + figureHeight
	total := 2*cardPadY + inner

	if !lay.fits(total + blockGap) {
		return
	}

	s.cardFrame(lay, total)

	left := lay.left + accentWidth + cardPadX
	right := lay.right - cardPadX
	pen := lay.y + cardPadY

	text.Draw(lay.dst, s.faces.Small, left, pen, cardLabel, s.pal.Muted, text.WithSpacing(labelTracking))
	pen += labelHeight + rowGap

	text.Draw(lay.dst, s.faces.Large, left, pen, cardCallsign, s.pal.Ink, text.WithScale(cardTitleScale))
	text.DrawRight(lay.dst, s.faces.Body, right, pen, cardType, s.pal.Ink)
	text.DrawRight(lay.dst, s.faces.Body, right, pen+detailHeight, cardTrack, s.pal.Muted)
	pen += middle + rowGap

	s.drawFigures(lay, left, right, pen)

	lay.advance(total + blockGap)
}

// cardFrame draws the border and the accent bar.
func (s *Scene) cardFrame(lay *layout, total int) {
	box := image.Rect(lay.left, lay.y, lay.right, lay.y+total)
	right, bottom := box.Max.X-1, box.Max.Y-1

	lay.dst.Line(box.Min.X, box.Min.Y, right, box.Min.Y, s.pal.Rule)
	lay.dst.Line(box.Min.X, bottom, right, bottom, s.pal.Rule)
	lay.dst.Line(box.Min.X, box.Min.Y, box.Min.X, bottom, s.pal.Rule)
	lay.dst.Line(right, box.Min.Y, right, bottom, s.pal.Rule)

	lay.dst.FillRect(image.Rect(box.Min.X, box.Min.Y, box.Min.X+accentWidth, box.Max.Y), s.pal.Accent)
}

// drawFigures sets the three numbers along the bottom of the card, each with
// its unit small and muted beside it.
//
// The column is the widest figure plus its unit, not a third of the card, so
// the three stay a group the eye reads together. Dividing the card would
// scatter them to the far corners on a wide canvas and pile them up on a
// narrow one.
func (s *Scene) drawFigures(lay *layout, left, right, top int) {
	column := s.figureColumn()
	unitTop := top + lineHeight(s.faces.Large) - lineHeight(s.faces.Small)

	for index, fig := range figures {
		start := left + index*column
		if start >= right {
			return
		}

		pen := text.Draw(lay.dst, s.faces.Large, start, top, fig.value, s.pal.Ink)
		text.Draw(lay.dst, s.faces.Small, pen+unitGap, unitTop, fig.unit, s.pal.Muted)
	}
}

// figureColumn measures how much room one figure and its unit need.
func (s *Scene) figureColumn() int {
	widest := 0

	for _, fig := range figures {
		value, _ := text.Measure(s.faces.Large, fig.value)
		unit, _ := text.Measure(s.faces.Small, fig.unit)
		widest = max(widest, value+unitGap+unit)
	}

	return widest + figureGap
}

// drawRows draws the compact one-line rows under the card.
//
// The columns are placed from the widest cell in each one, measured in the
// face they will be drawn in, rather than padded out with spaces. Spaces only
// line up while every cell happens to be the same number of characters, and
// the first five-letter callsign would break it.
func (s *Scene) drawRows(lay *layout) {
	line := lineHeight(s.faces.Body)
	if line == 0 {
		return
	}

	total := len(flightRows) * (line + rowLead)
	if !lay.fits(total + blockGap) {
		return
	}

	widths := s.columnWidths()
	top := lay.y

	for _, row := range flightRows {
		pen := lay.left

		for index, cell := range row {
			text.Draw(lay.dst, s.faces.Body, pen, top, cell, s.cellInk(index))
			pen += widths[index] + columnGap
		}

		top += line + rowLead
	}

	lay.advance(total + blockGap)
}

// columnWidths measures the widest cell in each column.
func (s *Scene) columnWidths() [columns]int {
	var widths [columns]int

	for _, row := range flightRows {
		for index, cell := range row {
			width, _ := text.Measure(s.faces.Body, cell)
			widths[index] = max(widths[index], width)
		}
	}

	return widths
}

// cellInk picks the colour for one cell of a compact row.
func (s *Scene) cellInk(column int) color.RGBA {
	if column == callsignColumn {
		return s.pal.Ink
	}

	return s.pal.Muted
}

// drawFaces draws the font sample: every face in turn, under its own name.
//
// A face that would not fit stops the block rather than being skipped over,
// because the faces are listed smallest first and anything after one that did
// not fit is taller still.
func (s *Scene) drawFaces(lay *layout) {
	labelHeight := lineHeight(s.faces.Small)
	if labelHeight == 0 {
		return
	}

	for _, entry := range s.faceList() {
		sample := lineHeight(entry.face)
		if sample == 0 {
			continue
		}

		block := labelHeight + labelLead + sample + blockGap
		if !lay.fits(block) {
			return
		}

		text.Draw(lay.dst, s.faces.Small, lay.left, lay.y, entry.name, s.pal.Muted,
			text.WithSpacing(labelTracking))
		text.Draw(lay.dst, entry.face, lay.left, lay.y+labelHeight+labelLead, sampleText, s.pal.Ink)

		lay.advance(block)
	}

	s.drawCredit(lay, labelHeight)
}

// drawCredit prints the font attribution under the sample. The licence asks
// for it, and the specimen is the one screen where it belongs.
func (s *Scene) drawCredit(lay *layout, labelHeight int) {
	if !lay.fits(labelHeight + blockGap) {
		return
	}

	text.Draw(lay.dst, s.faces.Small, lay.left, lay.y, creditText, s.pal.Muted)
	lay.advance(labelHeight + blockGap)
}

// faceList names the four faces for the sample block. The labels carry the
// pixel sizes rather than the file names, because the size is what a reader
// is comparing when they look at this.
func (s *Scene) faceList() [4]namedFace {
	return [4]namedFace{
		{name: "TERMINUS 6x12", face: s.faces.Small},
		{name: "TERMINUS 8x16", face: s.faces.Body},
		{name: "TERMINUS BOLD 8x16", face: s.faces.BodyBold},
		{name: "TERMINUS BOLD 16x32", face: s.faces.Large},
	}
}
