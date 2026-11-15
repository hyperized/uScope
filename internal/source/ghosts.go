package source

import (
	"slices"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
)

// The ghost ring's two caps.
//
// A ghost is kept for as long as the program runs, so something has to bound
// it or a receiver left on overnight would still be holding every aircraft
// that passed at breakfast.
const (
	// maxGhostTrails is how many lost trails are kept at once. Two thousand is
	// more traffic than a day over the Randstad, which is longer than anybody
	// leaves a scope on one range.
	maxGhostTrails = 2000

	// maxGhostPoints is how many fixes those trails may hold between them. At
	// 24 bytes a fix that is 24 MB, which is the number the cap was picked
	// from: it is the most a handheld should spend on tracks nobody is flying
	// any more.
	//
	// uAirwaves keeps 256 fixes per aircraft, so two thousand full trails come
	// to 512,000 points and the trail cap is what bites first on a real feed.
	// This one is here for a source with a longer history than that.
	maxGhostPoints = 1_000_000
)

// How a revived aircraft's old track is carried on its new one.
const (
	// stitchFixes is the most fixes a stitched history holds, which is
	// uAirwaves' own cap on one aircraft's history (maxPositionHistory in
	// pkg/airplane, unexported there). The prefix gives up its oldest fixes
	// as the new life grows, so a stitched track is never longer than one
	// the store could have kept unbroken.
	stitchFixes = 256

	// stitchFor is how long after the loss a prefix is still worth carrying:
	// 256 fixes at uAirwaves' one every ten seconds, the span the longest
	// unbroken history covers. A prefix older than that would draw a line
	// across a gap longer than any trail on screen.
	stitchFor = stitchFixes * 10 * time.Second
)

// Trail is a track with no aircraft on the end of it.
//
// It is what a source keeps when a contact is lost: the last position history
// the aircraft showed, plus the two things the radar needs to colour it by.
//
// Latitude, Longitude, Heading and VertRate are where the aircraft was and
// what it was doing on the last frame it was heard, which is what the radar
// holds a coasting aircraft at for its first minute as a ghost. A zero
// position reads as nothing to draw a head at.
//
// LostAt is the frame time the trail was given up on, which is what the
// radar measures the coast from.
//
// Points aliases the slice the source last saw rather than copying it, and
// that is safe for both sources for opposite reasons. Live takes a fresh deep
// copy of every history out of airplanes.Sorted on every frame, so the last
// one it took is nobody else's to write to. Demo stops flying an aircraft the
// moment it goes quiet, so the buffer under its trail is frozen.
type Trail struct {
	ICAO     string
	Callsign string
	Altitude float64
	Points   []airplane.PositionEntry
	LostAt   time.Time

	Latitude  float64
	Longitude float64
	Heading   float64
	VertRate  float64
}

// liveTrail is what one aircraft still in the sky last showed us, and the
// frame it showed it on.
//
// The generation is what turns "which aircraft are gone" into a comparison
// rather than a set difference: everything in the frame is stamped with the
// current one, and whatever still carries the last one has stopped
// transmitting.
type liveTrail struct {
	trail Trail
	gen   uint64
}

// ghosts remembers the trails of aircraft that have gone quiet.
//
// Both sources track them on every run, whether or not the radar is in the
// mode that draws them. It used to be tied to a flag, which meant a session
// that reached for the trail mode an hour in found the last hour empty: the
// tracking has to have been running before the question is asked, or the
// answer is always no. The ring below is what bounds the cost of leaving it
// on.
//
// The ring is a ring rather than a slice with its front cut off because
// eviction is by age and revival is by ICAO, and both have to be cheap.
// A slot number is an absolute push counter modulo the ring's length, so
// a trail whose aircraft comes back is emptied where it lies instead of
// being shuffled out, and the trails behind it keep the age they arrived at.
//
// ghosts is not safe for concurrent use. Each source holds one and calls into
// it under its own lock.
type ghosts struct {
	// live is what each aircraft in the sky last showed, and gen the frame
	// counter its entries are stamped with.
	live map[string]liveTrail
	gen  uint64

	// ring holds the lost trails, oldest at evicted and newest at pushed-1.
	// A slot emptied by a revival is left as a hole rather than closed up.
	ring    []Trail
	pushed  int
	evicted int
	points  int

	// at is where in the ring each ghost sits, by ICAO, so an aircraft that
	// comes back can be found without walking two thousand trails.
	at map[string]int

	// maxTrails and maxPoints are the two caps above.
	//
	// They are fields rather than the constants read straight from the code,
	// so a test can drive both eviction paths without a fixture of a million
	// fixes.
	maxTrails int
	maxPoints int

	// lost is the scratch slice sweep collects into, reused so a frame that
	// loses nothing allocates nothing.
	lost []string

	// out is the slice the frame carries, and dirty says whether the ring has
	// moved since it was last filled. On the ordinary frame nothing has, and
	// the same slice goes back out untouched.
	out   []Trail
	dirty bool

	// stitched is the old track of each aircraft that came back while its
	// ghost was still in the ring, by ICAO. keep puts it in front of the new
	// history on every frame until the new life no longer needs it; see
	// stitch.
	stitched map[string]prefix
}

