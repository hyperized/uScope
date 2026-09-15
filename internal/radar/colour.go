package radar

import (
	"errors"
	"fmt"
	"image/color"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/pkg/airlines"
)

// ColourMode says what an aircraft's colour on the scope means.
//
// It is a string type for the same reason theme.Kind is: it is what --colour
// parses into, so the flag spelling and the value are the same thing and
// cannot drift apart.
type ColourMode string

// The two modes, in the order the c key cycles them.
const (
	// ColourAltitude is the default: three bands of height, which is the one
	// piece of information a top-down scope cannot show by position.
	ColourAltitude ColourMode = "altitude"

	// ColourAirline paints each aircraft in its operator's colour instead.
	// Height then has to be read off the card and the rows, and what the scope
	// shows at a glance is who is flying rather than how high.
	ColourAirline ColourMode = "airline"
)

// ErrColour is returned for a --colour value that is neither spelling.
var ErrColour = errors.New("radar: unknown colour mode")

// ParseColour turns a --colour value into a ColourMode.
//
// The match is case sensitive, the same way theme.Parse is: --colour is an
// allow list rather than free text, so "Airline" is refused exactly as
// "airlines" would be.
func ParseColour(text string) (ColourMode, error) {
	switch ColourMode(text) {
	case ColourAltitude:
		return ColourAltitude, nil
	case ColourAirline:
		return ColourAirline, nil
	default:
		return ColourAltitude, fmt.Errorf("%w: %q", ErrColour, text)
	}
}

// Next cycles to the other mode. Anything that is not ColourAirline moves to
// it, so the zero value of ColourMode cycles the way ColourAltitude does and
// never has to be special-cased where one is read.
func (m ColourMode) Next() ColourMode {
	if m == ColourAirline {
		return ColourAltitude
	}

	return ColourAirline
}

// Toggle is an on or off setting a flag carries into the scene.
//
// It is a string type rather than a bool for the same reason ColourMode is one:
// the zero value has to be the default, and these settings default to on. A
// bool field left unset in a Config would turn the airfields off, which is not
// what "nobody said" means.
type Toggle string

// The two spellings an on/off flag accepts.
const (
	ToggleOn  Toggle = "on"
	ToggleOff Toggle = "off"
)

// ErrToggle is returned for an on/off value that is neither spelling.
var ErrToggle = errors.New("radar: value must be on or off")

// On reports whether the toggle is on. Anything that is not ToggleOff is,
// which is what makes the zero value usable.
func (t Toggle) On() bool { return t != ToggleOff }

// ParseToggle reads an on/off flag value.
//
// It is an allow list rather than strconv.ParseBool, which also takes "1",
// "t", "TRUE" and five other spellings. A setting with two states should have
// two spellings, and a typo should be refused rather than guessed at.
func ParseToggle(text string) (Toggle, error) {
	switch Toggle(text) {
	case ToggleOn:
		return ToggleOn, nil
	case ToggleOff:
		return ToggleOff, nil
	default:
		return ToggleOn, fmt.Errorf("%w: %q", ErrToggle, text)
	}
}

// Settings are what the command line picked before the scene existed.
//
// They travel as one struct rather than as a setter each, because the radar
// keeps gaining them: a package that grows an interface per flag ends up with
// a shelf of one-method interfaces that all mean "the command line said so".
// Every field's zero value is its default, so a Settings nobody filled in is
// the scene as it starts.
type Settings struct {
	// Colour is what an aircraft's colour means. Zero reads as ColourAltitude.
	Colour ColourMode

	// Airports is whether the airfield markers start on. Zero reads as on,
	// which is the scope at its most useful before anyone touches a key.
	Airports Toggle
}

// Apply sets the whole block on a scene that is already built, which is how
// the flags reach a scene the run loop built before it read the config.
func (s *Scene) Apply(set Settings) {
	s.colour = set.Colour
	s.airports = set.Airports.On()
}

// WithSettings applies a settings block at construction.
func WithSettings(set Settings) Option {
	return func(s *Scene) { s.Apply(set) }
}

// The legend's operator tally.
const (
	// legendSlots is how many operators the legend names. Four fits across the
	// column at the panel's width with room for a name beside each designator,
	// and a legend longer than that stops being something read at a glance.
	legendSlots = 4

	// maxOperators is how many distinct operators the per-frame counter table
	// holds. A scope covering a few hundred nautical miles sees a few dozen at
	// the outside, and anything past the table counts as OTHER rather than
	// growing a slice on the draw path.
	maxOperators = 64
)

// legendOther is the last legend entry, standing for every aircraft with no
// callsign and every callsign whose operator is not in the database.
const legendOther = "OTHER"

