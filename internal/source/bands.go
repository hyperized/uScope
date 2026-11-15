package source

import "math"

// The altitude bands.
//
// They are the radar's bands, and internal/radar reads them from here through
// AltitudeBandOf rather than keeping its own. The f key hides an aircraft by
// the band it is in and the corridor exposure hides a stretch of track by the
// band it was flown in, and two copies of the thresholds would be two chances
// for the scope to hide an aircraft while leaving its track on the ground.
const (
	// BandLow is everything under LowCeilingFt.
	BandLow = 0

	// BandMid runs from LowCeilingFt up to MidCeilingFt.
	BandMid = 1

	// BandHigh is MidCeilingFt and above.
	BandHigh = 2

	// BandCount is how many bands there are.
	BandCount = 3

	// LowCeilingFt is where the low band ends, in feet.
	LowCeilingFt = 10000.0

	// MidCeilingFt is where the middle band ends and the high one starts.
	MidCeilingFt = 25000.0
)

// AltitudeBandOf is the band an altitude in feet falls in, and false for an
// altitude that is no band at all.
//
// Zero means nobody has decoded an altitude yet rather than sea level: a
// uAirwaves Snapshot starts at zero and stays there until an altitude message
// arrives. NaN and anything negative are refused with it.
func AltitudeBandOf(altitudeFt float64) (int, bool) {
	switch {
	case altitudeFt <= 0 || math.IsNaN(altitudeFt):
		return 0, false
	case altitudeFt < LowCeilingFt:
		return BandLow, true
	case altitudeFt < MidCeilingFt:
		return BandMid, true
	default:
		return BandHigh, true
	}
}
