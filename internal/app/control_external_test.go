package app_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/app"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/pkg/backend"
)

// controlAddress is the loopback address every control test listens on. Port
// zero lets the kernel pick, so parallel tests never collide.
const controlAddress = "127.0.0.1:0"

// statusLines carries each status line Run writes, so a test can wait for the
// one that names the control port while the loop is still running. sayf makes
// one Write per line, which is what lets a Write stand for a line.
type statusLines chan string

func (s statusLines) Write(data []byte) (int, error) {
	s <- string(data)

	return len(data), nil
}

// controlClient is one operator connection.
type controlClient struct {
	conn   net.Conn
	reader *bufio.Reader
}

// dialControl waits for Run to say where the control port is and connects to
// it.
func dialControl(tb testing.TB, status statusLines) *controlClient {
	tb.Helper()

	const prefix = "control port listening on "

	var address string

	select {
	case line := <-status:
		if !strings.HasPrefix(line, prefix) {
			tb.Fatalf("first status line = %q, want it to name the control port", line)
		}

		address = strings.TrimSpace(strings.TrimPrefix(line, prefix))
	case <-time.After(testTimeout):
		tb.Fatal("timed out waiting for the control port to open")
	}

	var dialer net.Dialer

	conn, err := dialer.DialContext(tb.Context(), "tcp", address)
	if err != nil {
		tb.Fatalf("dial %s: %v", address, err)
	}

	tb.Cleanup(func() { _ = conn.Close() })

	return &controlClient{conn: conn, reader: bufio.NewReader(conn)}
}

// send writes one command and returns the reply line without its newline.
func (c *controlClient) send(tb testing.TB, line string) string {
	tb.Helper()

	_ = c.conn.SetDeadline(time.Now().Add(testTimeout))

	if _, err := io.WriteString(c.conn, line+"\n"); err != nil {
		tb.Fatalf("write %q: %v", line, err)
	}

	reply, err := c.reader.ReadString('\n')
	if err != nil {
		tb.Fatalf("reply to %q: %v", line, err)
	}

	return strings.TrimSuffix(reply, "\n")
}

// controlConfig is a terminal run with the control port on, at a size small
// enough that a frame costs nothing.
func controlConfig(set radar.Settings) app.Config {
	return app.Config{Backend: backend.Blocks, Size: image.Pt(8, 6), Control: controlAddress, Radar: set}
}

// quietBackend takes every frame and keeps none of them, so a test can tick
// the loop as often as it likes without filling a channel nobody drains.
type quietBackend struct{}

func (quietBackend) Size() (int, int)       { return 8, 6 }
func (quietBackend) Blit(*image.RGBA) error { return nil }
func (quietBackend) Close() error           { return nil }

// runControl is runAsync with a terminal that takes the frames and a keyboard
// that is in raw mode but never sends anything, so the first status line is
// always the control port's. opts come after those and can replace them.
func runControl(ctx context.Context, cfg app.Config, stdout io.Writer, opts ...app.Option) <-chan error {
	base := make([]app.Option, 0, 3+len(opts))
	base = append(base,
		app.WithTerminal(func(app.Config, backend.Kind) (backend.Backend, error) { return quietBackend{}, nil }),
		app.WithRawMode(func() (func() error, error) { return func() error { return nil }, nil }),
		app.WithInput(idleReader{}))

	return runAsync(ctx, cfg, stdout, append(base, opts...)...)
}

