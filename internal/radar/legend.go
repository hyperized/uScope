package radar

import (
	"image"
	"image/color"
	"math"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// The right column's fixed strings and the caps on what is drawn from a decoded
// field.
const (
	// filteredTag closes the selected strip's second line while the filter is
	// hiding the aircraft that strip is about. It carries its own separator so
	// it can be drawn straight after whatever came before it.
	filteredTag = " / FILTERED"

	squawkPrefix = "SQ "

	// distanceDecimals is one place. A tenth of a nautical mile is about 180
	// metres, which is finer than the position under it is worth.
	distanceDecimals = 1

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

	// swatchMark is how far outside a swatch the ring round the filtered entry
	// is drawn, so the legend says which of its own rows the scope is showing.
	//
	// A ring rather than a brighter swatch or a second colour: the swatches are
	// the one place in the scene where a colour means an altitude or an
	// operator and nothing else, and changing one to mark a selection would be
	// the legend lying about the thing it exists to explain.
	swatchMark = 2

	legendLow  = "< 10K FT"
	legendMid  = "10-25K FT"
	legendHigh = "> 25K FT"
)

// legendEntry is one altitude band and the colour that means it.
type legendEntry struct {
	col   color.RGBA
	label string
}

// drawColumn fills the right-hand column.
//
// The legend takes its room off the bottom first, because it is the one block
// there with a fixed height; the flight strips get everything left over, which
// is what fills the column at any height.
//
// The order the two are called in is the order they claim space, not the order
// they appear on screen. Reading down the frame it is the aircraft count, the
// selected flight's strip, the half strips under it and then the legend.
//
// An empty column is the answer on a canvas too narrow to hold one and on a
// scope the w key has widened, and there is nothing to draw either way. The
// count goes with the strips in the second case: it sits on their own line, and
// a filter is still legible from the F softkey in the bar.
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

	s.drawLegend(&col)
	s.drawStrips(&col, frame)
}

// selection is the aircraft the full strip is about.
//
// It is a struct rather than a pair of values because the filter added a third
// state. An aircraft can be selected and on the field, selected and hidden by
// the filter, or there can be nothing to select at all.
type selection struct {
	plane airplane.Snapshot

	// found is whether anything is selected. False is an empty sky, which is
	// what puts NO TRAFFIC where the callsign goes.
	found bool

	// hidden is whether the filter is keeping the selection off the field. The
	// pin survives that, so the board keeps drawing the aircraft and says
	// FILTERED on its second line until the filter widens again.
	hidden bool
}

// selectedPlane is the aircraft the full strip is about.
//
// A pinned aircraft the filter is hiding is found by ICAO rather than by index,
// because it has no index: the ICAO list holds the aircraft the filter let
// through, and this one is not among them.
func (s *Scene) selectedPlane(frame source.Frame) selection {
	if s.selHidden {
		plane, flying := findPlane(frame, s.selICAO)

		return selection{plane: plane, found: flying, hidden: flying}
	}

	if s.selIndex < 0 || s.selIndex >= len(s.icaos) {
		return selection{}
	}

	plane, flying := findPlane(frame, s.icaos[s.selIndex])

	return selection{plane: plane, found: flying}
}

// distance writes a range in nautical miles, or a dash when the aircraft has
// no position.
//
// uAirwaves reports MaxFloat64 rather than an error for an unknown position, so
// that sentinel has to be caught here or the strip would claim the aircraft is
// 179769313486231570000... nautical miles away.
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
func (s *Scene) drawLegend(col *layout) {
	face := s.faces.Small

	height := lineHeight(face)
	if height == 0 || !col.fits(height) {
		return
	}

	top := col.bottom - height

	if s.colour == ColourAirline {
		s.drawAirlineLegend(col, face, top)
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

	for band, entry := range s.legendEntries() {
		width, _ := text.Measure(face, entry.label, text.WithSpacing(labelTracking))
		if pen+swatchSide+swatchGap+width > col.right {
			break
		}

		box := image.Rect(pen, top, pen+swatchSide, top+swatchSide)
		col.dst.FillRect(box, entry.col)
		s.markLegendEntry(col.dst, box, s.filter.marksBand(band))

		pen = text.Draw(col.dst, face, box.Max.X+swatchGap, top, entry.label, s.pal.Muted,
			text.WithSpacing(labelTracking))
		pen += legendGap
	}
}

// markLegendEntry rings a legend swatch when the filter is on that entry, and
// does nothing when it is not.
//
// The ring sits outside the swatch rather than inside it, so the colour the
// legend is explaining keeps every one of its own pixels. It is drawn in the
// reading ink because it is a statement about the scene rather than another
// colour to look up.
//
//nolint:revive // flag-parameter: marked picks whether to draw, not a mode to branch deeper on.
func (s *Scene) markLegendEntry(dst *canvas.Canvas, box image.Rectangle, marked bool) {
	if !marked {
		return
	}

	dst.Rect(box.Inset(-swatchMark), s.pal.Ink)
}

// drawAirlineLegend names the operators with the most aircraft on the scope,
// then OTHER when anything on the field has no colour of its own.
//
// Every entry gets an equal slice of the column and its name is cut to what is
// left of that slice. A legend measured from the names instead would put the
// swatches in a different place on every frame, since the names change as
// aircraft come and go.
//
// The tally is counted at the top of Draw rather than here, because the f key's
// cycle walks these same entries in this same order and the minimal and 3D
// views have no legend to count one. See countOperators.
func (s *Scene) drawAirlineLegend(col *layout, face *psf.Font, top int) {
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
	s.markLegendEntry(dst, box, s.filter.marksOperator(entry.airline.ICAO))

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
	s.markLegendEntry(dst, box, s.filter.marksOther())

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
//
// It is indexed by the band constants rather than written out in order, because
// the f key selects a band by index and the legend has to ring the one it
// selected. Two lists in the same order by coincidence would be one reordering
// away from ringing the wrong row.
func (s *Scene) legendEntries() [bandCount]legendEntry {
	return [bandCount]legendEntry{
		bandLow:  {col: s.pal.AltLow, label: legendLow},
		bandMid:  {col: s.pal.AltMid, label: legendMid},
		bandHigh: {col: s.pal.AltHigh, label: legendHigh},
	}
}
