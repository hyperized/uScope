package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/pattern"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/rotate"
)

// errStub is a sentinel used wherever a test only needs some non-nil error,
// not a specific one.
var errStub = errors.New("stub")

// alwaysErrWriter fails every Write, for exercising sayf's dropped-error path.
type alwaysErrWriter struct{}

func (alwaysErrWriter) Write([]byte) (int, error) {
	return 0, errStub
}

func TestSayf(t *testing.T) {
	t.Parallel()

	t.Run("writes the formatted line", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		sayf(&buf, "hello %s %d\n", "world", 42)

		const want = "hello world 42\n"
		if got := buf.String(); got != want {
			t.Errorf("sayf wrote %q, want %q", got, want)
		}
	})

	t.Run("does not panic when the writer always errors", func(t *testing.T) {
		t.Parallel()

		sayf(alwaysErrWriter{}, "%s", "ignored")
	})
}

// Sentinels for TestEnter and TestEnterWarnsOnStdout. err113 wants static
// errors rather than errors.New calls scattered through the test bodies.
var (
	errEnterSentinelOne = errors.New("sentinel one")
	errEnterSentinelTwo = errors.New("sentinel two")
	errEnterUnrelated   = errors.New("boom")
)

// enterCase is one TestEnter table row.
type enterCase struct {
	name        string
	switchMode  func() (func() error, error)
	degrade     []error
	wantApplied bool
	wantErr     error
}

func enterCases() []enterCase {
	return []enterCase{
		{
			name: "success applies and returns the real restore",
			switchMode: func() (func() error, error) {
				return func() error { return nil }, nil
			},
			wantApplied: true,
		},
		{
			name: "tolerated error degrades",
			switchMode: func() (func() error, error) {
				return nil, fmt.Errorf("wrap: %w", errEnterSentinelOne)
			},
			degrade:     []error{errEnterSentinelOne},
			wantApplied: false,
		},
		{
			name: "untolerated error aborts",
			switchMode: func() (func() error, error) {
				return nil, errEnterUnrelated
			},
			degrade:     []error{errEnterSentinelOne},
			wantApplied: false,
			wantErr:     errEnterUnrelated,
		},
		{
			name: "second sentinel in a multi-degrade list matches",
			switchMode: func() (func() error, error) {
				return nil, fmt.Errorf("wrap: %w", errEnterSentinelTwo)
			},
			degrade:     []error{errEnterSentinelOne, errEnterSentinelTwo},
			wantApplied: false,
		},
	}
}

// TestEnter drives the mode-switch helper directly. The three outcomes it
// distinguishes are: applied cleanly, degraded on a tolerated error, and
// aborted on one that is not tolerated.
func TestEnter(t *testing.T) {
	t.Parallel()

	for _, testCase := range enterCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			restore, applied, err := enter(&buf, "thing", testCase.switchMode, testCase.degrade...)

			checkEnterResult(t, testCase, enterOutcome{restore: restore, applied: applied, err: err})
		})
	}
}

// enterOutcome bundles what enter returned, so checkEnterResult takes it as
// one value instead of a bare bool parameter driving its branches.
type enterOutcome struct {
	restore func() error
	applied bool
	err     error
}

// checkEnterResult asserts one enterCase's expected outcome against what
// enter actually returned.
func checkEnterResult(t *testing.T, testCase enterCase, got enterOutcome) {
	t.Helper()

	if got.applied != testCase.wantApplied {
		t.Errorf("applied = %v, want %v", got.applied, testCase.wantApplied)
	}

	if testCase.wantErr != nil {
		if !errors.Is(got.err, testCase.wantErr) {
			t.Errorf("err = %v, want wrapping %v", got.err, testCase.wantErr)
		}

		if got.restore != nil {
			t.Error("restore != nil on an aborting error, want nil")
		}

		return
	}

	if got.err != nil {
		t.Fatalf("err = %v, want nil", got.err)
	}

	if got.restore == nil {
		t.Fatal("restore = nil, want a usable restore func")
	}

	if restoreErr := got.restore(); restoreErr != nil {
		t.Errorf("restore() = %v, want nil", restoreErr)
	}
}