// TestRunControlDrivesTheRadar is the control port end to end against the real
// radar scene: every reply is the state after the command, keys go through the
// same bindings the keyboard reaches, and a key that would quit is refused
// while the loop carries on.
func TestRunControlDrivesTheRadar(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	status := make(statusLines, 4)
	ticker := newFakeTicker()
	done := runControl(ctx, controlConfig(radar.Settings{
		View: radar.ViewMinimal3D, MinimalShore: true, Tilt: 10, Orbit: radar.ToggleOff, Azimuth: 30,
	}), io.Discard, app.WithStderr(status), app.WithTicker(ticker.new))

	client := dialControl(t, status)

	for _, step := range []struct{ command, want string }{
		{"get", "ok view=minimal3d shore=on airports=off zoom=0 tilt=10 orbit=off azimuth=30 exaggerate=8 " +
			"range=auto theme=night look=glass colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"set tilt 25", "ok view=minimal3d shore=on airports=off zoom=0 tilt=25 orbit=off azimuth=30 exaggerate=8 " +
			"range=auto theme=night look=glass colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"key m", "ok view=minimal3d shore=off airports=off zoom=0 tilt=25 orbit=off azimuth=30 exaggerate=8 " +
			"range=auto theme=night look=glass colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"key ]", "ok view=minimal3d shore=off airports=off zoom=0 tilt=30 orbit=off azimuth=30 exaggerate=8 " +
			"range=auto theme=night look=glass colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"set azimuth 90", "ok view=minimal3d shore=off airports=off zoom=0 tilt=30 orbit=off azimuth=90 " +
			"exaggerate=8 range=auto theme=night look=glass colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"key right", "ok view=minimal3d shore=off airports=off zoom=0 tilt=30 orbit=off azimuth=105 " +
			"exaggerate=8 range=auto theme=night look=glass colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"set orbit on", "ok view=minimal3d shore=off airports=off zoom=0 tilt=30 orbit=on azimuth=105 " +
			"exaggerate=8 range=auto theme=night look=glass colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"key l", "ok view=minimal3d shore=off airports=off zoom=0 tilt=30 orbit=on azimuth=105 " +
			"exaggerate=8 range=auto theme=day look=glass colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"set look mono", "ok view=minimal3d shore=off airports=off zoom=0 tilt=30 orbit=on azimuth=105 " +
			"exaggerate=8 range=auto theme=day look=mono colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"set range 60", "ok view=minimal3d shore=off airports=off zoom=0 tilt=30 orbit=on azimuth=105 " +
			"exaggerate=8 range=60 theme=day look=mono colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"set view scope", "ok view=scope shore=on airports=on zoom=0 tilt=30 orbit=on azimuth=105 " +
			"exaggerate=8 range=60 theme=day look=mono colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"set lights off", "ok view=scope shore=on airports=on zoom=0 tilt=30 orbit=on azimuth=105 " +
			"exaggerate=8 range=60 theme=day look=mono colour=altitude bar=off lights=off stars=on " +
			"recenter=0 corridors=on filter=all board=off"},
		{"set recenter 2m", "ok view=scope shore=on airports=on zoom=0 tilt=30 orbit=on azimuth=105 " +
			"exaggerate=8 range=60 theme=day look=mono colour=altitude bar=off lights=off stars=on " +
			"recenter=2m corridors=on filter=all board=off"},
		{"set corridors off", "ok view=scope shore=on airports=on zoom=0 tilt=30 orbit=on azimuth=105 " +
			"exaggerate=8 range=60 theme=day look=mono colour=altitude bar=off lights=off stars=on " +
			"recenter=2m corridors=off filter=all board=off"},
		{"set filter below-high", "ok view=scope shore=on airports=on zoom=0 tilt=30 orbit=on azimuth=105 " +
			"exaggerate=8 range=60 theme=day look=mono colour=altitude bar=off lights=off stars=on " +
			"recenter=2m corridors=off filter=below-high board=off"},
		{"key f", "ok view=scope shore=on airports=on zoom=0 tilt=30 orbit=on azimuth=105 " +
			"exaggerate=8 range=60 theme=day look=mono colour=altitude bar=off lights=off stars=on " +
			"recenter=2m corridors=off filter=all board=off"},
		{"key f", "ok view=scope shore=on airports=on zoom=0 tilt=30 orbit=on azimuth=105 " +
			"exaggerate=8 range=60 theme=day look=mono colour=altitude bar=off lights=off stars=on " +
			"recenter=2m corridors=off filter=low board=off"},
		{"key q", "err quitting is not available over the control port"},
	} {
		if got := client.send(t, step.command); got != step.want {
			t.Errorf("%s:\n got %q\nwant %q", step.command, got, step.want)
		}
	}

	select {
	case err := <-done:
		t.Fatalf("Run returned %v while the control port was being driven", err)
	default:
	}

	cancel()

	if err := recvOrTimeout(t, done, testTimeout, "Run to return"); err != nil {
		t.Errorf("Run error = %v, want nil after cancel", err)
	}
}

