package statefile_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/control"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/statefile"
	"github.com/hyperized/uScope/internal/theme"
)

// testTimeout bounds every wait on the saver's goroutine.
const testTimeout = 5 * time.Second

// base is what the flags would hand Load: a 3D view, low camera, night glass.
func base() control.State {
	return control.State{
		Radar: radar.Settings{View: radar.ViewMinimal3D, Tilt: 10, Azimuth: 143, Exaggerate: 8, Stars: radar.ToggleOn},
		Theme: theme.KindAuto, Look: theme.LookGlass,
	}
}

// writeFile puts body in a fresh state file and returns its path.
func writeFile(tb testing.TB, body string) string {
	tb.Helper()

	path := filepath.Join(tb.TempDir(), "settings")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		tb.Fatal(err)
	}

	return path
}

// TestEncodeIsTheControlPortsSpelling pins the format: the header, then one
// "name value" per field in the order get reports them.
func TestEncodeIsTheControlPortsSpelling(t *testing.T) {
	t.Parallel()

	got := string(statefile.Encode(base()))

	const want = "view minimal3d\nshore off\nairports off\nzoom 0\ntilt 10\norbit on\nazimuth 143\n" +
		"exaggerate 8\nrange auto\ntheme auto\nlook glass\ncolour altitude\nbar off\nlights off\nstars on\n" +
		"recenter 0\ncorridors on\nfilter all\nboard off\n"

	if !strings.HasPrefix(got, "# ") || !strings.HasSuffix(got, want) {
		t.Errorf("Encode =\n%s\nwant a comment line and then\n%s", got, want)
	}
}

// TestEncodeDecodeRoundTrip reads every field Encode writes back to the same
// file, which is what proves the file uses names and values set accepts.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	state := base()
	state.Radar.RangeNm, state.Radar.Zoom, state.Theme = 60, -2, theme.KindDay

	encoded := statefile.Encode(state)

	decoded, err := statefile.Decode(bytes.NewReader(encoded), control.State{}, func(line int, err error) {
		t.Errorf("line %d skipped: %v", line, err)
	})
	if err != nil {
		t.Fatal(err)
	}

	if again := statefile.Encode(decoded); !bytes.Equal(again, encoded) {
		t.Errorf("round trip changed the file:\n%s\nwant\n%s", again, encoded)
	}
}

// TestCorridorsRoundTrip turns the corridor exposure off and reads it back, so
// a run restarted after "set corridors off" comes up without it. The
// zero Toggle reads as on, so only the off case proves the line is read.
func TestCorridorsRoundTrip(t *testing.T) {
	t.Parallel()

	state := base()
	state.Radar.Corridors = radar.ToggleOff

	encoded := statefile.Encode(state)
	if !bytes.Contains(encoded, []byte("\ncorridors off\n")) {
		t.Fatalf("Encode =\n%s\nwant a corridors off line", encoded)
	}

	decoded, err := statefile.Decode(bytes.NewReader(encoded), control.State{}, func(line int, err error) {
		t.Errorf("line %d skipped: %v", line, err)
	})
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Radar.Corridors != radar.ToggleOff {
		t.Errorf("Corridors = %q after a round trip, want %q", decoded.Radar.Corridors, radar.ToggleOff)
	}
}

// TestFilterRoundTrip writes the ceiling and reads it back, so a run
// restarted after "set filter below-high" still hides the overflights.
func TestFilterRoundTrip(t *testing.T) {
	t.Parallel()

	state := base()
	state.Radar.Filter = radar.FilterBelowHigh

	encoded := statefile.Encode(state)
	if !bytes.Contains(encoded, []byte("\nfilter below-high\n")) {
		t.Fatalf("Encode =\n%s\nwant a filter below-high line", encoded)
	}

	decoded, err := statefile.Decode(bytes.NewReader(encoded), control.State{}, func(line int, err error) {
		t.Errorf("line %d skipped: %v", line, err)
	})
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Radar.Filter != radar.FilterBelowHigh {
		t.Errorf("Filter = %q after a round trip, want %q", decoded.Radar.Filter, radar.FilterBelowHigh)
	}
}

