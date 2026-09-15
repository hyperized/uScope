package app_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/app"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/term"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/backend"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/rotate"
	"github.com/hyperized/uScope/pkg/vt"
)

// testTimeout bounds every blocking wait in this file, so a regression that
// hangs the run loop fails the test instead of the CI job.
const testTimeout = 3 * time.Second

// unusedPNGPath and fbPath are fixture values for tests that never actually
// touch the filesystem or a device: the seam under test is faked out before
// either path would be used for real.
const (
	unusedPNGPath = "unused.png"
	fbPath        = "/dev/fb0"
)

// recvOrTimeout receives one value from ch, failing the test if none arrives
// within timeout.
//
//nolint:ireturn // generic helper: T is whatever the caller's channel carries, interface or not.
func recvOrTimeout[T any](t *testing.T, ch <-chan T, timeout time.Duration, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", what)
	}

	var zero T

	return zero
}

// runAsync starts app.Run in its own goroutine and returns a channel that
// carries its result, so the test can wait on it with a bound.
func runAsync(ctx context.Context, cfg app.Config, stdout io.Writer, opts ...app.Option) <-chan error {
	done := make(chan error, 1)

	go func() {
		done <- app.Run(ctx, cfg, stdout, opts...)
	}()

	return done
}

// openFBSpy records whether the framebuffer opener was invoked, for the PNG
// path's "nothing else is touched" guarantee.
type openFBSpy struct {
	called bool
}

//nolint:ireturn // the seam under test returns the interface; the spy must match it.
func (s *openFBSpy) open(string) (app.Blitter, error) {
	s.called = true

	return nil, errStub
}

var errStub = errors.New("stub")

// --- PNG mode -----------------------------------------------------------

func TestRunPNGHappyPath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "out.png")
	cfg := app.Config{PNG: path, Size: image.Pt(64, 48)}

	var buf bytes.Buffer

	spy := &openFBSpy{}

	if err := app.Run(t.Context(), cfg, &buf, app.WithFramebuffer(spy.open)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if spy.called {
		t.Error("the framebuffer opener was called in PNG mode, want it untouched")
	}

	file, err := os.Open(path) //nolint:gosec // path is a t.TempDir() fixture, not user input.
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	img, err := png.Decode(file)
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}

	if got := img.Bounds(); got.Dx() != 64 || got.Dy() != 48 {
		t.Errorf("decoded size = %dx%d, want 64x48", got.Dx(), got.Dy())
	}

	wantLine := fmt.Sprintf("wrote %s %dx%d\n", path, 64, 48)
	if got := buf.String(); got != wantLine {
		t.Errorf("stdout = %q, want %q", got, wantLine)
	}
}

func TestRunPNGDefaultSize(t *testing.T) {
	t.Parallel()

	const (
		defaultWidth  = 1280
		defaultHeight = 720
	)

	for _, testCase := range []struct {
		name string
		size image.Point
	}{
		{name: "zero size", size: image.Point{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "out.png")
			cfg := app.Config{PNG: path, Size: testCase.size}

			var buf bytes.Buffer

			if err := app.Run(t.Context(), cfg, &buf); err != nil {
				t.Fatalf("Run: %v", err)
			}

			file, err := os.Open(path) //nolint:gosec // path is a t.TempDir() fixture.
			if err != nil {
				t.Fatalf("open %s: %v", path, err)
			}
			defer func() { _ = file.Close() }()

			img, err := png.Decode(file)
			if err != nil {
				t.Fatalf("png.Decode: %v", err)
			}

			got := img.Bounds()
			if got.Dx() != defaultWidth || got.Dy() != defaultHeight {
				t.Errorf("decoded size = %dx%d, want %dx%d", got.Dx(), got.Dy(), defaultWidth, defaultHeight)
			}
		})
	}
}

// A size with only one axis set is not a size. It reaches canvas.New, which
// is the one place that decides what a usable canvas is.
func TestRunPNGBadSize(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		size image.Point
	}{
		{name: "negative width", size: image.Pt(-5, 100)},
		{name: "negative height", size: image.Pt(100, -5)},
		{name: "zero width only", size: image.Pt(0, 100)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "out.png")
			cfg := app.Config{PNG: path, Size: testCase.size}

			var buf bytes.Buffer

			err := app.Run(t.Context(), cfg, &buf)
			if !errors.Is(err, canvas.ErrSize) {
				t.Fatalf("Run error = %v, want canvas.ErrSize", err)
			}
		})
	}
}

func TestRunPNGCreatorError(t *testing.T) {
	t.Parallel()

	cfg := app.Config{PNG: unusedPNGPath}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf, app.WithPNGCreator(func(string) (io.WriteCloser, error) {
		return nil, errStub
	}))

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

// limitedWriter accepts writes only up to limit bytes total, then fails.
// It never returns a short write, so it never breaks the io.Writer
// contract: a call either fully succeeds or is fully rejected.
type limitedWriter struct {
	limit   int
	written int
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if w.written+len(data) > w.limit {
		return 0, errStub
	}

	w.written += len(data)

	return len(data), nil
}

func (*limitedWriter) Close() error { return nil }

func TestRunPNGEncodeError(t *testing.T) {
	t.Parallel()

	const writeLimit = 8

	cfg := app.Config{PNG: unusedPNGPath, Size: image.Pt(64, 48)}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf, app.WithPNGCreator(func(string) (io.WriteCloser, error) {
		return &limitedWriter{limit: writeLimit}, nil
	}))

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

// closeFailWriter succeeds on every Write but fails on Close.
type closeFailWriter struct {
	bytes.Buffer
}

func (*closeFailWriter) Close() error { return errStub }

func TestRunPNGCloseError(t *testing.T) {
	t.Parallel()

	cfg := app.Config{PNG: unusedPNGPath, Size: image.Pt(64, 48)}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf, app.WithPNGCreator(func(string) (io.WriteCloser, error) {
		return &closeFailWriter{}, nil
	}))

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

// --- shared live/test-pattern fakes --------------------------------------

// blitCall records one Blit invocation.
type blitCall struct {
	bounds image.Rectangle
	rot    rotate.Rotation
}