// operatorCount is one airline and how many of it are on the scope.
type operatorCount struct {
	airline airlines.Airline
	count   int
}

// tally counts the operators in one frame.
//
// It lives on the Scene and is reset rather than rebuilt, because the legend
// is recomputed on every frame and the draw path allocates nothing. The table
// is a fixed array for that reason: a map would rehash, and a slice would
// grow the first time a busy frame arrived.
type tally struct {
	seen  [maxOperators]operatorCount
	count int

	// shown is how many of seen hold the ranked entries after rank, and other
	// says whether anything on the field had no colour at all.
	shown int
	other bool
}

// reset empties the table for a new frame.
func (t *tally) reset() {
	t.count, t.shown, t.other = 0, 0, false
}

// add counts one aircraft against its operator.
//
// The scan is linear because the table is short and a frame walks it once per
// aircraft: at a few dozen entries that is cheaper than hashing a key, and it
// costs nothing to set up.
func (t *tally) add(callsign string) {
	airline, known := airlines.Lookup(callsign)
	if !known {
		t.other = true

		return
	}

	for index := range t.count {
		if t.seen[index].airline.ICAO == airline.ICAO {
			t.seen[index].count++

			return
		}
	}

	if t.count == maxOperators {
		t.other = true

		return
	}

	t.seen[t.count] = operatorCount{airline: airline, count: 1}
	t.count++
}

// rank pulls the busiest operators to the front of the table, leaving the
// first shown entries in legend order.
//
// It is a partial selection sort rather than a full one because only four
// entries are ever drawn, and sorting the rest would be work thrown away. The
// table is rebuilt every frame, so reordering it in place costs nothing.
func (t *tally) rank() {
	limit := min(legendSlots, t.count)

	for slot := range limit {
		best := slot

		for index := slot + 1; index < t.count; index++ {
			if outranks(t.seen[index], t.seen[best]) {
				best = index
			}
		}

		t.seen[slot], t.seen[best] = t.seen[best], t.seen[slot]
	}

	t.shown = limit
}

// outranks reports whether left belongs above right in the legend.
//
// Aircraft count decides it, and the designator breaks a tie. The tie-break is
// not cosmetic: the aircraft list arrives sorted by distance, so without it two
// operators level on count would swap places whenever one of them moved, and
// the same input would stop producing the same frame.
func outranks(left, right operatorCount) bool {
	if left.count != right.count {
		return left.count > right.count
	}

	return left.airline.ICAO < right.airline.ICAO
}

// aircraftColour is the colour one aircraft is drawn in: its silhouette, its
// trail, its callsign on the card and its callsign in the compact rows.
func (s *Scene) aircraftColour(plane airplane.Snapshot) color.RGBA {
	if s.colour != ColourAirline {
		return s.bandColour(plane.Altitude)
	}

	return s.airlineColour(plane.Callsign)
}

// airlineColour is the operator's colour for a callsign.
//
// An aircraft with no callsign, or one whose three-letter prefix is not in the
// database, comes back muted. That is the same answer bandColour gives an
// aircraft with no decoded altitude, and it reads as "nothing known" rather
// than as a colour somebody has to look up.
func (s *Scene) airlineColour(callsign string) color.RGBA {
	airline, known := airlines.Lookup(callsign)
	if !known {
		return s.pal.Muted
	}

	return s.operatorColour(airline)
}

// operatorColour adapts a brand colour to the field it is being drawn on.
//
// A brand colour is picked for print and for a white page, so half of them
// disappear into night's near-black field and the bright half washes out on
// paper. pkg/airlines does the adapting; this only picks which way.
func (s *Scene) operatorColour(airline airlines.Airline) color.RGBA {
	if s.light {
		return airline.OnLight()
	}

	return airline.OnDark()
}

// callsignInk is the colour a callsign is set in.
//
// In airline mode it is the operator's own colour, which is the whole point of
// the mode: the same colour marks the aircraft on the scope and its name in
// the list beside it.
func (s *Scene) callsignInk(plane airplane.Snapshot) color.RGBA {
	if s.colour == ColourAirline {
		return s.airlineColour(plane.Callsign)
	}

	return s.pal.Ink
}

// rowCallsignInk is callsignInk for the compact row at index, where an
// unselected callsign is muted in altitude mode so the eye lands on the
// selection first.
func (s *Scene) rowCallsignInk(plane airplane.Snapshot, index int) color.RGBA {
	if index == s.selIndex || s.colour == ColourAirline {
		return s.callsignInk(plane)
	}

	return s.pal.Muted
}
