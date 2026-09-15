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

// The details block: the five figures about the selected aircraft that the
// card has no room for.
const (
	detailsLabel = "DETAILS"

	// detailsPairs is how many figures the block holds. In two columns they
	// take three lines and leave the last cell empty, which is where SQUAWK's
	// emergency tag goes when it has one; in one column they take five.
	detailsPairs   = 5
	detailsColumns = 2
	detailsLines   = 3

	// detailValueChars is the longest value the block can be asked to set. That
	// is a position: "90.00 N / 179.77 E" is eighteen characters at the far
	// corner of the world, and one more is left over so a column sized from it
	// is not sized to the exact edge. Everything else here is shorter, so a
	// column that holds a position holds all five figures.
	detailValueChars = 19

	labelVert   = "VERT"
	labelBrg    = "BRG"
	labelPos    = "POS"
	labelSeen   = "SEEN"
	labelSquawk = "SQUAWK"

	// detailUnknown stands in for a figure that cannot be worked out, which is
	// always because the aircraft has no position yet.
	detailUnknown = "---"

	// noSquawk is four dashes rather than three, so an absent code is the same
	// width as a real one and the line does not move when one arrives.
	noSquawk = "----"

	// noTraffic replaces the whole block when the sky is empty. The block keeps
	// its room rather than collapsing, because a column that changes height
	// whenever the last aircraft leaves range is worse to look at than one with
	// a gap in it.
	noTraffic = "NO TRAFFIC"

	emergencyTag = " EMERGENCY"

	vertLevel = "LEVEL"
	vertUnit  = " FT/MIN"

	// degreeSign is checked against all four embedded faces: Debian's Uni3
	// builds of Terminus all carry U+00B0.
	degreeSign = "°"

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

// detailsHeight is the block's height for a column of this inner width, or
// zero when a face is missing or the width will not hold even one pair.
//
// The height is fixed for a given canvas, which is the part that matters: the
// rows above grow into whatever is left, so a block whose height moved with
// its contents would make the row count jump about as aircraft came and went.
// It does change with the width, because a narrow column takes the five pairs
// in one column of five lines instead of two of three.
func (s *Scene) detailsHeight(width int) int {
	labelHeight := lineHeight(s.faces.Small)
	line := lineHeight(s.faces.Body)

	if labelHeight == 0 || line == 0 || width < s.detailWidest() {
		return 0
	}

	_, lines := s.detailsShape(width)

	return 2*cardPadY + labelHeight + rowGap + lines*line + (lines-1)*rowLead
}

// detailWidest is the room one pair needs: the indent every value starts at,
// plus the longest value that can land there.
func (s *Scene) detailWidest() int {
	return s.detailIndent() + detailValueChars*glyphWidth(s.faces.Body)
}

// detailsShape is how the five pairs are arranged for a column of this width.
//
// Two columns when the width holds the longest pair twice over, one column
// when it does not. Squeezing two columns into a width that cannot hold them
// is how the position and the age ended up written over each other the first
// time this block was built.
func (s *Scene) detailsShape(width int) (int, int) {
	if width/detailsColumns >= s.detailWidest() {
		return detailsColumns, detailsLines
	}

	return 1, detailsPairs
}

// The five pairs, in reading order. They are named rather than written as
// numbers at the call sites so each placement says which figure it is for.
const (
	pairVert = iota
	pairBrg
	pairPos
	pairSeen
	pairSquawk
)

// detailAt is where the pair at index lands inside the block.
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

// drawDetails puts the details block above the legend and takes its room out
// of the column.
//
// It is dropped whole rather than shortened when the column is tight, and it
// is the first thing in the column to go: a scope with three aircraft listed
// and no detail on the selected one is more use than one with a detail block
// and no list. That is what the minimum row count is doing in the fit test.
func (s *Scene) drawDetails(col *layout, frame source.Frame) {
	height := s.detailsHeight(col.right - col.left - 2*cardPadX)
	if height == 0 {
		return
	}

	needed := height + blockGap
	if len(frame.Planes) > 0 {
		needed += s.rowsHeight(minRowCount)
	}

	if !col.fits(needed) {
		return
	}

	top := col.bottom - height
	box := image.Rect(col.left, top, col.right, col.bottom)

	col.dst.Rect(box, s.pal.Rule)
	col.bottom -= height + blockGap

	left, right := box.Min.X+cardPadX, box.Max.X-cardPadX
	pen := top + cardPadY

	text.Draw(col.dst, s.faces.Small, left, pen, detailsLabel, s.pal.Muted, text.WithSpacing(labelTracking))
	pen += lineHeight(s.faces.Small) + rowGap

	plane, position := s.selectedPlane(frame)
	if position < 0 {
		text.DrawCentered(col.dst, s.faces.Body, (left+right)/2, pen, noTraffic, s.pal.Muted)

		return
	}

	// The pairs are measured against the block's own box, not against the
	// column's running bottom: that has already given the block its room away,
	// so a rectangle built from it would be inside out and image.Rect would
	// quietly flip it.
	s.drawDetailPairs(col.dst, image.Rect(left, pen, right, box.Max.Y), frame, plane)
}

// drawDetailPairs lays the five figures out, reading left to right and then
// down, in whatever shape the column's width allows.
func (s *Scene) drawDetailPairs(
	dst *canvas.Canvas, box image.Rectangle, frame source.Frame, plane airplane.Snapshot,
) {
	columns, _ := s.detailsShape(box.Dx())
	step := s.rowStep()

	left, top := detailAt(box, columns, step, pairVert)
	s.drawVert(dst, left, top, plane)

	left, top = detailAt(box, columns, step, pairBrg)
	s.drawBearing(dst, left, top, frame.Receiver, plane)

	left, top = detailAt(box, columns, step, pairPos)
	s.drawPosition(dst, left, top, plane)

	left, top = detailAt(box, columns, step, pairSeen)
	s.drawSeen(dst, left, top, frame.Now, plane.LastUpdate)

	left, top = detailAt(box, columns, step, pairSquawk)
	s.drawDetailSquawk(dst, left, top, plane)
}

// detailIndent is how far a value sits from its pair's left edge.
//
// It is measured off the longest label rather than off each label in turn, so
// every value in the block starts at the same x and the five of them read as
// two columns instead of five ragged pairs.
func (s *Scene) detailIndent() int {
	width, _ := text.Measure(s.faces.Small, labelSquawk, text.WithSpacing(labelTracking))

	return width + columnGap
}

// drawDetailLabel writes one pair's label and returns the x its value starts
// at.
//
// The label is set in the small face and the value in the body face, so it is
// nudged down by half the difference to sit on the value's own line rather
// than on its cap height.
func (s *Scene) drawDetailLabel(dst *canvas.Canvas, left, top int, label string) int {
	small, body := s.faces.Small, s.faces.Body
	offset := (lineHeight(body) - lineHeight(small)) / 2

	text.Draw(dst, small, left, top+offset, label, s.pal.Muted, text.WithSpacing(labelTracking))

	return left + s.detailIndent()
}

// drawVert writes the vertical rate with a triangle for its direction.
//
// The triangle carries the sign, so the figure itself is drawn unsigned: a
// minus sign next to a downward arrow would be the same fact twice, and the
// number lines up with the one above it without one.
func (s *Scene) drawVert(dst *canvas.Canvas, left, top int, plane airplane.Snapshot) {
	pen := s.drawDetailLabel(dst, left, top, labelVert)
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
// descending, which is also the answer for a rate nobody has decoded.
//
// It is one function rather than a test at each call site because the details
// block and the compact rows both have to agree: a row showing a triangle
// beside a figure the card calls LEVEL would be the same aircraft contradicting
// itself on one screen.
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

// drawBearing writes which way the aircraft is from the receiver, in degrees
// and in compass points.
//
// This is the bearing to the aircraft, not the course it is flying. The card
// above already carries the course, and the two are only the same when
// something is heading straight away from the antenna.
func (s *Scene) drawBearing(
	dst *canvas.Canvas, left, top int, receiver source.Receiver, plane airplane.Snapshot,
) {
	pen := s.drawDetailLabel(dst, left, top, labelBrg)
	face := s.faces.Body

	bearing, known := bearingTo(receiver, plane)
	if !known {
		text.Draw(dst, face, pen, top, detailUnknown, s.pal.Muted)

		return
	}

	pen = drawBytes(dst, face, pen, top, s.degrees(bearing), s.pal.Ink)
	pen = text.Draw(dst, face, pen, top, degreeSign, s.pal.Ink)
	pen = text.Draw(dst, face, pen, top, trackSeparator, s.pal.Muted)
	text.Draw(dst, face, pen, top, compass(bearing), s.pal.Ink)
}

// bearingTo is the compass bearing from the receiver to an aircraft.
//
// It uses the same local equirectangular approximation the scope projects
// with, so the figure here and the dot on the field agree. A great-circle
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
	pen := s.drawDetailLabel(dst, left, top, labelPos)
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
	pen := s.drawDetailLabel(dst, left, top, labelSeen)
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

// drawDetailSquawk writes the transponder code, and says so in the accent
// colour when the aircraft is squawking an emergency.
//
// This is the one place the accent marks something other than the selection.
// An emergency is the one thing on the scope worth taking the eye off
// everything else, which is what the accent is for.
func (s *Scene) drawDetailSquawk(dst *canvas.Canvas, left, top int, plane airplane.Snapshot) {
	pen := s.drawDetailLabel(dst, left, top, labelSquawk)
	face := s.faces.Body

	code := clip(plane.Squawk, maxSquawk)
	if code == "" {
		code = noSquawk
	}

	pen = text.Draw(dst, face, pen, top, code, s.pal.Ink)

	if plane.Emergency {
		text.Draw(dst, face, pen, top, emergencyTag, s.pal.Accent)
	}
}
