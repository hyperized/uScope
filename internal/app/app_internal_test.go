package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/adsb"
	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/term"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/backend"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/fonts"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/rotate"
	"github.com/hyperized/uScope/pkg/shore"
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

func TestClassify(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		key  input.Key
		want command
	}{
		{name: "lowercase q quits", key: input.Key{Kind: input.Rune, Rune: 'q'}, want: cmdQuit},
		{name: "uppercase Q quits", key: input.Key{Kind: input.Rune, Rune: 'Q'}, want: cmdQuit},
		{
			// v used to switch scenes; it now flips the radar's own minimal
			// view, entirely inside Scene.Handle, so classify itself must no
			// longer bind it to anything.
			name: "lowercase v is no longer bound here", key: input.Key{Kind: input.Rune, Rune: 'v'}, want: cmdNone,
		},
		{name: "uppercase V is no longer bound here", key: input.Key{Kind: input.Rune, Rune: 'V'}, want: cmdNone},
		{
			// s used to switch scenes before that; it must not still be bound
			// to anything left over from that either.
			name: "s is no longer bound", key: input.Key{Kind: input.Rune, Rune: 's'}, want: cmdNone,
		},
		{name: "lowercase l cycles the theme", key: input.Key{Kind: input.Rune, Rune: 'l'}, want: cmdNextTheme},
		{name: "uppercase L cycles the theme", key: input.Key{Kind: input.Rune, Rune: 'L'}, want: cmdNextTheme},
		{name: "lowercase k cycles the look", key: input.Key{Kind: input.Rune, Rune: 'k'}, want: cmdNextLook},
		{name: "uppercase K cycles the look", key: input.Key{Kind: input.Rune, Rune: 'K'}, want: cmdNextLook},
		{name: "another rune is unbound", key: input.Key{Kind: input.Rune, Rune: 'x'}, want: cmdNone},
		{name: "esc quits", key: input.Key{Kind: input.Esc}, want: cmdQuit},
		{name: "ctrl-c quits", key: input.Key{Kind: input.CtrlC}, want: cmdQuit},
		{name: "up is unbound", key: input.Key{Kind: input.Up}, want: cmdNone},
		{name: "down is unbound", key: input.Key{Kind: input.Down}, want: cmdNone},
		{name: "left is unbound", key: input.Key{Kind: input.Left}, want: cmdNone},
		{name: "right is unbound", key: input.Key{Kind: input.Right}, want: cmdNone},
		{name: "enter is unbound", key: input.Key{Kind: input.Enter}, want: cmdNone},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := classify(testCase.key); got != testCase.want {
				t.Errorf("classify(%+v) = %d, want %d", testCase.key, got, testCase.want)
			}
		})
	}
}

// Names of the runner struct's fields, shared between TestNewRunnerDefaults,
// assertOptionReplacesOnly and TestOptionsReplaceOnlyNamedField so the three
// tables cannot drift out of sync with each other.
const (
	fieldOpenFB        = "openFB"
	fieldOpenTerm      = "openTerm"
	fieldGetenv        = "getenv"
	fieldGOOS          = "goos"
	fieldStderr        = "stderr"
	fieldEnterGraphics = "enterGraphics"
	fieldMakeRawStdin  = "makeRawStdin"
	fieldReadRotation  = "readRotation"
	fieldNewTicker     = "newTicker"
	fieldCreatePNG     = "createPNG"
	fieldNow           = "now"
	fieldStdin         = "stdin"
	fieldLoadScenes    = "loadScenes"
	fieldSource        = "source"
	fieldScopeRange    = "scopeRange"
	fieldBattery       = "battery"
	fieldShoreSet      = "shoreSet"
)

// overrideRangeNm is a display range no default Scope starts at, so a test can
// tell a replaced range control from the one newRunner built.
const overrideRangeNm = 120

// optionOverrideSource is a Source whose type is not source.Empty, which is
// how a test tells a replaced source from the default one.
type optionOverrideSource struct{}

func (optionOverrideSource) Frame() source.Frame { return source.Frame{} }

func (optionOverrideSource) Close() error { return nil }

//nolint:nonamedreturns // mirrors the interface it satisfies.
func (optionOverrideSource) BiasTee() (supported, enabled bool) { return false, false }

func (optionOverrideSource) SetBiasTee(bool) error { return adsb.ErrBiasTeeUnsupported }

// wantSceneCount is how many scenes the production set holds: the radar and
// the pattern.
const wantSceneCount = 2

