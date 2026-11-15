package source_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
	"github.com/hyperized/uScope/internal/history"
	"github.com/hyperized/uScope/internal/source"
)

//nolint:gochecknoglobals // read-only test fixture.
var historyEpoch = time.Date(2026, time.November, 2, 9, 0, 0, 0, time.UTC)

// fixedClock always reads historyEpoch.
func fixedClock() time.Time { return historyEpoch }

// ghostNames lists a frame's ghosts by address, in the order they are drawn.
func ghostNames(frame source.Frame) []string {
	names := make([]string, 0, len(frame.Ghosts))
	for _, ghost := range frame.Ghosts {
		names = append(names, ghost.ICAO)
	}

	return names
}

// TestLiveHistorySurvivesARestart is the feature: a track lost before the
// restart and an aircraft still flying at it both come back as ghosts, and
// the one still flying is revived by its address when the feed hears it.
func TestLiveHistorySurvivesARestart(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "history")

	before, store := startLiveIngest(t, source.WithHistory(path), source.WithClock(fixedClock))

	addAircraft(t, store, "AAAAAA", "LOST1", 3000, 52.1, 4.2)
	before.Frame()
	store.Prune(0)
	before.Frame()

	addAircraft(t, store, "BBBBBB", "FLYING", 9000, 52.2, 4.3)
	before.Frame()

	if err := before.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	after, again := startLiveIngest(t, source.WithHistory(path), source.WithClock(fixedClock))

	if got := ghostNames(after.Frame()); !reflect.DeepEqual(got, []string{"AAAAAA", "BBBBBB"}) {
		t.Fatalf("ghosts after the restart %v, want [AAAAAA BBBBBB]", got)
	}

	addAircraft(t, again, "BBBBBB", "FLYING", 9000, 52.2, 4.3)

	if got := ghostNames(after.Frame()); !reflect.DeepEqual(got, []string{"AAAAAA"}) {
		t.Fatalf("ghosts once BBBBBB is heard again %v, want [AAAAAA]", got)
	}
}

// writeHistory puts trails in a history file at path.
func writeHistory(t *testing.T, path string, trails ...history.Trail) {
	t.Helper()

	var warn bytes.Buffer

	if !history.NewWriter(path, &warn).Write(&history.Snapshot{Trails: trails}) {
		t.Fatalf("writing the fixture: %s", warn.String())
	}
}

// oneFix is a lost trail of one fix.
func oneFix(icao string, lostAt time.Time) history.Trail {
	return history.Trail{ICAO: icao, LostAt: lostAt, Points: []airplane.PositionEntry{{Latitude: 52, Longitude: 4}}}
}

// TestLiveHistoryAgesTheFileOut checks the ring's own expiry applies to a
// file: six half-lives, measured from the clock at the moment it is read.
func TestLiveHistoryAgesTheFileOut(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		halfLife time.Duration
		want     []string
	}{
		{name: "the default half-life", halfLife: source.DefaultCorridorHalfLife, want: []string{"YOUNG"}},
		{name: "a short half-life", halfLife: 10 * time.Minute, want: []string{}},
		{name: "a long half-life", halfLife: 3 * time.Hour, want: []string{"OLD", "YOUNG"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "history")
			writeHistory(t, path,
				oneFix("OLD", historyEpoch.Add(-13*time.Hour)),
				oneFix("YOUNG", historyEpoch.Add(-time.Hour-time.Second)))

			live, err := source.NewLive(source.WithIngest(&fakeIngest{}), source.WithHistory(path),
				source.WithClock(fixedClock), source.WithCorridorHalfLife(testCase.halfLife))
			if err != nil {
				t.Fatalf("NewLive: %v", err)
			}

			if got := ghostNames(live.Frame()); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("ghosts %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestLiveHistoryShrugsOffABadFile checks a file that will not decode costs a
// line on stderr and nothing else: the source builds, the ring starts empty,
// and the next write replaces the file with a good one.
func TestLiveHistoryShrugsOffABadFile(t *testing.T) {
	t.Parallel()

	var good bytes.Buffer

	snap := history.Snapshot{Trails: []history.Trail{oneFix("AAAAAA", historyEpoch)}}
	if err := history.Encode(&good, &snap); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	for _, testCase := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "truncated", data: good.Bytes()[:good.Len()-4], want: history.ErrTruncated.Error()},
		{name: "a later version", data: append([]byte("uScopeH\x02"), good.Bytes()[8:]...),
			want: history.ErrVersion.Error()},
		{name: "not a history file", data: []byte("tilt 20\n"), want: history.ErrMagic.Error()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "history")
			if err := os.WriteFile(path, testCase.data, 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			var warn syncBuffer

			live, store := startLiveIngest(t, source.WithHistory(path), source.WithStderr(&warn),
				source.WithClock(fixedClock))

			if !strings.Contains(warn.String(), testCase.want) {
				t.Errorf("stderr %q does not say %q", warn.String(), testCase.want)
			}

			addAircraft(t, store, "CCCCCC", "", 1000, 52, 4)

			if frame := live.Frame(); len(frame.Ghosts) != 0 {
				t.Fatalf("ghosts %v from a bad file, want none", ghostNames(frame))
			}

			if err := live.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			if got, err := history.Load(path); err != nil || len(got.Trails) != 1 {
				t.Fatalf("the file after Close holds %d trails, %v; want the one flying at close", len(got.Trails), err)
			}
		})
	}
}

// TestLiveHistoryWriteFailureIsAWarning checks a volume that cannot be
// written costs one line and does not fail Close.
func TestLiveHistoryWriteFailureIsAWarning(t *testing.T) {
	t.Parallel()

	var warn syncBuffer

	path := filepath.Join(t.TempDir(), "missing", "history")
	live, store := startLiveIngest(t, source.WithHistory(path), source.WithStderr(&warn), source.WithClock(fixedClock))

	if warn.String() != "" {
		t.Fatalf("a missing file was reported: %q", warn.String())
	}

	addAircraft(t, store, "AAAAAA", "", 1000, 52, 4)
	live.Frame()

	if err := live.Close(); err != nil {
		t.Fatalf("Close = %v, want nil whatever the disk said", err)
	}

	if !strings.Contains(warn.String(), "warning: history file") {
		t.Fatalf("stderr %q, want the write failure reported", warn.String())
	}
}

// TestLiveHistoryWriterStopsWithClose runs the writer goroutine for real and
// checks Close waits for it and then writes the last snapshot itself.
func TestLiveHistoryWriterStopsWithClose(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "history")

	var store *airplanes.Airplanes

	ready := make(chan struct{})
	fake := &fakeIngest{streamFn: func(ctx context.Context, planes *airplanes.Airplanes) error {
		store = planes

		close(ready)
		<-ctx.Done()

		return ctx.Err()
	}}

	clock := historyEpoch

	live, err := source.NewLive(source.WithIngest(fake), source.WithHistory(path),
		source.WithClock(func() time.Time { return clock }))
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}

	live.Start(context.Background())
	<-ready

	addAircraft(t, store, "AAAAAA", "", 1000, 52, 4)
	live.Frame()
	store.Prune(0)

	clock = clock.Add(time.Minute)

	live.Frame()

	if err := live.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got, err := history.Load(path); err != nil || len(got.Trails) != 1 {
		t.Fatalf("the file holds %d trails, %v; want the one lost", len(got.Trails), err)
	}
}
