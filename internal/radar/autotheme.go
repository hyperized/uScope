package radar

import (
	"time"

	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/sun"
)

// autoThemeEvery is how long an auto theme's answer stands before the sun is
// asked again. It is the sky's cadence on purpose: the theme now swaps at the
// horizon, where the sky changes fastest, and a swap a minute late against a
// sky that moves every ten seconds would show. Thirty sums a second was the
// thing to avoid; six a minute is not.
const autoThemeEvery = skyEvery

// autoTheme is the state behind theme.KindAuto: whether it is on, and what the
// last answer was worked out from, which is what decides when to ask again.
type autoTheme struct {
	sunFix

	on bool
}

// sunFix is what the last answer about the sun was worked out from: the frame
// clock it was asked on and the receiver position it was asked for. The auto
// theme, the tilted views' sky and the header's sun block each keep one,
// because each is due on its own clock.
type sunFix struct {
	// at is the frame clock the last answer was worked out on, and the zero
	// time when there is no answer yet, which is what makes the first frame
	// after SetAutoTheme ask straight away.
	at time.Time

	// lat, lon and known are the receiver position that answer was worked out
	// for. A receiver that moves, or that is found for the first time, asks
	// again without waiting out the interval.
	lat, lon float64
	known    bool
}

// SetAutoTheme turns the auto theme on or off. While it is on, the scene picks
// the night or the day palette of the look it is holding from the sun at the
// receiver, at the horizon, and with no receiver position it picks night.
//
// It does not change the palette itself. internal/app hands over the look's
// night palette through SetPalette first, which is what theme.Look.Palette
// answers for theme.KindAuto, and the first Draw after this call puts the
// sun's answer on screen before anything is painted, so a day run never shows
// a night frame. Turning it on always asks again on that frame, even if it
// was already on, because a new look arrives through the same two calls.
func (s *Scene) SetAutoTheme(on bool) {
	s.sunTheme = autoTheme{on: on}
}

// resolveTheme is the auto theme's per-frame check. Most frames it is a few
// comparisons and returns; when an answer is due it asks the sun and, if the
// answer differs from the palette on screen, goes through SetPalette, which is
// the same path the l key takes, so the background layer, the legend and the
// key cap all follow on this frame.
func (s *Scene) resolveTheme(frame source.Frame) {
	if !s.sunTheme.on || !s.sunTheme.due(frame.Now, frame.Receiver) {
		return
	}

	s.sunTheme.remember(frame.Now, frame.Receiver)

	// Night when nothing is known, because a wrong night palette in daylight
	// is only dim, and a wrong day palette at night blinds the operator.
	day := s.sunTheme.known && sun.IsDay(s.sunTheme.lat, s.sunTheme.lon, frame.Now)
	if day == s.light {
		return
	}

	kind := theme.KindNight
	if day {
		kind = theme.KindDay
	}

	s.SetPalette(s.look.Palette(kind))
}

// known reports whether a receiver has a position at all. FixNone is the zero
// mode, so a frame nobody filled in counts as unknown.
func known(receiver source.Receiver) bool {
	return receiver.Mode != source.FixNone
}

// due reports whether the auto theme's answer has to be worked out again.
// See sunFix.stale.
func (a *autoTheme) due(now time.Time, receiver source.Receiver) bool {
	return a.stale(now, receiver, autoThemeEvery)
}

// stale reports whether the last answer has to be worked out again: there is
// none yet, every has passed, the receiver has moved or been found or lost,
// or the clock went backwards, which a replayed capture can do and which
// would otherwise freeze the answer until the replay caught up again.
func (f *sunFix) stale(now time.Time, receiver source.Receiver, every time.Duration) bool {
	if f.at.IsZero() || known(receiver) != f.known {
		return true
	}

	if receiver.Latitude != f.lat || receiver.Longitude != f.lon {
		return true
	}

	elapsed := now.Sub(f.at)

	return elapsed >= every || elapsed < 0
}

// remember records what an answer is being worked out from.
func (f *sunFix) remember(now time.Time, receiver source.Receiver) {
	f.at, f.lat, f.lon, f.known = now, receiver.Latitude, receiver.Longitude, known(receiver)
}
