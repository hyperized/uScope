package sun

import (
	"math"
	"testing"
	"time"
)

func TestJulianDay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		at   time.Time
		want float64
	}{
		{"j2000", time.Date(2000, time.January, 1, 12, 0, 0, 0, time.UTC), 2451545.0},
		{"unix epoch", time.Unix(0, 0).UTC(), 2440587.5},
		{"half second", time.Date(2000, time.January, 1, 12, 0, 0, 500000000, time.UTC), 2451545.0 + 0.5/86400},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := julianDay(testCase.at); math.Abs(got-testCase.want) > 1e-9 {
				t.Errorf("julianDay = %.9f, want %.9f", got, testCase.want)
			}
		})
	}
}

func TestRefraction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		elevation float64
		want, tol float64
	}{
		{"zenith", 90, 0, 0},
		{"high", 45, 0.0162, 0.002},
		{"five degrees", 5.5, 0.16, 0.02},
		{"horizon", 0, 0.48, 0.02},
		{"just below horizon", -0.5, 0.57, 0.03},
		{"civil twilight", -6, 0.055, 0.01},
		{"nadir", -90, 0, 1e-6},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := refraction(testCase.elevation)
			if math.Abs(got-testCase.want) > testCase.tol {
				t.Errorf("refraction(%v) = %.4f, want %.4f +/- %.4f",
					testCase.elevation, got, testCase.want, testCase.tol)
			}
		})
	}
}

func TestRefractionBounded(t *testing.T) {
	t.Parallel()

	for e := -90.0; e <= 90; e += 0.25 {
		if got := refraction(e); math.IsNaN(got) || math.Abs(got) > 1 {
			t.Fatalf("refraction(%v) = %v, want finite and under 1 degree", e, got)
		}
	}
}

func TestWrapLongitude(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want float64
	}{
		{4.9, 4.9},
		{364.9, 4.9},
		{-355.1, 4.9},
		{180, -180},
		{-180, -180},
		{0, 0},
	}

	for _, testCase := range tests {
		if got := wrapLongitude(testCase.in); math.Abs(got-testCase.want) > 1e-9 {
			t.Errorf("wrapLongitude(%v) = %v, want %v", testCase.in, got, testCase.want)
		}
	}
}

func TestSolarCoordinates(t *testing.T) {
	t.Parallel()

	// 2026-06-21 is within a day of the solstice: declination near 23.44,
	// equation of time near -1.7 minutes.
	tc := (julianDay(time.Date(2026, time.June, 21, 12, 0, 0, 0, time.UTC)) - j2000JD) / daysPerCentury
	decl, eqTime := solarCoordinates(tc)

	if d := degrees(decl); math.Abs(d-23.44) > 0.1 {
		t.Errorf("declination = %.3f, want about 23.44", d)
	}

	if math.Abs(eqTime-(-1.7)) > 0.5 {
		t.Errorf("equation of time = %.2f min, want about -1.7", eqTime)
	}
}