// TestRunControlWithoutAReporter covers a scene set where nothing reads its
// settings back: get still answers, with the zero block's defaults, and a key
// the scene does not take falls through to the loop's own bindings.
func TestRunControlWithoutAReporter(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	status := make(statusLines, 4)
	done := runControl(ctx, controlConfig(radar.Settings{}), io.Discard,
		app.WithScenes(newFakeDrawer()), app.WithStderr(status), app.WithTicker(newFakeTicker().new))

	client := dialControl(t, status)

	const want = "ok view=scope shore=on airports=on zoom=0 tilt=0 orbit=on azimuth=0 exaggerate=0 " +
		"range=auto theme=day look=glass colour=altitude bar=off lights=off stars=on recenter=0 " +
		"corridors=on filter=all board=off"

	if got := client.send(t, "key k"); !strings.Contains(got, "look=phosphor") {
		t.Errorf("key k = %q, want the look cycled to phosphor", got)
	}

	if got := client.send(t, "set look glass"); got != strings.Replace(want, "theme=day", "theme=night", 1) {
		t.Errorf("set look glass = %q", got)
	}

	if got := client.send(t, "key l"); got != want {
		t.Errorf("key l:\n got %q\nwant %q", got, want)
	}

	cancel()

	if err := recvOrTimeout(t, done, testTimeout, "Run to return"); err != nil {
		t.Errorf("Run error = %v, want nil after cancel", err)
	}
}

// failingListener accepts nothing: every Accept fails at once, which is what a
// listener whose socket has gone bad looks like from the server.
type failingListener struct{}

func (failingListener) Accept() (net.Conn, error) { return nil, errStub }
func (failingListener) Close() error              { return nil }
func (failingListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7777} }

// TestRunControlListenerFailures covers both ways the port can fail. A port
// that will not open stops Run before the first frame, because somebody
// asked for it; one that fails later only warns, because by then the picture
// is what matters.
func TestRunControlListenerFailures(t *testing.T) {
	t.Parallel()

	t.Run("a port that will not open is an error", func(t *testing.T) {
		t.Parallel()

		cfg := controlConfig(radar.Settings{})
		cfg.Control = "127.0.0.1:99999"

		done := runControl(t.Context(), cfg, io.Discard,
			app.WithScenes(newFakeDrawer()), app.WithStderr(io.Discard), app.WithTicker(newFakeTicker().new))

		if err := recvOrTimeout(t, done, testTimeout, "Run to return"); err == nil {
			t.Error("Run error = nil, want the listen failure")
		}
	})

	t.Run("a seam that fails is wrapped", func(t *testing.T) {
		t.Parallel()

		listen := func(context.Context, string, string) (net.Listener, error) { return nil, errStub }
		done := runControl(t.Context(), controlConfig(radar.Settings{}), io.Discard,
			app.WithScenes(newFakeDrawer()), app.WithStderr(io.Discard), app.WithTicker(newFakeTicker().new),
			app.WithListener(listen))

		if err := recvOrTimeout(t, done, testTimeout, "Run to return"); !errors.Is(err, errStub) {
			t.Errorf("Run error = %v, want %v", err, errStub)
		}
	})

	t.Run("a server that fails later only warns", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()

		status := make(statusLines, 4)
		listen := func(context.Context, string, string) (net.Listener, error) { return failingListener{}, nil }
		done := runControl(ctx, controlConfig(radar.Settings{}), io.Discard,
			app.WithScenes(newFakeDrawer()), app.WithStderr(status), app.WithTicker(newFakeTicker().new),
			app.WithListener(listen))

		if line := recvOrTimeout(t, status, testTimeout, "the listening line"); !strings.Contains(line, "7777") {
			t.Errorf("first status line = %q, want the address", line)
		}

		if line := recvOrTimeout(t, status, testTimeout, "the warning"); !strings.HasPrefix(line, "warning: ") {
			t.Errorf("second status line = %q, want a warning", line)
		}

		cancel()

		if err := recvOrTimeout(t, done, testTimeout, "Run to return"); err != nil {
			t.Errorf("Run error = %v, want nil: a failed control port must not stop the run", err)
		}
	})
}

