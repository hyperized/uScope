package radar

import (
	"errors"
	"fmt"
	"math"
)

// The bounds of --azimuth, in degrees clockwise from north. The upper bound is
// open: 360 is north again, and a compass that accepts both spellings of one
// direction gives a value two ways to be compared and one of them to be wrong.
const (
	MinAzimuth = 0.0
	MaxAzimuth = 360.0
)

// Errors for the camera and range knobs the command line takes. They live
// here, beside the bounds they enforce, so the flag parser refuses a value
// with the same words whichever package asks.
var (
	ErrZoom       = errors.New("radar: zoom out of range")
	ErrTilt       = errors.New("radar: tilt out of range")
	ErrExaggerate = errors.New("radar: exaggeration out of range")
	ErrAzimuth    = errors.New("radar: azimuth out of range")
)

// CheckZoom refuses a zoom outside MinZoom to MaxZoom.
func CheckZoom(zoom int) error {
	if zoom < MinZoom || zoom > MaxZoom {
		return fmt.Errorf("%w: got %d, want %d to %d", ErrZoom, zoom, MinZoom, MaxZoom)
	}

	return nil
}

// CheckTilt refuses an elevation outside MinTilt to MaxTilt, and NaN.
func CheckTilt(degrees float64) error {
	return within(degrees, MinTilt, MaxTilt, ErrTilt)
}

// CheckExaggerate refuses a stretch outside MinExaggerate to MaxExaggerate,
// and NaN.
func CheckExaggerate(factor float64) error {
	return within(factor, MinExaggerate, MaxExaggerate, ErrExaggerate)
}

// CheckAzimuth refuses an azimuth that is not in [MinAzimuth, MaxAzimuth), and
// NaN.
func CheckAzimuth(degrees float64) error {
	if math.IsNaN(degrees) || degrees < MinAzimuth || degrees >= MaxAzimuth {
		return fmt.Errorf("%w: got %g, want %g up to but not including %g",
			ErrAzimuth, degrees, MinAzimuth, MaxAzimuth)
	}

	return nil
}

// within is the closed-interval check the tilt and the stretch share. NaN fails
// both comparisons, so it is named rather than left to fall through them.
func within(value, low, high float64, sentinel error) error {
	if math.IsNaN(value) || value < low || value > high {
		return fmt.Errorf("%w: got %g, want %g to %g", sentinel, value, low, high)
	}

	return nil
}
