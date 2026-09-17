package radar

import (
	"image/color"
	"math"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// The small shapes and the stand-in words the flight strips are drawn from.
const (
	// detailUnknown stands in for a figure that cannot be worked out, which is
	// always because the aircraft has no position yet, and trackUnknown for a
	// direction nobody has decoded. uAirwaves marks the second with a negative
	// heading, so zero is due north and reads as 000; it is a negative figure
	// that would have a strip inventing a course.
	detailUnknown = "---"
	trackUnknown  = "---"

	// noSquawk is four dashes rather than three, so an absent code is the same
	// width as a real one and the line under the callsign does not move when
	// one arrives.
	noSquawk = "----"

	// noTraffic replaces the selected strip's callsign when the sky is empty.
	// The strip keeps its height rather than collapsing, because a column that
	// changes shape whenever the last aircraft leaves range is worse to look at
	// than one with a gap in it.
	noTraffic = "NO TRAFFIC"

	// vertMarker is the side of the climb and descent triangle, and vertGap the
	// air between it and the figure. It is a filled triangle rather than a
	// glyph because nothing guarantees a console font carries U+25B2 and
	// U+25BC, and a shape drawn on the canvas always renders.
	vertMarker = 7
	vertGap    = 6

	// arrowGap is the air between a direction figure and the arrow that
	// follows it. It is vertGap so that the scene's two small shapes sit off
	// their figures by the same amount.
	arrowGap = vertGap

	// arrowCell is the room the arrow is drawn in, and the three numbers that
	// shape it, all in pixels from its centre: the apex runs arrowApex ahead
	// along the bearing, and the two base corners sit arrowBase behind it,
	// arrowHalf either side.
	//
	// The cell is nine because that is what the 9x9 bitmap this replaced took,
	// and the columns beside it are laid out against that width. The needle
	// inside it is deliberately long and narrow: a five-pixel point against a
	// five-pixel base is a shape whose direction can be read at a glance,
	// where an equilateral triangle of the same area reads as a blob with a
	// corner.
	arrowCell = 9
	arrowApex = 5.0
	arrowBase = 3.0
	arrowHalf = 2.5

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

// level reports whether a vertical rate counts as neither climbing nor
// descending, which is also the answer for a rate nobody has decoded:
// uAirwaves starts an aircraft at a vertical rate of zero and leaves it there.
//
// It is one function rather than a test at each call site because the strips
// and the scope tag both have to agree: a strip showing a triangle beside a
// figure the tag has no arrow on would be the same aircraft contradicting
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

// drawTrend marks a climb or a descent after a level and returns the x just
// past whatever it drew, which is the pen it was handed when the aircraft is
// neither.
//
// The mark is a glyph where the face carries one and the drawn triangle where
// it does not. Nothing guarantees a console font has U+2191 and U+2193, which
// is the check the ellipsis went through, and a shape put on the canvas always
// renders.
//
// Both blocks that write a level use it, each in its own face and its own
// colour: the tag on the scope in the body face beside the reading ink, the
// selected aircraft's panel in the large one beside the altitude band. One
// function rather than two, because a panel showing a climb the tag has no
// arrow on would be the same aeroplane contradicting itself on one screen.
func (s *Scene) drawTrend(
	dst *canvas.Canvas, face *psf.Font, pen, top int, rate float64, ink color.RGBA,
) int {
	if level(rate) {
		return pen
	}

	glyph, mark := tagClimb, climbRune
	if rate < 0 {
		glyph, mark = tagDescend, descendRune
	}

	if _, has := face.Glyph(mark); has {
		return text.Draw(dst, face, pen, top, glyph, ink)
	}

	return s.drawVertMarker(dst, pen, top, rate, ink)
}

// arrowWidth is the room the direction arrow takes beside a figure, the gap
// before it included.
func (*Scene) arrowWidth() int {
	return arrowGap + arrowCell
}

// drawArrow draws the direction arrow at left, pointing at the angle the
// figure beside it has just given, and centred vertically on middle.
//
// Both places the scene writes a direction use it: a strip's TRK field and the
// bearing in its DIST field. An arrow rather than a compass point, because eight
// letters are eight sectors and NE says the same thing about 23 degrees as
// about 67, where the arrow says the angle itself.
//
// It is a triangle computed from the angle rather than a bitmap turned to it.
// The bitmap was rotated by nearest neighbour, which on a nine-pixel sprite
// meant most angles lost a pixel out of the shaft or grew a step in the head,
// so the one thing the arrow existed to say was the thing it said worst. Three
// corners worked out in float64 and handed to FillTriangle come out solid at
// every angle, and cost less than walking a sprite's bounding box.
//
// It is centred on the line rather than set on its baseline, the same way the
// climb triangle is. It is a shape beside type and not a glyph in it.
//
//nolint:varnamelen // middle is the line's vertical centre, the pixel idiom used throughout uScope.
func (*Scene) drawArrow(dst *canvas.Canvas, left, middle int, degrees float64, ink color.RGBA) {
	centreX, centreY := float64(left+arrowCell/2), float64(middle)

	// The compass, on a canvas whose y grows downward: zero points up the
	// screen and increasing turns clockwise. forward is the way the arrow
	// points and side is a quarter turn clockwise from it, which is what puts
	// the two base corners either side of the shaft whatever the angle.
	sin, cos := math.Sincos(degrees * math.Pi / halfCircle)
	forwardX, forwardY := sin, -cos
	sideX, sideY := cos, sin

	apexX, apexY := centreX+forwardX*arrowApex, centreY+forwardY*arrowApex
	backX, backY := centreX-forwardX*arrowBase, centreY-forwardY*arrowBase

	dst.FillTriangle(
		round(apexX), round(apexY),
		round(backX-sideX*arrowHalf), round(backY-sideY*arrowHalf),
		round(backX+sideX*arrowHalf), round(backY+sideY*arrowHalf),
		ink,
	)
}

// round is math.Round with the conversion the canvas wants, which is the same
// three lines at six call sites otherwise.
func round(value float64) int { return int(math.Round(value)) }

// bearingTo is the compass bearing from the receiver to an aircraft, which is
// what a strip's DIST field and the scope tag's bottom line both read.
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

// hasPosition reports whether an aircraft has a decoded position. The rule
// itself is positioned, in follow.go, so the scope, the centroid and this
// cannot come to different answers about the same aeroplane.
func hasPosition(plane airplane.Snapshot) bool {
	return positioned(plane.Latitude, plane.Longitude)
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
