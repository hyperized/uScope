package radar

import (
	"image"
	"image/color"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/psf"
)

// The bare 3D view's board column.
//
// The bare 3D view has no way to ask who an aeroplane is, and nobody is at
// the keyboard of a big screen. The column puts the strip board down the right of the
// picture: a count, the strips, and a legend for the colours on both.
const (
	// boardColumnDivisor is the share of the canvas width the column takes: a
	// quarter, which at 2560 pixels and scale 2 holds the rank, the callsign,
	// the level, the speed, the distance and DIR, and leaves the picture three
	// quarters of a 16:9 frame, still wider than it is tall.
	boardColumnDivisor = 4

	// boardOfInfix and boardCountSuffix are the title's words: "8 OF 12
	// AIRCRAFT" while a filter is holding some back, "12 AIRCRAFT" when not.
	boardOfInfix     = " OF "
	boardCountSuffix = " AIRCRAFT"

	// legendIn and legendOut are the legend's two corridor entries.
	legendIn  = dirInText
	legendOut = dirOutText
)

// boardShown reports whether the column is on screen: the bare 3D view with
// j pressed, or --minimal-board on.
func (s *Scene) boardShown() bool { return s.shown == ViewMinimal3D && s.board }

// toggleBoard flips the column, which is what j does.
//
// It reports false outside the bare 3D view, so the key falls through there
// the way h does outside the bare views. The full views have a column of their
// own, and the bare flat view is the picture the handheld runs edge to edge.
func (s *Scene) toggleBoard() bool {
	if s.shown != ViewMinimal3D {
		return false
	}

	s.board = !s.board

	return true
}

// carveBoard takes the column off the right of the scope box and returns it,
// or the zero rectangle when the column is off.
//
// The width is a quarter of the canvas rather than of the scope box, so the
// bar across the top and the column down the side are measured from the same
// thing and do not shift against each other when one of them is switched.
func (s *Scene) carveBoard(lay *layout) image.Rectangle {
	if !s.boardShown() {
		return image.Rectangle{}
	}

	column := lay.scope
	column.Min.X = max(column.Max.X-lay.dst.Bounds().Dx()/boardColumnDivisor, column.Min.X)
	lay.scope.Max.X = column.Min.X

	return column
}

// boardFraming is the share of the canvas width the 3D picture has to frame
// itself in: one, or the scope box's share once the column has taken its part.
//
// newCamera3 frames the outer ring on the box's width with a lens as wide as
// the box. Narrowing the box narrows the lens and the ring with it, so the
// picture would shrink to three quarters and leave bands of sky and ground
// above and below. Scaling the range the camera is framed on by the same share
// brings the camera in until the ring is the size on screen it was on the
// whole canvas, and the picture fills the box top to bottom again. The lens
// stays the box's own width, which is what the sky's horizon row and the stars
// are worked out from, so the three still agree.
func (s *Scene) boardFraming(lay *layout) float64 {
	if !s.boardShown() {
		return 1
	}

	// A canvas is never zero pixels wide: canvas.New refuses one.
	return float64(lay.scope.Dx()) / float64(lay.dst.Bounds().Dx())
}

// drawBoardColumn paints the column: the field, a hairline down its left
// edge, and the title, the board and the legend inside the frame's margin.
//
// The fill goes down after the picture. In the bare 3D view the sky is the
// frame's only clear and it only clears the scope box, so without the fill the
// column would show whatever the last frame left there.
func (s *Scene) drawBoardColumn(lay *layout, frame source.Frame, column image.Rectangle) {
	if column.Empty() {
		return
	}

	rule := lay.px(ruleHeight)
	margin := lay.dst.Bounds().Max.X - lay.right

	lay.dst.FillRect(column, s.pal.Field)
	lay.dst.FillRect(image.Rect(column.Min.X, column.Min.Y, column.Min.X+rule, column.Max.Y), s.pal.Rule)

	// The right edge is held off by a strip's own padding rather than by the
	// frame's margin. The board's fields are sized for their widest values, and
	// at a quarter of the canvas the eight pixels the margin would take are the
	// difference between keeping GS and dropping it.
	col := layout{
		dst:    lay.dst,
		left:   column.Min.X + rule,
		right:  column.Max.X - lay.px(stripPadX),
		top:    column.Min.Y + margin,
		bottom: column.Max.Y - margin,
		labels: lay.labels,
		scale:  lay.scale,
	}

	s.drawBoardLegend(&col)

	if !s.drawBoardTitle(&col, len(frame.Planes)) {
		return
	}

	plan, drawable := s.planBoardStrips(&col, stripsLeft(&col), col.right)
	if !drawable {
		return
	}

	s.drawPlannedStrips(&col, frame, plan, notSelected)
}

// drawBoardTitle writes the count over the board, with the filter's word set
// right on the same line, and takes its room off the top. It reports false
// when the column has no room for it, which leaves no room for a board either.
//
// It is what drawStripStatus is to the scope's column with the selection taken
// out, a size up: someone reads it from across a room rather than from a
// hand's length.
func (s *Scene) drawBoardTitle(col *layout, total int) bool {
	face := s.faces.Body

	height := col.lineHeight(face)
	if height == 0 || !col.fits(height) {
		return false
	}

	top, pen := col.top, stripsLeft(col)

	if s.filter.active() {
		pen = col.drawBytesTracked(face, pen, top, s.shownCount(s.shownPlanes()), s.pal.Data)
		pen = col.tracked(face, pen, top, boardOfInfix, s.pal.Data)

		label := s.filter.label()
		col.tracked(face, col.right-col.measureTracked(face, label), top, label, s.pal.Ink)
	}

	pen = col.drawBytesTracked(face, pen, top, s.count(total), s.pal.Data)
	col.tracked(face, pen, top, boardCountSuffix, s.pal.Data)

	col.top += height + col.px(blockGap)

	return true
}

