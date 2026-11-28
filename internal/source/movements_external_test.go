package source_test

import (
	"math"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/source"
)

// TestMovements checks that only inbound and outbound tracks lost at or after
// the moment are counted, each in its own figure.
func TestMovements(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, time.November, 28, 14, 0, 0, 0, time.UTC)
	before := since.Add(-time.Second)
	after := since.Add(time.Minute)

	for _, testCase := range []struct {
		name           string
		trails         []source.Trail
		wantArrivals   int
		wantDepartures int
	}{
		{name: "none", trails: nil},
		{
			name: "each class once",
			trails: []source.Trail{
				{Class: source.TrackInbound, LostAt: after},
				{Class: source.TrackOutbound, LostAt: after},
				{Class: source.TrackTransit, LostAt: after},
			},
			wantArrivals: 1, wantDepartures: 1,
		},
		{
			name: "the moment itself counts and the second before does not",
			trails: []source.Trail{
				{Class: source.TrackInbound, LostAt: since},
				{Class: source.TrackInbound, LostAt: before},
				{Class: source.TrackOutbound, LostAt: before},
			},
			wantArrivals: 1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			arrivals, departures := source.Movements(testCase.trails, since)
			if arrivals != testCase.wantArrivals || departures != testCase.wantDepartures {
				t.Errorf("Movements() = (%d, %d), want (%d, %d)",
					arrivals, departures, testCase.wantArrivals, testCase.wantDepartures)
			}
		})
	}
}

// TestAirfieldDistanceToNm checks the distance on the field's flat frame: a
// minute of latitude is a nautical mile, and a minute of longitude is a
// nautical mile shortened by the cosine of the field's latitude.
func TestAirfieldDistanceToNm(t *testing.T) {
	t.Parallel()

	const (
		minute    = 1.0 / 60
		tolerance = 1e-9
	)

	field := source.Airfield{ICAO: "EHAM", Latitude: schipholLat, Longitude: schipholLon}

	if got := field.DistanceToNm(schipholLat+minute, schipholLon); math.Abs(got-1) > tolerance {
		t.Errorf("a minute north = %v nm, want 1", got)
	}

	want := math.Cos(schipholLat * math.Pi / 180)
	if got := field.DistanceToNm(schipholLat, schipholLon+minute); math.Abs(got-want) > tolerance {
		t.Errorf("a minute east = %v nm, want %v", got, want)
	}
}
