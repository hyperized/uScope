package radar

import (
	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/pkg/airlines"
)

// The altitude bands as indices rather than as colours.
//
// bandColour reads them and so does the filter, which is the whole point of
// having them: the band an aircraft is painted in and the band the f key
// selects have to be one answer to one question, or the scope would hide an
// aeroplane whose own colour said it belonged on screen.
//
// They are the source's bands rather than a copy of them, so the one set of
// thresholds lives where the altitudes come from.
const (
	bandLow  = source.BandLow
	bandMid  = source.BandMid
	bandHigh = source.BandHigh

	// bandCount is how many bands there are, which is what every loop over
	// them runs to. The legend names exactly these three.
	bandCount = source.BandCount

	// bandUnknown is an altitude nobody has decoded, which is not a band.
	// It is painted muted, and no filter value selects it: the legend has
	// three entries and none of them is "not known", so an aircraft with no
	// altitude goes off the field as soon as a band is picked. It sorts above
	// every band, which the ceiling relies on.
	bandUnknown = bandCount
)

// The words the F cap shows for the value it is on.
//
// The three band spellings are the legend's own bands said short enough for a
// key cap. The legend has room for a swatch and "10-25K FT" beside it; the bar
// has room for six characters before it starts pushing the caps after it off
// the right margin.
const (
	labelFilterAll  = "ALL"
	labelFilterLow  = "<10K"
	labelFilterMid  = "10-25K"
	labelFilterHigh = ">25K"

	// labelFilterBelowHigh is the ceiling's word: everything under the high
	// band, which is the low and the mid band read together.
	labelFilterBelowHigh = "<25K"

	// filterBarLabel is what the F cap falls back to. It is never drawn:
	// filterState.label answers from the value itself and every value has a
	// word. It is here so the entry in keyCaps is not the one row of the table
	// with an empty label in it, which is trailBarLabel's reason too.
	filterBarLabel = "FILTER"
)

// filterBandLabels is what each band's cap says.
//
//nolint:gochecknoglobals // a label table is data, and an array cannot be const.
var filterBandLabels = [bandCount]string{
	bandLow:  labelFilterLow,
	bandMid:  labelFilterMid,
	bandHigh: labelFilterHigh,
}

// filterKind is what a filter value means, which is whatever the colour mode
// on screen means.
//
// It is unexported, the way trailMode is and unlike ColourMode and View,
// because no flag parses into it directly. BandFilter is the spelling
// --filter takes, and it covers the altitude and corridor values; the
// operator values are only ever reached from the f key, so there is nothing
// outside the package to hand them over.
type filterKind uint8

// The six kinds of value the filter can hold.
const (
	// filterAll draws the whole fleet, which is where every run starts and
	// where pressing c puts it back.
	filterAll filterKind = iota

	// filterBand keeps one altitude band, which is what the cycle offers while
	// the scope is coloured by height.
	filterBand

	// filterOperator keeps one airline, named by its three-letter designator
	// rather than by its place in the legend. A designator survives the legend
	// being re-ranked when one operator overtakes another, which a slot number
	// would not: the scope would quietly start showing a different airline
	// without anybody pressing a key.
	filterOperator

	// filterOther keeps the aircraft with no colour of their own: no callsign
	// at all, or a prefix the database does not know. It is the legend's own
	// last entry.
	filterOther

	// filterBelow keeps every altitude band at or under band: a ceiling
	// rather than a band. It is the inverse of what a single band does, and
	// what it exists for is hiding the overflights. Against three bands the
	// only ceiling that is not already a band or ALL is the one under the
	// high band, so that is the only one the cycle offers, but nothing here
	// assumes it.
	filterBelow

	// filterClass keeps one track class: arriving, leaving, or the rest. It is
	// what the cycle offers while the scope is coloured by corridor, and the
	// ceiling is offered there too.
	filterClass
)