func TestEnterWarnsOnStdout(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	_, _, err := enter(&buf, "widget", func() (func() error, error) {
		return nil, fmt.Errorf("wrap: %w", errEnterSentinelOne)
	}, errEnterSentinelOne)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}

	if got := buf.String(); !strings.Contains(got, "warning: widget unavailable") {
		t.Errorf("stdout = %q, want it to mention the warning", got)
	}
}

func TestQuits(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		key  input.Key
		want bool
	}{
		{name: "lowercase q quits", key: input.Key{Kind: input.Rune, Rune: 'q'}, want: true},
		{name: "uppercase Q quits", key: input.Key{Kind: input.Rune, Rune: 'Q'}, want: true},
		{name: "another rune does not quit", key: input.Key{Kind: input.Rune, Rune: 'x'}, want: false},
		{name: "esc quits", key: input.Key{Kind: input.Esc}, want: true},
		{name: "ctrl-c quits", key: input.Key{Kind: input.CtrlC}, want: true},
		{name: "up does not quit", key: input.Key{Kind: input.Up}, want: false},
		{name: "down does not quit", key: input.Key{Kind: input.Down}, want: false},
		{name: "left does not quit", key: input.Key{Kind: input.Left}, want: false},
		{name: "right does not quit", key: input.Key{Kind: input.Right}, want: false},
		{name: "enter does not quit", key: input.Key{Kind: input.Enter}, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := quits(testCase.key); got != testCase.want {
				t.Errorf("quits(%+v) = %v, want %v", testCase.key, got, testCase.want)
			}
		})
	}
}

// Names of the runner struct's fields, shared between TestNewRunnerDefaults,
// assertOptionReplacesOnly and TestOptionsReplaceOnlyNamedField so the three
// tables cannot drift out of sync with each other.
const (
	fieldOpenFB        = "openFB"
	fieldEnterGraphics = "enterGraphics"
	fieldMakeRawStdin  = "makeRawStdin"
	fieldReadRotation  = "readRotation"
	fieldNewTicker     = "newTicker"
	fieldCreatePNG     = "createPNG"
	fieldNow           = "now"
	fieldStdin         = "stdin"
	fieldScene         = "scene"
)

func TestNewRunnerDefaults(t *testing.T) {
	t.Parallel()

	run := newRunner()

	for _, testCase := range []struct {
		name  string
		isNil bool
	}{
		{name: fieldOpenFB, isNil: run.openFB == nil},
		{name: fieldEnterGraphics, isNil: run.enterGraphics == nil},
		{name: fieldMakeRawStdin, isNil: run.makeRawStdin == nil},
		{name: fieldReadRotation, isNil: run.readRotation == nil},
		{name: fieldNewTicker, isNil: run.newTicker == nil},
		{name: fieldCreatePNG, isNil: run.createPNG == nil},
		{name: fieldNow, isNil: run.now == nil},
		{name: fieldStdin, isNil: run.stdin == nil},
		{name: fieldScene, isNil: run.scene == nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.isNil {
				t.Errorf("newRunner().%s = nil, want non-nil", testCase.name)
			}
		})
	}
}

// funcPtr returns the code pointer behind a func value, which is the only
// way to compare two func values for identity: Go forbids == on them.
func funcPtr(fn any) uintptr {
	return reflect.ValueOf(fn).Pointer()
}

//nolint:ireturn // the seam under test returns the interface; the fake must match it.
func fakeOpenFB(string) (Blitter, error) { return nil, errStub }

func fakeEnterGraphics() (func() error, error) { return nil, errStub }

func fakeMakeRawStdin() (func() error, error) { return nil, errStub }

func fakeReadRotation(string) (rotate.Rotation, error) { return rotate.None, errStub }

