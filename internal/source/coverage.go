package source

import (
	"math"
	"sync"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/coverage"
)

// coverageInterval is how often the tracker is asked for a fresh Snapshot.
//
// A Snapshot value-copies the whole bin grid, a little over a kilobyte, under
// the tracker's own lock. The shape it describes is an accumulation over the
// whole run and moves over minutes rather than frames, so taking one per drawn
// frame would be a kilobyte of memmove thirty times a second for a picture
// that had not changed. One a second is faster than anyone can see it move.
const coverageInterval = time.Second

// coverageCache is the antenna's observed reception pattern and the throttle
// in front of it.
//
// Observations arrive on whichever goroutine decoded the fix, which for Live
// is the ingest's; the snapshot is read on the goroutine that draws. Both are
// safe: the tracker takes its own lock, and the cached copy sits behind mu.
type coverageCache struct {
	tracker *coverage.Tracker

	mu    sync.Mutex
	last  coverage.Snapshot
	taken time.Time
	have  bool
}

// newCoverage builds the cache around an empty tracker.
func newCoverage() *coverageCache {
	return &coverageCache{tracker: coverage.New()}
}

// observe folds one aircraft fix into the tracker, measured from the receiver.
//
// It mirrors uAirwaves' own positionObserver, with one guard ahead of it: an
// altitude at or below zero is what an undecoded altitude reports, not a real
// one on the ground, and folding it in would land the fix in the lowest band
// regardless of where the aircraft actually was.
//
// Past that, the guard is HaversineDistance's MaxFloat64 sentinel, which is
// what that function returns when either end is the undecoded (0, 0): a fix
// taken before the receiver knows where it is, or an aircraft whose position
// has not resolved yet, has no distance and no bearing worth binning. NaN is
// refused alongside it because Observe only tests for a negative distance,
// and NaN loses every comparison it is put through.
//
// It runs on the ingest goroutine, so it allocates nothing and takes one
// mutex.
func (c *coverageCache) observe(receiverLat, receiverLon, latitude, longitude, altitudeFt float64) {
	if altitudeFt <= 0 {
		return
	}

	distanceNm := airplanes.HaversineDistance(receiverLat, receiverLon, latitude, longitude)
	if distanceNm == math.MaxFloat64 || math.IsNaN(distanceNm) {
		return
	}

	c.tracker.Observe(distanceNm, bearingOf(receiverLat, receiverLon, latitude, longitude), altitudeFt)
}

// snapshot hands back the coverage state, taking a fresh copy at most once per
// coverageInterval and reusing the last one in between.
//
// The first call always takes one, so a frame drawn immediately after startup
// carries the bins that exist rather than an empty grid.
func (c *coverageCache) snapshot(now time.Time) coverage.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.have && now.Sub(c.taken) < coverageInterval {
		return c.last
	}

	c.last, c.taken, c.have = c.tracker.Snapshot(), now, true

	return c.last
}

// bearingOf is the compass bearing from the receiver to an aircraft, in
// degrees clockwise from north.
//
// It uses the same local equirectangular approximation internal/radar projects
// and draws bearings with, rather than uAirwaves' great-circle FlightBearing,
// for two reasons. That function lives in uAirwaves' internal/ui and cannot be
// imported at all, and over the few hundred nautical miles a coverage grid
// spans the two differ by a fraction of a degree, far inside the 22.5 degrees
// one bearing sector covers. Agreeing with the picture is worth more than the
// fraction.
func bearingOf(receiverLat, receiverLon, latitude, longitude float64) float64 {
	eastNm := (longitude - receiverLon) * nmPerDegree * math.Cos(receiverLat*math.Pi/halfCircle)
	northNm := (latitude - receiverLat) * nmPerDegree

	bearing := math.Atan2(eastNm, northNm) * halfCircle / math.Pi
	if bearing < 0 {
		bearing += degreesPerCircle
	}

	return bearing
}