func TestNewRunnerDefaults(t *testing.T) {
	t.Parallel()

	run := newRunner()

	for _, testCase := range []struct {
		name  string
		isNil bool
	}{
		{name: fieldOpenFB, isNil: run.openFB == nil},
		{name: fieldOpenTerm, isNil: run.openTerm == nil},
		{name: fieldGetenv, isNil: run.getenv == nil},
		{name: fieldGOOS, isNil: run.goos == ""},
		{name: fieldStderr, isNil: run.stderr == nil},
		{name: fieldEnterGraphics, isNil: run.enterGraphics == nil},
		{name: fieldMakeRawStdin, isNil: run.makeRawStdin == nil},
		{name: fieldReadRotation, isNil: run.readRotation == nil},
		{name: fieldNewTicker, isNil: run.newTicker == nil},
		{name: fieldCreatePNG, isNil: run.createPNG == nil},
		{name: fieldNow, isNil: run.now == nil},
		{name: fieldStdin, isNil: run.stdin == nil},
		{name: fieldLoadScenes, isNil: run.loadScenes == nil},
		{name: fieldSource, isNil: run.source == nil},
		{name: fieldScopeRange, isNil: run.scopeRange == nil},
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

//nolint:ireturn // as above: the terminal seam is the interface too.
func fakeOpenTerm(Config, backend.Kind) (backend.Backend, error) { return nil, errStub }

func fakeGetenv(string) string { return "" }

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

// fakeSceneLoader stands in for the production scene builder.
func fakeSceneLoader() ([]Drawer, error) { return []Drawer{optionOverrideDrawer{}}, nil }

// fakeBatteryReader stands in for a *battery.Status, so WithBattery's nil
// guard and its assignment can both be tested without a real poller behind
// either.
type fakeBatteryReader struct{}

func (fakeBatteryReader) GetPercentage() int8 { return 0 }

func (fakeBatteryReader) IsCharging() bool { return false }

// assertOptionReplacesOnly checks that applying an Option changed exactly
// the field named target and left every other seam at its production
// default.
func assertOptionReplacesOnly(t *testing.T, run *runner, target string) {
	t.Helper()

	for _, field := range []struct {
		name      string
		isDefault bool
	}{
		{fieldOpenFB, funcPtr(run.openFB) == funcPtr(openDevice)},
		{fieldOpenTerm, funcPtr(run.openTerm) == funcPtr(openTerminal)},
		{fieldGetenv, funcPtr(run.getenv) == funcPtr(os.Getenv)},
		{fieldGOOS, run.goos == runtime.GOOS},
		{fieldStderr, run.stderr == io.Writer(os.Stderr)},
		{fieldEnterGraphics, funcPtr(run.enterGraphics) == funcPtr(enterConsoleGraphics)},
		{fieldMakeRawStdin, funcPtr(run.makeRawStdin) == funcPtr(rawStdin)},
		{fieldReadRotation, funcPtr(run.readRotation) == funcPtr(rotate.FromSysfs)},
		{fieldNewTicker, funcPtr(run.newTicker) == funcPtr(realTicker)},
		{fieldCreatePNG, funcPtr(run.createPNG) == funcPtr(createFile)},
		{fieldNow, funcPtr(run.now) == funcPtr(time.Now)},
		{fieldStdin, isTermReader(run.stdin)},
		{fieldLoadScenes, funcPtr(run.loadScenes) == funcPtr(run.defaultScenes)},
		{fieldSource, run.source == source.Source(source.Empty{})},
		{fieldScopeRange, run.scopeRange.GetCurrent() != overrideRangeNm},
		{fieldBattery, run.battery == nil},
		{fieldShoreSet, run.shoreSet == nil},
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
		{name: "WithTerminal", option: WithTerminal(fakeOpenTerm), target: fieldOpenTerm},
		{name: "WithEnv", option: WithEnv(fakeGetenv), target: fieldGetenv},
		{name: "WithGOOS", option: WithGOOS("uscope-test-os"), target: fieldGOOS},
		{name: "WithStderr", option: WithStderr(io.Discard), target: fieldStderr},
		{name: "WithConsoleSwitch", option: WithConsoleSwitch(fakeEnterGraphics), target: fieldEnterGraphics},
		{name: "WithRawMode", option: WithRawMode(fakeMakeRawStdin), target: fieldMakeRawStdin},
		{name: "WithRotationReader", option: WithRotationReader(fakeReadRotation), target: fieldReadRotation},
		{name: "WithTicker", option: WithTicker(fakeNewTicker), target: fieldNewTicker},
		{name: "WithPNGCreator", option: WithPNGCreator(fakeCreatePNG), target: fieldCreatePNG},
		{name: "WithClock", option: WithClock(fakeNow), target: fieldNow},
		{name: "WithInput", option: WithInput(optionOverrideReader{}), target: fieldStdin},
		{name: "WithScenes", option: WithScenes(optionOverrideDrawer{}), target: fieldLoadScenes},
		{name: "WithSceneLoader", option: WithSceneLoader(fakeSceneLoader), target: fieldLoadScenes},
		{name: "WithSource", option: WithSource(optionOverrideSource{}), target: fieldSource},
		{
			name:   "WithScopeRange",
			option: WithScopeRange(scope.New(scope.WithCurrent(overrideRangeNm))),
			target: fieldScopeRange,
		},
		{name: "WithBattery", option: WithBattery(fakeBatteryReader{}), target: fieldBattery},
		{name: "WithShore", option: WithShore(&shore.Set{}), target: fieldShoreSet},
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

func TestDefaultScenes(t *testing.T) {
	t.Parallel()

	scenes, err := newRunner().defaultScenes()
	if err != nil {
		t.Fatalf("defaultScenes() = %v, want the production scene set", err)
	}

	if len(scenes) != wantSceneCount {
		t.Fatalf("defaultScenes() returned %d scenes, want %d", len(scenes), wantSceneCount)
	}

	for index, scene := range scenes {
		if scene == nil {
			t.Errorf("defaultScenes()[%d] = nil, want a Drawer", index)
		}
	}
}

// --- the framebuffer adapter ---------------------------------------------

// stubBlitter is a Blitter that records what it was handed. The external
// tests have a channel-based double for watching a running loop; this one is
// for calling the adapter directly.
type stubBlitter struct {
	width    int
	height   int
	rot      rotate.Rotation
	blitErr  error
	closeErr error
	closed   bool
}

func (s *stubBlitter) Blit(_ *image.RGBA, rot rotate.Rotation) error {
	s.rot = rot

	return s.blitErr
}

func (s *stubBlitter) Close() error {
	s.closed = true

	return s.closeErr
}

func (s *stubBlitter) Width() int      { return s.width }
func (s *stubBlitter) Height() int     { return s.height }
func (*stubBlitter) BitsPerPixel() int { return 16 }
func (*stubBlitter) Stride() int       { return 0 }
func (*stubBlitter) String() string    { return "stub" }

// TestFBBackendSize checks the adapter swaps the axes for the two quarter
// turns and leaves them alone for the other two, because that is the whole
// reason the rotation is bound at open rather than passed per frame.
func TestFBBackendSize(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name          string
		rot           rotate.Rotation
		width, height int
	}{
		{name: "upright", rot: rotate.None, width: 720, height: 1280},
		{name: "clockwise", rot: rotate.Clockwise, width: 1280, height: 720},
		{name: "upside down", rot: rotate.UpsideDown, width: 720, height: 1280},
		{name: "counter-clockwise", rot: rotate.CounterClockwise, width: 1280, height: 720},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			back := &fbBackend{dev: &stubBlitter{width: 720, height: 1280}, rot: testCase.rot}

			width, height := back.Size()
			if width != testCase.width || height != testCase.height {
				t.Errorf("Size() = %dx%d, want %dx%d", width, height, testCase.width, testCase.height)
			}
		})
	}
}

func TestFBBackendBlit(t *testing.T) {
	t.Parallel()

	t.Run("passes the bound rotation through", func(t *testing.T) {
		t.Parallel()

		dev := &stubBlitter{width: 10, height: 10}
		back := &fbBackend{dev: dev, rot: rotate.CounterClockwise}

		if err := back.Blit(image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
			t.Fatalf("Blit: %v", err)
		}

		if dev.rot != rotate.CounterClockwise {
			t.Errorf("device got rotation %v, want %v", dev.rot, rotate.CounterClockwise)
		}
	})

	t.Run("wraps a device failure", func(t *testing.T) {
		t.Parallel()

		back := &fbBackend{dev: &stubBlitter{width: 10, height: 10, blitErr: errStub}, rot: rotate.None}

		if err := back.Blit(image.NewRGBA(image.Rect(0, 0, 10, 10))); !errors.Is(err, errStub) {
			t.Errorf("err = %v, want wrapping %v", err, errStub)
		}
	})
}

func TestFBBackendClose(t *testing.T) {
	t.Parallel()

	t.Run("closes the device", func(t *testing.T) {
		t.Parallel()

		dev := &stubBlitter{}

		if err := (&fbBackend{dev: dev}).Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		if !dev.closed {
			t.Error("the device was not closed")
		}
	})

	t.Run("wraps a device failure", func(t *testing.T) {
		t.Parallel()

		if err := (&fbBackend{dev: &stubBlitter{closeErr: errStub}}).Close(); !errors.Is(err, errStub) {
			t.Errorf("err = %v, want wrapping %v", err, errStub)
		}
	})
}

// TestOpenTerminal exercises the production terminal builder on whatever
// machine the tests are running on.
//
// TestPattern is set in every case on purpose: it turns the alternate screen
// off, so running the test binary straight from a terminal cannot clear the
// operator's screen. `go test` hands the binary a pipe anyway, which is the
// path this covers: no window to measure, so the backend falls back to an
// assumed grid and still works.
//
// Coverage note: the error return needs termbackend.Open to fail, which
// takes a terminal that refuses a write. That is the same kind of shortfall
// as openDevice above, and pkg/termbackend's own tests cover the failure
// with a writer that errors.
func TestOpenTerminal(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		kind backend.Kind
		size image.Point
	}{
		{name: "blocks", kind: backend.Blocks},
		{name: "kitty with the default canvas", kind: backend.Kitty},
		{name: "kitty with an explicit canvas", kind: backend.Kitty, size: image.Pt(320, 240)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dev, err := openTerminal(Config{TestPattern: true, Size: testCase.size}, testCase.kind)
			if err != nil {
				t.Fatalf("openTerminal: %v", err)
			}

			t.Cleanup(func() { _ = dev.Close() })

			width, height := dev.Size()
			if width < 1 || height < 1 {
				t.Errorf("Size() = %dx%d, want something drawable", width, height)
			}

			if testCase.size != (image.Point{}) && (width != testCase.size.X || height != testCase.size.Y) {
				t.Errorf("Size() = %dx%d, want the requested %v", width, height, testCase.size)
			}
		})
	}
}

