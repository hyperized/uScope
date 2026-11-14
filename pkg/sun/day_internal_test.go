package sun

import (
	"testing"
	"time"
)

// Elevation is geometric plus refraction and nothing else, so the rise
// definition and the theme's horizon are measured on the same sun.
func TestGeometricIsElevationWithoutRefraction(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.June, 21, 4, 0, 0, 0, time.UTC)
	geo := geometric(52.3, 4.8, at)

	if got, want := Elevation(52.3, 4.8, at), geo+refraction(geo); got != want {
		t.Errorf("Elevation = %v, want %v", got, want)
	}
}

// bisect returns the far end, so a rise is never reported a moment before the
// limb has cleared the horizon.
func TestBisectReturnsTheFarSide(t *testing.T) {
	t.Parallel()

	const lat, lon = 52.3105, 4.7683

	below := time.Date(2026, time.June, 21, 3, 10, 0, 0, time.UTC)
	above := below.Add(scanStep)

	if up(lat, lon, below) || !up(lat, lon, above) {
		t.Fatal("bracket does not straddle sunrise")
	}

	got := bisect(lat, lon, below, above, rising)
	if !up(lat, lon, got) {
		t.Errorf("bisect = %v, which is still below the horizon", got)
	}

	if span := above.Sub(below) >> bisectSteps; up(lat, lon, got.Add(-2*span)) {
		t.Errorf("bisect = %v, more than one step past the crossing", got)
	}
}
