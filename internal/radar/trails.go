package radar

// shortTrailFixes is how many fixes the short mode keeps.
//
// uAirwaves samples one position every ten seconds, so twelve of them is the
// last two minutes of flight. That is the assumption the number rests on: a
// source with a different cadence would give the short mode a different span
// of time, and the mode is defined by the fixes rather than by the clock
// because a fix is the only thing a PositionEntry carries.
const shortTrailFixes = 12

// trailMode is how much of an aircraft's track the scene draws, which the t
// key cycles.
//
// It is unexported, unlike ColourMode and View, because there is no flag that
// parses into it: the four modes are reachable from the key and from nowhere
// else. A published type with no way for a caller to produce one would be API
// for the sake of symmetry.
//
// It is a string rather than an integer so the zero value can be given a
// meaning without depending on which constant iota happened to number first.
// The zero value reads as trailLong, which is what a scene that was never
// told otherwise shows.
type trailMode string

// The four modes, in the order the t key cycles them.
const (
	// trailOff draws no track at all, live or lost. It is first in the cycle
	// because it is the one somebody reaches for on purpose: a field of forty
	// aircraft with two minutes of history each is a field where the traffic
	// is hard to pick out of its own trails.
	trailOff trailMode = "off"

	// trailShort draws the last shortTrailFixes of a track, fading to nothing
	// at the tail. It is the mode that answers "where is this one going" and
	// not "where has it been".
	trailShort trailMode = "short"

	// trailLong draws the whole history, fading to a quarter strength at the
	// tail. It is the default and it is what the scope has always drawn.
	trailLong trailMode = "long"

	// trailAll draws the whole history at full strength and adds the ghosts:
	// the tracks of aircraft that have stopped transmitting. It is the mode
	// for looking at the shapes the traffic makes over a session rather than
	// at the traffic.
	trailAll trailMode = "all"
)

// next cycles to the mode after this one: off, short, long, all, off.
//
// Anything that is not one of the other three moves to all, so the zero value
// cycles the way trailLong does and never has to be special-cased where one
// is read. That is View.Next's rule, for View.Next's reason.
func (m trailMode) next() trailMode {
	switch m {
	case trailOff:
		return trailShort
	case trailShort:
		return trailLong
	case trailAll:
		return trailOff
	case trailLong:
		fallthrough
	default:
		return trailAll
	}
}

// label is what the key cap says, which is the mode itself rather than the
// word TRAILS.
//
// A cap reading TRAILS says there is a trail setting without saying which of
// four states it is in, which is the question it is being asked. The two
// cycling caps already on the bar are labelled the same way.
func (m trailMode) label() string {
	switch m {
	case trailOff:
		return "OFF"
	case trailShort:
		return "SHORT"
	case trailAll:
		return "ALL"
	case trailLong:
		fallthrough
	default:
		return "LONG"
	}
}

// ghostsDrawn reports whether the tracks of aircraft that have gone quiet are
// on screen.
//
// Only trailAll draws them. The other three are about the traffic that is
// flying, and a ghost under a trail that is itself cut short or turned off
// would be a track with no aeroplane on it and nothing to explain it.
func (m trailMode) ghostsDrawn() bool { return m == trailAll }

// trailPlan is which part of one history is drawn and how dim its oldest
// segment is.
//
// from is an index into the fixes rather than a count, so the caller reslices
// once and everything downstream counts from the tail of what is left.
type trailPlan struct {
	from  int
	floor float64
}

// plan works out what to draw of a history of count fixes, reporting false
// when there is nothing to draw.
//
// Fewer than two fixes is nothing whatever the mode says: one point is not a
// line. Beyond that the mode decides how far back the trail runs and how
// faint it starts, and both are read off here rather than at the two places
// that draw, so the flat scope and the perspective view cannot disagree about
// what a mode means.
func (m trailMode) plan(count int) (trailPlan, bool) {
	if count < 2 || m == trailOff {
		return trailPlan{}, false
	}

	switch m {
	case trailShort:
		return trailPlan{from: max(0, count-shortTrailFixes), floor: 0}, true
	case trailAll:
		return trailPlan{floor: trailMaxAlpha}, true
	case trailOff, trailLong:
		fallthrough
	default:
		return trailPlan{floor: trailMinAlpha}, true
	}
}
