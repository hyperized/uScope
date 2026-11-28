package source

import "time"

// Movements counts the lost tracks the corridor classifier put at the field
// since a moment: arrivals are the inbound ones, tracks that ended near the
// field, and departures the outbound ones, tracks that began there and were
// later lost. Both are counted by when they were lost, so a departure counts
// once it has left coverage rather than when it took off. A transit track
// counts as neither.
//
// It reads nothing but the trails it is handed and allocates nothing, so it
// is safe on the draw path and on any goroutine that owns the slice.
func Movements(trails []Trail, since time.Time) (int, int) {
	var arrivals, departures int

	for index := range trails {
		trail := &trails[index]
		if trail.LostAt.Before(since) {
			continue
		}

		if trail.Class == TrackInbound {
			arrivals++
		}

		if trail.Class == TrackOutbound {
			departures++
		}
	}

	return arrivals, departures
}