// --- scene selection ------------------------------------------------------

// failingFontLoader is a fontLoader that never produces a font, so a test can
// drop it into any of buildScenes' four positions.
func failingFontLoader() (*psf.Font, error) { return nil, errStub }

func TestParseScene(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		text    string
		want    SceneKind
		wantErr bool
	}{
		{name: sceneRadar, text: sceneRadar, want: Radar},
		{name: scenePattern, text: scenePattern, want: Pattern},
		{name: "specimen is no longer a scene", text: "specimen", wantErr: true},
		{name: "unknown name", text: "waterfall", wantErr: true},
		{name: "empty", text: "", wantErr: true},
		{name: "wrong case", text: "Pattern", wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseScene(testCase.text)

			if testCase.wantErr {
				if !errors.Is(err, ErrScene) {
					t.Fatalf("ParseScene(%q) error = %v, want ErrScene", testCase.text, err)
				}

				// A rejected value still has to come back as the default
				// rather than as whatever the switch happened to leave, so
				// a caller that ignores the error draws something sane.
				if got != Radar {
					t.Errorf("ParseScene(%q) = %v on error, want %v", testCase.text, got, Radar)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseScene(%q) = %v, want no error", testCase.text, err)
			}

			if got != testCase.want {
				t.Errorf("ParseScene(%q) = %v, want %v", testCase.text, got, testCase.want)
			}
		})
	}
}

func TestSceneKindString(t *testing.T) {
	t.Parallel()

	// outOfRange is past the last scene, which is what a corrupted or
	// hand-built value looks like.
	const outOfRange SceneKind = 99

	for _, testCase := range []struct {
		name string
		kind SceneKind
		want string
	}{
		{name: sceneRadar, kind: Radar, want: sceneRadar},
		{name: scenePattern, kind: Pattern, want: scenePattern},
		{name: "out of range", kind: outOfRange, want: "invalid"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.kind.String(); got != testCase.want {
				t.Errorf("SceneKind(%d).String() = %q, want %q", testCase.kind, got, testCase.want)
			}
		})
	}
}

func TestSceneKindRoundTrips(t *testing.T) {
	t.Parallel()

	for _, kind := range [...]SceneKind{Pattern} {
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()

			got, err := ParseScene(kind.String())
			if err != nil {
				t.Fatalf("ParseScene(%q): %v", kind.String(), err)
			}

			if got != kind {
				t.Errorf("ParseScene(%q) = %v, want %v", kind.String(), got, kind)
			}
		})
	}
}

