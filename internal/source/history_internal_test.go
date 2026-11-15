package source

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uScope/internal/history"
)

// lostICAO is the address of the trail loseOne puts in the ring.
const lostICAO = "L00001"

// routeRing is a ring holding two bundles of three tracks each, enough for
// the route book to make a route of each.
func routeRing() ghosts {
	tracker := newGhosts()
	tracker.corridors = routeField

	lines := []Trail{
		routeLine(TrackInbound, 30, 0, 0, 0, 9000, 500),
		routeLine(TrackInbound, 30, 0.3, 0, 0, 9000, 500),
		routeLine(TrackInbound, 30, -0.3, 0, 0, 9000, 500),
		routeLine(TrackOutbound, 0, 0, 0, 30, 500, 9000),
		routeLine(TrackOutbound, 0, 0, 0.3, 30, 500, 9000),
		routeLine(TrackOutbound, 0, 0, -0.3, 30, 500, 9000),
	}

	for index, line := range lines {
		line.ICAO = "R0000" + string(rune('0'+index))
		line.LostAt = routeEpoch.Add(time.Duration(index-len(lines)) * time.Minute)
		tracker.push(line)
	}

	return tracker
}

// throughFile snapshots a ring, encodes and decodes it, and hands back the
// trails a fresh ring would be restored from.
func throughFile(t *testing.T, tracker *ghosts) []history.Trail {
	t.Helper()

	var buf bytes.Buffer

	snap := history.Snapshot{Trails: tracker.snapshot(nil)}
	if err := history.Encode(&buf, &snap); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := history.Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	return decoded.Trails
}

func TestHistoryRoundTripKeepsTheRoutes(t *testing.T) {
	t.Parallel()

	before := routeRing()
	want := before.frame(nil, Receiver{}, routeEpoch)

	if len(want.routes) != 2 {
		t.Fatalf("the fixture makes %d routes, want 2", len(want.routes))
	}

	after := newGhosts()
	after.corridors = routeField
	after.restore(throughFile(t, &before), routeEpoch)

	got := after.frame(nil, Receiver{}, routeEpoch)

	if !reflect.DeepEqual(got.trails, want.trails) {
		t.Errorf("trails differ after a round trip:\n got %+v\nwant %+v", got.trails, want.trails)
	}

	if !reflect.DeepEqual(got.routes, want.routes) {
		t.Errorf("routes differ after a round trip:\n got %+v\nwant %+v", got.routes, want.routes)
	}

	if got.generation == 0 || got.routeGeneration == 0 {
		t.Errorf("generations %d and %d, want both moved so the first frame computes",
			got.generation, got.routeGeneration)
	}
}

// savedTrail is a one-fix trail as a file would hold it.
func savedTrail(icao string, lostAt time.Time, class uint8) history.Trail {
	return history.Trail{
		ICAO: icao, LostAt: lostAt, Class: class, Points: []airplane.PositionEntry{{Latitude: 1}},
	}
}

func TestRestoreRefuses(t *testing.T) {
	t.Parallel()

	expiry := corridorExposureLives * DefaultCorridorHalfLife
	now := corridorEpoch

	tracker := newGhosts()
	tracker.live["FLYING"] = liveTrail{}

	tracker.restore([]history.Trail{
		savedTrail("STALE", now.Add(-expiry-time.Second), 0),
		savedTrail("EDGE", now.Add(-expiry), 0),
		savedTrail("CLASS", now, uint8(TrackOutbound)+1),
		savedTrail("FLYING", now, 0),
		savedTrail("KEPT", now, uint8(TrackOutbound)),
	}, now)

	if got := ghostICAOs(tracker.list()); !reflect.DeepEqual(got, []string{"EDGE", "KEPT"}) {
		t.Fatalf("restored %v, want [EDGE KEPT]", got)
	}
}

