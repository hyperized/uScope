package statefile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/control"
	"github.com/hyperized/uScope/internal/radar"
)

var (
	errScripted = errors.New("scripted failure")
	errTempDir  = errors.New("temporary file outside the state file's directory")
)

// fakeFile is a temporary file whose steps fail on request.
type fakeFile struct {
	failWrite, failSync, failClose bool
	data                           bytes.Buffer
	closed                         int
}

func (f *fakeFile) Write(data []byte) (int, error) {
	if f.failWrite {
		return 0, errScripted
	}

	return f.data.Write(data) //nolint:wrapcheck // stands in for *os.File, which does not wrap either.
}

func (f *fakeFile) Sync() error {
	if f.failSync {
		return errScripted
	}

	return nil
}

func (f *fakeFile) Close() error {
	f.closed++

	if f.failClose {
		return errScripted
	}

	return nil
}

func (*fakeFile) Name() string { return "/var/lib/uscope/.settings.123" }

// fakeDisk records what the saver did to the filesystem.
type fakeDisk struct {
	mu         sync.Mutex
	file       *fakeFile
	failCreate bool
	failRename bool
	renames    []string
	removed    []string
}

func (d *fakeDisk) install(saver *Saver) {
	saver.createTemp = func(dir, pattern string) (tempFile, error) {
		d.mu.Lock()
		defer d.mu.Unlock()

		if d.failCreate {
			return nil, errScripted
		}

		if dir != "/var/lib/uscope" || pattern != ".settings.*" {
			return nil, fmt.Errorf("%w: %s %s", errTempDir, dir, pattern)
		}

		d.file = &fakeFile{failWrite: d.file.failWrite, failSync: d.file.failSync, failClose: d.file.failClose}

		return d.file, nil
	}
	saver.rename = func(from, target string) error {
		d.mu.Lock()
		defer d.mu.Unlock()

		if d.failRename {
			return errScripted
		}

		d.renames = append(d.renames, from+" -> "+target+": "+d.file.data.String())

		return nil
	}
	saver.remove = func(name string) error {
		d.mu.Lock()
		defer d.mu.Unlock()

		d.removed = append(d.removed, name)

		return nil
	}
}

// TestWriteFailures fails each step of the atomic write in turn: every one
// is reported, the temporary file is removed, and the file is closed exactly
// once on every path.
func TestWriteFailures(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		disk       *fakeDisk
		want       string
		wantRemove bool
	}{
		{name: "create", disk: &fakeDisk{file: &fakeFile{}, failCreate: true}, want: "creating a temporary file"},
		{name: "write", disk: &fakeDisk{file: &fakeFile{failWrite: true}}, want: "writing", wantRemove: true},
		{name: "sync", disk: &fakeDisk{file: &fakeFile{failSync: true}}, want: "syncing", wantRemove: true},
		{name: "close", disk: &fakeDisk{file: &fakeFile{failClose: true}}, want: "closing", wantRemove: true},
		{name: "rename", disk: &fakeDisk{file: &fakeFile{}, failRename: true}, want: "replacing", wantRemove: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var warn bytes.Buffer

			saver := New("/var/lib/uscope/settings", &warn)
			disk := testCase.disk
			disk.install(saver)

			saver.Save(control.State{})
			saver.flush()

			if !strings.Contains(warn.String(), testCase.want) || !errors.Is(saver.write(nil), errScripted) {
				t.Errorf("warning %q, want it to mention %q", warn.String(), testCase.want)
			}

			if got := len(disk.removed) > 0; got != testCase.wantRemove {
				t.Errorf("removed %v, want removal %t", disk.removed, testCase.wantRemove)
			}

			if !testCase.disk.failCreate && disk.file.closed != 1 {
				t.Errorf("closed %d times, want once", disk.file.closed)
			}
		})
	}
}

// TestFlushRecoversAfterAFailure checks the once-only warning is once per
// failing streak: a write that succeeds re-arms it.
func TestFlushRecoversAfterAFailure(t *testing.T) {
	t.Parallel()

	var warn bytes.Buffer

	saver := New("/var/lib/uscope/settings", &warn)
	disk := fakeDisk{file: &fakeFile{}, failRename: true}
	disk.install(saver)

	for tilt := range 3 {
		saver.Save(control.State{Radar: radar.Settings{Tilt: float64(10 + tilt)}})
		saver.flush()
	}

	disk.failRename = false

	saver.Save(control.State{Radar: radar.Settings{Tilt: 20}})
	saver.flush()

	disk.failRename = true

	saver.Save(control.State{Radar: radar.Settings{Tilt: 21}})
	saver.flush()

	if got := strings.Count(warn.String(), "warning:"); got != 2 {
		t.Errorf("%d warnings, want one per failing streak:\n%s", got, warn.String())
	}

	if len(disk.renames) != 1 || !strings.Contains(disk.renames[0], "tilt 20\n") {
		t.Errorf("renames %q, want the one good write", disk.renames)
	}
}