// prefix is a revived aircraft's track from before it was lost, and when it
// was lost.
type prefix struct {
	points []airplane.PositionEntry
	lostAt time.Time
}

// newGhosts builds the tracker.
//
// Every source calls it, so there is no off state and no zero value to guard
// against: the two maps are always there and observe can always write to
// them. The ring itself is still allocated lazily in push, because a receiver
// that never loses a contact never needs one.
func newGhosts() ghosts {
	return ghosts{
		live:      make(map[string]liveTrail),
		at:        make(map[string]int),
		stitched:  make(map[string]prefix),
		maxTrails: maxGhostTrails,
		maxPoints: maxGhostPoints,
	}
}

// observe folds one frame's aircraft into the ring and hands back the ghosts
// to draw, oldest first.
//
// The slice is the tracker's own and is refilled on the next frame that
// changes anything, which is the contract Frame.Planes already carries.
//
// It writes into planes: an aircraft carrying a stitched prefix gets its
// PositionHistory replaced with the joined track, so the frame the caller
// hands on draws one line where uAirwaves holds two lives.
func (g *ghosts) observe(planes airplanes.List, now time.Time) []Trail {
	g.gen++

	for index := range planes {
		g.keep(&planes[index], now)
	}

	g.sweep(now)

	return g.list()
}

// keep records what one aircraft's trail looks like on this frame, and takes
// it back out of the ring if it had already been given up for lost.
//
// An aircraft with no ICAO address is ignored. Every Mode S frame carries
// one, so a snapshot without one is a decode that went wrong rather than an
// aeroplane, and the empty string is how an emptied ring slot is marked.
//
// An aircraft whose ghost is still in the ring takes that ghost's track as a
// prefix, and the prefix goes in front of its history here, before the
// history is remembered: the trail a second loss pushes is then the whole
// track, old life and new.
func (g *ghosts) keep(plane *airplane.Snapshot, now time.Time) {
	if plane.ICAO == "" {
		return
	}

	if lost, held := g.revive(plane.ICAO); held && now.Sub(lost.LostAt) <= stitchFor {
		g.stitched[plane.ICAO] = prefix{points: lost.Points, lostAt: lost.LostAt}
	}

	g.stitch(plane, now)

	g.live[plane.ICAO] = liveTrail{
		gen: g.gen,
		trail: Trail{
			ICAO:      plane.ICAO,
			Callsign:  plane.Callsign,
			Altitude:  plane.Altitude,
			Points:    plane.PositionHistory,
			Latitude:  plane.Latitude,
			Longitude: plane.Longitude,
			Heading:   plane.Heading,
			VertRate:  plane.VertRate,
		},
	}
}

// stitch puts a revived aircraft's old track in front of its new history.
//
// uAirwaves forgets an aircraft a minute after its last message and starts it
// again from an empty history when the next one arrives, so without this the
// trail of an aircraft at the edge of coverage restarts from a dot every time
// it drops out. The prefix is cut from its oldest end so the two together stay
// within stitchFixes, and once the new life holds that many on its own, or the
// loss is older than stitchFor, the prefix is given up for good.
//
// A first new fix identical to the last old one is dropped, so the seam has
// no zero-length segment in it. PositionEntry carries no timestamp, so the
// position is the only thing two fixes can be compared on.
//
// The joined history is a fresh slice on every frame. Frame already copies
// every history out of uAirwaves, so this is one more of the same, and it
// means nothing the ring keeps aliases a buffer that is written to later.
func (g *ghosts) stitch(plane *airplane.Snapshot, now time.Time) {
	old, held := g.stitched[plane.ICAO]
	if !held {
		return
	}

	fresh := plane.PositionHistory
	room := stitchFixes - len(fresh)

	if room <= 0 || now.Sub(old.lostAt) > stitchFor {
		delete(g.stitched, plane.ICAO)

		return
	}

	points := old.points[max(0, len(old.points)-room):]
	if len(points) > 0 && len(fresh) > 0 && points[len(points)-1] == fresh[0] {
		fresh = fresh[1:]
	}

	joined := make([]airplane.PositionEntry, 0, len(points)+len(fresh))
	joined = append(joined, points...)
	joined = append(joined, fresh...)
	plane.PositionHistory = joined
}

