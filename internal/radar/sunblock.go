package radar

import (
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/sun"
	"github.com/hyperized/uScope/pkg/text"
)

// daylightEvery is how long the sun block's answer stands. It is the sky's
// cadence, so the condition word turns over on the same frame the tilted
// views' sky crosses the matching keyframe rather than up to a minute later.
// Working out a sunrise is a scan of the whole day, about three hundred calls
// to the sun's position, and ten seconds apart that is noise.
const daylightEvery = skyEvery

// The sun block's marks and spacing.
const (
	// riseMark and setMark sit in front of the two times. Every embedded
	// Terminus face carries U+2191 and U+2193, which TestSunBlockGlyphs
	// checks, so there is no fallback spelling to keep.
	riseMark = "↑ "
	setMark  = "↓ "

	// daylightCap is the room the formatted times take: two marks of four
	// bytes, two clocks of five and the space between them is 19.
	daylightCap = 24
)

// daylight is what the sun block draws, worked out on its own clock rather
// than per frame: the condition at the receiver and the day's sunrise and
// sunset already formatted, so a frame only measures and draws.
type daylight struct {
	sunFix

	condition sun.Condition

	// elevation is the sun's height at the receiver in degrees that the
	// condition was read from, kept for the lights under LightsAuto so they
	// ride on this answer instead of asking the sun a second time.
	elevation float64

	// times is the formatted "↑ 05:19 ↓ 22:07", a view into buf. Either half
	// is missing on a day the sun does not rise or does not set, and both are
	// on a day it does neither, which leaves the word on its own.
	times []byte
	buf   [daylightCap]byte
}

// resolveDaylight is the sun block's per-frame check, a few comparisons on
// most frames and a sunrise and a sunset when the answer is due.
//
// The times are for the calendar day frame.Now shows in its own zone, which
// is time.Local for a live clock: the same zone the header's LCL clock is set
// in, so the sunrise and the clock beside it are on one wall. A machine with no
// zoneinfo or TZ is in UTC and shows UTC for both, which is wrong in the same way
// for both rather than wrong for one of them.
func (s *Scene) resolveDaylight(frame source.Frame) {
	if !s.daylight.stale(frame.Now, frame.Receiver, daylightEvery) {
		return
	}

	s.daylight.remember(frame.Now, frame.Receiver)

	if !s.daylight.known {
		s.daylight.times = s.daylight.buf[:0]

		return
	}

	lat, lon, now := s.daylight.lat, s.daylight.lon, frame.Now
	s.daylight.elevation = sun.Elevation(lat, lon, now)
	s.daylight.condition = sun.ConditionOf(s.daylight.elevation)

	out := s.daylight.buf[:0]
	if rise, found := sun.Rise(lat, lon, now, now.Location()); found {
		out = rise.AppendFormat(append(out, riseMark...), clockFormat)
	}

	if set, found := sun.Set(lat, lon, now, now.Location()); found {
		if len(out) > 0 {
			out = append(out, ' ')
		}

		out = set.AppendFormat(append(out, setMark...), clockFormat)
	}

	s.daylight.times = out
}

// drawSunBlock sets the condition word and the day's two times ending at
// rightX, centred on middle, and reports the x the block starts at and
// whether it was drawn.
//
// The word is in the band's own ink because it is a label for the times
// after it as much as a reading, and the times are the band's muted ink: they
// change once a day and are looked up, not watched. Nothing is drawn with no
// receiver position, because a sunrise for nowhere is not a quieter answer
// but a wrong one.
//
// limit is the leftmost x the block may reach. It drops whole rather than
// being cut when it would cross it, the way the battery has no partial form:
// half a condition word is worse than none, and the source label it would
// otherwise squeeze says which feed everything else on screen came off.
func (s *Scene) drawSunBlock(lay *layout, rightX, middle, limit int) (int, bool) {
	if !s.daylight.known {
		return rightX, false
	}

	small := s.faces.Small
	word := s.daylight.condition.String()
	times := s.daylight.times

	width := lay.measureTracked(small, word)
	if len(times) > 0 {
		width += lay.px(clockLabelGap) + lay.measureBytes(small, times)
	}

	left := rightX - width
	if left < limit {
		return rightX, false
	}

	top := middle - lay.lineHeight(small)/2
	pen := lay.tracked(small, left, top, word, s.pal.BandInk)

	if len(times) > 0 {
		lay.drawBytes(small, pen+lay.px(clockLabelGap), top, times, s.bandMuted())
	}

	return left, true
}

// wordmarkWidth is how far drawWordmark would reach from lay.left if nothing
// cut the source label: the wordmark, the dot, the label and SWEEP while it
// runs, plus the gap the label keeps from whatever is on its right.
//
// The sun block measures against it so it gives way before the label is cut.
func (s *Scene) wordmarkWidth(lay *layout, frame source.Frame) int {
	small := s.faces.Small

	title, _ := text.Measure(s.faces.BodyBold, appTitle, lay.glyphs(), lay.tracking(headerTracking))

	width := title + lay.px(sourceGap) + 2*lay.px(dotRadius) + lay.px(dotGap)
	width += lay.measureTracked(small, frame.Source.Label) + lay.px(sourceLabelGap)

	if frame.Sweeping {
		width += lay.measureTracked(small, sweepSuffix)
	}

	return width
}