func TestRestoreOrdersClampsAndDedupes(t *testing.T) {
	t.Parallel()

	now := corridorEpoch

	later := savedTrail("TWICE", now.Add(-time.Minute), 0)
	later.Points = append(later.Points, airplane.PositionEntry{Latitude: 2})

	tracker := newGhosts()
	tracker.restore([]history.Trail{
		savedTrail("FUTURE", now.Add(time.Hour), 0),
		later,
		savedTrail("TWICE", now.Add(-time.Hour), 0),
		savedTrail("OLDEST", now.Add(-2*time.Hour), 0),
	}, now)

	trails := tracker.list()
	if got := ghostICAOs(trails); !reflect.DeepEqual(got, []string{"OLDEST", "TWICE", "FUTURE"}) {
		t.Fatalf("restored %v, want [OLDEST TWICE FUTURE]", got)
	}

	if len(trails[1].Points) != 2 {
		t.Errorf("TWICE kept %d fixes, want the later trail's 2", len(trails[1].Points))
	}

	if !trails[2].LostAt.Equal(now) {
		t.Errorf("FUTURE lost at %v, want it pulled back to %v", trails[2].LostAt, now)
	}

	if tracker.points != 4 {
		t.Errorf("the ring counts %d points, want 4 with the dropped duplicate's given back", tracker.points)
	}
}

func TestRestoreKeepsToTheCaps(t *testing.T) {
	t.Parallel()

	now := corridorEpoch

	tracker := newGhosts()
	tracker.maxTrails = 2
	tracker.restore([]history.Trail{
		savedTrail("A", now.Add(-3*time.Minute), 0),
		savedTrail("B", now.Add(-2*time.Minute), 0),
		savedTrail("C", now.Add(-time.Minute), 0),
	}, now)

	if got := ghostICAOs(tracker.list()); !reflect.DeepEqual(got, []string{"B", "C"}) {
		t.Fatalf("restored %v, want the newest two", got)
	}
}

func TestLoseAllPushesTheSky(t *testing.T) {
	t.Parallel()

	tracker := newGhosts()
	tracker.corridors = placedCorridors(time.Hour)
	tracker.observe(airplanes.List{arrivalSnapshot("E00001"), snapshotWithHistory("E00002", 0)}, Receiver{},
		corridorEpoch)

	closedAt := corridorEpoch.Add(time.Second)
	tracker.loseAll(closedAt)

	trails := tracker.list()
	if len(trails) != 1 || trails[0].ICAO != "E00001" {
		t.Fatalf("ghosts %v, want the one aircraft with a track", ghostICAOs(trails))
	}

	if !trails[0].LostAt.Equal(closedAt) || trails[0].Class != TrackInbound || len(tracker.live) != 0 {
		t.Errorf("ghost %v class %d, live %d; want lost at close, classified, and nothing left flying",
			trails[0].LostAt, trails[0].Class, len(tracker.live))
	}
}

// historyLive is a Live keeping its history at a path in a temporary
// directory, on a fixed clock, with its warnings in the returned buffer.
func historyLive(t *testing.T, path string) (*Live, *bytes.Buffer) {
	t.Helper()

	var warn bytes.Buffer

	live, err := NewLive(WithIngest(stubIngest{}), WithHistory(path), WithStderr(&warn),
		WithClock(func() time.Time { return corridorEpoch }))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	return live, &warn
}

// loseOne puts one lost trail in the live source's ring.
func loseOne(live *Live) {
	live.mu.Lock()
	defer live.mu.Unlock()

	live.ghosts.push(Trail{ICAO: lostICAO, Points: []airplane.PositionEntry{{Latitude: 1}}, LostAt: corridorEpoch})
}

func TestWakeHistory(t *testing.T) {
	t.Parallel()

	live, _ := historyLive(t, filepath.Join(t.TempDir(), "history"))

	live.wakeHistory()

	if len(live.saveWake) != 0 {
		t.Fatal("a ring that has not moved woke the writer")
	}

	loseOne(live)
	live.wakeHistory()
	live.wakeHistory()

	if len(live.saveWake) != 1 {
		t.Fatalf("%d wake-ups waiting, want the two calls folded into 1", len(live.saveWake))
	}

	plain, err := NewLive(WithIngest(stubIngest{}))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	loseOne(plain)
	plain.wakeHistory()
}