// revive empties the ring slot of an aircraft that has come back, and hands
// back the trail that was in it.
//
// Without it the same aeroplane would be drawn twice: once as the live trail
// it is flying now, and once as the ghost of the gap it was quiet through.
// The trail it returns is what keep stitches onto the new history. The ring
// never holds the same fix twice, because the old track leaves the ring here
// and only comes back as part of the trail the next loss pushes.
func (g *ghosts) revive(icao string) (Trail, bool) {
	pushed, held := g.at[icao]
	if !held {
		return Trail{}, false
	}

	slot := pushed % g.maxTrails
	lost := g.ring[slot]

	delete(g.at, icao)
	g.clear(slot)

	return lost, true
}

// sweep moves out every aircraft that was in the last frame and is not in
// this one.
//
// The lost are sorted before they are pushed because a map is not ordered.
// The ring is the order the ghosts are drawn in, so two aircraft lost on the
// same frame would otherwise stack in whichever order the runtime felt like
// that run, and the same feed has to draw the same frame.
//
// Each trail is stamped with the frame time it was lost on, which is what the
// radar's coast is measured from.
func (g *ghosts) sweep(now time.Time) {
	g.lost = g.lost[:0]

	for icao, entry := range g.live {
		if entry.gen != g.gen {
			g.lost = append(g.lost, icao)
		}
	}

	slices.Sort(g.lost)

	for _, icao := range g.lost {
		trail := g.live[icao].trail
		trail.LostAt = now

		g.push(trail)
		delete(g.live, icao)
		delete(g.stitched, icao)
	}
}

// push moves one trail into the ring, evicting from the oldest end until it
// fits under both caps.
//
// A trail with nothing on it is dropped rather than kept. An aircraft heard
// once, with no position decoded before it went quiet, left no track behind,
// and a ghost of no fixes is a slot of the ring spent on nothing.
func (g *ghosts) push(trail Trail) {
	if len(trail.Points) == 0 {
		return
	}

	if g.ring == nil {
		g.ring = make([]Trail, g.maxTrails)
	}

	for g.full(len(trail.Points)) {
		g.evict()
	}

	g.ring[g.pushed%g.maxTrails] = trail
	g.at[trail.ICAO] = g.pushed
	g.points += len(trail.Points)
	g.pushed++
	g.dirty = true
}

// full reports whether one more trail of this many points would put the ring
// over either cap.
//
// An empty ring is never full, whatever it is handed. A single trail longer
// than the whole point budget still goes in, because the alternative is
// evicting everything and then holding nothing.
func (g *ghosts) full(points int) bool {
	if g.pushed == g.evicted {
		return false
	}

	return g.pushed-g.evicted >= g.maxTrails || g.points+points > g.maxPoints
}

// evict gives up the oldest trail in the ring.
//
// A slot already emptied by a revival costs nothing here: it holds no points
// and no ICAO, so the tally and the index are both left as they were.
func (g *ghosts) evict() {
	slot := g.evicted % g.maxTrails

	delete(g.at, g.ring[slot].ICAO)
	g.clear(slot)
	g.evicted++
}

// clear empties one slot, giving up its points so the garbage collector can
// have the trail back.
func (g *ghosts) clear(slot int) {
	g.points -= len(g.ring[slot].Points)
	g.ring[slot] = Trail{}
	g.dirty = true
}

// list refills the slice the frame carries, skipping the holes revivals left.
//
// Most frames change nothing at all, so the walk is skipped and what goes
// back out is the slice that went out last time. The scene only reads it.
func (g *ghosts) list() []Trail {
	if !g.dirty {
		return g.out
	}

	g.out = g.out[:0]

	for index := g.evicted; index < g.pushed; index++ {
		trail := g.ring[index%g.maxTrails]
		if trail.ICAO == "" {
			continue
		}

		g.out = append(g.out, trail)
	}

	g.dirty = false

	return g.out
}