func TestBuildScenesFontFailure(t *testing.T) {
	t.Parallel()

	// One case per position, because each one is a separate error return
	// with its own message naming the face that would not load.
	for index, name := range [...]string{"small", "body", "bold", "large"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			loaders := [...]fontLoader{fonts.Small, fonts.Body, fonts.BodyBold, fonts.Large}
			loaders[index] = failingFontLoader

			scenes, err := buildScenes(loaders[0], loaders[1], loaders[2], loaders[3], sceneDeps{})
			if !errors.Is(err, errStub) {
				t.Fatalf("buildScenes with a failing %s loader = %v, want the loader's error", name, err)
			}

			if scenes != nil {
				t.Errorf("buildScenes returned %d scenes alongside an error, want none", len(scenes))
			}

			if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q does not name the face that failed (%q)", err, name)
			}
		})
	}
}

func TestBuildScenesSucceeds(t *testing.T) {
	t.Parallel()

	scenes, err := buildScenes(fonts.Small, fonts.Body, fonts.BodyBold, fonts.Large,
		sceneDeps{source: source.Empty{}, scopeRange: scope.New()})
	if err != nil {
		t.Fatalf("buildScenes: %v", err)
	}

	if len(scenes) != wantSceneCount {
		t.Fatalf("buildScenes returned %d scenes, want %d", len(scenes), wantSceneCount)
	}
}

// plainSceneName is the name every markerDrawer test scene gets when the
// point is that it does not implement Themed or Configured and so must be
// left alone by a fan-out.
const plainSceneName = "plain"

// markerDrawer is a Drawer whose identity a test can check, which an empty
// struct's would not be.
type markerDrawer struct {
	name string
}

func (*markerDrawer) Draw(*canvas.Canvas, time.Duration) {}

// themedMarker is a Drawer that also implements Themed, recording every
// palette it is handed. It stands in for the radar scene, which is what lets
// cycleTheme's fan-out be tested without loading a font.
type themedMarker struct {
	palettes []theme.Palette
}

func (*themedMarker) Draw(*canvas.Canvas, time.Duration) {}

func (m *themedMarker) SetPalette(pal theme.Palette) { m.palettes = append(m.palettes, pal) }

// TestSessionCycleTheme checks the l key's fan-out directly: every scene that
// implements Themed gets the new palette, a plain Drawer is left alone, and
// two cycles land back where they started.
func TestSessionCycleTheme(t *testing.T) {
	t.Parallel()

	themed := &themedMarker{}
	plain := &markerDrawer{name: plainSceneName}

	ses := &session{scenes: []Drawer{themed, plain}, themeKind: theme.KindNight}

	ses.cycleTheme()

	if ses.themeKind != theme.KindDay {
		t.Errorf("themeKind after one cycle = %v, want %v", ses.themeKind, theme.KindDay)
	}

	if len(themed.palettes) != 1 || themed.palettes[0] != theme.Day {
		t.Errorf("SetPalette calls = %v, want exactly one call with theme.Day", themed.palettes)
	}

	ses.cycleTheme()

	if ses.themeKind != theme.KindNight {
		t.Errorf("themeKind after two cycles = %v, want %v", ses.themeKind, theme.KindNight)
	}

	if len(themed.palettes) != 2 || themed.palettes[1] != theme.Night {
		t.Errorf("SetPalette calls = %v, want a second call with theme.Night", themed.palettes)
	}
}

// TestSessionCycleLook checks the k key's fan-out directly: every scene that
// implements Themed gets the new palette, a plain Drawer is left alone, and
// three cycles land back on glass. Missing a scene here means switching to it
// after pressing k still shows the look it had before the cycle.
func TestSessionCycleLook(t *testing.T) {
	t.Parallel()

	themed := &themedMarker{}
	plain := &markerDrawer{name: plainSceneName}

	ses := &session{scenes: []Drawer{themed, plain}, look: theme.LookGlass}

	ses.cycleLook()

	if ses.look != theme.LookPhosphor {
		t.Errorf("look after one cycle = %v, want %v", ses.look, theme.LookPhosphor)
	}

	if len(themed.palettes) != 1 || themed.palettes[0] != theme.PhosphorNight {
		t.Errorf("SetPalette calls = %v, want exactly one call with theme.PhosphorNight", themed.palettes)
	}

	ses.cycleLook()

	if ses.look != theme.LookMono {
		t.Errorf("look after two cycles = %v, want %v", ses.look, theme.LookMono)
	}

	if len(themed.palettes) != 2 || themed.palettes[1] != theme.MonoNight {
		t.Errorf("SetPalette calls = %v, want a second call with theme.MonoNight", themed.palettes)
	}

	ses.cycleLook()

	if ses.look != theme.LookGlass {
		t.Errorf("look after three cycles = %v, want %v", ses.look, theme.LookGlass)
	}

	if len(themed.palettes) != 3 || themed.palettes[2] != theme.Night {
		t.Errorf("SetPalette calls = %v, want a third call with theme.Night", themed.palettes)
	}
}

// TestSessionCyclesLeaveTheOtherAxisAlone checks that k moves only the look
// and l moves only the theme. If cycleLook touched themeKind, pressing k at
// night would silently jump to a day page; if cycleTheme touched look,
// pressing l would silently change which look is on screen instead of just
// its time of day.
func TestSessionCyclesLeaveTheOtherAxisAlone(t *testing.T) {
	t.Parallel()

	t.Run("k does not move the theme", func(t *testing.T) {
		t.Parallel()

		themed := &themedMarker{}
		ses := &session{scenes: []Drawer{themed}, themeKind: theme.KindNight, look: theme.LookGlass}

		ses.cycleLook()

		if ses.themeKind != theme.KindNight {
			t.Errorf("themeKind after cycleLook = %v, want it left at %v", ses.themeKind, theme.KindNight)
		}
	})

	t.Run("l does not move the look", func(t *testing.T) {
		t.Parallel()

		themed := &themedMarker{}
		ses := &session{scenes: []Drawer{themed}, themeKind: theme.KindNight, look: theme.LookPhosphor}

		ses.cycleTheme()

		if ses.look != theme.LookPhosphor {
			t.Errorf("look after cycleTheme = %v, want it left at %v", ses.look, theme.LookPhosphor)
		}

		if len(themed.palettes) != 1 || themed.palettes[0] != theme.PhosphorDay {
			t.Errorf("SetPalette calls = %v, want exactly one call with theme.PhosphorDay", themed.palettes)
		}
	})
}