func fakeNewTicker(time.Duration) (<-chan time.Time, func()) { return nil, func() {} }

func fakeCreatePNG(string) (io.WriteCloser, error) { return nil, errStub }

func fakeNow() time.Time { return time.Time{} }

type optionOverrideReader struct{}

func (optionOverrideReader) Read([]byte) (int, error) { return 0, errStub }

type optionOverrideDrawer struct{}

func (optionOverrideDrawer) Draw(*canvas.Canvas, time.Duration) {}

// isDefaultScene reports whether scene is still the production default. All
// instances of pattern.Scene are behaviourally identical empty structs, so
// the type is the only thing worth checking rather than the pointer.
func isDefaultScene(scene Drawer) bool {
	_, ok := scene.(*pattern.Scene)

	return ok
}

// assertOptionReplacesOnly checks that applying an Option changed exactly
// the field named target and left the other eight at their production
// defaults.
func assertOptionReplacesOnly(t *testing.T, run *runner, target string) {
	t.Helper()

	for _, field := range []struct {
		name      string
		isDefault bool
	}{
		{fieldOpenFB, funcPtr(run.openFB) == funcPtr(openDevice)},
		{fieldEnterGraphics, funcPtr(run.enterGraphics) == funcPtr(enterConsoleGraphics)},
		{fieldMakeRawStdin, funcPtr(run.makeRawStdin) == funcPtr(rawStdin)},
		{fieldReadRotation, funcPtr(run.readRotation) == funcPtr(rotate.FromSysfs)},
		{fieldNewTicker, funcPtr(run.newTicker) == funcPtr(realTicker)},
		{fieldCreatePNG, funcPtr(run.createPNG) == funcPtr(createFile)},
		{fieldNow, funcPtr(run.now) == funcPtr(time.Now)},
		{fieldStdin, run.stdin == io.Reader(os.Stdin)},
		{fieldScene, isDefaultScene(run.scene)},
	} {
		wantDefault := field.name != target
		if field.isDefault != wantDefault {
			t.Errorf("field %s is default = %v, want %v", field.name, field.isDefault, wantDefault)
		}
	}
}

func TestOptionsReplaceOnlyNamedField(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		option Option
		target string
	}{
		{name: "WithFramebuffer", option: WithFramebuffer(fakeOpenFB), target: fieldOpenFB},
		{name: "WithConsoleSwitch", option: WithConsoleSwitch(fakeEnterGraphics), target: fieldEnterGraphics},
		{name: "WithRawMode", option: WithRawMode(fakeMakeRawStdin), target: fieldMakeRawStdin},
		{name: "WithRotationReader", option: WithRotationReader(fakeReadRotation), target: fieldReadRotation},
		{name: "WithTicker", option: WithTicker(fakeNewTicker), target: fieldNewTicker},
		{name: "WithPNGCreator", option: WithPNGCreator(fakeCreatePNG), target: fieldCreatePNG},
		{name: "WithClock", option: WithClock(fakeNow), target: fieldNow},
		{name: "WithInput", option: WithInput(optionOverrideReader{}), target: fieldStdin},
		{name: "WithScene", option: WithScene(optionOverrideDrawer{}), target: fieldScene},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			run := newRunner(testCase.option)

			assertOptionReplacesOnly(t, run, testCase.target)
		})
	}
}

