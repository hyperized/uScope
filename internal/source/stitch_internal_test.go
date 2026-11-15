package source

import (
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
)

// stitchEpoch is the frame clock the stitching tests start from.
//
//nolint:gochecknoglobals // a fixed instant, read only.
var stitchEpoch = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// fixesFrom is a run of count fixes whose latitudes start at first, so the
// two lives of one aircraft can be told apart by value.
func fixesFrom(first, count int) []airplane.PositionEntry {
	fixes := make([]airplane.PositionEntry, count)
	for index := range fixes {
		fixes[index] = airplane.PositionEntry{Latitude: float64(first + index), Longitude: 4}
	}

	return fixes
}

// stitchICAO is the one aircraft the stitching tests lose and hear again.
const stitchICAO = "ABC123"

// heard is one frame with that aircraft in it, carrying the given history.
func heard(tracker *ghosts, fixes []airplane.PositionEntry, now time.Time) airplanes.List {
	planes := airplanes.List{{ICAO: stitchICAO, PositionHistory: fixes, Latitude: 52, Longitude: 4, Heading: 90}}
	tracker.observe(planes, now)

	return planes
}

// TestStitchJoinsTheTwoLives checks the whole round trip: an aircraft lost
// and heard again carries its old track in front of the new one, the ghost
// leaves the ring, and a second loss pushes one trail holding both lives with
// each fix in it once.
func TestStitchJoinsTheTwoLives(t *testing.T) {
	t.Parallel()

	tracker := newGhosts()
	heard(&tracker, fixesFrom(0, 5), stitchEpoch)

	lost := tracker.observe(nil, stitchEpoch.Add(time.Second))
	if len(lost) != 1 || lost[0].Heading != 90 || lost[0].Latitude != 52 {
		t.Fatalf("ghost after the loss = %+v, want one carrying the last heading and position", lost)
	}

	planes := heard(&tracker, fixesFrom(100, 3), stitchEpoch.Add(time.Minute))

	if got := len(planes[0].PositionHistory); got != 8 {
		t.Fatalf("stitched history = %d fixes, want 5 old plus 3 new", got)
	}

	if planes[0].PositionHistory[4].Latitude != 4 || planes[0].PositionHistory[5].Latitude != 100 {
		t.Errorf("seam = %v, want the old track's end then the new one's start", planes[0].PositionHistory[3:7])
	}

	if got := tracker.list(); len(got) != 0 {
		t.Errorf("ring after the revival = %d trails, want the ghost gone", len(got))
	}

	again := tracker.observe(nil, stitchEpoch.Add(2*time.Minute))
	if len(again) != 1 || len(again[0].Points) != 8 {
		t.Fatalf("ring after the second loss = %+v, want one trail of 8 fixes", again)
	}

	if _, held := tracker.stitched[stitchICAO]; held {
		t.Error("prefix survived the second loss, want it handed to the ghost")
	}
}

// TestStitchDropsADuplicateSeamFix checks that a new life starting on the
// very fix the old one ended on does not draw a zero-length segment.
func TestStitchDropsADuplicateSeamFix(t *testing.T) {
	t.Parallel()

	tracker := newGhosts()
	heard(&tracker, fixesFrom(0, 3), stitchEpoch)
	tracker.observe(nil, stitchEpoch)

	planes := heard(&tracker, fixesFrom(2, 3), stitchEpoch.Add(time.Minute))

	if got := len(planes[0].PositionHistory); got != 5 {
		t.Errorf("stitched history = %d fixes, want 3 + 3 less the shared one", got)
	}
}

// TestStitchGivesUpThePrefix covers the two ways a prefix ends: the new life
// growing to a full history of its own, and the loss getting older than any
// trail on screen.
func TestStitchGivesUpThePrefix(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		fresh int
		after time.Duration
		want  int
	}{
		{name: "trimmed to the cap", fresh: stitchFixes - 2, after: time.Minute, want: stitchFixes},
		{name: "new life is full", fresh: stitchFixes, after: time.Minute, want: stitchFixes},
		{name: "loss too old", fresh: 3, after: stitchFor + time.Second, want: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tracker := newGhosts()
			heard(&tracker, fixesFrom(0, 10), stitchEpoch)
			tracker.observe(nil, stitchEpoch)
			// The prefix is taken on a frame inside the window, then read on
			// one that may not be.
			tracker.observe(airplanes.List{{ICAO: stitchICAO}}, stitchEpoch.Add(time.Second))

			planes := heard(&tracker, fixesFrom(1000, testCase.fresh), stitchEpoch.Add(testCase.after))

			if got := len(planes[0].PositionHistory); got != testCase.want {
				t.Errorf("history = %d fixes, want %d", got, testCase.want)
			}
		})
	}
}

// TestStitchSkipsALossPastTheWindow checks that a ghost much older than
// stitchFor is revived without being carried: the gap is longer than any
// trail, so a line across it would be a line through nowhere.
func TestStitchSkipsALossPastTheWindow(t *testing.T) {
	t.Parallel()

	tracker := newGhosts()
	heard(&tracker, fixesFrom(0, 4), stitchEpoch)
	tracker.observe(nil, stitchEpoch)

	planes := heard(&tracker, fixesFrom(100, 2), stitchEpoch.Add(stitchFor+time.Minute))

	if got := len(planes[0].PositionHistory); got != 2 {
		t.Errorf("history = %d fixes, want the new life alone", got)
	}

	if len(tracker.stitched) != 0 {
		t.Error("an expired loss was kept as a prefix")
	}
}