// dispatchFixture builds a session with one Themed scene, so a dispatch test
// can check the cases that mutate the session (cmdNextTheme, cmdNextLook)
// alongside the ones that only affect control flow (cmdQuit, cmdNone). It
// returns a fresh session on every call so parallel subtests do not share one.
func dispatchFixture() (*session, *themedMarker) {
	themed := &themedMarker{}

	return &session{scenes: []Drawer{themed}, themeKind: theme.KindNight, look: theme.LookGlass}, themed
}

// dispatchCase is one TestDispatch table row. wantPalette is only meaningful
// when wantRepaint is true; cmdQuit and cmdNone never repaint.
type dispatchCase struct {
	name          string
	action        command
	wantStop      bool
	wantThemeKind theme.Kind
	wantLook      theme.Look
	wantRepaint   bool
	wantPalette   theme.Palette
}

// dispatchCases covers every branch dispatch's switch has: the one that ends
// the loop, the two that each drive one cycle and leave the other axis alone,
// and the one that does nothing at all.
func dispatchCases() []dispatchCase {
	return []dispatchCase{
		{
			name:          "cmdQuit stops the loop without repainting",
			action:        cmdQuit,
			wantStop:      true,
			wantThemeKind: theme.KindNight,
			wantLook:      theme.LookGlass,
		},
		{
			name:          "cmdNextTheme cycles the theme, leaves the look alone, and keeps running",
			action:        cmdNextTheme,
			wantThemeKind: theme.KindDay,
			wantLook:      theme.LookGlass,
			wantRepaint:   true,
			wantPalette:   theme.Day,
		},
		{
			name:          "cmdNextLook cycles the look, leaves the theme alone, and keeps running",
			action:        cmdNextLook,
			wantThemeKind: theme.KindNight,
			wantLook:      theme.LookPhosphor,
			wantRepaint:   true,
			wantPalette:   theme.PhosphorNight,
		},
		{
			name:          "cmdNone leaves the session untouched and keeps running",
			action:        cmdNone,
			wantThemeKind: theme.KindNight,
			wantLook:      theme.LookGlass,
		},
	}
}

// checkDispatchRepaint asserts one dispatchCase's expectation about whether
// the session repainted: no SetPalette call at all, or exactly one carrying
// the palette the case names.
func checkDispatchRepaint(t *testing.T, themed *themedMarker, testCase dispatchCase) {
	t.Helper()

	if !testCase.wantRepaint {
		if len(themed.palettes) != 0 {
			t.Errorf("SetPalette calls = %v, want none", themed.palettes)
		}

		return
	}

	if len(themed.palettes) != 1 || themed.palettes[0] != testCase.wantPalette {
		t.Errorf("SetPalette calls = %v, want exactly one call with %v", themed.palettes, testCase.wantPalette)
	}
}

// TestDispatch checks the switch that turns a classified command into a
// session action: cmdQuit stops the loop without touching the session,
// cmdNextTheme and cmdNextLook each drive their own cycle and keep the loop
// running, and cmdNone touches nothing. Swapping two cases here would make q
// merely change colour instead of quitting, or make l or k silently end the
// program.
func TestDispatch(t *testing.T) {
	t.Parallel()

	for _, testCase := range dispatchCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ses, themed := dispatchFixture()

			if stop := dispatch(ses, testCase.action); stop != testCase.wantStop {
				t.Errorf("dispatch(%d) = %v, want %v", testCase.action, stop, testCase.wantStop)
			}

			if ses.themeKind != testCase.wantThemeKind {
				t.Errorf("themeKind = %v, want %v", ses.themeKind, testCase.wantThemeKind)
			}

			if ses.look != testCase.wantLook {
				t.Errorf("look = %v, want %v", ses.look, testCase.wantLook)
			}

			checkDispatchRepaint(t, themed, testCase)
		})
	}
}

// configuredMarker is a Drawer that also implements Configured, recording
// every settings block it is handed. It stands in for the radar scene, the
// only one with settings of its own, which is what lets applySettings' fan-
// out be tested without loading a font.
type configuredMarker struct {
	settings []radar.Settings
}

func (*configuredMarker) Draw(*canvas.Canvas, time.Duration) {}

func (m *configuredMarker) Apply(set radar.Settings) { m.settings = append(m.settings, set) }

// TestApplySettings checks applySettings' fan-out directly, the same
// property TestSessionCycleTheme pins for applyPalette: every scene that
// implements Configured gets the settings block it was called with, and a
// plain Drawer, such as the orientation pattern, is left alone.
func TestApplySettings(t *testing.T) {
	t.Parallel()

	configured := &configuredMarker{}
	plain := &markerDrawer{name: plainSceneName}

	want := radar.Settings{Colour: radar.ColourAirline, Airports: radar.ToggleOff}

	applySettings([]Drawer{configured, plain}, want)

	if len(configured.settings) != 1 || configured.settings[0] != want {
		t.Errorf("Apply calls = %v, want exactly one call with %v", configured.settings, want)
	}
}

// TestNilOptionsKeepTheirDefaults covers the guards on the two options that
// take something a caller could reasonably pass as nil. An option handed a
// value it cannot use leaves the default alone rather than half-configuring
// the runner.
func TestNilOptionsKeepTheirDefaults(t *testing.T) {
	t.Parallel()

	t.Run("WithSource(nil)", func(t *testing.T) {
		t.Parallel()

		if got := newRunner(WithSource(nil)).source; got != source.Source(source.Empty{}) {
			t.Errorf("source = %#v, want the empty default", got)
		}
	})

	t.Run("WithScopeRange(nil)", func(t *testing.T) {
		t.Parallel()

		if got := newRunner(WithScopeRange(nil)).scopeRange; got == nil {
			t.Error("scopeRange = nil, want the default")
		}
	})

	t.Run("WithBattery(nil)", func(t *testing.T) {
		t.Parallel()

		if got := newRunner(WithBattery(nil)).battery; got != nil {
			t.Errorf("battery = %#v, want nil (a machine with no battery, not a caller mistake)", got)
		}
	})
}