func TestResolveRotation(t *testing.T) {
	t.Parallel()

	t.Run("auto-rotate off returns the configured rotation without reading sysfs", func(t *testing.T) {
		t.Parallel()

		called := false
		run := &runner{readRotation: func(string) (rotate.Rotation, error) {
			called = true

			return rotate.None, nil
		}}

		cfg := Config{AutoRotate: false, Rotation: rotate.CounterClockwise}

		var buf bytes.Buffer

		got := run.resolveRotation(cfg, &buf)

		if got != rotate.CounterClockwise {
			t.Errorf("resolveRotation = %v, want %v", got, rotate.CounterClockwise)
		}

		if called {
			t.Error("readRotation was called with AutoRotate off, want it untouched")
		}
	})

	t.Run("auto-rotate on returns what the reader reports", func(t *testing.T) {
		t.Parallel()

		run := &runner{readRotation: func(string) (rotate.Rotation, error) {
			return rotate.UpsideDown, nil
		}}

		cfg := Config{AutoRotate: true}

		var buf bytes.Buffer

		got := run.resolveRotation(cfg, &buf)

		if got != rotate.UpsideDown {
			t.Errorf("resolveRotation = %v, want %v", got, rotate.UpsideDown)
		}
	})

	t.Run("auto-rotate on with a failing reader warns and falls back to none", func(t *testing.T) {
		t.Parallel()

		run := &runner{readRotation: func(string) (rotate.Rotation, error) {
			return rotate.None, errStub
		}}

		cfg := Config{AutoRotate: true}

		var buf bytes.Buffer

		got := run.resolveRotation(cfg, &buf)

		if got != rotate.None {
			t.Errorf("resolveRotation = %v, want %v", got, rotate.None)
		}

		if !strings.Contains(buf.String(), "warning: cannot read") {
			t.Errorf("stdout = %q, want a warning", buf.String())
		}
	})
}

// TestOpenDeviceOnDarwin exercises the production framebuffer opener. This
// build has no framebuffer, so fbdev.Open always fails; which error it
// returns is a platform detail this test does not pin down.
func TestOpenDeviceOnDarwin(t *testing.T) {
	t.Parallel()

	if _, err := openDevice("/dev/fb0"); err == nil {
		t.Error("openDevice(\"/dev/fb0\") = nil error, want non-nil on this platform")
	}
}

// TestEnterConsoleGraphicsOnDarwin exercises the production console switch.
// vt.Graphics always fails off Linux, so this always returns an error here,
// regardless of whether the test runner has a controlling terminal.
//
//nolint:paralleltest // touches the real controlling terminal; must not race other tests over it.
func TestEnterConsoleGraphicsOnDarwin(t *testing.T) {
	restore, err := enterConsoleGraphics()
	if err == nil {
		if restore != nil {
			_ = restore()
		}

		t.Fatal("enterConsoleGraphics() = nil error, want non-nil: vt.Graphics always fails off Linux")
	}

	t.Logf("enterConsoleGraphics: %v", err)
}

// TestRawStdinOnDarwin exercises the production raw-mode switch. Whether it
// succeeds depends on whether the test runner's stdin is a real terminal, so
// both outcomes are accepted; a surprise success is restored immediately so
// the test runner's own terminal is not left in raw mode.
//
//nolint:paralleltest // touches the real stdin file descriptor; must not race other tests over it.
func TestRawStdinOnDarwin(t *testing.T) {
	restore, err := rawStdin()
	if err != nil {
		t.Logf("rawStdin: %v (expected off a real terminal)", err)

		return
	}

	t.Log("rawStdin unexpectedly succeeded on this stdin; restoring immediately")

	if restore != nil {
		_ = restore()
	}
}

func TestRealTicker(t *testing.T) {
	t.Parallel()

	const interval = 10 * time.Millisecond

	ch, stop := realTicker(interval)
	defer stop()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("realTicker did not tick within 1s")
	}
}

func TestCreateFile(t *testing.T) {
	t.Parallel()

	t.Run("succeeds in an existing directory", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "out.png")

		file, err := createFile(path)
		if err != nil {
			t.Fatalf("createFile: %v", err)
		}

		if err := file.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	t.Run("fails in a nonexistent directory", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "missing", "out.png")

		if _, err := createFile(path); err == nil {
			t.Fatal("createFile into a nonexistent directory = nil error, want non-nil")
		}
	})
}

func TestNewScene(t *testing.T) {
	t.Parallel()

	if newScene() == nil {
		t.Fatal("newScene() = nil, want a Drawer")
	}
}