// fakeBlitter is the Blitter double used by both test-pattern and live-mode
// tests. Calls are delivered on a channel so a concurrently running loop can
// be observed without a lock.
type fakeBlitter struct {
	width, height, bpp, stride int
	text                       string
	blitErr                    error
	closeErr                   error
	calls                      chan blitCall
	closed                     chan struct{}
}

func newFakeBlitter(width, height, bpp, stride int, text string) *fakeBlitter {
	return &fakeBlitter{
		width: width, height: height, bpp: bpp, stride: stride, text: text,
		calls:  make(chan blitCall, 8),
		closed: make(chan struct{}),
	}
}

func (f *fakeBlitter) Blit(img *image.RGBA, rot rotate.Rotation) error {
	f.calls <- blitCall{bounds: img.Bounds(), rot: rot}

	return f.blitErr
}

func (f *fakeBlitter) Close() error {
	close(f.closed)

	return f.closeErr
}

func (f *fakeBlitter) Width() int        { return f.width }
func (f *fakeBlitter) Height() int       { return f.height }
func (f *fakeBlitter) BitsPerPixel() int { return f.bpp }
func (f *fakeBlitter) Stride() int       { return f.stride }
func (f *fakeBlitter) String() string    { return f.text }

// drawCall records one Draw invocation.
type drawCall struct {
	bounds  image.Rectangle
	elapsed time.Duration
}

// fakeDrawer is the Drawer double.
type fakeDrawer struct {
	calls chan drawCall
}

func newFakeDrawer() *fakeDrawer {
	return &fakeDrawer{calls: make(chan drawCall, 8)}
}

func (f *fakeDrawer) Draw(dst *canvas.Canvas, elapsed time.Duration) {
	f.calls <- drawCall{bounds: dst.Bounds(), elapsed: elapsed}
}

// --- test-pattern mode ----------------------------------------------------

func TestRunTestPatternHappyPath(t *testing.T) {
	t.Parallel()

	const (
		physWidth  = 720
		physHeight = 1280
		bpp        = 16
		stride     = 1440
	)

	blitter := newFakeBlitter(physWidth, physHeight, bpp, stride, "720x1280 16bpp stride=1440")
	drawer := newFakeDrawer()

	consoleSwitch := &switchSpy{}
	rawSwitch := &switchSpy{}

	cfg := app.Config{
		FBPath:      fbPath,
		TestPattern: true,
		Rotation:    rotate.Clockwise,
		Backend:     backend.Framebuffer,
	}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf,
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScenes(drawer),
		app.WithConsoleSwitch(consoleSwitch.switchMode),
		app.WithRawMode(rawSwitch.switchMode),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	call := recvOrTimeout(t, blitter.calls, testTimeout, "Blit call")

	if call.bounds.Dx() != defaultWidth || call.bounds.Dy() != defaultHeight {
		t.Errorf("canvas handed to Blit is %dx%d, want %dx%d",
			call.bounds.Dx(), call.bounds.Dy(), defaultWidth, defaultHeight)
	}

	if call.rot != rotate.Clockwise {
		t.Errorf("Blit rotation = %v, want %v", call.rot, rotate.Clockwise)
	}

	select {
	case <-blitter.closed:
	default:
		t.Error("Close was not called")
	}

	const want = "fb=/dev/fb0 720x1280 16bpp stride=1440 rotate=1 logical=1280x720\n"
	if got := buf.String(); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}

	// Test-pattern mode must never touch console or terminal state: that is
	// what makes it safe to run over ssh, where there is no VT to switch and
	// often no terminal to put in raw mode.
	if consoleSwitch.called {
		t.Error("console switch was called in test-pattern mode, want it untouched")
	}

	if rawSwitch.called {
		t.Error("raw mode switch was called in test-pattern mode, want it untouched")
	}
}

const (
	defaultWidth  = 1280
	defaultHeight = 720
)

func TestRunTestPatternFramebufferError(t *testing.T) {
	t.Parallel()

	cfg := app.Config{FBPath: fbPath, TestPattern: true, Backend: backend.Framebuffer}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf, app.WithFramebuffer(func(string) (app.Blitter, error) {
		return nil, errStub
	}))

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

func TestRunTestPatternBlitError(t *testing.T) {
	t.Parallel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	blitter.blitErr = errStub

	cfg := app.Config{FBPath: fbPath, TestPattern: true, Backend: backend.Framebuffer}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf,
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
	)

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}

	select {
	case <-blitter.closed:
	default:
		t.Error("Close was not called after a Blit error")
	}
}

func TestRunTestPatternZeroSizeCanvasError(t *testing.T) {
	t.Parallel()

	blitter := newFakeBlitter(0, 0, 0, 0, "empty")

	cfg := app.Config{FBPath: fbPath, TestPattern: true, Backend: backend.Framebuffer}

	var buf bytes.Buffer

	err := app.Run(t.Context(), cfg, &buf,
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
	)

	if !errors.Is(err, canvas.ErrSize) {
		t.Errorf("err = %v, want wrapping %v", err, canvas.ErrSize)
	}
}

// --- live mode helpers ------------------------------------------------

// switchSpy is a WithConsoleSwitch/WithRawMode double that always succeeds
// and records that it was invoked.
type switchSpy struct {
	called bool
}

func (s *switchSpy) switchMode() (func() error, error) {
	s.called = true

	return func() error { return nil }, nil
}

// orderedRestore is a restore function that appends a label to order when
// called, optionally returning an error, for pinning restore sequencing.
func orderedRestore(order *[]string, label string, restoreErr error) func() (func() error, error) {
	return func() (func() error, error) {
		return func() error {
			*order = append(*order, label)

			return restoreErr
		}, nil
	}
}

// degradeSwitch always fails with an error wrapping sentinel.
func degradeSwitch(sentinel error) func() (func() error, error) {
	return func() (func() error, error) {
		return nil, fmt.Errorf("wrap: %w", sentinel)
	}
}

// abortSwitch always fails with err, unwrapped by any tolerated sentinel.
func abortSwitch(err error) func() (func() error, error) {
	return func() (func() error, error) {
		return nil, err
	}
}

// fakeTicker is the WithTicker double. new is called exactly once per Run,
// so interval and stopCalls need no synchronization beyond the
// happens-before edge that receiving Run's result already provides.
type fakeTicker struct {
	ch        chan time.Time
	interval  time.Duration
	stopCalls int
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ch: make(chan time.Time)}
}