// TestBuildScenesFillsInWhatItWasNotGiven covers the two stand-ins. A caller
// that only wants the pattern scene should not have to supply a receiver and a
// range control it will never read.
func TestBuildScenesFillsInWhatItWasNotGiven(t *testing.T) {
	t.Parallel()

	scenes, err := buildScenes(fonts.Small, fonts.Body, fonts.BodyBold, fonts.Large, sceneDeps{})
	if err != nil {
		t.Fatalf("buildScenes with no source and no range: %v", err)
	}

	if len(scenes) != wantSceneCount {
		t.Fatalf("buildScenes returned %d scenes, want %d", len(scenes), wantSceneCount)
	}

	// Drawing proves the stand-ins are usable rather than merely non-nil.
	canv, err := canvas.New(64, 48)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	for index, scene := range scenes {
		if scene == nil {
			t.Fatalf("scene %d is nil", index)
		}

		scene.Draw(canv, 0)
	}
}

// isTermReader reports whether stdin is the production reader, which wraps
// the terminal's file descriptor rather than os.Stdin itself.
func isTermReader(src io.Reader) bool {
	_, ok := src.(*term.Reader)

	return ok
}

// --- the bias-tee toggle ---------------------------------------------------

// biasTeeTestTimeout bounds every blocking wait in the tests below, so a
// regression that deadlocks the guard fails the test instead of hanging the
// job.
const biasTeeTestTimeout = 3 * time.Second

// biasStderr is a goroutine-safe io.Writer. The toggler writes its warnings
// from the worker goroutine that runs flip, and the tests below read them
// back from the goroutine that called wait, so a plain bytes.Buffer would be
// a race.
type biasStderr struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *biasStderr) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n, err := b.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("biasStderr: %w", err)
	}

	return n, nil
}

func (b *biasStderr) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// errBiasTeeSet is what fakeBiasSource's SetBiasTee reports when a test wants
// a failing flip. It is a sentinel because err113 wants a static error, and
// the tests only check that the message reaches stderr, never the identity.
//
//nolint:gochecknoglobals // error sentinel, not state.
var errBiasTeeSet = errors.New("bias-tee: set failed")

// fakeBiasSource is a biasTeeSource, and a full source.Source besides, whose
// cached state and SetBiasTee outcome a test controls directly. The call
// count and the last argument SetBiasTee ran with are read from a different
// goroutine than the one that calls Toggle, so both live behind a mutex and
// come back through accessors rather than being read off the struct.
type fakeBiasSource struct {
	mu sync.Mutex

	supported bool
	enabled   bool
	err       error
	panicWith any

	calls   int
	lastArg bool
}

//nolint:nonamedreturns // mirrors the interface it satisfies.
func (f *fakeBiasSource) BiasTee() (supported, enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.supported, f.enabled
}

func (f *fakeBiasSource) SetBiasTee(enable bool) error {
	f.mu.Lock()
	f.calls++
	f.lastArg = enable
	err := f.err
	panicWith := f.panicWith
	f.mu.Unlock()

	if panicWith != nil {
		panic(panicWith)
	}

	return err
}

// Frame stands in for a real receiver's frame. Nothing in these tests reads
// it; it exists so fakeBiasSource can be handed to WithSource, which wants a
// full source.Source rather than the two bias-tee methods alone.
func (*fakeBiasSource) Frame() source.Frame { return source.Frame{} }

// Close is a no-op: nothing here holds a real device.
func (*fakeBiasSource) Close() error { return nil }

// callCount reports how many times SetBiasTee has run so far.
func (f *fakeBiasSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// lastEnable reports the enable value SetBiasTee was last called with.
func (f *fakeBiasSource) lastEnable() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.lastArg
}

// TestBiasTogglerToggle checks the read-modify-write at the centre of a
// press: the target state comes from the cached read BiasTee returns, not a
// live poll, so a source reporting itself off is asked to turn on and one
// reporting itself on is asked to turn off.
func TestBiasTogglerToggle(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		enabled    bool
		wantEnable bool
	}{
		{name: "off asks to turn on", enabled: false, wantEnable: true},
		{name: "on asks to turn off", enabled: true, wantEnable: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			src := &fakeBiasSource{supported: true, enabled: testCase.enabled}
			toggler := newBiasToggler(src, io.Discard)

			toggler.Toggle()
			toggler.wait()

			if got := src.callCount(); got != 1 {
				t.Fatalf("SetBiasTee called %d times, want 1", got)
			}

			if got := src.lastEnable(); got != testCase.wantEnable {
				t.Errorf("SetBiasTee called with %v, want %v", got, testCase.wantEnable)
			}
		})
	}
}

// blockingBiasSource is a biasTeeSource, and a full source.Source besides,
// whose SetBiasTee blocks until the test releases it. It backs two tests:
// that a second press while one is in flight is dropped, and that Run waits
// for an outstanding flip before it returns.
type blockingBiasSource struct {
	mu    sync.Mutex
	calls int

	// started closes the moment SetBiasTee begins, which is how a test
	// knows the flip has actually reached the device call rather than
	// merely been requested.
	started   chan struct{}
	startOnce sync.Once
	release   <-chan struct{}
}

// newBlockingBiasSource returns a source whose SetBiasTee call blocks until
// release is closed.
func newBlockingBiasSource(release <-chan struct{}) *blockingBiasSource {
	return &blockingBiasSource{started: make(chan struct{}), release: release}
}

//nolint:nonamedreturns // mirrors the interface it satisfies.
func (*blockingBiasSource) BiasTee() (supported, enabled bool) { return true, false }

func (b *blockingBiasSource) SetBiasTee(bool) error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()

	b.startOnce.Do(func() { close(b.started) })

	<-b.release

	return nil
}

