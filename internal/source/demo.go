package source

import (
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/hyperized/uAirwaves/pkg/adsb"
	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
)

// The demo fleet's shape.
const (
	// demoLabel is what the header shows instead of a source, so nobody
	// mistakes invented aircraft for decoded ones.
	demoLabel = "DEMO"

	// Schiphol, which is where this was developed and where a scope full of
	// traffic is the normal state of the world.
	demoLatitude  = 52.3105
	demoLongitude = 4.7683

	// defaultSeed makes --demo produce the same fleet every run. A demo that
	// looks different each time is no use for comparing two builds.
	defaultSeed = 20260915

	// demoHistoryInterval matches uAirwaves' own position-history spacing, so
	// a demo trail has the same shape and the same gaps as a real one.
	demoHistoryInterval = 10 * time.Second

	// demoMaxHistory caps the trail. At one fix per demoHistoryInterval that
	// is forty minutes of track, which is already far past the edge of any
	// scope range.
	demoMaxHistory = 240

	// demoSeedFixes is how much history each aircraft starts with, back
	// propagated along its track. Without it the first frame has no trails at
	// all, which is exactly the frame --png captures.
	demoSeedFixes = 18

	// demoMaxStep caps how much simulated time one frame may advance. A
	// process stopped at a breakpoint for a minute should not teleport the
	// whole fleet off the scope when it resumes.
	demoMaxStep = 5 * time.Second

	// nmPerDegree is one degree of latitude in nautical miles, which is the
	// definition of the unit.
	nmPerDegree = 60.0

	// minCosLatitude stops the longitude step blowing up at the poles. At a
	// latitude where the cosine is this small, a nautical mile of easting is
	// most of a degree, and the demo has no business up there anyway.
	minCosLatitude = 1e-6

	// jitterBearing and jitterDistance are how far the seed may move an
	// aircraft from its table position, in degrees and as a fraction.
	jitterBearing  = 5.0
	jitterDistance = 0.1

	degreesPerCircle = 360.0
	halfCircle       = 180.0
)

// craftSpec is one row of the invented fleet.
//
// bearing and distanceNm place the aircraft relative to the receiver at the
// first frame; track is the course it then flies in a straight line. An
// aircraft with velocity zero stays where it is.
type craftSpec struct {
	icao      string
	callsign  string
	squawk    string
	altitude  float64
	velocity  float64
	track     float64
	vertRate  float64
	bearing   float64
	distance  float64
	emergency bool
}

// demoFleet is the twelve aircraft.
//
// Two of them are deliberately awkward. LFV21 has no velocity and a track of
// zero, which is how an aircraft whose velocity message has not arrived yet
// looks in a Snapshot, and the scope draws it as a bare circle rather than a
// silhouette pointing north. 4951BA has no callsign, so the card and the rows
// have to fall back to the ICAO hex.
//
// The altitudes cover all three bands on purpose, so the legend has something
// to explain on every frame.
//
//nolint:mnd,gochecknoglobals // a fleet is data; naming twelve altitudes would not make it clearer.
var demoFleet = [...]craftSpec{
	{
		icao: "484AC1", callsign: "KLM123", squawk: "1000",
		altitude: 2400, velocity: 190, track: 41, vertRate: 1800,
		bearing: 205, distance: 6,
	},
	{
		icao: "4CA2D3", callsign: "RYR7X", squawk: "2201",
		altitude: 8500, velocity: 280, track: 123, vertRate: -1200,
		bearing: 95, distance: 14,
	},
	{
		icao: "3C6745", callsign: "DLH4EA", squawk: "5417",
		altitude: 36000, velocity: 452, track: 268,
		bearing: 320, distance: 31,
	},
	{
		icao: "4BAA9E", callsign: "THY12M", squawk: "3623",
		altitude: 24000, velocity: 410, track: 195, vertRate: -900,
		bearing: 30, distance: 22,
	},
	{
		icao: "471F2C", callsign: "EZY45AB", squawk: "1347",
		altitude: 11500, velocity: 300, track: 87, vertRate: 2100,
		bearing: 250, distance: 9,
	},
	{
		icao: "4CA8B7", callsign: "AFR88DK", squawk: "6142",
		altitude: 39000, velocity: 470, track: 312,
		bearing: 140, distance: 38,
	},
	{
		icao: "4400F1", callsign: "BAW7GT", squawk: "4571",
		altitude: 18000, velocity: 380, track: 156, vertRate: -1500,
		bearing: 15, distance: 17,
	},
	{
		icao: "A4B2E9", callsign: "UAL931", squawk: "7002",
		altitude: 33000, velocity: 465, track: 245,
		bearing: 285, distance: 27,
	},
	{
		icao: "3949CE", callsign: "TRA6157", squawk: "2764",
		altitude: 4800, velocity: 230, track: 358, vertRate: 1500,
		bearing: 175, distance: 11,
	},
	{
		icao: "4951BA", squawk: "1553",
		altitude: 27500, velocity: 430, track: 74,
		bearing: 350, distance: 25,
	},
	{
		icao: "484B0D", callsign: "LFV21", squawk: "7600",
		altitude: 1200, velocity: 0, track: 0,
		bearing: 60, distance: 4, emergency: true,
	},
	{
		icao: "4CAFE2", callsign: "EIN6DW", squawk: "3311",
		altitude: 15000, velocity: 340, track: 211, vertRate: -800,
		bearing: 110, distance: 19,
	},
}