func (f *fakeTicker) new(interval time.Duration) (<-chan time.Time, func()) {
	f.interval = interval

	return f.ch, func() { f.stopCalls++ }
}

// countingReader records how many times Read was called, for asserting the
// reader goroutine never ran when raw mode degraded.
type countingReader struct {
	calls int
}

func (r *countingReader) Read([]byte) (int, error) {
	r.calls++

	return 0, io.EOF
}

// idleReader always reports "nothing yet, no error", the shape of a
// raw-mode read that timed out. It never blocks, so it stays responsive to
// context cancellation the same way the real reader is.
type idleReader struct{}

func (idleReader) Read([]byte) (int, error) { return 0, nil }

// onceReader hands back data on the first Read and reports a clean EOF on
// every call after that.
type onceReader struct {
	data []byte
	done bool
}

func (r *onceReader) Read(buf []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}

	r.done = true

	return copy(buf, r.data), nil
}

// stagedReader hands back stage1 on the first Read, then waits for advance
// to be closed before handing back stage2 on the second Read. Every Read
// after that reports a clean EOF.
type stagedReader struct {
	stage1  []byte
	stage2  []byte
	advance chan struct{}
	step    int
}

func (r *stagedReader) Read(buf []byte) (int, error) {
	switch r.step {
	case 0:
		r.step++

		return copy(buf, r.stage1), nil
	case 1:
		<-r.advance

		r.step++

		return copy(buf, r.stage2), nil
	default:
		return 0, io.EOF
	}
}

// liveConfig is the shared Config for live-mode tests: a square canvas
// keeps rotation out of the way of tests that are not about geometry.
func liveConfig(fps int) app.Config {
	return app.Config{FBPath: fbPath, FPS: fps, Backend: backend.Framebuffer}
}

// --- live mode: quit keys -------------------------------------------------

func TestRunLiveQuitKeys(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		data []byte
	}{
		{name: "lowercase q", data: []byte{'q'}},
		{name: "uppercase Q", data: []byte{'Q'}},
		{name: "ctrl-c", data: []byte{0x03}},
		{name: "esc", data: []byte{0x1b}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
			defer cancel()

			blitter := newFakeBlitter(100, 100, 16, 200, "fake")
			drawer := newFakeDrawer()
			ticker := newFakeTicker()

			done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
				app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
				app.WithScenes(drawer),
				app.WithConsoleSwitch((&switchSpy{}).switchMode),
				app.WithRawMode((&switchSpy{}).switchMode),
				app.WithInput(&onceReader{data: testCase.data}),
				app.WithTicker(ticker.new),
			)

			err := recvOrTimeout(t, done, testTimeout, "Run to return after a quit key")
			if err != nil {
				t.Errorf("Run: %v, want nil", err)
			}

			if ticker.stopCalls != 1 {
				t.Errorf("ticker stop calls = %d, want 1", ticker.stopCalls)
			}
		})
	}
}

// TestRunLiveNonQuitKeyContinues checks that an ordinary key does not end
// the run: a frame must still be drawn after it, and the run only ends once
// a quit key follows.
func TestRunLiveNonQuitKeyContinues(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	drawer := newFakeDrawer()
	ticker := newFakeTicker()

	reader := &stagedReader{stage1: []byte{'x'}, stage2: []byte{'q'}, advance: make(chan struct{})}

	done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScenes(drawer),
		app.WithConsoleSwitch((&switchSpy{}).switchMode),
		app.WithRawMode((&switchSpy{}).switchMode),
		app.WithInput(reader),
		app.WithTicker(ticker.new),
	)

	ticker.ch <- time.Now()

	recvOrTimeout(t, drawer.calls, testTimeout, "a Draw call after the non-quit key")
	recvOrTimeout(t, blitter.calls, testTimeout, "a Blit call after the non-quit key")

	close(reader.advance)

	err := recvOrTimeout(t, done, testTimeout, "Run to return after the quit key")
	if err != nil {
		t.Errorf("Run: %v, want nil", err)
	}
}

// --- live mode: context cancellation, ticks, blit errors, FPS -------------

func TestRunLiveContextCancellationEndsRun(t *testing.T) {
	t.Parallel()

	watchdog, cancelWatchdog := context.WithTimeout(t.Context(), testTimeout)
	defer cancelWatchdog()

	ctx, cancel := context.WithCancel(watchdog)

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	drawer := newFakeDrawer()
	ticker := newFakeTicker()

	done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScenes(drawer),
		app.WithConsoleSwitch(degradeSwitch(vt.ErrNotConsole)),
		app.WithRawMode(degradeSwitch(term.ErrNotTerminal)),
		app.WithTicker(ticker.new),
	)

	ticker.ch <- time.Now()

	recvOrTimeout(t, drawer.calls, testTimeout, "a Draw call before cancellation")

	cancel()

	err := recvOrTimeout(t, done, testTimeout, "Run to return after context cancellation")
	if err != nil {
		t.Errorf("Run: %v, want nil", err)
	}

	if ticker.stopCalls != 1 {
		t.Errorf("ticker stop calls = %d, want 1", ticker.stopCalls)
	}
}

func TestRunLiveTicksDrawFrames(t *testing.T) {
	t.Parallel()

	const (
		ticksToSend = 3
		fps         = 10
	)

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	drawer := newFakeDrawer()
	ticker := newFakeTicker()

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	done := runAsync(runCtx, liveConfig(fps), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScenes(drawer),
		app.WithConsoleSwitch(degradeSwitch(vt.ErrNotConsole)),
		app.WithRawMode(degradeSwitch(term.ErrNotTerminal)),
		app.WithTicker(ticker.new),
		app.WithClock(func() time.Time { return start }),
	)

	for tick := 1; tick <= ticksToSend; tick++ {
		offset := time.Duration(tick) * time.Second
		ticker.ch <- start.Add(offset)

		draw := recvOrTimeout(t, drawer.calls, testTimeout, "a Draw call")
		if draw.elapsed != offset {
			t.Errorf("tick %d: Draw elapsed = %v, want %v", tick, draw.elapsed, offset)
		}

		recvOrTimeout(t, blitter.calls, testTimeout, "a Blit call")
	}

	cancelRun()

	err := recvOrTimeout(t, done, testTimeout, "Run to return")
	if err != nil {
		t.Errorf("Run: %v, want nil", err)
	}

	if ticker.interval != time.Second/fps {
		t.Errorf("ticker interval = %v, want %v", ticker.interval, time.Second/fps)
	}

	if ticker.stopCalls != 1 {
		t.Errorf("ticker stop calls = %d, want 1", ticker.stopCalls)
	}
}

