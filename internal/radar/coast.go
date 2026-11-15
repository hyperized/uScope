package radar

import (
	"image/color"
	"math"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
)

// How an aircraft comes onto the field and goes off it.
//
// uAirwaves forgets an aircraft a minute after its last message, and before
// these the scope did the same in one frame: an aircraft at the edge of
// coverage blinked out with its whole trail and came back as a dot. The three
// numbers below are what stand between the radio and the picture.
const (
	// coastFor is how long a lost aircraft is still drawn, held where it was
	// last heard with its trail behind it, fading from full strength to
	// nothing. A minute is uAirwaves' own prune threshold, so an aircraft
	// goes off the field about as slowly as it went quiet, and one that is
	// heard again inside the minute never left it.
	coastFor = time.Minute

	// fadeInFor is how long an aircraft takes to come up to full strength the
	// first time it is drawn. Two seconds is long enough to read as arriving
	// rather than switching on, and short enough that nobody waits for it.
	fadeInFor = 2 * time.Second

	// filterBandFt is the dead band either side of a filter's altitude edge.
	// An aircraft holding FL250 reports 24,975, 25,000 and 25,025 by turns,
	// and with no band at all it flicked between the mid and the high band's
	// filters every few seconds. Five hundred feet is a whole flight level either way
	// of the step a level aircraft's altitude jitters by.
	filterBandFt = 500.0
)

// The filter's memory of each aircraft.
//
// seenSlots is 1024, a power of two so a slot is a mask rather than a
// remainder. A receiver hears a few hundred aircraft at once on the busiest
// day, so the table stays under half full with all of them in it. A probe
// looks at seenProbe slots from the hashed one; when all of them hold other
// aircraft, the one stamped longest ago is overwritten. An aircraft still on
// the field is stamped every frame, so what gets evicted is one that left, and
// losing a stale entry costs nothing but a fade-in nobody would have seen.
const (
	seenBits  = 10
	seenSlots = 1 << seenBits
	seenMask  = seenSlots - 1
	seenProbe = 8

	// icaoChars is how long a Mode S address is in hex, hexBits what one
	// digit carries, and hexLetter the value of A.
	icaoChars = 6
	hexBits   = 4
	hexLetter = 10

	// keyBits is the width of the multiply the hash shifts down from.
	keyBits = 32

	// fibonacci is 2^32 over the golden ratio, the multiplier for
	// Fibonacci hashing. Consecutive addresses, which an operator's fleet
	// often holds, land far apart.
	fibonacci = 0x9E3779B1
)

// seenEntry is what the scene remembers about one aircraft.
//
// shown is the filter's last decision and shownAt the render clock at which
// it last went from hidden to shown, which is where the fade-in is measured
// from. decided is false for an entry that has not been decided since it was
// last stamped, which is how an aircraft that left and came back long after
// is treated as new.
type seenEntry struct {
	shownAt time.Duration
	gen     uint64
	icao    uint32
	used    bool
	decided bool
	shown   bool
}

// seenTable is the scene's per-aircraft memory, an open-addressing table on
// the Scene rather than a map, because Draw allocates nothing and a map grows
// as new aircraft arrive. See seenSlots for the capacity and the eviction
// rule.
//
// gen is the frame counter entries are stamped with, elapsed the render clock
// of the frame being drawn, and instant whether an aircraft first shown on
// this frame is drawn at full strength straight away. That is the first frame
// after the scene is built or the filter changes, and any frame drawn at a
// render clock of zero: --png draws exactly one frame, and every screenshot in
// the README would otherwise come out with every aircraft at the start of its
// fade. warm is what remembers the first frame has been drawn.
type seenTable struct {
	slots   [seenSlots]seenEntry
	gen     uint64
	elapsed time.Duration
	instant bool
	warm    bool
}

// begin starts a frame.
func (t *seenTable) begin(elapsed time.Duration) {
	t.gen++
	t.elapsed = elapsed
	t.instant = !t.warm || elapsed <= 0
	t.warm = true
}

