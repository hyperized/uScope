package source

import (
	"math"
	"sync"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uAirwaves/pkg/coverage"
)

// coverageInterval is how often a fresh copy of the coverage state is taken.
//
// A refresh value-copies the tracker's Snapshot, a little over a kilobyte,
// and uScope's own CoverageGrid, sixteen. The shape they describe is an
// accumulation over the whole run and moves over minutes rather than frames,
// so refreshing per drawn frame would be seventeen kilobytes of memmove
// thirty times a second for a picture that had not changed. One a second is
// faster than anyone can see it move.
const coverageInterval = time.Second

// sectorWidthDeg is the compass width of one bearing sector. The count comes
// from uAirwaves; the arithmetic is here because the tracker keeps its own
// copy unexported.
const sectorWidthDeg = degreesPerCircle / coverage.BearingSectorCount

// CoverageGrid is where the antenna has heard an aircraft, counted by bearing
// sector, then altitude band, then distance bin.
//
// uAirwaves' own tracker keeps two projections of this instead: Cells is
// altitude by distance over every bearing, and Sectors is one farthest
// distance per bearing over every altitude. Either one alone is a shape the
// other contradicts, and the two together cannot describe an antenna that
// hears one sector far at low level and not at high, which is exactly what a
// chimney or a neighbouring roof does to a real one. That needs the third
// axis, so uScope counts it here and leaves the tracker to the range figures
// it is good at.
//
// The bins are uAirwaves' own: sixteen bearing sectors of 22.5 degrees, ten
// altitude bands of 5,000 feet, twenty-five distance bins of 10 nautical
// miles. Anything at or past 250 nm, 50,000 ft or 360 degrees clamps into the
// last bin of its axis, and a count saturates at math.MaxUint32 rather than
// wrapping to nothing on a session left running for a month.
//
// Cells is a value array, so copying a CoverageGrid deep-copies the counts:
// a grid handed out on a Frame cannot be changed under the scene by the
// ingest goroutine. It comes to 16,000 bytes, which is why a fresh copy is
// taken once a second rather than once a frame.
type CoverageGrid struct {
	Cells [coverage.BearingSectorCount][coverage.AltitudeBandCount][coverage.DistanceBinCount]uint32
}

// observe folds one already-validated fix into the grid. The caller holds the
// lock and has done the guarding; this is the indexing and nothing else.
func (g *CoverageGrid) observe(distanceNm, bearingDeg, altitudeFt float64) {
	saturatingInc(&g.Cells[bearingSector(bearingDeg)][altitudeBand(altitudeFt)][distanceBin(distanceNm)])
}

// coverageCache is the antenna's observed reception pattern and the throttle
// in front of it.
//
// Observations arrive on whichever goroutine decoded the fix, which for Live
// is the ingest's; the copies are read on the goroutine that draws. mu guards
// the grid and the two cached copies, and it is also held across the tracker
// calls. That last part is not redundancy: the tracker has a lock of its own,
// and snapshot has to hold mu while it asks for a Snapshot, so observe takes
// mu before the tracker as well. Both paths lock in the same order and
// neither can wait on the other.
type coverageCache struct {
	tracker *coverage.Tracker

	mu sync.Mutex

	// grid is the live count, written by observe and never handed out.
	grid CoverageGrid

	// last and lastGrid are what a frame gets between two refreshes.
	last     coverage.Snapshot
	lastGrid CoverageGrid
	taken    time.Time
	have     bool
}

// newCoverage builds the cache around an empty tracker and an empty grid.
func newCoverage() *coverageCache {
	return &coverageCache{tracker: coverage.New()}
}

// observe folds one aircraft fix into the tracker and the grid, measured from
// the receiver.
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

	bearingDeg := bearingOf(receiverLat, receiverLon, latitude, longitude)

	c.mu.Lock()
	c.tracker.Observe(distanceNm, bearingDeg, altitudeFt)
	c.grid.observe(distanceNm, bearingDeg, altitudeFt)
	c.mu.Unlock()
}

// snapshot hands back the coverage state, taking a fresh copy at most once
// per coverageInterval and reusing the last one in between.
//
// The first call always takes one, so a frame drawn immediately after startup
// carries the bins that exist rather than an empty grid.
func (c *coverageCache) snapshot(now time.Time) (coverage.Snapshot, CoverageGrid) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.have && now.Sub(c.taken) < coverageInterval {
		return c.last, c.lastGrid
	}

	c.last, c.lastGrid = c.tracker.Snapshot(), c.grid
	c.taken, c.have = now, true

	return c.last, c.lastGrid
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

// bearingSector maps a bearing in [0,360) to its sector index.
//
// bearingOf never returns a negative, so there is no wrap to do, but it can
// return 360 exactly: a bearing a hair under zero rounds up to it when the
// circle is added. That is the one input the clamp is here for.
func bearingSector(bearingDeg float64) int {
	sector := int(bearingDeg / sectorWidthDeg)
	if sector > coverage.BearingSectorCount-1 {
		return coverage.BearingSectorCount - 1
	}

	return sector
}

// altitudeBand maps a barometric altitude in feet to its band index, clamping
// anything at or above the top of the grid into the last band.
func altitudeBand(altitudeFt float64) int {
	band := int(altitudeFt / coverage.AltitudeBandFt)
	if band > coverage.AltitudeBandCount-1 {
		return coverage.AltitudeBandCount - 1
	}

	return band
}

// distanceBin maps a ground distance in nautical miles to its bin index,
// clamping anything at or beyond the outer edge of the grid into the last bin.
func distanceBin(distanceNm float64) int {
	bin := int(distanceNm / coverage.DistanceBinNm)
	if bin > coverage.DistanceBinCount-1 {
		return coverage.DistanceBinCount - 1
	}

	return bin
}

// saturatingInc counts one more observation, stopping at math.MaxUint32.
//
// A cell that has saturated has heard four billion fixes and is as far inside
// the envelope as a cell can be, so wrapping it to zero would erase the best
// evidence in the grid.
func saturatingInc(counter *uint32) {
	if *counter < math.MaxUint32 {
		*counter++
	}
}