// craft is one aircraft in flight: its fixed identity plus where it is now
// and where it has been.
type craft struct {
	spec      craftSpec
	latitude  float64
	longitude float64
	history   []airplane.PositionEntry
	lastFix   time.Time
	messages  int64
}

// Demo is a synthetic source: twelve invented aircraft on straight tracks
// around a fixed receiver.
//
// It exists so the radar can be built and looked at on a machine with no
// receiver in it, and so the render path has something deterministic to draw
// in a test. Nothing in it touches a radio, a socket or a file.
//
// Demo is safe for concurrent use, but the Planes in a Frame alias its own
// buffers and are only valid until the next Frame call. That is what keeps it
// free of per-frame allocations.
type Demo struct {
	mu     sync.Mutex
	fleet  []craft
	list   airplanes.List
	ticks  uint64
	last   time.Time
	now    func() time.Time
	seed   uint64
	lat    float64
	lon    float64
	manual bool
}

// DemoOption configures a Demo at construction.
type DemoOption func(*Demo)

// WithDemoLocation moves the invented receiver. Without it the fleet flies
// around Schiphol.
func WithDemoLocation(latitude, longitude float64) DemoOption {
	return func(d *Demo) {
		d.lat, d.lon = latitude, longitude
		d.manual = true
	}
}

// WithDemoClock replaces the clock the fleet is advanced by, which is what
// lets a test fly an hour of traffic without waiting for one. A nil function
// leaves time.Now in place.
func WithDemoClock(now func() time.Time) DemoOption {
	return func(d *Demo) {
		if now != nil {
			d.now = now
		}
	}
}

// WithSeed changes how the fleet is scattered around the receiver. The seed
// only moves the starting positions; the tracks, altitudes and callsigns come
// from the table and never vary.
func WithSeed(seed uint64) DemoOption {
	return func(d *Demo) { d.seed = seed }
}

// NewDemo builds the fleet and back-fills every trail, so the first frame
// already has something on it.
func NewDemo(opts ...DemoOption) (*Demo, error) {
	demo := &Demo{
		now:  time.Now,
		seed: defaultSeed,
		lat:  demoLatitude,
		lon:  demoLongitude,
	}

	for _, opt := range opts {
		opt(demo)
	}

	if err := demo.validate(); err != nil {
		return nil, err
	}

	demo.fleet = demo.build()
	demo.list = make(airplanes.List, 0, len(demo.fleet))

	return demo, nil
}