// reset forgets every aircraft, which is what a new filter starts from: a
// decision made against one filter's edges says nothing about another's. The
// frame after it draws everything at full strength, because the filter
// changing is a key press and the answer to a key press should not take two
// seconds to arrive.
func (t *seenTable) reset() {
	t.slots = [seenSlots]seenEntry{}
	t.warm = false
}

// touch finds an aircraft's entry, making one when there is none, and stamps
// it with this frame. It returns nil for an address that is not six hex
// digits, which has no key; such an aircraft is filtered on its altitude alone
// and drawn at full strength.
//
// An entry last stamped before the previous frame is treated as undecided,
// because nothing drew that aircraft in between and the decision it carries is
// about some earlier flight.
func (t *seenTable) touch(icao string) *seenEntry {
	key, ok := icaoKey(icao)
	if !ok {
		return nil
	}

	home := slotOf(key)
	victim := home

	for probe := range seenProbe {
		slot := &t.slots[(home+probe)&seenMask]

		switch {
		case !slot.used:
			*slot = seenEntry{icao: key, used: true, gen: t.gen}

			return slot
		case slot.icao == key:
			if slot.gen+1 < t.gen {
				slot.decided = false
			}

			slot.gen = t.gen

			return slot
		case slot.gen < t.slots[victim].gen:
			victim = (home + probe) & seenMask
		default:
		}
	}

	t.slots[victim] = seenEntry{icao: key, used: true, gen: t.gen}

	return &t.slots[victim]
}

// find is touch without the side effects: the entry, or nil when the aircraft
// has none. A probe can stop at the first empty slot because entries are only
// ever overwritten in place, never removed, so a hole means the key was never
// put further along.
func (t *seenTable) find(icao string) *seenEntry {
	key, ok := icaoKey(icao)
	if !ok {
		return nil
	}

	home := slotOf(key)

	for probe := range seenProbe {
		slot := &t.slots[(home+probe)&seenMask]
		if !slot.used {
			return nil
		}

		if slot.icao == key {
			return slot
		}
	}

	return nil
}

// show records that the filter let an aircraft through, starting its fade-in
// when it has just come on.
func (t *seenTable) show(entry *seenEntry) {
	if entry == nil {
		return
	}

	if !entry.decided || !entry.shown {
		entry.shownAt = t.elapsed
		if t.instant {
			entry.shownAt -= fadeInFor
		}
	}

	entry.decided, entry.shown = true, true
}

// hide records that the filter held an aircraft back.
func (*seenTable) hide(entry *seenEntry) {
	if entry == nil {
		return
	}

	entry.decided, entry.shown = true, false
}

// slotOf is the home slot of an address.
func slotOf(key uint32) int {
	return int((key * fibonacci) >> (keyBits - seenBits))
}

// icaoKey reads a six-digit hex address into its 24 bits without allocating,
// either case accepted.
func icaoKey(icao string) (uint32, bool) {
	if len(icao) != icaoChars {
		return 0, false
	}

	var key uint32

	for index := range icaoChars {
		digit, ok := hexDigit(icao[index])
		if !ok {
			return 0, false
		}

		key = key<<hexBits | digit
	}

	return key, true
}

// hexDigit is one hex character's value.
func hexDigit(char byte) (uint32, bool) {
	switch {
	case char >= '0' && char <= '9':
		return uint32(char - '0'), true
	case char >= 'A' && char <= 'F':
		return uint32(char-'A') + hexLetter, true
	case char >= 'a' && char <= 'f':
		return uint32(char-'a') + hexLetter, true
	default:
		return 0, false
	}
}

// presence is how far through its fade-in an aircraft is, from 0 to 1. An
// aircraft the table has no decision for is at full strength, and so is one
// whose fade would have started after now, which is a render clock that has
// gone back to zero.
func (s *Scene) presence(icao string) float64 {
	entry := s.seen.find(icao)
	if entry == nil || !entry.decided {
		return 1
	}

	age := s.seen.elapsed - entry.shownAt
	if age < 0 || age >= fadeInFor {
		return 1
	}

	return float64(age) / float64(fadeInFor)
}