func TestRunLiveBlitError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	blitter.blitErr = errStub
	drawer := newFakeDrawer()
	ticker := newFakeTicker()

	done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScenes(drawer),
		app.WithConsoleSwitch(degradeSwitch(vt.ErrNotConsole)),
		app.WithRawMode(degradeSwitch(term.ErrNotTerminal)),
		app.WithTicker(ticker.new),
	)

	ticker.ch <- time.Now()

	err := recvOrTimeout(t, done, testTimeout, "Run to return after a Blit error")
	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}

	if ticker.stopCalls != 1 {
		t.Errorf("ticker stop calls = %d, want 1", ticker.stopCalls)
	}

	select {
	case <-blitter.closed:
	default:
		t.Error("Close was not called after a Blit error")
	}
}

func TestRunLiveFPSGuard(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		fps  int
	}{
		{name: "zero FPS falls back to the default", fps: 0},
		{name: "negative FPS falls back to the default", fps: -1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const defaultFPS = 30

			watchdog, cancelWatchdog := context.WithTimeout(t.Context(), testTimeout)
			defer cancelWatchdog()

			ctx, cancel := context.WithCancel(watchdog)
			cancel()

			blitter := newFakeBlitter(100, 100, 16, 200, "fake")
			ticker := newFakeTicker()

			done := runAsync(ctx, liveConfig(testCase.fps), &bytes.Buffer{},
				app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
				app.WithConsoleSwitch(degradeSwitch(vt.ErrNotConsole)),
				app.WithRawMode(degradeSwitch(term.ErrNotTerminal)),
				app.WithTicker(ticker.new),
			)

			err := recvOrTimeout(t, done, testTimeout, "Run to return on an already-cancelled context")
			if err != nil {
				t.Errorf("Run: %v, want nil", err)
			}

			if ticker.interval != time.Second/defaultFPS {
				t.Errorf("ticker interval = %v, want %v", ticker.interval, time.Second/defaultFPS)
			}
		})
	}
}

// --- live mode: degrading vs aborting --------------------------------

func TestRunLiveConsoleSwitchDegrades(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	drawer := newFakeDrawer()
	ticker := newFakeTicker()

	var buf bytes.Buffer

	done := runAsync(runCtx, liveConfig(30), &buf,
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScenes(drawer),
		app.WithConsoleSwitch(degradeSwitch(vt.ErrNotConsole)),
		app.WithRawMode(degradeSwitch(term.ErrNotTerminal)),
		app.WithTicker(ticker.new),
	)

	ticker.ch <- time.Now()

	recvOrTimeout(t, drawer.calls, testTimeout, "a Draw call despite the degraded console switch")

	cancelRun()

	err := recvOrTimeout(t, done, testTimeout, "Run to return")
	if err != nil {
		t.Errorf("Run: %v, want nil", err)
	}

	got := buf.String()
	if !strings.Contains(got, "warning: console graphics mode unavailable") {
		t.Errorf("stdout = %q, want a console graphics mode warning", got)
	}
}

func TestRunLiveRawModeDegrades(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		sentinel error
	}{
		{name: "not a terminal", sentinel: term.ErrNotTerminal},
		{name: "unsupported platform", sentinel: term.ErrUnsupported},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
			defer cancel()

			runCtx, cancelRun := context.WithCancel(ctx)
			defer cancelRun()

			blitter := newFakeBlitter(100, 100, 16, 200, "fake")
			drawer := newFakeDrawer()
			ticker := newFakeTicker()
			reader := &countingReader{}

			var buf bytes.Buffer

			done := runAsync(runCtx, liveConfig(30), &buf,
				app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
				app.WithScenes(drawer),
				app.WithConsoleSwitch((&switchSpy{}).switchMode),
				app.WithRawMode(degradeSwitch(testCase.sentinel)),
				app.WithInput(reader),
				app.WithTicker(ticker.new),
			)

			ticker.ch <- time.Now()

			recvOrTimeout(t, drawer.calls, testTimeout, "a Draw call despite the degraded raw mode")

			// With no working keyboard, cancelling the context is the only
			// way out: this is the Ctrl-C-from-the-shell path taking over
			// for the keys a raw terminal would otherwise deliver.
			cancelRun()

			err := recvOrTimeout(t, done, testTimeout, "Run to return")
			if err != nil {
				t.Errorf("Run: %v, want nil", err)
			}

			if reader.calls != 0 {
				t.Errorf("reader was called %d times, want 0: raw mode never applied", reader.calls)
			}

			got := buf.String()
			if !strings.Contains(got, "warning: raw keyboard mode unavailable") {
				t.Errorf("stdout = %q, want a raw keyboard mode warning", got)
			}
		})
	}
}

func TestRunLiveConsoleSwitchAborts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")

	err := app.Run(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithConsoleSwitch(abortSwitch(errStub)),
	)

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

func TestRunLiveRawModeAborts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")

	err := app.Run(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithConsoleSwitch((&switchSpy{}).switchMode),
		app.WithRawMode(abortSwitch(errStub)),
	)

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

// TestRunLiveRestoreOrder pins the unwind order documented on live: the
// terminal is restored before the console mode, and a failing restore does
// not change Run's result.
func TestRunLiveRestoreOrder(t *testing.T) {
	t.Parallel()

	watchdog, cancelWatchdog := context.WithTimeout(t.Context(), testTimeout)
	defer cancelWatchdog()

	ctx, cancel := context.WithCancel(watchdog)
	cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")

	var order []string

	done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithConsoleSwitch(orderedRestore(&order, "vt", nil)),
		app.WithRawMode(orderedRestore(&order, "term", errStub)),
		app.WithInput(idleReader{}),
	)

	err := recvOrTimeout(t, done, testTimeout, "Run to return on an already-cancelled context")
	if err != nil {
		t.Errorf("Run: %v, want nil: a failing restore must not change the result", err)
	}

	want := []string{"term", "vt"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Errorf("restore order = %v, want %v", order, want)
	}
}

