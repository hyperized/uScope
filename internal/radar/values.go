package radar

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/text"
)

// The line of values under the panel's three figures: what the selected
// aircraft is doing that a number in a large face cannot carry.
//
// These used to be a block of their own with a border and a DETAILS heading,
// sitting between the rows and the legend. Two bordered blocks about the same
// aeroplane is one too many, so the figures moved into the panel and the block
// went. Bearing went with it: the compact rows already have a BRG column, and
// the panel was repeating a value the operator could read three lines lower.
const (
	labelVert = "VERT"
	labelPos  = "POS"
	labelSeen = "SEEN"

	// cardPairs is how many pairs the line holds, which is what every loop
	// over them runs to.
	cardPairs = 3

	// cardValueChars is the longest value one pair can be asked to set. That
	// is a position: "90.00 N / 179.77 E" is eighteen characters at the far
	// corner of the world, and one more is left over so a cell sized from it
	// is not sized to the exact edge. The other two are shorter, so a cell
	// that holds a position holds all three.
	cardValueChars = 19

	// detailUnknown stands in for a figure that cannot be worked out, which is
	// always because the aircraft has no position yet.
	detailUnknown = "---"

	// noSquawk is four dashes rather than three, so an absent code is the same
	// width as a real one and the panel's corner does not move when one
	// arrives.
	noSquawk = "----"

	// noTraffic replaces the panel's contents when the sky is empty. The panel
	// keeps its height rather than collapsing, because a column that changes
	// shape whenever the last aircraft leaves range is worse to look at than
	// one with a gap in it.
	noTraffic = "NO TRAFFIC"

	emergencyTag = " EMERGENCY"

	vertLevel = "LEVEL"
	vertUnit  = " FT/MIN"

	// vertMarker is the side of the climb and descent triangle, and vertGap the
	// air between it and the figure. It is a filled triangle rather than a
	// glyph because nothing guarantees a console font carries U+25B2 and
	// U+25BC, and a shape drawn on the canvas always renders.
	vertMarker = 7
	vertGap    = 6

	// levelBand is how small a vertical rate has to be before the aircraft
	// counts as level. Mode S reports a rate on every velocity message and it
	// is rarely a flat zero in the cruise, so a raw figure would show a few
	// dozen feet a minute of noise as a climb.
	levelBand = 100.0
)

// The age buckets the SEEN figure reports.
//
// They are coarse on purpose. A figure counting up second by second is
// movement the eye keeps going back to, and what the line is for is telling a
// fresh contact from one the receiver has lost, which does not need a number.
const (
	seenJustNow = 5 * time.Second
	seenQuarter = 15 * time.Second
	seenHalf    = 30 * time.Second
	seenMinute  = time.Minute
)

// The words those buckets read as.
const (
	seenNowText     = "JUST NOW"
	seenQuarterText = "< 15 S"
	seenHalfText    = "< 30 S"
	seenMinuteText  = "< 1 MIN"
	seenStaleText   = "1+ MIN"
)

// The three pairs, in reading order. They are named rather than written as
// numbers at the call sites so each placement says which figure it is for.
const (
	pairVert = iota
	pairPos
	pairSeen
)

// cardPairWidest is the room one pair needs: the indent every value starts at,
// plus the longest value that can land there, or zero without a body face to
// set the values in.
func (s *Scene) cardPairWidest() int {
	glyph := glyphWidth(s.faces.Body)
	if glyph == 0 {
		return 0
	}

	return s.cardLabelIndent() + cardValueChars*glyph
}

// cardValueShape is how the three pairs are arranged for a panel of this inner
// width: how many go side by side, and how many lines that takes.
//
// Three across is the panel at the uConsole's width. A narrower one wraps them
// rather than squeezing, for the reason every other block here does: the values
// are set in a bitmap face with one design size, so a narrower cell means fewer
// characters, not smaller ones. A width that will not hold one whole pair draws
// no line at all, and the panel is that much shorter.
func (s *Scene) cardValueShape(width int) (int, int) {
	widest := s.cardPairWidest()
	if widest <= 0 {
		return 0, 0
	}

	columns := min(width/widest, cardPairs)
	if columns <= 0 {
		return 0, 0
	}

	return columns, (cardPairs + columns - 1) / columns
}

