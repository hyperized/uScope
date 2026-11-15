package radar

import (
	"slices"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/source"
)

// seenICAO is the address the table tests key by.
const seenICAO = "3C6745"

// TestICAOKey checks the address parse: six hex digits in either case, and
// nothing else.
func TestICAOKey(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		icao string
		want uint32
		ok   bool
	}{
		{icao: "3C6745", want: 0x3C6745, ok: true},
		{icao: "abcdef", want: 0xABCDEF, ok: true},
		{icao: "000000", want: 0, ok: true},
		{icao: "48AC1"},
		{icao: "484AC12"},
		{icao: "48GAC1"},
		{icao: ""},
	} {
		t.Run(testCase.icao, func(t *testing.T) {
			t.Parallel()

			got, ok := icaoKey(testCase.icao)
			if got != testCase.want || ok != testCase.ok {
				t.Errorf("icaoKey(%q) = %X, %v, want %X, %v", testCase.icao, got, ok, testCase.want, testCase.ok)
			}
		})
	}
}

// collidingAddresses is count addresses that all hash to the same home slot,
// found by walking the address space, so a test can fill one probe window.
func collidingAddresses(tb testing.TB, count int) []string {
	tb.Helper()

	var found []string

	target := slotOf(1)

	for key := uint32(1); len(found) < count && key < 1<<24; key++ {
		if slotOf(key) == target {
			found = append(found, hexAddress(key))
		}
	}

	if len(found) < count {
		tb.Fatalf("found %d colliding addresses, want %d", len(found), count)
	}

	return found
}

// hexAddress spells a 24-bit key the way uAirwaves does.
func hexAddress(key uint32) string {
	const digits = "0123456789ABCDEF"

	out := make([]byte, icaoChars)
	for index := icaoChars - 1; index >= 0; index-- {
		out[index] = digits[key&0xF]
		key >>= 4
	}

	return string(out)
}

// TestSeenTableEvictsTheStalest fills one probe window, then adds one more
// address: the entry stamped longest ago gives up its slot, and the others
// are all still found.
func TestSeenTableEvictsTheStalest(t *testing.T) {
	t.Parallel()

	addresses := collidingAddresses(t, seenProbe+1)

	var table seenTable

	table.begin(0)

	for _, icao := range addresses[:seenProbe] {
		table.touch(icao)
	}

	// Everything but one in the middle of the window is seen again on a
	// later frame, which leaves that one the stalest.
	const stalest = seenProbe / 2

	table.begin(time.Second)

	for index, icao := range addresses[:seenProbe] {
		if index != stalest {
			table.touch(icao)
		}
	}

	if entry := table.touch(addresses[seenProbe]); entry == nil || entry.decided {
		t.Fatalf("touch of a new address in a full window = %+v, want a fresh entry", entry)
	}

	for index, icao := range addresses {
		if kept := table.find(icao) != nil; kept != (index != stalest) {
			t.Errorf("%s kept = %v, want only the stalest evicted", icao, kept)
		}
	}

	if table.find("ZZZZZZ") != nil || table.touch("ZZZZZZ") != nil {
		t.Error("an address that is not hex was given an entry")
	}
}

// TestSeenTableForgetsAStaleDecision checks that an entry nobody touched for
// a frame is treated as new when its aircraft turns up again.
func TestSeenTableForgetsAStaleDecision(t *testing.T) {
	t.Parallel()

	var table seenTable

	table.begin(0)
	table.show(table.touch(seenICAO))
	table.begin(time.Second)
	table.begin(2 * time.Second)

	if entry := table.touch(seenICAO); entry.decided {
		t.Error("a decision two frames old was kept, want it forgotten")
	}
}

// TestPresence covers the fade-in's edges: no entry, an undecided one, the
// middle of the fade, and a render clock that has gone back.
func TestPresence(t *testing.T) {
	t.Parallel()

	scene := &Scene{}
	scene.seen.begin(0)
	scene.seen.begin(time.Second)

	if got := scene.presence(seenICAO); got != 1 {
		t.Errorf("presence with no entry = %v, want 1", got)
	}

	entry := scene.seen.touch(seenICAO)
	if got := scene.presence(seenICAO); got != 1 {
		t.Errorf("presence undecided = %v, want 1", got)
	}

	scene.seen.show(entry)
	scene.seen.begin(2 * time.Second)

	if got := scene.presence(seenICAO); got != 0.5 {
		t.Errorf("presence a second in = %v, want 0.5", got)
	}

	scene.seen.begin(0)

	if got := scene.presence(seenICAO); got != 1 {
		t.Errorf("presence after the clock went back = %v, want 1", got)
	}
}