// faded is fade for a share that is usually all of it. A colour at full
// strength is handed back untouched rather than mixed with itself, so a frame
// with nothing fading draws exactly the bytes it drew before any of this.
func (s *Scene) faded(col color.RGBA, share float64) color.RGBA {
	if share >= 1 {
		return col
	}

	return s.fade(col, share)
}

// coastShare is how strongly a lost trail is drawn as a coasting aircraft,
// from 1 when it was lost to 0 at coastFor, and false once it is past that and
// only a ghost. The age is measured on the frame clock, the one the ghost ring
// stamps LostAt with and expires trails on, so the two cannot disagree about
// how long ago a loss was. A loss after now is a clock that went back, and is
// drawn as if it had just happened.
func coastShare(ghost *source.Trail, now time.Time) (float64, bool) {
	age := now.Sub(ghost.LostAt)
	if age >= coastFor {
		return 0, false
	}

	return min(1, 1-float64(age)/float64(coastFor)), true
}

// coastStart is the index of the first ghost still coasting. The ring hands
// its trails over in the order they were lost, so the coasting ones are a run
// at the end of it and the walk back stops at the first that is not.
func coastStart(ghosts []source.Trail, now time.Time) int {
	start := len(ghosts)

	for start > 0 {
		if _, coasting := coastShare(&ghosts[start-1], now); !coasting {
			break
		}

		start--
	}

	return start
}

// coastPlane is a lost trail dressed as the aircraft it was, for the drawing
// code that takes a snapshot: the last position, heading and vertical rate it
// was heard with, and its whole track for the model's bank.
func coastPlane(ghost *source.Trail) airplane.Snapshot {
	return airplane.Snapshot{
		ICAO:            ghost.ICAO,
		Callsign:        ghost.Callsign,
		Altitude:        ghost.Altitude,
		Heading:         ghost.Heading,
		VertRate:        ghost.VertRate,
		Latitude:        ghost.Latitude,
		Longitude:       ghost.Longitude,
		PositionHistory: ghost.Points,
	}
}

// passes is visible's rule for an aircraft by its parts, which is how a
// coasting aircraft is filtered on the altitude and callsign it was last
// heard with. It reads the aircraft's last decision from the table and writes
// this one back.
func (s *Scene) passes(icao string, altitude float64, callsign string) bool {
	entry := s.seen.touch(icao)
	if !s.admits(altitude, callsign, entry) {
		s.seen.hide(entry)

		return false
	}

	s.seen.show(entry)

	return true
}

// inBand is the altitude filters with the dead band applied: an aircraft
// that was shown stays shown until it is filterBandFt past an edge, and one
// that was hidden stays hidden until it is filterBandFt inside. One with no
// decision behind it is held to the edges themselves, which is the rule
// bandOf gives.
func (s *Scene) inBand(altitude float64, entry *seenEntry) bool {
	if _, known := source.AltitudeBandOf(altitude); !known {
		return false
	}

	floor, ceiling := s.filter.edges()

	margin := 0.0
	if entry != nil && entry.decided {
		margin = -filterBandFt
		if entry.shown {
			margin = filterBandFt
		}
	}

	return altitude >= floor-margin && altitude < ceiling+margin
}

// bandFloors and bandCeilings are each band's edges in feet, the bottom one
// open below and the top one open above.
//
//nolint:gochecknoglobals // two tables of thresholds, read only.
var (
	bandFloors   = [bandCount]float64{math.Inf(-1), source.LowCeilingFt, source.MidCeilingFt}
	bandCeilings = [bandCount]float64{source.LowCeilingFt, source.MidCeilingFt, math.Inf(1)}
)

// edges is the altitude range a band lets through, before any dead band.
func (f filterState) edges() (float64, float64) {
	return bandFloors[f.band], bandCeilings[f.band]
}

// setFilter changes the filter, and forgets the old filter's decisions when it
// really is a change.
func (s *Scene) setFilter(filter filterState) {
	if filter != s.filter {
		s.seen.reset()
	}

	s.filter = filter
}