// TestFlushSkipsWhatIsAlreadyThere covers the two writes flush declines: no
// change since the last flush, and a change that renders the same bytes.
func TestFlushSkipsWhatIsAlreadyThere(t *testing.T) {
	t.Parallel()

	saver := New("/var/lib/uscope/settings", &bytes.Buffer{})
	disk := fakeDisk{file: &fakeFile{}}
	disk.install(saver)

	saver.flush()

	state := control.State{Radar: radar.Settings{Tilt: 20}}
	saver.Save(state)
	saver.flush()
	saver.Save(state)
	saver.flush()

	if len(disk.renames) != 1 {
		t.Errorf("%d writes, want one: %q", len(disk.renames), disk.renames)
	}
}

// TestRunCoalescesABurst drives the delay by hand: ten changes before it runs
// out are one write of the last of them.
func TestRunCoalescesABurst(t *testing.T) {
	t.Parallel()

	fire := make(chan time.Time)
	asked := make(chan time.Duration, 1)

	saver := New("/var/lib/uscope/settings", &bytes.Buffer{}, WithDelay(time.Second))
	saver.after = func(delay time.Duration) <-chan time.Time {
		asked <- delay

		return fire
	}

	disk := fakeDisk{file: &fakeFile{}}
	disk.install(saver)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		saver.Run(ctx)
		close(done)
	}()

	saver.Save(control.State{Radar: radar.Settings{Tilt: 10}})

	if delay := <-asked; delay != time.Second {
		t.Errorf("waited %v, want the configured second", delay)
	}

	for tilt := 11; tilt <= 20; tilt++ {
		saver.Save(control.State{Radar: radar.Settings{Tilt: float64(tilt)}})
	}

	fire <- time.Time{}

	// The Saves above left one wake-up behind, which flushes nothing new;
	// the delay it asks for is never fired, and the cancel ends it.
	<-asked
	cancel()
	<-done

	disk.mu.Lock()
	defer disk.mu.Unlock()

	if len(disk.renames) != 1 || !strings.Contains(disk.renames[0], "tilt 20\n") {
		t.Errorf("renames %q, want one write of tilt 20", disk.renames)
	}
}

// TestParseLineSkipsNothingLines covers the lines that apply nothing and are
// not errors either.
func TestParseLineSkipsNothingLines(t *testing.T) {
	t.Parallel()

	for _, line := range []string{"", "   ", "# tilt 40", "  # indented"} {
		change, err := parseLine(line)
		if change != nil || err != nil {
			t.Errorf("parseLine(%q) = %v, %v; want nothing", line, change != nil, err)
		}
	}
}

// gapRig is a Saver on a hand-driven clock and timer, with a fake disk.
type gapRig struct {
	saver *Saver
	disk  *fakeDisk
	fire  chan time.Time
	asked chan time.Duration
	clock atomic.Int64
}

func newGapRig() *gapRig {
	rig := &gapRig{fire: make(chan time.Time), asked: make(chan time.Duration, 1)}
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	rig.saver = New("/var/lib/uscope/settings", &bytes.Buffer{}, WithDelay(time.Second), WithMinGap(10*time.Second))
	rig.saver.now = func() time.Time { return start.Add(time.Duration(rig.clock.Load())) }
	rig.saver.after = func(delay time.Duration) <-chan time.Time {
		rig.asked <- delay

		return rig.fire
	}

	rig.disk = &fakeDisk{file: &fakeFile{}}
	rig.disk.install(rig.saver)

	return rig
}

// change saves a tilt, lets the delay run out at the given clock reading,
// and returns once Run has moved past the delay.
func (rig *gapRig) change(tilt float64, at time.Duration) {
	rig.saver.Save(control.State{Radar: radar.Settings{Tilt: tilt}})
	<-rig.asked

	rig.clock.Store(int64(at))

	rig.fire <- time.Time{}
}

// waitFor returns once count writes have landed. Each change waits for the
// write before it, or its Save could land before that flush and be written
// by it.
func (rig *gapRig) waitFor(count int) {
	deadline := time.Now().Add(time.Second)

	for rig.writes() < count && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

func (rig *gapRig) writes() int {
	rig.disk.mu.Lock()
	defer rig.disk.mu.Unlock()

	return len(rig.disk.renames)
}

// TestRunKeepsTheMinimumGap drives the clock by hand: the first change is
// written after the delay alone, the next waits out what is left of the gap
// since that write, and a stop part way through a wait still writes.
func TestRunKeepsTheMinimumGap(t *testing.T) {
	t.Parallel()

	rig := newGapRig()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		rig.saver.Run(ctx)
		close(done)
	}()

	rig.change(10, 0)
	rig.waitFor(1)

	// Three seconds later a second change: the delay, then seven more.
	rig.change(20, 3*time.Second)

	if wait := <-rig.asked; wait != 7*time.Second {
		t.Errorf("held off %v, want the 7s left of the gap", wait)
	}

	if got := rig.writes(); got != 1 {
		t.Errorf("%d writes inside the gap, want 1", got)
	}

	rig.fire <- time.Time{}

	rig.waitFor(2)

	// Past the gap a change waits for the delay alone.
	rig.change(30, 30*time.Second)
	rig.waitFor(3)

	// One inside the gap is written at once by a stop.
	rig.change(40, 30*time.Second)
	<-rig.asked
	cancel()
	<-done

	rig.disk.mu.Lock()
	defer rig.disk.mu.Unlock()

	if len(rig.disk.renames) != 4 || !strings.Contains(rig.disk.renames[3], "tilt 40\n") {
		t.Errorf("renames %d, want four ending with tilt 40", len(rig.disk.renames))
	}
}