// TestCoastShare pins the coast's fade against the frame clock.
func TestCoastShare(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

	for _, testCase := range []struct {
		name     string
		ago      time.Duration
		want     float64
		coasting bool
	}{
		{name: "just lost", want: 1, coasting: true},
		{name: "half way", ago: 30 * time.Second, want: 0.5, coasting: true},
		{name: "a clock that went back", ago: -time.Second, want: 1, coasting: true},
		{name: "done", ago: coastFor},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			trail := source.Trail{LostAt: now.Add(-testCase.ago)}

			got, coasting := coastShare(&trail, now)
			if got != testCase.want || coasting != testCase.coasting {
				t.Errorf("coastShare = %v, %v, want %v, %v", got, coasting, testCase.want, testCase.coasting)
			}
		})
	}

	ghosts := []source.Trail{{LostAt: now.Add(-time.Hour)}, {LostAt: now.Add(-10 * time.Second)}, {LostAt: now}}
	if got := coastStart(ghosts, now); got != 1 {
		t.Errorf("coastStart = %d, want 1", got)
	}
}

// TestDeadBandOnBothEdges walks the mid band's two edges: an aircraft keeps
// the answer it had until it is 500 ft past an edge, in either direction.
func TestDeadBandOnBothEdges(t *testing.T) {
	t.Parallel()

	scene := &Scene{filter: filterState{kind: filterBand, band: bandMid}}
	scene.seen.begin(0)

	for step, testCase := range []struct {
		altitude float64
		want     bool
	}{
		{altitude: 10100, want: true},
		{altitude: 9600, want: true},
		{altitude: 9400},
		{altitude: 10400},
		{altitude: 10600, want: true},
		{altitude: 25400, want: true},
		{altitude: 25600},
		{altitude: 0},
	} {
		scene.seen.begin(time.Duration(step+1) * time.Second)

		if got := scene.passes(seenICAO, testCase.altitude, "", source.TrackTransit); got != testCase.want {
			t.Errorf("step %d at %.0f ft: passes = %v, want %v", step, testCase.altitude, got, testCase.want)
		}
	}

	// An address with no key gets the edges themselves and no memory.
	if scene.passes("GHOST1", 9600, "", source.TrackTransit) {
		t.Error("an unkeyed aircraft under the floor passed, want the plain edge")
	}
}

// TestSetFilterForgetsOnlyOnAChange checks that the table is wiped when the
// filter really changes and kept when it is handed the same one, which is
// what Apply(Current()) does on every control-port set.
func TestSetFilterForgetsOnlyOnAChange(t *testing.T) {
	t.Parallel()

	scene := &Scene{}
	scene.seen.begin(0)
	scene.seen.touch(seenICAO)

	scene.setFilter(filterState{})

	if scene.seen.find(seenICAO) == nil {
		t.Error("the same filter again wiped the table")
	}

	scene.setFilter(filterState{kind: filterBelow, band: bandMid})

	if scene.seen.find(seenICAO) != nil || scene.seen.warm {
		t.Error("a new filter kept the old filter's memory")
	}
}

// TestCoastHeadNeedsAPlace checks the head's two refusals: a trail with no
// position, and a position the projection cannot place.
func TestCoastHeadNeedsAPlace(t *testing.T) {
	t.Parallel()

	const side = 100

	scene, canv := barScene(t, ViewMinimal, side, side)
	lay := scene.newLayout(canv)
	before := slices.Clone(canv.Image().Pix)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

	geom := scopeGeometry{centerX: side / 2, centerY: side / 2, outer: 45, rangeR: 40}

	proj, ok := newProjector(geom, geo{lat: 52, lon: 4}, 10)
	if !ok {
		t.Fatal("newProjector refused a plain geometry")
	}

	for _, trail := range []source.Trail{
		{ICAO: seenICAO, LostAt: now, Heading: 90},
		{ICAO: seenICAO, LostAt: now, Heading: 90, Latitude: 60, Longitude: 4},
	} {
		scene.drawCoastHead(&lay, proj, &trail, now)
	}

	if !slices.Equal(canv.Image().Pix, before) {
		t.Error("a head with nowhere to go was drawn")
	}
}

// TestCoasting3Refusals checks the model's two refusals in perspective: a
// trail with no position, and one the filter holds back on the altitude it
// was last heard at.
func TestCoasting3Refusals(t *testing.T) {
	t.Parallel()

	scene, view := envelopeFixture()
	scene.faces = Faces{}
	scene.filter = filterState{kind: filterBand, band: bandLow}

	canv := blankCanvas(t)
	blank := blankCanvas(t)
	lay := scene.newLayout(canv)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

	for _, trail := range []source.Trail{
		{ICAO: seenICAO, LostAt: now, Heading: 90, Altitude: 3000},
		{ICAO: seenICAO, LostAt: now, Heading: 90, Altitude: 36000, Latitude: layerBaseLat, Longitude: layerBaseLon},
	} {
		scene.drawCoasting3(&lay, view, &trail, now)
	}

	if !identicalPixels(canv, blank) {
		t.Error("a coasting model with nowhere to go or filtered out was drawn")
	}
}