// cardLabelIndent is how far a value sits from its pair's left edge.
//
// It is measured off the longest label rather than off each label in turn, so
// every value on the line starts at the same offset and the pairs read as
// columns instead of three ragged pieces.
func (s *Scene) cardLabelIndent() int {
	width, _ := text.Measure(s.faces.Small, labelSeen, text.WithSpacing(labelTracking))

	return width + columnGap
}

// detailAt is where the pair at index lands inside the box.
func detailAt(box image.Rectangle, columns, step, index int) (int, int) {
	span := box.Dx() / columns

	return box.Min.X + (index%columns)*span, box.Min.Y + (index/columns)*step
}

// rowStep is the pitch of one compact row, or zero without a body face.
func (s *Scene) rowStep() int {
	line := lineHeight(s.faces.Body)
	if line == 0 {
		return 0
	}

	return line + rowLead
}

// drawCardValues lays the three figures out under the panel's numbers,
// reading left to right and then down, in whatever shape the width allows.
func (s *Scene) drawCardValues(
	dst *canvas.Canvas, box image.Rectangle, columns int, frame source.Frame, plane airplane.Snapshot,
) {
	if columns <= 0 {
		return
	}

	step := s.rowStep()

	left, top := detailAt(box, columns, step, pairVert)
	s.drawVert(dst, left, top, plane)

	left, top = detailAt(box, columns, step, pairPos)
	s.drawPosition(dst, left, top, plane)

	left, top = detailAt(box, columns, step, pairSeen)
	s.drawSeen(dst, left, top, frame.Now, plane.LastUpdate)
}

// drawValueLabel writes one pair's label and returns the x its value starts
// at.
//
// The label is set in the small face and the value in the body face, so it is
// nudged down by half the difference to sit on the value's own line rather
// than on its cap height.
func (s *Scene) drawValueLabel(dst *canvas.Canvas, left, top int, label string) int {
	small, body := s.faces.Small, s.faces.Body
	offset := (lineHeight(body) - lineHeight(small)) / 2

	text.Draw(dst, small, left, top+offset, label, s.pal.Muted, text.WithSpacing(labelTracking))

	return left + s.cardLabelIndent()
}

// drawVert writes the vertical rate with a triangle for its direction.
//
// The triangle carries the sign, so the figure itself is drawn unsigned: a
// minus sign next to a downward arrow would be the same fact twice, and the
// number lines up with the one above it without one.
func (s *Scene) drawVert(dst *canvas.Canvas, left, top int, plane airplane.Snapshot) {
	pen := s.drawValueLabel(dst, left, top, labelVert)
	face := s.faces.Body

	if level(plane.VertRate) {
		text.Draw(dst, face, pen, top, vertLevel, s.pal.Ink)

		return
	}

	pen = s.drawVertMarker(dst, pen, top, plane.VertRate, s.aircraftColour(plane))
	pen = drawBytes(dst, face, pen, top, s.thousands(math.Abs(plane.VertRate)), s.pal.Ink)
	text.Draw(dst, face, pen, top, vertUnit, s.pal.Muted)
}

// level reports whether a vertical rate counts as neither climbing nor
// descending, which is also the answer for a rate nobody has decoded:
// uAirwaves starts an aircraft at a vertical rate of zero and leaves it there.
//
// It is one function rather than a test at each call site because the panel
// and the compact rows both have to agree: a row showing a triangle beside a
// figure the panel calls LEVEL would be the same aircraft contradicting itself
// on one screen.
func level(rate float64) bool {
	return math.IsNaN(rate) || math.IsInf(rate, 0) || math.Abs(rate) < levelBand
}