// Frame reports a bias-tee-capable receiver with nothing else on it, which is
// all TestRunWaitsForAnOutstandingBiasTeeToggle needs from a frame.
func (*blockingBiasSource) Frame() source.Frame {
	return source.Frame{BiasTee: source.BiasTeeState{Supported: true}}
}

// Close is a no-op: nothing here holds a real device.
func (*blockingBiasSource) Close() error { return nil }

// callCount reports how many times SetBiasTee has run so far.
func (b *blockingBiasSource) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.calls
}

// TestBiasTogglerDropsAPressWhileOneIsInFlight is the case the guard exists
// for. A bias-tee flip is a GPIO line, not a queue: a press that arrives
// while the first one is still in the air is dropped, not run once the first
// one finishes. A third press, after the first has completed, proves the
// guard was cleared rather than left stuck.
func TestBiasTogglerDropsAPressWhileOneIsInFlight(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	src := newBlockingBiasSource(release)
	toggler := newBiasToggler(src, io.Discard)

	toggler.Toggle()

	select {
	case <-src.started:
	case <-time.After(biasTeeTestTimeout):
		t.Fatal("timed out waiting for the first flip to start")
	}

	toggler.Toggle() // dropped: the guard is still held by the first press.

	close(release)
	toggler.wait()

	if got := src.callCount(); got != 1 {
		t.Fatalf("SetBiasTee called %d times while one was in flight, want 1", got)
	}

	toggler.Toggle()
	toggler.wait()

	if got := src.callCount(); got != 2 {
		t.Errorf("SetBiasTee called %d times after the guard cleared, want 2", got)
	}
}

// TestBiasTogglerUnsupportedSourceWarns checks that a source with no
// bias-tee gets a warning on stderr and is never asked to set one.
func TestBiasTogglerUnsupportedSourceWarns(t *testing.T) {
	t.Parallel()

	src := &fakeBiasSource{supported: false}

	var stderr biasStderr

	toggler := newBiasToggler(src, &stderr)

	toggler.Toggle()
	toggler.wait()

	if got := src.callCount(); got != 0 {
		t.Errorf("SetBiasTee called %d times for an unsupported source, want 0", got)
	}

	if stderr.String() == "" {
		t.Error("stderr is empty, want a warning about the missing bias-tee")
	}
}

// TestBiasTogglerSetFailureIsLoggedAndSwallowed checks that a failing
// SetBiasTee does not stop the toggler: wait returns normally, the failure
// reaches stderr, and the guard is clear so the next press still runs.
func TestBiasTogglerSetFailureIsLoggedAndSwallowed(t *testing.T) {
	t.Parallel()

	src := &fakeBiasSource{supported: true, err: errBiasTeeSet}

	var stderr biasStderr

	toggler := newBiasToggler(src, &stderr)

	toggler.Toggle()
	toggler.wait()

	if got := src.callCount(); got != 1 {
		t.Fatalf("SetBiasTee called %d times, want 1", got)
	}

	if !strings.Contains(stderr.String(), errBiasTeeSet.Error()) {
		t.Errorf("stderr = %q, want it to mention %q", stderr.String(), errBiasTeeSet)
	}

	toggler.Toggle()
	toggler.wait()

	if got := src.callCount(); got != 2 {
		t.Errorf("SetBiasTee called %d times after a failing press, want 2 (the guard should have cleared)", got)
	}
}

// TestBiasTogglerRecoversAPanickingSource checks that a source whose
// SetBiasTee panics does not take the test down with it: the panic is
// recovered, its message reaches stderr, and the guard clears so the next
// press runs.
func TestBiasTogglerRecoversAPanickingSource(t *testing.T) {
	t.Parallel()

	src := &fakeBiasSource{supported: true, panicWith: "dongle unplugged"}

	var stderr biasStderr

	toggler := newBiasToggler(src, &stderr)

	toggler.Toggle()
	toggler.wait()

	if got := src.callCount(); got != 1 {
		t.Fatalf("SetBiasTee called %d times, want 1", got)
	}

	if !strings.Contains(stderr.String(), "dongle unplugged") {
		t.Errorf("stderr = %q, want it to mention the panic value", stderr.String())
	}

	toggler.Toggle()
	toggler.wait()

	if got := src.callCount(); got != 2 {
		t.Errorf("SetBiasTee called %d times after a panicking press, want 2 (the guard should have cleared)", got)
	}
}

// TestNewRunnerBuildsBiasTeeOverTheAppliedOptions pins that the toggler is
// built last: it must see the source and the stderr as WithSource and
// WithStderr left them, not the production defaults newRunner starts from.
func TestNewRunnerBuildsBiasTeeOverTheAppliedOptions(t *testing.T) {
	t.Parallel()

	src := &fakeBiasSource{supported: true}

	var stderr biasStderr

	run := newRunner(WithSource(src), WithStderr(&stderr))

	if run.biasTee == nil {
		t.Fatal("run.biasTee = nil, want a toggler built over the replaced source and stderr")
	}

	run.biasTee.Toggle()
	run.biasTee.wait()

	if got := src.callCount(); got != 1 {
		t.Errorf("SetBiasTee called %d times, want 1 (the toggler should be wired to the replaced source)", got)
	}

	src.supported = false

	run.biasTee.Toggle()
	run.biasTee.wait()

	if stderr.String() == "" {
		t.Error("stderr is empty, want the toggler to warn through the replaced writer")
	}
}

// signalBlitter is a Blitter that closes ready the first time it is blitted
// to. TestRunWaitsForAnOutstandingBiasTeeToggle needs to know a frame has
// been drawn before it presses b, because drawing is what fills in the radar
// scene's cached bias-tee state.
type signalBlitter struct {
	ready         chan struct{}
	once          sync.Once
	width, height int
}

func newSignalBlitter(width, height int) *signalBlitter {
	return &signalBlitter{ready: make(chan struct{}), width: width, height: height}
}

func (s *signalBlitter) Blit(*image.RGBA, rotate.Rotation) error {
	s.once.Do(func() { close(s.ready) })

	return nil
}

