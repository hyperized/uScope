package source

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"time"

	"github.com/hyperized/uScope/internal/history"
)

// WithHistory keeps the ghost ring in the file at path across a restart: it
// is read back before the first frame and written once a minute while the
// ring is changing, and once more at Close. The empty string, the default,
// keeps nothing.
//
// The file is a cache. One that is missing, unreadable or not a history file
// at all is a line on stderr and an empty ring, never a source that will not
// start.
func WithHistory(path string) LiveOption {
	return func(l *Live) { l.historyPath = path }
}

// openHistory reads the file back into the ring and builds the writer that
// keeps it. NewLive calls it once the corridors are settled, because the
// half-life they carry is what decides which trails are too old to bring back.
//
// The version the ring is at afterwards counts as written: what was just
// read is what the file already holds, so the first minute's check has
// nothing to write unless something has moved since.
func (l *Live) openHistory() {
	if l.historyPath == "" {
		return
	}

	l.restoreHistory()
	l.savedVersion = l.ghosts.version
	l.historyOut = history.NewWriter(l.historyPath, l.stderr)
	l.saveWake = make(chan struct{}, 1)
}

// restoreHistory loads the file into the ring. A first run has no file, and
// says nothing about it.
func (l *Live) restoreHistory() {
	snap, err := history.Load(l.historyPath)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}

	if err != nil {
		_, _ = fmt.Fprintf(l.stderr, "uScope: %v; starting with no lost tracks\n", err)

		return
	}

	l.ghosts.restore(snap.Trails, l.now())
}

// wakeHistory asks the writer for a snapshot when the ring has moved since
// the last one was written. It runs on the drawing goroutine under mu, so it
// does no more than compare and offer: the send never blocks, and a writer
// already awake takes the change with it.
func (l *Live) wakeHistory() {
	if l.saveWake == nil || l.ghosts.version == l.savedVersion {
		return
	}

	select {
	case l.saveWake <- struct{}{}:
	default:
	}
}

// saveLoop is the writer goroutine. Start runs it on the same group as the
// ingest, so Close waits for it before writing the last snapshot itself.
func (l *Live) saveLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.saveWake:
			l.saveHistory()
		}
	}
}

// saveHistory writes the ring if it has moved since the last write.
//
// The copy is taken under mu and the encoding and the disk happen after it is
// let go, so a frame waits for two thousand struct copies at most and never
// for the disk. The copy is shallow on purpose: a trail's fixes are never
// written once it is in the ring, so the snapshot can share them.
//
// The version is only recorded once the write succeeds, so a failed write is
// tried again at the next minute's check rather than waiting for the ring to
// move.
func (l *Live) saveHistory() {
	l.mu.Lock()
	version := l.ghosts.version

	if version == l.savedVersion {
		l.mu.Unlock()

		return
	}

	l.snapshot.Trails = l.ghosts.snapshot(l.snapshot.Trails[:0])
	l.mu.Unlock()

	if !l.historyOut.Write(&l.snapshot) {
		return
	}

	l.mu.Lock()
	l.savedVersion = version
	l.mu.Unlock()
}

// closeHistory pushes every aircraft still in the sky into the ring as lost
// now, then writes the last snapshot.
//
// The aircraft come from the ring's own record of the last frame, not from
// the store: the trails the radar last drew are the ones worth keeping, and
// reading the store again would mean a deep copy of every history for
// nothing. An aircraft still flying through a restart comes back as a ghost
// and is revived by its address as soon as the feed hears it again, which is
// what the ring already does for any contact that goes quiet for a while.
func (l *Live) closeHistory() {
	if l.historyOut == nil {
		return
	}

	l.mu.Lock()
	l.ghosts.loseAll(l.now())
	l.mu.Unlock()

	l.saveHistory()
}

// restore pushes trails read back from a file into the ring, oldest first.
//
// It refuses what the ring itself would never have held: a trail older than
// the ring's expiry measured against now, a class that does not exist, and a
// trail for an aircraft already in the sky. Of two trails for one address the
// later wins, the way a revival and a second loss would have left it. The
// caps are the ring's, applied by push, so a file written under bigger caps
// keeps its newest trails and gives up its oldest.
//
// A LostAt after now is pulled back to now. The ring is ordered by LostAt and
// expire stops at the first trail still young enough, so a trail from a
// clock that ran fast would otherwise hold every trail behind it in place.
// The input is sorted in place for the same reason.
func (g *ghosts) restore(trails []history.Trail, now time.Time) {
	slices.SortStableFunc(trails, func(a, b history.Trail) int { return a.LostAt.Compare(b.LostAt) })

	cutoff := now.Add(-corridorExposureLives * g.corridors.halfLife)

	for index := range trails {
		saved := &trails[index]
		if saved.LostAt.Before(cutoff) || saved.Class > uint8(TrackOutbound) {
			continue
		}

		if _, flying := g.live[saved.ICAO]; flying {
			continue
		}

		g.revive(saved.ICAO)
		g.push(Trail{
			ICAO:     saved.ICAO,
			Callsign: saved.Callsign,
			Altitude: saved.Altitude,
			Points:   saved.Points,
			LostAt:   earliest(saved.LostAt, now),
			Class:    TrackClass(saved.Class),
		})
	}
}

// snapshot appends the ring to dst, oldest first and without the holes.
func (g *ghosts) snapshot(dst []history.Trail) []history.Trail {
	for index := g.evicted; index < g.pushed; index++ {
		trail := &g.ring[index%g.maxTrails]
		if trail.ICAO == "" {
			continue
		}

		dst = append(dst, history.Trail{
			ICAO:     trail.ICAO,
			Callsign: trail.Callsign,
			Altitude: trail.Altitude,
			Points:   trail.Points,
			LostAt:   trail.LostAt,
			Class:    uint8(trail.Class),
		})
	}

	return dst
}

// loseAll gives up every aircraft still in the sky, as of now. Moving the
// generation on with nothing observed is all it takes: sweep then finds every
// live entry stamped with the last one, classifies it and pushes it.
func (g *ghosts) loseAll(now time.Time) {
	g.gen++
	g.sweep(now)
}

// earliest is the earlier of two times.
func earliest(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}

	return a
}