// filterState is the value the f key cycles.
//
// It is one comparable struct rather than three fields on the Scene so the
// whole filter can be replaced in one assignment, which is what pressing c
// does, and so a test can write the value it expects as a literal.
type filterState struct {
	kind filterKind

	// band is which of the three altitude bands, when kind is filterBand,
	// and the highest band still shown, when kind is filterBelow.
	band int

	// icao is the operator's designator, when kind is filterOperator.
	icao string

	// class is the track class, when kind is filterClass.
	class source.TrackClass
}

// active reports whether the filter is holding anything back. The F cap is
// drawn filled when it is, hollow when it is not, which is the same rule the
// trail cap follows for its own off state.
func (f filterState) active() bool { return f.kind != filterAll }

// label is what the F cap says, which is the value rather than the word
// FILTER. A cap reading FILTER says there is a filter without saying what it
// is filtering to, which is the question it is being asked.
func (f filterState) label() string {
	switch f.kind {
	case filterBand:
		return filterBandLabels[f.band]
	case filterBelow:
		return labelFilterBelowHigh
	case filterOperator:
		return f.icao
	case filterOther:
		return legendOther
	case filterClass:
		return classLabel(f.class)
	case filterAll:
		fallthrough
	default:
		return labelFilterAll
	}
}

// marksBand reports whether the filter is on the legend's band-th altitude
// entry, marksOperator and marksOther are the same question about the
// airline legend's two kinds of row, and marksClass about the corridor
// legend's. The legend rings whichever of its own
// entries answers true, so it says which row the scope is showing rather than
// leaving the key bar to say it alone.
//
// A ceiling marks every band it lets through, so the legend rings the low and
// the mid swatch together and the picture and the legend agree about what is
// on the field.
func (f filterState) marksBand(band int) bool {
	switch f.kind {
	case filterBand:
		return f.band == band
	case filterBelow:
		return band <= f.band
	case filterAll, filterOperator, filterOther, filterClass:
		fallthrough
	default:
		return false
	}
}

func (f filterState) marksOperator(icao string) bool {
	return f.kind == filterOperator && f.icao == icao
}

func (f filterState) marksOther() bool { return f.kind == filterOther }

func (f filterState) marksClass(class source.TrackClass) bool {
	return f.kind == filterClass && f.class == class
}

// bandOf is which altitude band an aircraft is in.
//
// The thresholds are source.AltitudeBandOf's.
// An altitude of zero means nobody has decoded one yet, not sea level, and a
// NaN means the same; both come back bandUnknown, which is what bandColour
// paints muted.
func bandOf(altitude float64) int {
	band, known := source.AltitudeBandOf(altitude)
	if !known {
		return bandUnknown
	}

	return band
}

// visible reports whether one aircraft passes the filter.
//
// It is the whole of the filter as far as the rest of the scene is concerned.
// Every consumer asks this and asks nothing else: the flat scope, minimal
// mode, the 3D view, the strip board, the selection index, the centroid and
// the auto range. One predicate in one place stops two views disagreeing
// about which aeroplanes are on the field, which is the rule trailMode.plan
// already follows for the trails.
//
// The altitude filters remember their last answer for each aircraft and hold
// it inside a dead band either side of the edge; see inBand. Asking twice in
// one frame gets the same answer twice, because the band an answer is held in
// always contains the altitude that produced it.
//
// A coasting aircraft goes through passes, the same rule by its parts, on the
// altitude and callsign it was last heard with and the class its trail was
// given. Ghosts past their coast do
// not go through it at all: hiding a track on a reading that old would be a
// decision nobody could check against the picture.
func (s *Scene) visible(plane airplane.Snapshot) bool {
	return s.passes(plane.ICAO, plane.Altitude, plane.Callsign, s.filterClass(plane))
}