func (*signalBlitter) Close() error { return nil }

func (s *signalBlitter) Width() int { return s.width }

func (s *signalBlitter) Height() int { return s.height }

func (*signalBlitter) BitsPerPixel() int { return 16 }

func (*signalBlitter) Stride() int { return 0 }

func (*signalBlitter) String() string { return "signal" }

// manualTicker is a frame clock the test drives by hand: sending on tick
// makes the loop draw exactly one frame, and nothing else does.
type manualTicker struct {
	tick chan time.Time
}

func newManualTicker() *manualTicker {
	return &manualTicker{tick: make(chan time.Time, 1)}
}

func (m *manualTicker) new(time.Duration) (<-chan time.Time, func()) {
	return m.tick, func() {}
}

// biasTeeLiveLoop is the fixture TestRunWaitsForAnOutstandingBiasTeeToggle
// drives: a live loop, built with default scenes so the real radar scene and
// its b binding are in play, whose source blocks the bias-tee flip until the
// test releases it. Keys arrive over a pipe standing in for the keyboard.
type biasTeeLiveLoop struct {
	src     *blockingBiasSource
	blitter *signalBlitter
	ticker  *manualTicker
	keys    *io.PipeWriter
	release chan struct{}
	done    <-chan error
}

// startBiasTeeLiveLoop wires the fixture above and starts Run on it.
func startBiasTeeLiveLoop(t *testing.T) *biasTeeLiveLoop {
	t.Helper()

	release := make(chan struct{})
	src := newBlockingBiasSource(release)
	blitter := newSignalBlitter(64, 48)
	ticker := newManualTicker()
	pipeReader, pipeWriter := io.Pipe()
	succeed := func() (func() error, error) { return func() error { return nil }, nil }

	ctx, cancel := context.WithTimeout(t.Context(), biasTeeTestTimeout)
	t.Cleanup(cancel)

	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, Config{Backend: backend.Framebuffer}, io.Discard,
			WithFramebuffer(func(string) (Blitter, error) { return blitter, nil }),
			WithSource(src),
			WithConsoleSwitch(succeed),
			WithRawMode(succeed),
			WithInput(pipeReader),
			WithTicker(ticker.new),
		)
	}()

	return &biasTeeLiveLoop{src: src, blitter: blitter, ticker: ticker, keys: pipeWriter, release: release, done: done}
}

// pressAndWaitForFlip draws one frame, presses b, and waits for the flip to
// reach the source's SetBiasTee, so the caller knows the guard is held.
func (l *biasTeeLiveLoop) pressAndWaitForFlip(t *testing.T) {
	t.Helper()

	l.ticker.tick <- time.Now()

	select {
	case <-l.blitter.ready:
	case <-time.After(biasTeeTestTimeout):
		t.Fatal("timed out waiting for the first frame to draw")
	}

	if _, err := l.keys.Write([]byte("b")); err != nil {
		t.Fatalf("write b: %v", err)
	}

	select {
	case <-l.src.started:
	case <-time.After(biasTeeTestTimeout):
		t.Fatal("timed out waiting for the bias-tee flip to start")
	}
}

// quit presses q and closes the key pipe. Closing it is what makes the
// reader goroutine see EOF and return, which live's own group.Wait needs
// before Run can reach its own deferred wait on the bias-tee toggler.
func (l *biasTeeLiveLoop) quit(t *testing.T) {
	t.Helper()

	if _, err := l.keys.Write([]byte("q")); err != nil {
		t.Fatalf("write q: %v", err)
	}

	if err := l.keys.Close(); err != nil {
		t.Fatalf("close the key pipe: %v", err)
	}
}

// TestRunWaitsForAnOutstandingBiasTeeToggle checks the promise Run's first
// line makes: a control transfer still in the air when the loop quits must
// finish before Run hands the source back to its caller, because main closes
// the source the moment Run returns.
func TestRunWaitsForAnOutstandingBiasTeeToggle(t *testing.T) {
	t.Parallel()

	loop := startBiasTeeLiveLoop(t)

	loop.pressAndWaitForFlip(t)
	loop.quit(t)

	const notYetWindow = 300 * time.Millisecond

	select {
	case err := <-loop.done:
		t.Fatalf("Run returned (%v) with the bias-tee flip still in flight, want it to wait", err)
	case <-time.After(notYetWindow):
	}

	close(loop.release)

	select {
	case err := <-loop.done:
		if err != nil {
			t.Errorf("Run: %v, want nil", err)
		}
	case <-time.After(biasTeeTestTimeout):
		t.Fatal("timed out waiting for Run to return once the flip had finished")
	}

	if got := loop.src.callCount(); got != 1 {
		t.Errorf("SetBiasTee called %d times, want 1", got)
	}
}

// noopToggler is a Toggler that does nothing, standing in for a real
// biasToggler wherever a test only needs a distinguishable, non-nil value.
type noopToggler struct{}

func (noopToggler) Toggle() {}

// TestBuildScenesWiresBiasTeeThrough checks the other half of the stand-in
// rule TestBuildScenesFillsInWhatItWasNotGiven covers: a caller that does
// supply a toggler gets a radar scene built with it, rather than the nil
// biasTee that leaves the b key unbound.
func TestBuildScenesWiresBiasTeeThrough(t *testing.T) {
	t.Parallel()

	scenes, err := buildScenes(fonts.Small, fonts.Body, fonts.BodyBold, fonts.Large,
		sceneDeps{source: source.Empty{}, scopeRange: scope.New(), biasTee: noopToggler{}})
	if err != nil {
		t.Fatalf("buildScenes with a non-nil biasTee: %v", err)
	}

	if len(scenes) != wantSceneCount {
		t.Fatalf("buildScenes returned %d scenes, want %d", len(scenes), wantSceneCount)
	}

	canv, err := canvas.New(64, 48)
	if err != nil {
		t.Fatalf("canvas.New: %v", err)
	}

	for index, scene := range scenes {
		if scene == nil {
			t.Fatalf("scene %d is nil", index)
		}

		scene.Draw(canv, 0)
	}
}