// --- terminal backends ----------------------------------------------------

// fakeBackend is the backend.Backend double. Its size can change between
// frames, which is how a terminal window being resized looks from the run
// loop's side of the interface.
type fakeBackend struct {
	mutex    sync.Mutex
	width    int
	height   int
	blitErr  error
	closeErr error
	calls    chan image.Rectangle
	closed   chan struct{}
}

func newFakeBackend(width, height int) *fakeBackend {
	return &fakeBackend{
		width:  width,
		height: height,
		calls:  make(chan image.Rectangle, 8),
		closed: make(chan struct{}),
	}
}

func (f *fakeBackend) Size() (int, int) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	return f.width, f.height
}

func (f *fakeBackend) Blit(img *image.RGBA) error {
	f.calls <- img.Bounds()

	f.mutex.Lock()
	defer f.mutex.Unlock()

	return f.blitErr
}

func (f *fakeBackend) Close() error {
	close(f.closed)

	return f.closeErr
}

// resize is what a SIGWINCH amounts to from the run loop's point of view.
func (f *fakeBackend) resize(width, height int) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.width, f.height = width, height
}

// termSpy stands in for the terminal backend builder and records which kind
// the run loop asked for, which is the observable half of auto detection.
type termSpy struct {
	kinds chan backend.Kind
	back  *fakeBackend
	err   error
}

func newTermSpy(back *fakeBackend) *termSpy {
	return &termSpy{kinds: make(chan backend.Kind, 4), back: back}
}

//nolint:ireturn // the seam returns the interface, so the double has to.
func (s *termSpy) open(_ app.Config, kind backend.Kind) (backend.Backend, error) {
	s.kinds <- kind

	if s.err != nil {
		return nil, s.err
	}

	return s.back, nil
}

// goosLinux is the only platform auto mode tries the framebuffer on.
const goosLinux = "linux"

// envFrom turns a map into the environment lookup the detector takes.
func envFrom(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// checkTerminalFrame asserts the one frame landed and that the status line
// went to stderr. The frames are on stdout, so a status line there would sit
// in the middle of the picture.
func checkTerminalFrame(t *testing.T, back *fakeBackend, stdout, stderr, wantLabel string) {
	t.Helper()

	call := recvOrTimeout(t, back.calls, testTimeout, "Blit call")
	if call.Dx() != termWidth || call.Dy() != termHeight {
		t.Errorf("canvas handed to Blit is %dx%d, want %dx%d", call.Dx(), call.Dy(), termWidth, termHeight)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing on it", stdout)
	}

	if stderr != wantLabel {
		t.Errorf("stderr = %q, want %q", stderr, wantLabel)
	}
}

// The fake terminal's size, repeated across the terminal-backend tables.
const (
	termWidth  = 100
	termHeight = 80
)

func TestRunTerminalTestPattern(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		kind      backend.Kind
		wantLabel string
	}{
		{name: "blocks", kind: backend.Blocks, wantLabel: "blocks 100x80 logical=100x80\n"},
		{name: "kitty", kind: backend.Kitty, wantLabel: "kitty 100x80 logical=100x80\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			back := newFakeBackend(termWidth, termHeight)
			spy := newTermSpy(back)
			console := &switchSpy{}

			var stdout, stderr bytes.Buffer

			cfg := app.Config{TestPattern: true, Backend: testCase.kind}

			err := app.Run(t.Context(), cfg, &stdout,
				app.WithTerminal(spy.open),
				app.WithStderr(&stderr),
				app.WithConsoleSwitch(console.switchMode),
			)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			if got := recvOrTimeout(t, spy.kinds, testTimeout, "backend kind"); got != testCase.kind {
				t.Errorf("asked for backend %v, want %v", got, testCase.kind)
			}

			checkTerminalFrame(t, back, stdout.String(), stderr.String(), testCase.wantLabel)

			// Switching the VT into graphics mode would blank the very
			// screen a terminal backend is drawing on.
			if console.called {
				t.Error("console switch was called for a terminal backend, want it untouched")
			}
		})
	}
}

func TestRunTerminalOpenError(t *testing.T) {
	t.Parallel()

	spy := newTermSpy(nil)
	spy.err = errStub

	var stdout bytes.Buffer

	err := app.Run(t.Context(), app.Config{Backend: backend.Blocks}, &stdout,
		app.WithTerminal(spy.open),
		app.WithStderr(io.Discard),
	)

	if !errors.Is(err, errStub) {
		t.Errorf("err = %v, want wrapping %v", err, errStub)
	}
}

// autoCase is one auto-detection scenario.
type autoCase struct {
	name    string
	goos    string
	fbOpens bool
	vars    map[string]string
	wantFB  bool
	wantHow backend.Kind
}

// autoCases pins the order auto mode decides in: the device's own screen
// first, then the terminal, with Kitty graphics only where the terminal is
// known to draw them.
func autoCases() []autoCase {
	return []autoCase{
		{name: "linux with a framebuffer", goos: goosLinux, fbOpens: true, wantFB: true},
		{
			name:    "linux without a framebuffer falls through to the terminal",
			goos:    goosLinux,
			vars:    map[string]string{"TERM": "linux"},
			wantHow: backend.Blocks,
		},
		{
			name:    "linux without a framebuffer in ghostty",
			goos:    goosLinux,
			vars:    map[string]string{"TERM": "xterm-ghostty"},
			wantHow: backend.Kitty,
		},
		{
			name:    "mac in ghostty never tries the framebuffer",
			goos:    "darwin",
			fbOpens: true,
			vars:    map[string]string{"TERM_PROGRAM": "ghostty"},
			wantHow: backend.Kitty,
		},
		{
			name:    "mac in a plain terminal",
			goos:    "darwin",
			fbOpens: true,
			vars:    map[string]string{},
			wantHow: backend.Blocks,
		},
	}
}