// drawVertMarker draws the climb or descent triangle and returns the x just
// past it. It is filled in the aircraft's own colour, so the marker says which
// aeroplane it belongs to as well as which way it is going.
func (s *Scene) drawVertMarker(dst *canvas.Canvas, left, top int, rate float64, col color.RGBA) int {
	half := vertMarker / 2
	middle := top + lineHeight(s.faces.Body)/2
	apex, base := middle-half, middle+half

	if rate < 0 {
		apex, base = base, apex
	}

	dst.FillTriangle(left+half, apex, left, base, left+vertMarker-1, base, col)

	return left + vertMarker + vertGap
}

// bearingTo is the compass bearing from the receiver to an aircraft, which is
// what the compact rows' BRG column reads.
//
// It uses the same local equirectangular approximation the scope projects
// with, so the figure there and the dot on the field agree. A great-circle
// bearing would differ by a fraction of a degree over the range a scope
// covers, and disagreeing with the picture is the worse error.
func bearingTo(receiver source.Receiver, plane airplane.Snapshot) (float64, bool) {
	if !hasPosition(plane) || (receiver.Latitude == 0 && receiver.Longitude == 0) {
		return 0, false
	}

	eastNm := (plane.Longitude - receiver.Longitude) * nmPerDegree * math.Cos(receiver.Latitude*math.Pi/halfCircle)
	northNm := (plane.Latitude - receiver.Latitude) * nmPerDegree

	if math.IsNaN(eastNm) || math.IsNaN(northNm) || (eastNm == 0 && northNm == 0) {
		return 0, false
	}

	bearing := math.Atan2(eastNm, northNm) * halfCircle / math.Pi
	if bearing < 0 {
		bearing += degreesPerCircle
	}

	return bearing, true
}

// hasPosition reports whether an aircraft has a decoded position.
//
// Exactly (0, 0) is the undecoded state rather than a spot in the Gulf of
// Guinea: a Snapshot starts there and stays until a position message resolves,
// and uAirwaves' own distance function reads it the same way.
func hasPosition(plane airplane.Snapshot) bool {
	if math.IsNaN(plane.Latitude) || math.IsNaN(plane.Longitude) {
		return false
	}

	return plane.Latitude != 0 || plane.Longitude != 0
}

// drawPosition writes where the aircraft is, to two decimals.
//
// Two places is about a nautical mile, which is as much as a figure read off a
// scope is worth. The receiver's own line in the header carries four, because
// that one is a claim about where the antenna is rather than about where an
// aeroplane was a moment ago.
func (s *Scene) drawPosition(dst *canvas.Canvas, left, top int, plane airplane.Snapshot) {
	pen := s.drawValueLabel(dst, left, top, labelPos)
	face := s.faces.Body

	if !hasPosition(plane) {
		text.Draw(dst, face, pen, top, detailUnknown, s.pal.Muted)

		return
	}

	// One half at a time: both share the scene's coordinate buffer, so
	// formatting the second would overwrite the first.
	pen = drawBytes(dst, face, pen, top, s.place(plane.Latitude, 'N', 'S'), s.pal.Ink)
	pen = text.Draw(dst, face, pen, top, coordinateSeparator, s.pal.Muted)
	drawBytes(dst, face, pen, top, s.place(plane.Longitude, 'E', 'W'), s.pal.Ink)
}

// drawSeen writes how long ago the aircraft was last heard.
func (s *Scene) drawSeen(dst *canvas.Canvas, left, top int, now, last time.Time) {
	pen := s.drawValueLabel(dst, left, top, labelSeen)
	text.Draw(dst, s.faces.Body, pen, top, seenBucket(now.Sub(last)), s.pal.Ink)
}

// seenBucket names how long ago a contact was last heard.
//
// A negative age reads as just now. That happens when the frame's clock and
// the aircraft's last update come from two sources a few milliseconds apart,
// and reporting it as a minute old would be worse than rounding it to zero.
func seenBucket(age time.Duration) string {
	switch {
	case age < seenJustNow:
		return seenNowText
	case age < seenQuarter:
		return seenQuarterText
	case age < seenHalf:
		return seenHalfText
	case age < seenMinute:
		return seenMinuteText
	default:
		return seenStaleText
	}
}
