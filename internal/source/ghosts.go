package source

import (
	"slices"

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

// Trail is a track with no aircraft on the end of it.
//
// It is what a source keeps when a contact is lost: the last position history
// the aircraft showed, plus the two things the radar needs to colour it by.
// There is no position and no heading, because there is no longer an
// aeroplane to have either.
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
		maxTrails: maxGhostTrails,
		maxPoints: maxGhostPoints,
	}
}

// observe folds one frame's aircraft into the ring and hands back the ghosts
// to draw, oldest first.
//
// The slice is the tracker's own and is refilled on the next frame that
// changes anything, which is the contract Frame.Planes already carries.
func (g *ghosts) observe(planes airplanes.List) []Trail {
	g.gen++

	for _, plane := range planes {
		g.keep(plane)
	}

	g.sweep()

	return g.list()
}

// keep records what one aircraft's trail looks like on this frame, and takes
// it back out of the ring if it had already been given up for lost.
//
// An aircraft with no ICAO address is ignored. Every Mode S frame carries
// one, so a snapshot without one is a decode that went wrong rather than an
// aeroplane, and the empty string is how an emptied ring slot is marked.
func (g *ghosts) keep(plane airplane.Snapshot) {
	if plane.ICAO == "" {
		return
	}

	g.revive(plane.ICAO)

	g.live[plane.ICAO] = liveTrail{
		gen: g.gen,
		trail: Trail{
			ICAO:     plane.ICAO,
			Callsign: plane.Callsign,
			Altitude: plane.Altitude,
			Points:   plane.PositionHistory,
		},
	}
}

// revive empties the ring slot of an aircraft that has come back.
//
// Without it the same aeroplane would be drawn twice: once as the live trail
// it is flying now, and once as the ghost of the gap it was quiet through.
func (g *ghosts) revive(icao string) {
	pushed, held := g.at[icao]
	if !held {
		return
	}

	delete(g.at, icao)
	g.clear(pushed % g.maxTrails)
}

// sweep moves out every aircraft that was in the last frame and is not in
// this one.
//
// The lost are sorted before they are pushed because a map is not ordered.
// The ring is the order the ghosts are drawn in, so two aircraft lost on the
// same frame would otherwise stack in whichever order the runtime felt like
// that run, and the same feed has to draw the same frame.
func (g *ghosts) sweep() {
	g.lost = g.lost[:0]

	for icao, entry := range g.live {
		if entry.gen != g.gen {
			g.lost = append(g.lost, icao)
		}
	}

	slices.Sort(g.lost)

	for _, icao := range g.lost {
		g.push(g.live[icao].trail)
		delete(g.live, icao)
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