// filterClass is the aircraft's track class when the filter is holding to
// one, and transit otherwise. visible is asked several times a frame per
// aircraft and every other filter ignores the class, so Classify only runs
// when the answer is read.
func (s *Scene) filterClass(plane airplane.Snapshot) source.TrackClass {
	if s.filter.kind != filterClass {
		return source.TrackTransit
	}

	return s.exposure.corridors.Classify(plane)
}

// admits is the filter's decision, given what the table remembers.
func (s *Scene) admits(altitude float64, callsign string, class source.TrackClass, entry *seenEntry) bool {
	switch s.filter.kind {
	case filterClass:
		return class == s.filter.class
	case filterBand, filterBelow:
		// An aircraft with no altitude decoded stays hidden under a ceiling
		// the way it does under a band.
		return s.inBand(altitude, entry)
	case filterOperator:
		airline, known := airlines.Lookup(callsign)

		return known && airline.ICAO == s.filter.icao
	case filterOther:
		_, known := airlines.Lookup(callsign)

		return !known
	case filterAll:
		fallthrough
	default:
		return true
	}
}

// cycleFilter moves the filter on one, which is what f does.
//
// The three cycles are the three legends. In altitude mode it is a fixed list
// of five: ALL, the three bands, the ceiling under the high band, ALL again.
// Corridor mode is the same shape over the classes: ALL, IN, OUT, OTHER, the
// ceiling, ALL. In airline mode it is whatever the legend was showing on the
// frame the key arrived between, which is why the tally is counted at the top
// of Draw rather than inside the block that draws the legend.
func (s *Scene) cycleFilter() {
	switch s.colour {
	case ColourAirline:
		s.setFilter(s.nextOperatorFilter())
	case ColourCorridor:
		s.setFilter(nextClassFilter(s.filter))
	case ColourAltitude:
		fallthrough
	default:
		s.setFilter(nextBandFilter(s.filter))
	}
}

// nextClassFilter is the value after this one in corridor mode's cycle.
//
// The ceiling closes it for the sake of the field's own traffic: an overflight is OTHER, but so
// is a light aircraft crossing the field at two thousand feet, and the
// ceiling is how the field's own traffic is shown in all three colours with
// the airways overhead taken away. Anything that is neither a class nor the
// ceiling starts at IN, by nextBandFilter's rule.
func nextClassFilter(current filterState) filterState {
	switch current.kind {
	case filterClass:
		return classFilterAfter(current.class)
	case filterBelow:
		return filterState{}
	case filterAll, filterBand, filterOperator, filterOther:
		fallthrough
	default:
		return filterState{kind: filterClass, class: source.TrackInbound}
	}
}

// classFilterAfter is the step after one class: IN to OUT, OUT to OTHER, and
// OTHER on to the ceiling.
func classFilterAfter(class source.TrackClass) filterState {
	switch class {
	case source.TrackInbound:
		return filterState{kind: filterClass, class: source.TrackOutbound}
	case source.TrackOutbound:
		return filterState{kind: filterClass, class: source.TrackTransit}
	case source.TrackTransit:
		fallthrough
	default:
		return filterState{kind: filterBelow, band: bandMid}
	}
}

// nextBandFilter is the value after this one in altitude mode's cycle.
//
// The ceiling comes after the high band rather than before the low one, so the
// cycle reads the three bands in order first and the press that would have
// gone back to ALL lands on the one view of the field the bands cannot give.
//
// Anything that is neither a band nor a ceiling starts at the lowest band, so
// the value left behind by airline mode cycles the way ALL does and never has
// to be special-cased. That is trailMode.next's rule, for trailMode.next's
// reason.
func nextBandFilter(current filterState) filterState {
	switch current.kind {
	case filterBand:
		if current.band+1 < bandCount {
			return filterState{kind: filterBand, band: current.band + 1}
		}

		return filterState{kind: filterBelow, band: bandMid}
	case filterBelow:
		return filterState{}
	case filterAll, filterOperator, filterOther, filterClass:
		fallthrough
	default:
		return filterState{kind: filterBand, band: bandLow}
	}
}