// TestCorridorFilterRoundTrip writes corridor colouring held to the arrivals
// and reads both back, so a run restarted after "set filter in" is still
// showing only the traffic coming in.
func TestCorridorFilterRoundTrip(t *testing.T) {
	t.Parallel()

	state := base()
	state.Radar.Colour = radar.ColourCorridor
	state.Radar.Filter = radar.FilterIn

	encoded := statefile.Encode(state)
	for _, line := range []string{"\ncolour corridor\n", "\nfilter in\n"} {
		if !bytes.Contains(encoded, []byte(line)) {
			t.Fatalf("Encode =\n%s\nwant a %q line", encoded, line)
		}
	}

	decoded, err := statefile.Decode(bytes.NewReader(encoded), control.State{}, func(line int, err error) {
		t.Errorf("line %d skipped: %v", line, err)
	})
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Radar.Colour != radar.ColourCorridor || decoded.Radar.Filter != radar.FilterIn {
		t.Errorf("colour, filter = %q, %q after a round trip, want corridor, in",
			decoded.Radar.Colour, decoded.Radar.Filter)
	}
}

// TestLoad covers what a start can find on disk. The file wins for what it
// holds, the base stands for the rest, and nothing is fatal.
func TestLoad(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		body     string
		want     func(*control.State)
		warnings []string
	}{
		{
			name: "partial file overrides only its fields",
			body: "# kept by hand\n\ntilt 40\n  look   mono  \n",
			want: func(s *control.State) { s.Radar.Tilt, s.Look = 40, theme.LookMono },
		},
		{
			name:     "unknown setting is skipped",
			body:     "tilt 40\ntrails long\nzoom 2\n",
			want:     func(s *control.State) { s.Radar.Tilt, s.Radar.Zoom = 40, 2 },
			warnings: []string{"line 2", "unknown setting"},
		},
		{
			name:     "invalid value is skipped",
			body:     "tilt 400\nzoom 3\n",
			want:     func(s *control.State) { s.Radar.Zoom = 3 },
			warnings: []string{"line 1", "bad value for tilt"},
		},
		{
			name:     "wrong number of words is skipped",
			body:     "tilt\nzoom 1 2\nzoom 2\n",
			want:     func(s *control.State) { s.Radar.Zoom = 2 },
			warnings: []string{"line 1", "line 2", "one name and one value"},
		},
		{
			name:     "line too long stops the read",
			body:     "zoom 1\n" + strings.Repeat("x", control.MaxLine+1) + "\nzoom 2\n",
			want:     func(s *control.State) { s.Radar.Zoom = 1 },
			warnings: []string{"after line 1", "the rest is skipped"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var warn bytes.Buffer

			got := statefile.Load(writeFile(t, testCase.body), base(), &warn)

			want := base()
			testCase.want(&want)

			if got.String() != want.String() {
				t.Errorf("Load =\n %s\nwant\n %s", got, want)
			}

			for _, needle := range testCase.warnings {
				if !strings.Contains(warn.String(), needle) {
					t.Errorf("warnings %q do not mention %q", warn.String(), needle)
				}
			}

			if len(testCase.warnings) == 0 && warn.Len() != 0 {
				t.Errorf("unexpected warning %q", warn.String())
			}
		})
	}
}

// TestLoadWithoutAFile covers the first start, which is silent, and a path
// that cannot be opened or read at all, which warns once and starts from the
// base.
func TestLoadWithoutAFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	notADir := writeFile(t, "tilt 40\n")

	for _, testCase := range []struct {
		name string
		path string
		warn string
	}{
		{name: "missing file", path: filepath.Join(dir, "missing")},
		{name: "parent is a file", path: filepath.Join(notADir, "settings"), warn: "starting from the flags"},
		{name: "path is a directory", path: dir, warn: "not a regular file"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var warn bytes.Buffer

			if got := statefile.Load(testCase.path, base(), &warn); got.String() != base().String() {
				t.Errorf("Load = %s, want the base", got)
			}

			if testCase.warn == "" && warn.Len() != 0 || !strings.Contains(warn.String(), testCase.warn) {
				t.Errorf("warning %q, want one mentioning %q", warn.String(), testCase.warn)
			}

			if strings.Count(warn.String(), "\n") > 1 {
				t.Errorf("more than one warning: %q", warn.String())
			}
		})
	}
}

// runSaver starts saver.Run and returns the function that stops it and waits.
func runSaver(tb testing.TB, saver *statefile.Saver) func() {
	tb.Helper()

	ctx, cancel := context.WithCancel(tb.Context())
	done := make(chan struct{})

	go func() {
		saver.Run(ctx)
		close(done)
	}()

	return func() {
		cancel()

		select {
		case <-done:
		case <-time.After(testTimeout):
			tb.Fatal("timed out waiting for the saver to stop")
		}
	}
}

