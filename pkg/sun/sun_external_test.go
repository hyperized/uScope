package sun_test

import (
	"math"
	"testing"
	"time"

	"github.com/hyperized/uScope/pkg/sun"
)

const (
	amsLat = 52.37
	amsLon = 4.90
)

func utc(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, time.UTC)
}

// Reference noon elevations come from 90 - |lat - declination| with the
// solstice declination of 23.44 degrees, cross-checked against NOAA's solar
// calculator for 2026-06-21 and 2026-12-21 (Amsterdam, Tromso, Cape Town).
func TestElevationNoon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		lat, lon  float64
		at        time.Time
		want, tol float64
	}{
		{"amsterdam june solstice", amsLat, amsLon, utc(time.June, 21, 11, 30), 61.1, 0.5},
		{"amsterdam december solstice", amsLat, amsLon, utc(time.December, 21, 11, 30), 14.2, 0.5},
		{"equator march equinox", 0, 0, utc(time.March, 20, 12, 7), 90, 1},
		{"cape town december solstice", -33.92, 18.42, utc(time.December, 21, 10, 45), 79.5, 1},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := sun.Elevation(testCase.lat, testCase.lon, testCase.at)
			if math.Abs(got-testCase.want) > testCase.tol {
				t.Errorf("Elevation = %.2f, want %.1f +/- %.1f", got, testCase.want, testCase.tol)
			}
		})
	}
}

// Sunrise in Amsterdam on 2026-06-21 is about 03:20 UTC and sunset on
// 2026-12-21 about 15:30 UTC; instants are kept 20+ minutes clear of those.
func TestIsDay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lat, lon float64
		at       time.Time
		want     bool
	}{
		{"amsterdam june 04:00 utc", amsLat, amsLon, utc(time.June, 21, 4, 0), true},
		{"amsterdam june 03:00 utc is before sunrise", amsLat, amsLon, utc(time.June, 21, 3, 0), false},
		{"amsterdam june 23:00 utc", amsLat, amsLon, utc(time.June, 21, 23, 0), false},
		{"amsterdam december 09:00 utc", amsLat, amsLon, utc(time.December, 21, 9, 0), true},
		{"amsterdam december 16:00 utc is after sunset", amsLat, amsLon, utc(time.December, 21, 16, 0), false},
		{"amsterdam december 17:00 utc", amsLat, amsLon, utc(time.December, 21, 17, 0), false},
		{"tromso midnight sun", 69.65, 18.96, utc(time.June, 21, 0, 30), true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := sun.IsDay(testCase.lat, testCase.lon, testCase.at); got != testCase.want {
				t.Errorf("IsDay = %v, want %v (elevation %.2f)", got, testCase.want,
					sun.Elevation(testCase.lat, testCase.lon, testCase.at))
			}
		})
	}
}

func TestElevationBands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lat, lon float64
		at       time.Time
		low      float64
		high     float64
	}{
		{"amsterdam june 23:00 utc is twilight", amsLat, amsLon, utc(time.June, 21, 23, 0), -18, 0},
		{"tromso midnight sun", 69.65, 18.96, utc(time.June, 21, 0, 30), 0, 90},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := sun.Elevation(testCase.lat, testCase.lon, testCase.at)
			if got <= testCase.low || got >= testCase.high {
				t.Errorf("Elevation = %.2f, want within (%.0f, %.0f)", got, testCase.low, testCase.high)
			}
		})
	}
}

func TestElevationZoneIndependent(t *testing.T) {
	t.Parallel()

	instant := utc(time.June, 21, 9, 15)
	cest := time.FixedZone("CEST", 2*60*60)

	inUTC := sun.Elevation(amsLat, amsLon, instant)
	inLocal := sun.Elevation(amsLat, amsLon, instant.In(cest))

	if inUTC != inLocal {
		t.Errorf("UTC %.6f != local %.6f", inUTC, inLocal)
	}
}

func TestElevationNormalisesInput(t *testing.T) {
	t.Parallel()

	instant := utc(time.June, 21, 9, 15)

	tests := []struct {
		name       string
		lat1, lon1 float64
		lat2, lon2 float64
	}{
		{"longitude wraps", amsLat, 364.9, amsLat, 4.9},
		{"negative longitude wraps", amsLat, -355.1, amsLat, 4.9},
		{"latitude clamps north", 95, amsLon, 90, amsLon},
		{"latitude clamps south", -95, amsLon, -90, amsLon},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			a := sun.Elevation(testCase.lat1, testCase.lon1, instant)
			b := sun.Elevation(testCase.lat2, testCase.lon2, instant)

			if math.Abs(a-b) > 1e-9 {
				t.Errorf("got %.9f and %.9f, want equal", a, b)
			}
		})
	}
}

func BenchmarkElevation(b *testing.B) {
	instant := utc(time.June, 21, 9, 15)

	b.ReportAllocs()

	for b.Loop() {
		sun.Elevation(amsLat, amsLon, instant)
	}
}