// runAutoCase runs one scenario in test-pattern mode, which is the shortest
// path that still goes all the way through backend selection.
func runAutoCase(t *testing.T, testCase autoCase) {
	t.Helper()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	spy := newTermSpy(newFakeBackend(termWidth, termHeight))

	openFB := func(string) (app.Blitter, error) {
		if testCase.fbOpens {
			return blitter, nil
		}

		return nil, errStub
	}

	var stdout, stderr bytes.Buffer

	err := app.Run(t.Context(), app.Config{FBPath: fbPath, TestPattern: true}, &stdout,
		app.WithFramebuffer(openFB),
		app.WithTerminal(spy.open),
		app.WithGOOS(testCase.goos),
		app.WithEnv(envFrom(testCase.vars)),
		app.WithStderr(&stderr),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if testCase.wantFB {
		if !strings.HasPrefix(stdout.String(), "fb=") {
			t.Errorf("stdout = %q, want the framebuffer status line", stdout.String())
		}

		return
	}

	if got := recvOrTimeout(t, spy.kinds, testTimeout, "backend kind"); got != testCase.wantHow {
		t.Errorf("auto picked %v, want %v", got, testCase.wantHow)
	}
}

func TestRunAutoBackend(t *testing.T) {
	t.Parallel()

	for _, testCase := range autoCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runAutoCase(t, testCase)
		})
	}
}

// TestRunLiveFrameLimit checks --frames: the loop ends on its own after the
// count, with nobody pressing anything. This is what makes uScope testable
// from a shell pipeline.
func TestRunLiveFrameLimit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	back := newFakeBackend(64, 48)
	spy := newTermSpy(back)
	ticker := newFakeTicker()

	cfg := app.Config{FPS: 30, Frames: 2, Backend: backend.Blocks}

	done := runAsync(ctx, cfg, &bytes.Buffer{},
		app.WithTerminal(spy.open),
		app.WithStderr(io.Discard),
		app.WithRawMode((&switchSpy{}).switchMode),
		app.WithInput(idleReader{}),
		app.WithTicker(ticker.new),
	)

	for frame := range 3 {
		select {
		case ticker.ch <- time.Now():
		case err := <-done:
			if frame < 2 {
				t.Fatalf("Run returned after %d frames: %v, want 2", frame, err)
			}

			return
		case <-time.After(testTimeout):
			t.Fatal("timed out feeding the ticker")
		}
	}

	if err := recvOrTimeout(t, done, testTimeout, "Run to return after 2 frames"); err != nil {
		t.Errorf("Run: %v", err)
	}

	if len(back.calls) != 2 {
		t.Errorf("blits = %d, want 2", len(back.calls))
	}
}

// TestRunLiveResize checks that a backend changing its mind about the size
// gets a canvas that matches, rather than an ErrSize on the next blit.
func TestRunLiveResize(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	back := newFakeBackend(64, 48)
	spy := newTermSpy(back)
	ticker := newFakeTicker()

	cfg := app.Config{FPS: 30, Frames: 2, Backend: backend.Blocks}

	done := runAsync(ctx, cfg, &bytes.Buffer{},
		app.WithTerminal(spy.open),
		app.WithStderr(io.Discard),
		app.WithRawMode((&switchSpy{}).switchMode),
		app.WithInput(idleReader{}),
		app.WithTicker(ticker.new),
	)

	ticker.ch <- time.Now()

	first := recvOrTimeout(t, back.calls, testTimeout, "first Blit")
	if first.Dx() != 64 || first.Dy() != 48 {
		t.Fatalf("first frame is %dx%d, want 64x48", first.Dx(), first.Dy())
	}

	back.resize(80, 60)

	ticker.ch <- time.Now()

	second := recvOrTimeout(t, back.calls, testTimeout, "second Blit")
	if second.Dx() != 80 || second.Dy() != 60 {
		t.Errorf("frame after the resize is %dx%d, want 80x60", second.Dx(), second.Dy())
	}

	if err := recvOrTimeout(t, done, testTimeout, "Run to return"); err != nil {
		t.Errorf("Run: %v", err)
	}
}

// TestRunLiveResizeToNothing covers the backend reporting a size no canvas
// can be built from. A terminal reporting zero columns should end the run
// with the reason, not panic somewhere inside the drawing code.
func TestRunLiveResizeToNothing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	back := newFakeBackend(64, 48)
	spy := newTermSpy(back)
	ticker := newFakeTicker()

	cfg := app.Config{FPS: 30, Backend: backend.Blocks}

	done := runAsync(ctx, cfg, &bytes.Buffer{},
		app.WithTerminal(spy.open),
		app.WithStderr(io.Discard),
		app.WithRawMode((&switchSpy{}).switchMode),
		app.WithInput(idleReader{}),
		app.WithTicker(ticker.new),
	)

	ticker.ch <- time.Now()

	recvOrTimeout(t, back.calls, testTimeout, "first Blit")

	back.resize(0, 0)

	ticker.ch <- time.Now()

	err := recvOrTimeout(t, done, testTimeout, "Run to fail on an impossible canvas")
	if !errors.Is(err, canvas.ErrSize) {
		t.Errorf("err = %v, want wrapping %v", err, canvas.ErrSize)
	}
}

// --- scene selection ------------------------------------------------------

// namedDrawer is a Drawer that reports every frame it is asked for on its own
// channel, so a test can tell which of two scenes the loop is drawing.
type namedDrawer struct {
	name  string
	calls chan string
}

func newNamedDrawer(name string, calls chan string) *namedDrawer {
	return &namedDrawer{name: name, calls: calls}
}

func (d *namedDrawer) Draw(*canvas.Canvas, time.Duration) {
	d.calls <- d.name
}

func TestRunDrawsTheSelectedScene(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		kind app.SceneKind
		want string
	}{
		{name: "radar is the first scene", kind: app.Radar, want: "first"},
		{name: "pattern is the second", kind: app.Pattern, want: "second"},
		{name: "specimen is the third", kind: app.Specimen, want: "third"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			calls := make(chan string, 3)
			cfg := app.Config{
				PNG:   filepath.Join(t.TempDir(), "out.png"),
				Size:  image.Pt(16, 16),
				Scene: testCase.kind,
			}

			err := app.Run(t.Context(), cfg, io.Discard,
				app.WithScenes(
					newNamedDrawer("first", calls),
					newNamedDrawer("second", calls),
					newNamedDrawer("third", calls)))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			got := recvOrTimeout(t, calls, testTimeout, "a Draw call")
			if got != testCase.want {
				t.Errorf("--scene %v drew %q, want %q", testCase.kind, got, testCase.want)
			}

			select {
			case extra := <-calls:
				t.Errorf("a second scene also drew (%q), want only the selected one", extra)
			default:
			}
		})
	}
}

