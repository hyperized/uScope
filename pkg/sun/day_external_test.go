package sun_test

import (
	"math"
	"testing"
	"time"

	"github.com/hyperized/uScope/pkg/sun"
)

const (
	schipholLat = 52.3105
	schipholLon = 4.7683
)

func TestConditionOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		elevation float64
		want      sun.Condition
	}{
		{"high noon", 60, sun.Day},
		{"just over the horizon", 0.001, sun.Day},
		{"on the horizon", 0, sun.CivilTwilight},
		{"civil floor", -6, sun.NauticalTwilight},
		{"just over civil", -5.999, sun.CivilTwilight},
		{"nautical floor", -12, sun.AstronomicalTwilight},
		{"just over nautical", -11.999, sun.NauticalTwilight},
		{"astronomical floor", -18, sun.Night},
		{"just over astronomical", -17.999, sun.AstronomicalTwilight},
		{"midnight", -50, sun.Night},
		{"not a number", math.NaN(), sun.Night},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := sun.ConditionOf(testCase.elevation); got != testCase.want {
				t.Errorf("ConditionOf(%v) = %v, want %v", testCase.elevation, got, testCase.want)
			}
		})
	}
}

func TestConditionString(t *testing.T) {
	t.Parallel()

	const night = "NIGHT"

	tests := []struct {
		condition sun.Condition
		want      string
	}{
		{sun.Day, "DAY"},
		{sun.CivilTwilight, "CIVIL TWILIGHT"},
		{sun.NauticalTwilight, "NAUTICAL TWILIGHT"},
		{sun.AstronomicalTwilight, "ASTRONOMICAL TWILIGHT"},
		{sun.Night, night},
		{sun.Condition(-1), night},
		{sun.Day + 1, night},
	}

	for _, testCase := range tests {
		t.Run(testCase.want, func(t *testing.T) {
			t.Parallel()

			if got := testCase.condition.String(); got != testCase.want {
				t.Errorf("String() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func amsterdam(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	return loc
}

// The expected times are NOAA's sunrise equation (zenith 90.833 degrees, the
// almanac convention) for Schiphol, recomputed independently in Python from
// the NOAA solar calculator's spreadsheet formulas. timeanddate.com gives
// 05:18/22:06 and 08:48/16:29 for central Amsterdam, which sits 0.13 degrees
// further east and so half a minute earlier.
func TestRiseAndSetAtSchiphol(t *testing.T) {
	t.Parallel()

	loc := amsterdam(t)

	tests := []struct {
		name      string
		day       time.Time
		rise, set string
	}{
		{"june solstice", time.Date(2026, time.June, 21, 15, 0, 0, 0, loc), "05:19", "22:07"},
		{"december solstice", time.Date(2026, time.December, 21, 3, 0, 0, 0, loc), "08:48", "16:30"},
		{"spring forward", time.Date(2026, time.March, 29, 12, 0, 0, 0, loc), "07:23", "20:10"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rise, found := sun.Rise(schipholLat, schipholLon, testCase.day, loc)
			if !found {
				t.Fatal("Rise reported no sunrise")
			}

			set, found := sun.Set(schipholLat, schipholLon, testCase.day, loc)
			if !found {
				t.Fatal("Set reported no sunset")
			}

			within(t, "rise", rise, testCase.rise)
			within(t, "set", set, testCase.set)

			if rise.Location() != loc || set.Location() != loc {
				t.Errorf("times are in %v and %v, want %v", rise.Location(), set.Location(), loc)
			}
		})
	}
}

// within fails unless got shows want on a wall clock, give or take a minute.
func within(t *testing.T, what string, got time.Time, want string) {
	t.Helper()

	wall, err := time.ParseInLocation("15:04", want, got.Location())
	if err != nil {
		t.Fatalf("bad want %q: %v", want, err)
	}

	gotMinutes := got.Hour()*60 + got.Minute()
	wantMinutes := wall.Hour()*60 + wall.Minute()

	if diff := gotMinutes - wantMinutes; diff < -1 || diff > 1 {
		t.Errorf("%s = %s, want %s +/- 1 minute", what, got.Format("15:04"), want)
	}

	if got.Second() != 0 || got.Nanosecond() != 0 {
		t.Errorf("%s = %v, want it rounded to the minute", what, got)
	}
}

func TestRiseAndSetPolar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		lat  float64
		day  time.Time
	}{
		{"midnight sun at 80N", 80, time.Date(2026, time.June, 21, 0, 0, 0, 0, time.UTC)},
		{"polar night at 80N", 80, time.Date(2026, time.December, 21, 0, 0, 0, 0, time.UTC)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got, ok := sun.Rise(testCase.lat, 0, testCase.day, time.UTC); ok || !got.IsZero() {
				t.Errorf("Rise = %v, %v; want zero, false", got, ok)
			}

			if got, ok := sun.Set(testCase.lat, 0, testCase.day, time.UTC); ok || !got.IsZero() {
				t.Errorf("Set = %v, %v; want zero, false", got, ok)
			}
		})
	}
}

// A nil location is UTC, and the day is the UTC calendar day of the instant
// rather than the day the instant's own zone shows.
func TestRiseNilLocationIsUTC(t *testing.T) {
	t.Parallel()

	loc := amsterdam(t)
	// 00:30 in Amsterdam on the 22nd is still the 21st in UTC.
	instant := time.Date(2026, time.June, 22, 0, 30, 0, 0, loc)

	got, ok := sun.Rise(schipholLat, schipholLon, instant, nil)
	if !ok {
		t.Fatal("Rise reported no sunrise")
	}

	if got.Location() != time.UTC || got.Day() != 21 {
		t.Errorf("Rise = %v, want the 21st in UTC", got)
	}

	within(t, "rise", got, "03:19")
}