func TestSaveLoopWritesOnAWake(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "history")
	live, _ := historyLive(t, path)

	loseOne(live)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		live.saveLoop(ctx)
		close(done)
	}()

	// The channel holds one wake-up, so the third send can only go through
	// once the loop has taken the second, which is after it finished the
	// write the first one asked for.
	for range 3 {
		live.saveWake <- struct{}{}
	}

	cancel()
	<-done

	snap, err := history.Load(path)
	if err != nil || len(snap.Trails) != 1 || snap.Trails[0].ICAO != lostICAO {
		t.Fatalf("the file holds %+v, %v; want the one lost trail", snap.Trails, err)
	}
}

func TestSaveHistoryRetriesAfterAFailure(t *testing.T) {
	t.Parallel()

	live, warn := historyLive(t, filepath.Join(t.TempDir(), "missing", "history"))

	loseOne(live)
	live.saveHistory()
	live.saveHistory()

	if live.savedVersion == live.ghosts.version {
		t.Error("a failed write was recorded as written")
	}

	if lines := strings.Count(warn.String(), "\n"); lines != 1 {
		t.Errorf("%d warning lines, want 1:\n%s", lines, warn.String())
	}
}

func TestSaveHistorySkipsAnUnmovedRing(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "history")
	live, _ := historyLive(t, path)

	live.saveHistory()

	if _, err := history.Load(path); err == nil {
		t.Fatal("a ring that never moved was written")
	}
}

// fullRing is a ring at its trail cap with every trail a full uAirwaves
// history of points fixes.
func fullRing(points int) ghosts {
	tracker := newGhosts()

	for index := range maxGhostTrails {
		fixes := make([]airplane.PositionEntry, points)
		for fix := range fixes {
			// Coordinates with every bit of a float64 in use, which is what a
			// decoded position looks like and what gob spends the most on.
			fixes[fix] = airplane.PositionEntry{
				Latitude:  52.123456789123 + float64(fix)*1e-5,
				Longitude: 4.987654321987 + float64(index)*1e-5,
				Altitude:  35000.123 + float64(fix),
			}
		}

		tracker.push(Trail{
			ICAO: fmt.Sprintf("A%05d", index), Callsign: "KLM1234", Points: fixes, LostAt: corridorEpoch,
		})
	}

	return tracker
}

// BenchmarkHistorySnapshot prices the copy the writer takes under mu, at the
// ring's trail cap with every trail a full uAirwaves history.
func BenchmarkHistorySnapshot(b *testing.B) {
	tracker := fullRing(256)
	dst := tracker.snapshot(nil)

	b.ReportAllocs()

	for b.Loop() {
		dst = tracker.snapshot(dst[:0])
	}
}

// BenchmarkHistoryEncode prices writing the file and reports its size, at the
// trail cap with full uAirwaves histories and at the point cap as well.
func BenchmarkHistoryEncode(b *testing.B) {
	for _, testCase := range []struct {
		name   string
		points int
	}{
		{name: "2000x256", points: 256},
		{name: "2000x500", points: maxGhostPoints / maxGhostTrails},
	} {
		b.Run(testCase.name, func(b *testing.B) {
			tracker := fullRing(testCase.points)
			snap := history.Snapshot{Trails: tracker.snapshot(nil)}

			var buf bytes.Buffer

			for b.Loop() {
				buf.Reset()

				if err := history.Encode(&buf, &snap); err != nil {
					b.Fatal(err)
				}
			}

			b.ReportMetric(float64(buf.Len()), "bytes/file")
		})
	}
}

func TestSnapshotSkipsTheHoles(t *testing.T) {
	t.Parallel()

	tracker := newGhosts()
	tracker.restore([]history.Trail{
		savedTrail(lostICAO, corridorEpoch.Add(-time.Minute), 0),
		savedTrail("A00002", corridorEpoch, 0),
	}, corridorEpoch)
	tracker.revive(lostICAO)

	got := tracker.snapshot(nil)
	if len(got) != 1 || got[0].ICAO != "A00002" {
		t.Fatalf("snapshot %+v, want A00002 alone", got)
	}
}