func TestRunSceneOutOfRange(t *testing.T) {
	t.Parallel()

	cfg := app.Config{
		PNG:   filepath.Join(t.TempDir(), "out.png"),
		Size:  image.Pt(16, 16),
		Scene: app.Specimen,
	}

	// Only one scene was built, so there is no second one to start on. The
	// flag layer's allow list makes this unreachable in the real program;
	// the check is here so a future scene added to the enum and forgotten
	// in the builder fails loudly instead of drawing the wrong thing.
	err := app.Run(t.Context(), cfg, io.Discard, app.WithScenes(newFakeDrawer()))
	if !errors.Is(err, app.ErrNoScene) {
		t.Fatalf("Run with a scene index past the set = %v, want ErrNoScene", err)
	}
}

func TestRunSceneLoaderError(t *testing.T) {
	t.Parallel()

	cfg := app.Config{PNG: filepath.Join(t.TempDir(), "out.png"), Size: image.Pt(16, 16)}

	spy := &openFBSpy{}

	err := app.Run(t.Context(), cfg, io.Discard,
		app.WithFramebuffer(spy.open),
		app.WithSceneLoader(func() ([]app.Drawer, error) { return nil, errStub }))
	if !errors.Is(err, errStub) {
		t.Fatalf("Run with a failing scene loader = %v, want the loader's error", err)
	}

	// A font that will not parse has to stop the program before it opens a
	// device, which is the whole reason the fonts are loaded at startup.
	if spy.called {
		t.Error("the framebuffer opener was called after the scene loader failed, want it untouched")
	}
}

// sceneSwitchBudget bounds how long a test will keep ticking while it waits
// for the v keypress to be picked up. It is half of testTimeout so the run
// context, which is bounded by the whole of it, cannot expire first and turn
// a slow keypress into a timed-out tick.
const sceneSwitchBudget = testTimeout / 2

func TestRunLiveSwitchesScene(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	calls := make(chan string, 1)
	blitter := newFakeBlitter(16, 16, 16, 32, "fake")
	ticker := newFakeTicker()

	done := runAsync(runCtx, liveConfig(30), io.Discard,
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScenes(newNamedDrawer("first", calls), newNamedDrawer("second", calls)),
		app.WithConsoleSwitch((&switchSpy{}).switchMode),
		app.WithRawMode((&switchSpy{}).switchMode),
		app.WithInput(&onceReader{data: []byte("v")}),
		app.WithTicker(ticker.new),
	)

	if !drewSecondScene(t, ticker, blitter, calls) {
		t.Error("the second scene never drew after v, want the loop to switch to it")
	}

	cancelRun()

	if err := recvOrTimeout(t, done, testTimeout, "Run to return"); err != nil {
		t.Errorf("Run: %v, want nil", err)
	}
}

// drewSecondScene ticks the loop until the second scene paints a frame, or
// until the budget runs out.
//
// Ticking in a loop rather than once is the whole point. The v keypress
// arrives on the reader's own goroutine, and nothing in the test can say when
// that goroutine is scheduled or, once the key is queued, whether the loop's
// select takes the key or a waiting tick first. A fixed number of tries is
// not enough: on a loaded machine the reader can still be waiting to run
// after a dozen frames have been drawn, which is exactly how the first
// version of this test failed about once in a hundred runs. What is certain
// is that once the key has been handled, every later tick draws the second
// scene, so the test keeps ticking until it sees one.
//
// Both the draw and the blit channel are drained every round. The blitter's
// buffer is small, and a full one stops the loop dead, which would wedge the
// next tick rather than fail the test.
func drewSecondScene(t *testing.T, ticker *fakeTicker, blitter *fakeBlitter, calls chan string) bool {
	t.Helper()

	deadline := time.Now().Add(sceneSwitchBudget)

	for time.Now().Before(deadline) {
		sendTick(t, ticker)

		name := recvOrTimeout(t, calls, testTimeout, "a Draw call")
		recvOrTimeout(t, blitter.calls, testTimeout, "a Blit call")

		if name == "second" {
			return true
		}
	}

	return false
}

// sendTick delivers one frame tick, failing the test rather than blocking
// for ever if the run loop has already stopped receiving.
func sendTick(t *testing.T, ticker *fakeTicker) {
	t.Helper()

	select {
	case ticker.ch <- time.Now():
	case <-time.After(testTimeout):
		t.Fatal("timed out delivering a tick to the run loop")
	}
}

// themedNamedDrawer is a namedDrawer that also implements app.Themed,
// reporting every palette it is handed on its own channel. It stands in for
// the radar and specimen scenes, which is what lets the l key be tested
// without loading a font.
type themedNamedDrawer struct {
	name     string
	calls    chan string
	palettes chan theme.Palette
}

func newThemedNamedDrawer(name string, calls chan string, palettes chan theme.Palette) *themedNamedDrawer {
	return &themedNamedDrawer{name: name, calls: calls, palettes: palettes}
}

func (d *themedNamedDrawer) Draw(*canvas.Canvas, time.Duration) {
	d.calls <- d.name
}

func (d *themedNamedDrawer) SetPalette(pal theme.Palette) {
	d.palettes <- pal
}