// Frame advances the fleet to the current clock and takes a snapshot.
func (d *Demo) Frame() Frame {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now()
	d.advance(now)
	d.ticks++

	return Frame{
		Planes:   d.snapshot(now),
		Receiver: Receiver{Latitude: d.lat, Longitude: d.lon, HasFix: true, Label: LabelManual},
		Source:   adsb.SourceInfo{Label: demoLabel, Connected: true, BytesIn: d.ticks},
		Stats:    d.stats(),
		Now:      now,
	}
}

// Close satisfies Source. There is nothing to release: the demo owns no
// goroutine, no file and no socket.
func (*Demo) Close() error { return nil }

// validate range-checks an operator-supplied receiver position.
func (d *Demo) validate() error {
	if !d.manual {
		return nil
	}

	if d.lat < minLatitude || d.lat > maxLatitude {
		return fmt.Errorf("%w: latitude %g is outside %g to %g", ErrCoordinate, d.lat, minLatitude, maxLatitude)
	}

	if d.lon < minLongitude || d.lon > maxLongitude {
		return fmt.Errorf("%w: longitude %g is outside %g to %g", ErrCoordinate, d.lon, minLongitude, maxLongitude)
	}

	return nil
}

// build places every aircraft and back-fills its trail.
func (d *Demo) build() []craft {
	rng := rand.New(rand.NewPCG(d.seed, d.seed^math.MaxUint32)) //nolint:gosec // a demo fleet, not a key.
	fleet := make([]craft, 0, len(demoFleet))

	for _, spec := range demoFleet {
		bearing := spec.bearing + (rng.Float64()*2-1)*jitterBearing
		distance := spec.distance * (1 + (rng.Float64()*2-1)*jitterDistance)

		latitude, longitude := offset(d.lat, d.lon, bearing, distance)
		fleet = append(fleet, newCraft(spec, latitude, longitude))
	}

	return fleet
}

// advance flies every aircraft forward by the time since the last frame.
func (d *Demo) advance(now time.Time) {
	if d.last.IsZero() {
		d.last = now
		d.seedFixTimes(now)

		return
	}

	elapsed := now.Sub(d.last)
	if elapsed <= 0 {
		return
	}

	d.last = now

	if elapsed > demoMaxStep {
		elapsed = demoMaxStep
	}

	for index := range d.fleet {
		d.fleet[index].fly(elapsed, now)
	}
}

// seedFixTimes backdates every trail so the first real fix lands one interval
// after the fleet starts, rather than immediately on the second frame.
func (d *Demo) seedFixTimes(now time.Time) {
	for index := range d.fleet {
		d.fleet[index].lastFix = now
	}
}

// snapshot fills the reused list and sorts it the way airplanes.Sorted does,
// so the radar cannot tell the two sources apart by their ordering.
func (d *Demo) snapshot(now time.Time) airplanes.List {
	d.list = d.list[:0]

	for index := range d.fleet {
		d.list = append(d.list, d.fleet[index].snapshot(now))
	}

	receiverLat, receiverLon := d.lat, d.lon

	slices.SortFunc(d.list, func(left, right airplane.Snapshot) int {
		distLeft := airplanes.HaversineDistance(receiverLat, receiverLon, left.Latitude, left.Longitude)
		distRight := airplanes.HaversineDistance(receiverLat, receiverLon, right.Latitude, right.Longitude)

		if order := cmp.Compare(distLeft, distRight); order != 0 {
			return order
		}

		return cmp.Compare(left.ICAO, right.ICAO)
	})

	return d.list
}

// stats reports the tick count in the shape the header expects, so the stats
// line has something to move.
func (d *Demo) stats() adsb.Stats {
	named := uint64(0)

	for index := range d.fleet {
		if d.fleet[index].spec.callsign != "" {
			named++
		}
	}

	return adsb.Stats{
		TotalFrames:      d.ticks,
		RecoveredFrames:  0,
		CallsignsDecoded: named,
		CallsignsApplied: named,
	}
}