// boardKey is one entry of the column's legend: a swatch and the word for it.
// outline draws the swatch as a hairline box, which is how the corridor
// entries say they are a direction rather than a band of altitude.
type boardKey struct {
	col     color.RGBA
	label   string
	outline bool
}

// maxBoardKeys is the most entries the column's legend can carry: the three
// altitude bands, IN and OUT. Corridor colouring's three classes fit inside
// it.
const maxBoardKeys = bandCount + 2

// boardKeys is what the column's legend lists and how many entries of the
// array are used.
//
// In altitude colouring that is the bands the filter is letting through, then
// IN and OUT. A band the filter is hiding has nothing on screen to explain,
// and on a quarter of the canvas the room it would take is the room IN and
// OUT need. In airline colouring the operators keep the legend drawLegend
// draws for them, a row of their own, and these are only IN and OUT. In
// corridor colouring the classes are the colours on the picture, so they are
// the whole legend, filled the way a band is: an outlined IN beside a filled
// one would be the same word explained twice. A class the filter is hiding is
// left out for the reason a band is.
func (s *Scene) boardKeys() ([maxBoardKeys]boardKey, int) {
	var keys [maxBoardKeys]boardKey

	count := 0

	if s.colour == ColourCorridor {
		for _, entry := range s.classEntries() {
			if s.filter.kind == filterClass && !s.filter.marksClass(entry.class) {
				continue
			}

			keys[count] = boardKey{col: entry.col, label: entry.label}
			count++
		}

		return keys, count
	}

	if s.colour != ColourAirline {
		for band, entry := range s.legendEntries() {
			if s.filter.active() && !s.filter.marksBand(band) {
				continue
			}

			keys[count] = boardKey{col: entry.col, label: entry.label}
			count++
		}
	}

	keys[count] = boardKey{col: s.classColour(source.TrackInbound), label: legendIn, outline: true}
	keys[count+1] = boardKey{col: s.classColour(source.TrackOutbound), label: legendOut, outline: true}

	return keys, count + 2
}

// drawBoardLegend sets the legend along the bottom of the column and takes its
// room off the bottom, wrapping onto a second row when one will not hold every
// entry.
//
// The rows are counted before anything is drawn, because they are set from the
// bottom up and the first entry belongs on the top one.
func (s *Scene) drawBoardLegend(col *layout) {
	if s.colour == ColourAirline {
		s.drawLegend(col)
	}

	face := s.faces.Small

	height := col.lineHeight(face)
	if height == 0 {
		return
	}

	keys, count := s.boardKeys()
	entries := keys[:count]
	left := stripsLeft(col)

	rows := boardKeyRows(col, face, entries, left)
	pitch := height + col.px(rowLead)

	if !col.fits(rows * pitch) {
		return
	}

	top := col.bottom - rows*pitch + col.px(rowLead)
	pen := left

	for _, key := range entries {
		var fits bool

		pen, top, fits = boardKeySlot(col, face, key, pen, top, left, pitch)
		if !fits {
			continue
		}

		s.drawBoardKey(col, face, key, pen, top)
		pen += boardKeyWidth(col, face, key) + col.px(legendGap)
	}

	col.bottom -= rows*pitch + col.px(blockGap)
}

// boardKeyRows is how many rows the entries take. It walks them through
// boardKeySlot the way the drawing does, with the row number standing in for
// the y and a pitch of one, so the count and the drawing cannot disagree.
func boardKeyRows(col *layout, face *psf.Font, entries []boardKey, left int) int {
	rows, pen := 1, left

	for _, key := range entries {
		var (
			fits bool
			top  int
		)

		pen, top, fits = boardKeySlot(col, face, key, pen, rows, left, 1)
		if fits {
			rows = top
			pen += boardKeyWidth(col, face, key) + col.px(legendGap)
		}
	}

	return rows
}

// boardKeySlot is where one entry goes: on the row at top from pen if it fits
// there, or at the start of the next row, pitch further down, if it does not.
// It reports false for an entry too wide for a row of its own, which is
// skipped rather than drawn into the margin.
func boardKeySlot(
	col *layout, face *psf.Font, key boardKey, pen, top, left, pitch int,
) (int, int, bool) {
	width := boardKeyWidth(col, face, key)

	if pen+width <= col.right {
		return pen, top, true
	}

	if left+width > col.right {
		return pen, top, false
	}

	return left, top + pitch, true
}

// boardKeyWidth is the room one entry takes: its swatch, the gap and the word.
func boardKeyWidth(col *layout, face *psf.Font, key boardKey) int {
	return col.px(swatchSide) + col.px(swatchGap) + col.measureTracked(face, key.label)
}

// drawBoardKey sets one entry at pen on the row at top. The caller steps on by
// boardKeyWidth rather than by what the type returns, so the rows drawn are
// the rows boardKeyRows counted.
func (s *Scene) drawBoardKey(col *layout, face *psf.Font, key boardKey, pen, top int) {
	side := col.px(swatchSide)
	box := image.Rect(pen, top, pen+side, top+side)

	if key.outline {
		col.dst.StrokeRect(box, col.unit(), key.col)
	} else {
		col.dst.FillRect(box, key.col)
	}

	col.tracked(face, box.Max.X+col.px(swatchGap), top, key.label, s.pal.Muted)
}