// TestRunLiveSwitchesTheme mirrors TestRunLiveSwitchesScene, but for the l
// key rather than s. Unlike a scene switch, a palette change is reported
// synchronously by SetPalette itself, so this does not need
// drewSecondScene's tick-until-you-see-it loop: building the scene set
// applies the starting theme once up front (so the first palette every scene
// reports is Night), and the l keypress is the second and last report from
// each, with no tick required to observe either.
func TestRunLiveSwitchesTheme(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	calls := make(chan string, 4)
	palettes := make(chan theme.Palette, 4)
	blitter := newFakeBlitter(16, 16, 16, 32, "fake")
	ticker := newFakeTicker()

	done := runAsync(ctx, liveConfig(30), io.Discard,
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScenes(
			newThemedNamedDrawer("first", calls, palettes),
			newThemedNamedDrawer("second", calls, palettes),
		),
		app.WithConsoleSwitch((&switchSpy{}).switchMode),
		app.WithRawMode((&switchSpy{}).switchMode),
		app.WithInput(&onceReader{data: []byte("l")}),
		app.WithTicker(ticker.new),
	)

	for range 2 {
		if got := recvOrTimeout(t, palettes, testTimeout, "the starting palette"); got != theme.Night {
			t.Errorf("starting palette = %v, want %v", got, theme.Night)
		}
	}

	for range 2 {
		if got := recvOrTimeout(t, palettes, testTimeout, "the palette after l"); got != theme.Paper {
			t.Errorf("palette after l = %v, want %v", got, theme.Paper)
		}
	}

	cancel()

	if err := recvOrTimeout(t, done, testTimeout, "Run to return"); err != nil {
		t.Errorf("Run: %v, want nil", err)
	}
}

// configuredNamedDrawer is a namedDrawer that also implements app.Configured,
// reporting every settings block it is handed on its own channel. It stands
// in for the radar scene, the only one with settings of its own, the same
// way themedNamedDrawer stands in for a scene with a theme.
type configuredNamedDrawer struct {
	name     string
	calls    chan string
	settings chan radar.Settings
}

func newConfiguredNamedDrawer(name string, calls chan string, settings chan radar.Settings) *configuredNamedDrawer {
	return &configuredNamedDrawer{name: name, calls: calls, settings: settings}
}

func (d *configuredNamedDrawer) Draw(*canvas.Canvas, time.Duration) {
	d.calls <- d.name
}

func (d *configuredNamedDrawer) Apply(set radar.Settings) {
	d.settings <- set
}

// TestRunAppliesTheConfiguredSettings checks that Config.Radar reaches a
// scene that implements Configured, the same wiring TestRunLiveSwitchesTheme
// pins for Config.Theme and Themed. Unlike the theme, nothing in this package
// cycles these settings at run time, so PNG mode is enough to observe the one
// application Run makes at startup.
func TestRunAppliesTheConfiguredSettings(t *testing.T) {
	t.Parallel()

	calls := make(chan string, 1)
	settings := make(chan radar.Settings, 1)

	want := radar.Settings{Colour: radar.ColourAirline, Airports: radar.ToggleOff}

	cfg := app.Config{
		PNG:   filepath.Join(t.TempDir(), "out.png"),
		Size:  image.Pt(16, 16),
		Radar: want,
	}

	err := app.Run(t.Context(), cfg, io.Discard,
		app.WithScenes(newConfiguredNamedDrawer("radar", calls, settings)))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := recvOrTimeout(t, settings, testTimeout, "the configured settings"); got != want {
		t.Errorf("Apply received %v, want %v", got, want)
	}

	recvOrTimeout(t, calls, testTimeout, "a Draw call")
}

// --- live mode: a scene with keys of its own -------------------------------

// keyDrawer is a scene that binds a key. It stands in for the radar, which is
// the only real one, so the loop's key routing can be tested without loading a
// font or building a receiver.
type keyDrawer struct {
	name  string
	calls chan string

	// takes is the rune this scene claims. Everything else falls through.
	takes rune

	// handled carries every key the scene took, so a test can tell "the scene
	// consumed it" from "the loop ignored it".
	handled chan rune
}

func newKeyDrawer(name string, takes rune, calls chan string, handled chan rune) *keyDrawer {
	return &keyDrawer{name: name, calls: calls, takes: takes, handled: handled}
}

func (d *keyDrawer) Draw(*canvas.Canvas, time.Duration) {
	d.calls <- d.name
}

func (d *keyDrawer) Handle(key input.Key) bool {
	if key.Kind != input.Rune || key.Rune != d.takes {
		return false
	}

	d.handled <- key.Rune

	return true
}

// TestRunLiveSceneTakesItsOwnKeys covers the scene getting first refusal. A
// scene that claims v must stop the loop switching away from it, which is
// the whole point of letting a scene bind keys at all; v is what the loop
// itself would otherwise take the key to mean.
func TestRunLiveSceneTakesItsOwnKeys(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	calls := make(chan string, 4)
	handled := make(chan rune, 2)
	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	ticker := newFakeTicker()

	done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScenes(newKeyDrawer("first", 'v', calls, handled), newNamedDrawer("second", calls)),
		app.WithConsoleSwitch((&switchSpy{}).switchMode),
		app.WithRawMode((&switchSpy{}).switchMode),
		app.WithInput(&onceReader{data: []byte{'v'}}),
		app.WithTicker(ticker.new),
	)

	if got := recvOrTimeout(t, handled, testTimeout, "the scene to take v"); got != 'v' {
		t.Errorf("scene handled %q, want %q", got, 'v')
	}

	ticker.ch <- time.Now()

	if got := recvOrTimeout(t, calls, testTimeout, "a Draw call"); got != "first" {
		t.Errorf("scene after s is %q, want the loop to have left it alone", got)
	}

	cancel()

	if err := recvOrTimeout(t, done, testTimeout, "Run to return"); err != nil {
		t.Errorf("Run: %v", err)
	}
}

// TestRunLiveSceneLetsQuitThrough is the other half of the contract: a key the
// scene does not claim reaches the loop, so q still quits whichever scene is
// on screen.
func TestRunLiveSceneLetsQuitThrough(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	blitter := newFakeBlitter(100, 100, 16, 200, "fake")
	ticker := newFakeTicker()

	done := runAsync(ctx, liveConfig(30), &bytes.Buffer{},
		app.WithFramebuffer(func(string) (app.Blitter, error) { return blitter, nil }),
		app.WithScenes(newKeyDrawer("first", 't', make(chan string, 4), make(chan rune, 2))),
		app.WithConsoleSwitch((&switchSpy{}).switchMode),
		app.WithRawMode((&switchSpy{}).switchMode),
		app.WithInput(&onceReader{data: []byte{'q'}}),
		app.WithTicker(ticker.new),
	)

	if err := recvOrTimeout(t, done, testTimeout, "Run to return after q"); err != nil {
		t.Errorf("Run: %v, want nil", err)
	}
}
