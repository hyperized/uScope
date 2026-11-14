// Package sun computes the sun's elevation at a place and time, so a caller
// can pick a day or night palette without any network or ephemeris data.
package sun

import (
	"math"
	"time"
)

// Sunset is the elevation at which the day palette gives way to the night one:
// the apparent horizon. Elevation already includes refraction, so 0 here is
// the sun's upper limb at the horizon, which is what sunrise and sunset mean.
const Sunset = 0.0

// CivilLimit, NauticalLimit and AstronomicalLimit are the lowest elevations
// of the three twilights: the sun six, twelve and eighteen degrees below the
// horizon. Below AstronomicalLimit it is night. The tilted views' sky keys one
// of its colour keyframes on CivilLimit; none of the three is the line between
// the day and night palettes, which is Sunset.
const (
	CivilLimit        = -6.0
	NauticalLimit     = -12.0
	AstronomicalLimit = -18.0
)

const (
	unixEpochJD      = 2440587.5 // Julian day of 1970-01-01T00:00:00Z
	j2000JD          = 2451545.0 // Julian day of 2000-01-01T12:00:00Z
	secondsPerDay    = 86400.0
	daysPerCentury   = 36525.0
	minutesPerDay    = 1440.0
	degPerMinute     = 4.0 // the earth turns one degree every four minutes
	fullCircle       = 360.0
	halfCircle       = 180.0
	maxLatitude      = 90.0
	arcSecPerDeg     = 3600.0
	nanosPerSecond   = 1e9
	secondsPerMinute = 60.0
)

// Elevation returns the sun's apparent elevation above the horizon in degrees
// at the given position and instant, negative below the horizon. Latitude is
// degrees north, longitude degrees east. Accuracy is within about 0.5 degree,
// which is what a day/night decision needs.
func Elevation(lat, lon float64, at time.Time) float64 {
	elevation := geometric(lat, lon, at)

	return elevation + refraction(elevation)
}

// geometric is the sun's centre above the horizon in degrees with no
// refraction: where the sun is rather than where it is seen.
func geometric(lat, lon float64, at time.Time) float64 {
	lat = math.Max(-maxLatitude, math.Min(maxLatitude, lat))
	lon = wrapLongitude(lon)

	utc := at.UTC()
	jd := julianDay(utc)
	t := (jd - j2000JD) / daysPerCentury

	decl, eqTime := solarCoordinates(t)

	// Longitude is folded into the clock so the hour angle is 0 at local solar noon.
	minutes := float64(utc.Hour()*60+utc.Minute()) + float64(utc.Second())/secondsPerMinute +
		float64(utc.Nanosecond())/(nanosPerSecond*secondsPerMinute)
	solarTime := math.Mod(minutes+eqTime+degPerMinute*lon, minutesPerDay)
	hourAngle := solarTime/degPerMinute - halfCircle

	latR := radians(lat)
	cosZenith := math.Sin(latR)*math.Sin(decl) + math.Cos(latR)*math.Cos(decl)*math.Cos(radians(hourAngle))
	// Rounding can push the value a hair past 1, which would make Acos return NaN.
	cosZenith = math.Max(-1, math.Min(1, cosZenith))

	return halfCircle/2 - degrees(math.Acos(cosZenith))
}

// IsDay reports whether the sun is above the horizon (Sunset) at the position
// and instant.
func IsDay(lat, lon float64, at time.Time) bool {
	return Elevation(lat, lon, at) > Sunset
}

// wrapLongitude folds any longitude into [-180, 180) so callers that
// accumulate or offset a longitude do not get an error for a valid place.
func wrapLongitude(lon float64) float64 {
	wrapped := math.Mod(lon+halfCircle, fullCircle)
	if wrapped < 0 {
		wrapped += fullCircle
	}

	return wrapped - halfCircle
}

// julianDay converts an instant to a Julian day number, keeping sub-second
// precision so the result does not stair-step at the scale of a second.
func julianDay(t time.Time) float64 {
	return float64(t.Unix())/secondsPerDay + float64(t.Nanosecond())/(secondsPerDay*nanosPerSecond) + unixEpochJD
}

// solarCoordinates returns the declination in radians and the equation of
// time in minutes for the given Julian centuries since J2000.
//
//nolint:mnd // published NOAA series coefficients
func solarCoordinates(centuries float64) (float64, float64) {
	meanLong := math.Mod(280.46646+centuries*(36000.76983+centuries*0.0003032), fullCircle)
	meanAnom := 357.52911 + centuries*(35999.05029-0.0001537*centuries)
	eccentricity := 0.016708634 - centuries*(0.000042037+0.0000001267*centuries)

	anomR := radians(meanAnom)
	centre := math.Sin(anomR)*(1.914602-centuries*(0.004817+0.000014*centuries)) +
		math.Sin(2*anomR)*(0.019993-0.000101*centuries) +
		math.Sin(3*anomR)*0.000289

	trueLong := meanLong + centre
	omega := radians(125.04 - 1934.136*centuries)
	apparentLong := radians(trueLong - 0.00569 - 0.00478*math.Sin(omega))

	meanObliq := 23 + (26+(21.448-centuries*(46.815+centuries*(0.00059-centuries*0.001813)))/60)/60
	obliq := radians(meanObliq + 0.00256*math.Cos(omega))

	decl := math.Asin(math.Sin(obliq) * math.Sin(apparentLong))

	y := math.Pow(math.Tan(obliq/2), 2)
	meanLongR := radians(meanLong)
	eqTime := degPerMinute * degrees(y*math.Sin(2*meanLongR)-
		2*eccentricity*math.Sin(anomR)+
		4*eccentricity*y*math.Sin(anomR)*math.Cos(2*meanLongR)-
		0.5*y*y*math.Sin(4*meanLongR)-
		1.25*eccentricity*eccentricity*math.Sin(2*anomR))

	return decl, eqTime
}

// refraction returns the atmospheric refraction in degrees for a geometric
// elevation, using NOAA's piecewise fit. The fit is tuned for standard
// pressure and temperature, which is fine at this accuracy.
//
//nolint:mnd // published NOAA fit coefficients
func refraction(elevation float64) float64 {
	var arcSeconds float64

	switch {
	case elevation > 85: //nolint:mnd // above 85 degrees refraction is negligible
		return 0
	case elevation > 5: //nolint:mnd // NOAA fit boundary
		tanE := math.Tan(radians(elevation))
		tan2 := tanE * tanE
		tan3 := tan2 * tanE
		arcSeconds = 58.1/tanE - 0.07/tan3 + 0.000086/(tan3*tan2)
	case elevation > -0.575:
		e := elevation
		arcSeconds = 1735 + e*(-518.2+e*(103.4+e*(-12.79+e*0.711)))
	default:
		// The 1/tan form shrinks toward zero as the sun sinks, so it stays bounded.
		arcSeconds = -20.772 / math.Tan(radians(elevation))
	}

	return arcSeconds / arcSecPerDeg
}

func radians(deg float64) float64 { return deg * math.Pi / halfCircle }

func degrees(rad float64) float64 { return rad * halfCircle / math.Pi }
