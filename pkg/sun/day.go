package sun

import "time"

// Condition is which part of the solar day an elevation falls in: day, one of
// the three twilights, or night.
type Condition int

// The five conditions, darkest first. The zero value is Night, so a Condition
// nobody worked out reads as the dark end rather than as daylight, which is
// the same side the auto theme errs on.
const (
	Night Condition = iota
	AstronomicalTwilight
	NauticalTwilight
	CivilTwilight
	Day
)

// String returns the condition's name in capitals, the register the header
// sets every fixed word in, and NIGHT for anything outside the five.
func (c Condition) String() string {
	switch c {
	case Day:
		return "DAY"
	case CivilTwilight:
		return "CIVIL TWILIGHT"
	case NauticalTwilight:
		return "NAUTICAL TWILIGHT"
	case AstronomicalTwilight:
		return "ASTRONOMICAL TWILIGHT"
	case Night:
		fallthrough
	default:
		return "NIGHT"
	}
}

// ConditionOf classifies an elevation in degrees, as Elevation returns it.
//
// Each band includes its upper bound and excludes its lower one: exactly 0 is
// civil twilight, exactly -6 is nautical. The first of those is what matters,
// because IsDay calls 0 night too, so the header's word and the auto theme
// never disagree at the horizon. A NaN compares false everywhere and lands on
// Night.
func ConditionOf(elevation float64) Condition {
	switch {
	case elevation > Sunset:
		return Day
	case elevation > CivilLimit:
		return CivilTwilight
	case elevation > NauticalLimit:
		return NauticalTwilight
	case elevation > AstronomicalLimit:
		return AstronomicalTwilight
	default:
		return Night
	}
}

// scanStep is the coarse scan's stride. A crossing is found as a sign change
// between two samples, so a dip below the horizon shorter than this can be
// missed, which only happens within a few days of the polar day's edge and
// costs a missing time rather than a wrong one. Ten minutes is 144 samples a
// day, which is nothing at the once-a-day rate callers ask at.
const scanStep = 10 * time.Minute

// direction is which way a crossing goes. It is a type of its own so the two
// call sites read as rising and setting rather than as true and false.
type direction bool

const (
	rising  direction = true
	setting direction = false
)

// riseAltitude is the geometric elevation of the sun's centre at an almanac
// sunrise or sunset: 34 arc minutes of standard refraction at the horizon and
// 16 of semi-diameter, the convention the US Naval Observatory and NOAA both
// publish against.
const riseAltitude = -0.833

// bisectSteps halves a scanStep bracket down to under a second, so the
// rounding to the minute that follows is decided by the crossing rather than
// by where the bracket happened to stop.
const bisectSteps = 10

// Rise returns the first moment on the civil day containing day, as a
// calendar day in loc, at which the sun rises, rounded to the minute and
// expressed in loc. It reports false when the sun does not rise that day, in
// the polar night or the polar day.
//
// A nil loc reads as UTC. The day runs from local midnight to the next one,
// which on a daylight saving change is 23 or 25 hours rather than 24, and
// that is the day a clock on the wall shows.
//
// Sunrise here is the almanac's: the upper limb on the horizon, with the
// centre at riseAltitude. That is a different line from Sunset, which is the
// apparent centre on the horizon, and the difference is two or three minutes
// at Dutch latitudes. The almanac's is what a weather app or a newspaper
// prints, so a time on screen that disagreed with every published one by
// three minutes would read as a bug; the theme switch is the one that can
// afford to be a couple of minutes late.
func Rise(lat, lon float64, day time.Time, loc *time.Location) (time.Time, bool) {
	return crossing(lat, lon, day, loc, rising)
}

// Set is Rise for the moment the upper limb drops below the horizon.
func Set(lat, lon float64, day time.Time, loc *time.Location) (time.Time, bool) {
	return crossing(lat, lon, day, loc, setting)
}

// crossing scans the local day for the first sample pair the sun crosses the
// horizon between in the wanted direction, then bisects inside it.
func crossing(lat, lon float64, day time.Time, loc *time.Location, way direction) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}

	local := day.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	end := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, loc)

	before := up(lat, lon, start)

	for sample := start; sample.Before(end); sample = sample.Add(scanStep) {
		// No clamp to end: every day length a time zone produces, 23, 23.5,
		// 24 or 25 hours, is a whole number of steps, so the last sample is
		// midnight itself.
		next := sample.Add(scanStep)
		after := up(lat, lon, next)

		if after != before && after == bool(way) {
			return bisect(lat, lon, sample, next, way).Round(time.Minute).In(loc), true
		}

		before = after
	}

	return time.Time{}, false
}

// bisect narrows a bracket whose ends are on opposite sides of the horizon
// and returns the end on the far side: the first instant known to be up for a
// rise, down for a set.
func bisect(lat, lon float64, near, far time.Time, way direction) time.Time {
	for range bisectSteps {
		mid := near.Add(far.Sub(near) / 2)
		if up(lat, lon, mid) == bool(way) {
			far = mid
		} else {
			near = mid
		}
	}

	return far
}

// up reports whether the sun's upper limb is over the horizon.
func up(lat, lon float64, at time.Time) bool {
	return geometric(lat, lon, at) > riseAltitude
}
