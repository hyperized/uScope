package radar

import (
	"image"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/text"
)

// The header band's fixed strings and spacing.
const (
	appTitle    = "USCOPE"
	clockFormat = "15:04"

	// sourceGap is the air between the wordmark and the source dot, dotRadius
	// the dot itself, and dotGap the air before the source label.
	sourceGap = 14
	dotRadius = 3
	dotGap    = 6

	// estimatePrefix opens the receiver line when the position was worked out
	// from the aircraft rather than sensed. The plus-minus says out loud that
	// this is a guess with a radius, not a fix.
	estimatePrefix = "EST ±"

	// noFixText is the receiver line when nothing is known. It is set in caps
	// like every other label in the scene rather than in the sentence case
	// uAirwaves uses, because next to USCOPE and EST a lower-case line reads
	// as a different kind of thing.
	noFixText = "NO FIX"

	// coordinateSeparator sits between the two halves of a position.
	coordinateSeparator = " / "

	// maxSourceLabel is how much of the source label is drawn. A BEAST
	// address is operator-supplied and could be any length; the stats line is
	// not the place to find that out.
	maxSourceLabel = 24
)

// keyCap is one entry in the bottom bar: the cap, then what the key does.
type keyCap struct {
	key   string
	label string
}

// keyCaps is the key legend, in the order the keys are worth reaching for.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var keyCaps = [...]keyCap{
	{key: "Q", label: "QUIT"},
	{key: "N/P", label: "SELECT"},
	{key: "+/-", label: "RANGE"},
	{key: "A", label: "AUTO"},
	{key: "T", label: "TRAILS"},
	{key: "L", label: "THEME"},
	{key: "S", label: "SCENE"},
}

// drawKeyBar puts the key legend on the bottom edge and takes the room it
// used out of the layout, so everything above flows into what is left.
//
// It runs first for that reason: the bar belongs to the bottom of the screen
// whatever else is on it, and a bar pushed down by a block above would end up
// somewhere in the middle.
func (s *Scene) drawKeyBar(lay *layout) {
	face := s.faces.Small

	capHeight := lineHeight(face)
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
		pen = text.Draw(lay.dst, face, pen, top+capPadY, entry.label, s.pal.Muted)
		pen += entryGap

		if pen >= lay.right {
			break
		}
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

// drawHeader draws the band across the top: the wordmark and the source on
// the left with the receiver position under them, the clock on the right, a
// hairline under all of it.
//
// The band is filled with pal.Band and everything on it is set in pal.BandInk
// rather than the scene's usual muted/ink split, because paper's band is a
// navy strip and needs its own contrast rather than the page's. Night's Band
// and BandInk repeat Field and Ink, so there the band is invisible and the
// text reads exactly as it did before the band existed.
func (s *Scene) drawHeader(lay *layout, frame source.Frame) {
	markHeight := lineHeight(s.faces.BodyBold)
	smallHeight := lineHeight(s.faces.Small)
	clockHeight := lineHeight(s.faces.Large)

	if markHeight == 0 || smallHeight == 0 || clockHeight == 0 {
		return
	}

	band := max(markHeight+rowLead+smallHeight, clockHeight) + 2*headerPadY

	total := band + ruleHeight + blockGap
	if !lay.fits(total) {
		return
	}

	lay.dst.FillRect(image.Rect(lay.left, lay.top, lay.right, lay.top+band), s.pal.Band)

	top := lay.top + headerPadY

	s.drawWordmark(lay, top, frame)
	s.drawReceiverLine(lay, top+markHeight+rowLead, frame.Receiver)

	clock := frame.Now.AppendFormat(s.clock[:0], clockFormat)
	drawBytesRight(lay.dst, s.faces.Large, lay.right, lay.top+(band-clockHeight)/2, clock, s.pal.BandInk)

	rule := lay.top + band
	lay.dst.FillRect(image.Rect(lay.left, rule, lay.right, rule+ruleHeight), s.pal.Rule)

	lay.top += total
}

// drawWordmark sets USCOPE, then the ingest source beside it behind a dot.
//
// The dot is filled when the source is connected and hollow when it is not,
// which is one glance rather than a word to read. It is the low-altitude
// green rather than the accent, because the accent marks the selected
// aircraft and nothing else, and it is left in its own colour rather than
// moved onto the band's BandInk/Muted split: it is a status light, not text.
func (s *Scene) drawWordmark(lay *layout, top int, frame source.Frame) {
	// Both faces are known to be present: drawHeader drops the whole band
	// unless all three of its faces loaded, so there is nothing to check here.
	face := s.faces.Small
	markHeight := lineHeight(s.faces.BodyBold)

	pen := text.Draw(lay.dst, s.faces.BodyBold, lay.left, top, appTitle, s.pal.BandInk,
		text.WithSpacing(headerTracking))

	pen += sourceGap
	middle := top + markHeight/2

	if frame.Source.Connected {
		lay.dst.FillCircle(pen+dotRadius, middle, dotRadius, s.pal.AltLow)
	} else {
		lay.dst.Circle(pen+dotRadius, middle, dotRadius, s.pal.Muted)
	}

	pen += 2*dotRadius + dotGap
	text.Draw(lay.dst, face, pen, top+(markHeight-face.Height())/2,
		clip(frame.Source.Label, maxSourceLabel), s.pal.BandInk, text.WithSpacing(labelTracking))
}

// drawReceiverLine says where the scope is centred and how much that is
// worth.
//
// The three forms are deliberately different lengths and shapes so they
// cannot be confused at a glance: a fix reads as two coordinates, an estimate
// as a radius, and nothing at all as two words. Everything here is set in
// BandInk: it sits inside the header band, where the scene's usual
// muted/ink split gives way to the band's own contrast.
//
// The face is known to be present for the same reason drawWordmark's is:
// drawHeader drops the band rather than half of it.
func (s *Scene) drawReceiverLine(lay *layout, top int, receiver source.Receiver) {
	face := s.faces.Small

	switch receiver.Label {
	case source.LabelEstimate:
		pen := text.Draw(lay.dst, face, lay.left, top, estimatePrefix, s.pal.BandInk)
		pen = drawBytes(lay.dst, face, pen, top, s.whole(receiver.ConfidenceNm), s.pal.BandInk)
		text.Draw(lay.dst, face, pen, top, rangeUnit, s.pal.BandInk)
	case source.LabelNone:
		text.Draw(lay.dst, face, lay.left, top, noFixText, s.pal.BandInk)
	default:
		pen := drawBytes(lay.dst, face, lay.left, top, s.coordinate(receiver.Latitude, 'N', 'S'), s.pal.BandInk)
		pen = text.Draw(lay.dst, face, pen, top, coordinateSeparator, s.pal.BandInk)
		drawBytes(lay.dst, face, pen, top, s.coordinate(receiver.Longitude, 'E', 'W'), s.pal.BandInk)
	}
}

// clip cuts a string to a rune count without allocating.
//
// Everything drawn here that came off the air goes through this: a callsign
// is eight characters by the standard, but the standard is not what arrives
// when a frame is half corrupt, and a long one would run over the column
// beside it.
func clip(value string, runes int) string {
	count := 0

	for offset := range value {
		if count == runes {
			return value[:offset]
		}

		count++
	}

	return value
}