// waitForFile polls until the file at path holds want.
func waitForFile(tb testing.TB, path, want string) {
	tb.Helper()

	deadline := time.Now().Add(testTimeout)

	for {
		body, err := os.ReadFile(path) //nolint:gosec // the test's own temp dir.
		if err == nil && strings.Contains(string(body), want) {
			return
		}

		if time.Now().After(deadline) {
			tb.Fatalf("%s never held %q; last read %q, %v", path, want, body, err)
		}

		time.Sleep(time.Millisecond)
	}
}

// TestSaverReplacesTheFile writes twice to a real directory: the second
// write replaces the first whole, and no temporary file is left beside it.
func TestSaverReplacesTheFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "settings")

	saver := statefile.New(path, &bytes.Buffer{},
		statefile.WithDelay(time.Millisecond), statefile.WithMinGap(time.Millisecond))
	stop := runSaver(t, saver)

	state := base()
	saver.Save(state)
	waitForFile(t, path, "tilt 10\n")

	state.Radar.Tilt = 25
	saver.Save(state)
	waitForFile(t, path, "tilt 25\n")
	stop()

	body, err := os.ReadFile(path) //nolint:gosec // the test's own temp dir.
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(body, statefile.Encode(state)) {
		t.Errorf("file =\n%s\nwant\n%s", body, statefile.Encode(state))
	}

	// os.CreateTemp makes the file 0600 and the rename keeps it, so a
	// shared machine does not get to read what the operator set.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %v, want -rw-------", mode)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want the state file alone: %v", len(entries), entries)
	}
}

// warnLines hands each warning the saver writes to the test, one per Write.
type warnLines struct {
	ch chan string
}

func (b warnLines) Write(data []byte) (int, error) {
	b.ch <- string(data)

	return len(data), nil
}

// TestSaverReportsAFailureOnce saves into a directory that does not exist:
// the first failure warns, the next stays quiet, and the run carries on.
func TestSaverReportsAFailureOnce(t *testing.T) {
	t.Parallel()

	warn := warnLines{ch: make(chan string, 4)}
	path := filepath.Join(t.TempDir(), "missing", "settings")
	saver := statefile.New(path, warn, statefile.WithDelay(time.Millisecond), statefile.WithMinGap(time.Millisecond))
	stop := runSaver(t, saver)

	state := base()
	saver.Save(state)

	select {
	case line := <-warn.ch:
		if !strings.Contains(line, "uScope carries on") {
			t.Errorf("warning %q", line)
		}
	case <-time.After(testTimeout):
		t.Fatal("no warning for a failed write")
	}

	state.Radar.Tilt = 30
	saver.Save(state)
	stop()

	select {
	case line := <-warn.ch:
		t.Errorf("second warning %q, want the failure reported once", line)
	default:
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat = %v, want no file", err)
	}
}

// TestWithDelayIgnoresNonsense checks a zero or negative delay leaves the
// default rather than writing on every Save.
func TestWithDelayIgnoresNonsense(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings")
	saver := statefile.New(path, &bytes.Buffer{}, statefile.WithDelay(0), statefile.WithDelay(-time.Second),
		statefile.WithMinGap(0))
	stop := runSaver(t, saver)

	saver.Save(base())

	// Well inside the default delay nothing has been written yet; the stop
	// is what flushes it.
	time.Sleep(statefile.DefaultDelay / 10)

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file written before the delay: %v", err)
	}

	stop()
	waitForFile(t, path, "tilt 10\n")
}

// FuzzDecode feeds Decode arbitrary bytes. It must not panic, and whatever it
// makes of them must survive a second trip through Encode and Decode
// unchanged, which is what a restart after a restart does.
func FuzzDecode(f *testing.F) {
	f.Add([]byte("tilt 40\nlook mono\n"))
	f.Add([]byte("# comment\n\nview scope\nshore off\nrange 60\n"))
	f.Add([]byte("bogus\nzoom 9\n\x00\xff\n"))
	f.Add(statefile.Encode(base()))

	f.Fuzz(func(t *testing.T, data []byte) {
		ignore := func(int, error) {}

		first, _ := statefile.Decode(bytes.NewReader(data), base(), ignore)
		encoded := statefile.Encode(first)

		second, err := statefile.Decode(bytes.NewReader(encoded), control.State{}, ignore)
		if err != nil {
			t.Fatalf("decoding Encode's own output: %v", err)
		}

		if again := statefile.Encode(second); !bytes.Equal(again, encoded) {
			t.Fatalf("second round trip changed the file:\n%s\nwant\n%s", again, encoded)
		}
	})
}