// nextOperatorFilter is the value after this one in airline mode's cycle, read
// off the legend the last drawn frame left behind.
//
// A value whose operator has since left the field falls to the end of the
// cycle rather than being hunted for. The alternative is a key that behaves
// differently depending on how long ago it was last pressed.
func (s *Scene) nextOperatorFilter() filterState {
	switch s.filter.kind {
	case filterOther:
		return filterState{}
	case filterOperator:
		return s.operatorAfter(s.filter.icao)
	case filterAll, filterBand, filterBelow, filterClass:
		fallthrough
	default:
		return s.operatorAt(0)
	}
}

// operatorAfter is the legend entry following the one designated icao.
func (s *Scene) operatorAfter(icao string) filterState {
	for slot := range s.counts.shown {
		if s.counts.seen[slot].airline.ICAO == icao {
			return s.operatorAt(slot + 1)
		}
	}

	return s.otherOrAll()
}

// operatorAt is the legend's slot-th entry, or what closes the cycle when the
// legend is shorter than that.
func (s *Scene) operatorAt(slot int) filterState {
	if slot >= s.counts.shown {
		return s.otherOrAll()
	}

	return filterState{kind: filterOperator, icao: s.counts.seen[slot].airline.ICAO}
}

// otherOrAll closes airline mode's cycle: the OTHER entry when the legend
// draws one, and back to ALL when everything on the field has a colour.
//
// Following the legend rather than offering OTHER unconditionally is what
// keeps the key honest. A value standing for nothing on the field would hide
// the whole fleet and say OTHER about it, which reads as a bug rather than as
// an empty category.
func (s *Scene) otherOrAll() filterState {
	if s.counts.other {
		return filterState{kind: filterOther}
	}

	return filterState{}
}

// countOperators tallies the frame's operators, for the legend and for the
// filter both.
//
// It runs at the top of Draw rather than inside drawAirlineLegend, because
// three things now read the answer and only one of them draws: the legend
// names the busiest four, the f key's cycle walks those same four in the same
// order, and visible tests every aircraft against whichever one is picked.
// Counting it where the legend is drawn would leave the filter reading the
// frame before, and would leave it reading nothing at all in minimal and 3D,
// neither of which draws a legend.
//
// It counts the whole frame rather than what survives the filter. Counting the
// survivors would shrink the legend to one entry the moment an operator was
// picked, and leave the cycle with nowhere to go next.
//
// Altitude mode counts nothing and empties the table. Nothing reads it there,
// and a table left full of the last airline frame's operators would be a stale
// cycle waiting for the next press of c.
func (s *Scene) countOperators(frame source.Frame) {
	s.counts.reset()

	if s.colour != ColourAirline {
		return
	}

	for _, plane := range frame.Planes {
		s.counts.add(plane.Callsign)
	}

	s.counts.rank()
}

// shownPlanes is how many aircraft the filter lets through on this frame.
//
// It is the length of the ICAO index syncSelection has just rebuilt rather
// than a second pass over the fleet. The two would have to agree in any case,
// and a board that counted differently from the selection would give the full
// strip to the wrong aeroplane.
func (s *Scene) shownPlanes() int { return len(s.icaos) }

// findPlane is the aircraft with this ICAO, whether the filter is showing it
// or not.
//
// It is what lets a pinned selection survive being filtered out: the card is
// still about that aeroplane, it is still flying, and the panel has to draw it
// from somewhere. A pin whose aircraft has genuinely left the list finds
// nothing here, which is what hands the selection back to the nearest contact.
func findPlane(frame source.Frame, icao string) (airplane.Snapshot, bool) {
	if icao == "" {
		return airplane.Snapshot{}, false
	}

	for _, plane := range frame.Planes {
		if plane.ICAO == icao {
			return plane, true
		}
	}

	return airplane.Snapshot{}, false
}