// newCraft places one aircraft and walks its trail backwards along its own
// track, so a fresh fleet looks like one that has been flying for a while.
func newCraft(spec craftSpec, latitude, longitude float64) craft {
	one := craft{
		spec:      spec,
		latitude:  latitude,
		longitude: longitude,
		history:   make([]airplane.PositionEntry, demoSeedFixes, demoMaxHistory),
		messages:  int64(demoSeedFixes),
	}

	stepNm := spec.velocity * demoHistoryInterval.Hours()
	back := reciprocal(spec.track)

	// Fill from the end so the newest fix lands last, which is the order the
	// trail is drawn in.
	fixLat, fixLon := latitude, longitude

	for index := demoSeedFixes - 1; index >= 0; index-- {
		one.history[index] = airplane.PositionEntry{
			Latitude:  fixLat,
			Longitude: fixLon,
			Altitude:  spec.altitude,
		}
		fixLat, fixLon = offset(fixLat, fixLon, back, stepNm)
	}

	return one
}

// reciprocal is the opposite course.
func reciprocal(track float64) float64 {
	return math.Mod(track+halfCircle, degreesPerCircle)
}

// offset walks distanceNm along a compass bearing from a position, on a local
// flat-earth approximation.
//
// Good enough over the tens of nautical miles a scope covers, and the radar
// projects with the same approximation, so the demo and the drawing agree.
func offset(latitude, longitude, bearing, distanceNm float64) (float64, float64) {
	radians := bearing * math.Pi / halfCircle
	lat := latitude + distanceNm*math.Cos(radians)/nmPerDegree

	cosLat := math.Cos(lat * math.Pi / halfCircle)
	if math.Abs(cosLat) < minCosLatitude {
		return clampLatitude(lat), wrapLongitude(longitude)
	}

	lon := longitude + distanceNm*math.Sin(radians)/(nmPerDegree*cosLat)

	return clampLatitude(lat), wrapLongitude(lon)
}

// clampLatitude pins a latitude to the range that exists.
func clampLatitude(latitude float64) float64 {
	return min(max(latitude, minLatitude), maxLatitude)
}

// wrapLongitude brings a longitude back into range the way the meridian does,
// so an aircraft flying east past 180 comes out at -180 rather than off the
// end of the world.
func wrapLongitude(longitude float64) float64 {
	wrapped := math.Mod(longitude+halfCircle, degreesPerCircle)
	if wrapped < 0 {
		wrapped += degreesPerCircle
	}

	return wrapped - halfCircle
}

// fly moves one aircraft along its track and records a fix when the interval
// is up.
func (c *craft) fly(elapsed time.Duration, now time.Time) {
	c.messages++

	if c.spec.velocity > 0 {
		c.latitude, c.longitude = offset(
			c.latitude, c.longitude, c.spec.track, c.spec.velocity*elapsed.Hours())
	}

	if now.Sub(c.lastFix) < demoHistoryInterval {
		return
	}

	c.lastFix = now
	c.pushFix()
}

// pushFix appends the current position to the trail, dropping the oldest fix
// once the cap is reached. The slice keeps its capacity, so the trail costs
// one allocation per aircraft for the life of the process.
func (c *craft) pushFix() {
	entry := airplane.PositionEntry{
		Latitude:  c.latitude,
		Longitude: c.longitude,
		Altitude:  c.spec.altitude,
	}

	if len(c.history) < demoMaxHistory {
		c.history = append(c.history, entry)

		return
	}

	copy(c.history, c.history[1:])
	c.history[len(c.history)-1] = entry
}

// snapshot copies one aircraft into the shape uAirwaves hands out. The trail
// is aliased rather than copied, which is the allocation this whole source
// exists to avoid.
func (c *craft) snapshot(now time.Time) airplane.Snapshot {
	return airplane.Snapshot{
		ICAO:            c.spec.icao,
		Callsign:        c.spec.callsign,
		Altitude:        c.spec.altitude,
		Heading:         c.spec.track,
		Velocity:        c.spec.velocity,
		VertRate:        c.spec.vertRate,
		Latitude:        c.latitude,
		Longitude:       c.longitude,
		LastUpdate:      now,
		Squawk:          c.spec.squawk,
		Emergency:       c.spec.emergency,
		MessageCount:    c.messages,
		PositionHistory: c.history,
	}
}