// wildcardListener is a real loopback listener that reports the address a
// --control :PORT bind would, so the warning can be tested without opening a
// port to the network.
type wildcardListener struct {
	net.Listener
}

func (wildcardListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv6unspecified, Port: 7777} }

// TestRunControlWarnsOffLoopback checks a control port reachable from other
// machines says so on stderr once, right after the line that names it, and
// that a loopback port does not.
func TestRunControlWarnsOffLoopback(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		wrap func(net.Listener) net.Listener
		warn bool
	}{
		{name: "every interface", wrap: func(l net.Listener) net.Listener { return wildcardListener{l} }, warn: true},
		{name: "loopback", wrap: func(l net.Listener) net.Listener { return l }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
			defer cancel()

			listen := func(ctx context.Context, network, address string) (net.Listener, error) {
				var config net.ListenConfig

				listener, err := config.Listen(ctx, network, address)
				if err != nil {
					return nil, fmt.Errorf("listening: %w", err)
				}

				return testCase.wrap(listener), nil
			}

			status := make(statusLines, 4)
			done := runControl(ctx, controlConfig(radar.Settings{}), io.Discard,
				app.WithScenes(newFakeDrawer()), app.WithStderr(status), app.WithTicker(newFakeTicker().new),
				app.WithListener(listen))

			recvOrTimeout(t, status, testTimeout, "the listening line")

			var warned bool

			select {
			case line := <-status:
				warned = strings.HasPrefix(line, "warning: ") && strings.Contains(line, "no authentication")
			case <-time.After(100 * time.Millisecond):
			}

			if warned != testCase.warn {
				t.Errorf("warned about the bind = %v, want %v", warned, testCase.warn)
			}

			cancel()

			if err := recvOrTimeout(t, done, testTimeout, "Run to return"); err != nil {
				t.Errorf("Run error = %v, want nil after cancel", err)
			}
		})
	}
}

// TestRunControlWarnsBeforeGraphicsMode checks that on the framebuffer the
// control port's lines, the off-loopback warning among them, are written
// before the console switches to graphics, while they can still be read.
func TestRunControlWarnsBeforeGraphicsMode(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	const switched = "console switched to graphics\n"

	status := make(statusLines, 8)
	consoleSwitch := func() (func() error, error) {
		status <- switched

		return func() error { return nil }, nil
	}

	listen := func(ctx context.Context, network, address string) (net.Listener, error) {
		var config net.ListenConfig

		listener, err := config.Listen(ctx, network, address)
		if err != nil {
			return nil, fmt.Errorf("listening: %w", err)
		}

		return wildcardListener{listener}, nil
	}

	cfg := liveConfig(30)
	cfg.Control = controlAddress

	// On the framebuffer the status lines go to stdout, which is the console
	// itself, so that is where the test listens.
	done := runAsync(ctx, cfg, status,
		app.WithFramebuffer(func(string) (app.Blitter, error) {
			return newFakeBlitter(100, 100, 16, 200, "fake"), nil
		}),
		app.WithScenes(newFakeDrawer()),
		app.WithConsoleSwitch(consoleSwitch),
		app.WithRawMode(func() (func() error, error) { return func() error { return nil }, nil }),
		app.WithInput(idleReader{}),
		app.WithTicker(newFakeTicker().new),
		app.WithListener(listen))

	var lines []string
	for len(lines) < 3 {
		lines = append(lines, recvOrTimeout(t, status, testTimeout, "a status line"))
	}

	if !strings.HasPrefix(lines[0], "control port listening") || !strings.HasPrefix(lines[1], "warning: ") ||
		lines[2] != switched {
		t.Errorf("status lines in order %q, want the port, its warning, then the switch", lines)
	}

	cancel()

	if err := recvOrTimeout(t, done, testTimeout, "Run to return"); err != nil {
		t.Errorf("Run error = %v, want nil after cancel", err)
	}
}
