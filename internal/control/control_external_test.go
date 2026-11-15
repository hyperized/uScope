package control_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/control"
	"github.com/hyperized/uScope/internal/input"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/theme"
)

const (
	readLimit = 2 * time.Second

	wordGet  = "get"
	wordHelp = "help"

	settingView       = "view"
	settingTilt       = "tilt"
	settingOrbit      = "orbit"
	settingAzimuth    = "azimuth"
	settingExaggerate = "exaggerate"
	settingTheme      = "theme"
	settingLook       = "look"
	settingColour     = "colour"
)

// TestParseBare pins the commands that take no arguments, and the exact
// spelling the allow list demands.
func TestParseBare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		line     string
		wantVerb control.Verb
		wantErr  error
	}{
		{name: wordGet, line: wordGet, wantVerb: control.VerbGet},
		{name: wordHelp, line: wordHelp, wantVerb: control.VerbHelp},
		{name: "get with tabs around", line: "\tget  ", wantVerb: control.VerbGet},
		{name: "get with argument", line: "get extra", wantErr: control.ErrUsage},
		{name: "help with argument", line: "help x", wantErr: control.ErrUsage},
		{name: "empty", line: "", wantErr: control.ErrUnknownCommand},
		{name: "whitespace only", line: " \t ", wantErr: control.ErrUnknownCommand},
		{name: "upper case", line: "GET", wantErr: control.ErrUnknownCommand},
		{name: "unknown word", line: "frobnicate", wantErr: control.ErrUnknownCommand},
		{name: "invalid utf-8", line: "get \xff", wantErr: control.ErrNotUTF8},
		{name: "too long", line: strings.Repeat("a", control.MaxLine+1), wantErr: control.ErrLineTooLong},
		{name: "max length is fine", line: strings.Repeat("a", control.MaxLine), wantErr: control.ErrUnknownCommand},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd, err := control.Parse(testCase.line)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Parse(%q) error = %v, want %v", testCase.line, err, testCase.wantErr)
			}

			if cmd.Verb != testCase.wantVerb {
				t.Fatalf("Parse(%q) verb = %d, want %d", testCase.line, cmd.Verb, testCase.wantVerb)
			}
		})
	}
}

// TestParseSetUsage pins the shape set must have before any value is read.
func TestParseSetUsage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		wantErr error
	}{
		{name: "no arguments", line: "set", wantErr: control.ErrUsage},
		{name: "one argument", line: "set tilt", wantErr: control.ErrUsage},
		{name: "three arguments", line: "set tilt 20 30", wantErr: control.ErrUsage},
		{name: "unknown setting", line: "set bogus 1", wantErr: control.ErrUnknownSetting},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, err := control.Parse(testCase.line); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Parse(%q) error = %v, want %v", testCase.line, err, testCase.wantErr)
			}
		})
	}
}

// TestParseSetBadValue pins that every setting refuses a bad value with
// ErrValue and with the same sentinel its command-line flag reports.
func TestParseSetBadValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		line     string
		wantErrs []error
	}{
		{name: settingView, line: "set view 3D", wantErrs: []error{radar.ErrView}},
		{name: "shore", line: "set shore maybe", wantErrs: []error{radar.ErrToggle}},
		{name: "airports", line: "set airports maybe", wantErrs: []error{radar.ErrToggle}},
		{name: "bar", line: "set bar yes", wantErrs: []error{radar.ErrToggle}},
		{name: "board", line: "set board yes", wantErrs: []error{radar.ErrToggle}},
		{name: "lights", line: "set lights blinking", wantErrs: []error{radar.ErrToggle}},
		{name: "stars", line: "set stars twinkling", wantErrs: []error{radar.ErrToggle}},
		{name: "corridors", line: "set corridors faint", wantErrs: []error{radar.ErrToggle}},
		{name: "filter", line: "set filter <25K", wantErrs: []error{radar.ErrFilter}},
		{name: "zoom not a number", line: "set zoom far", wantErrs: []error{radar.ErrZoom}},
		{name: "zoom out of range", line: "set zoom 99", wantErrs: []error{radar.ErrZoom}},
		{name: settingTilt, line: "set tilt 99", wantErrs: []error{radar.ErrTilt}},
		{name: settingOrbit, line: "set orbit spin", wantErrs: []error{radar.ErrToggle}},
		{name: settingAzimuth, line: "set azimuth 360", wantErrs: []error{radar.ErrAzimuth}},
		{name: settingExaggerate, line: "set exaggerate 0", wantErrs: []error{radar.ErrExaggerate}},
		{name: "range number", line: "set range 99999", wantErrs: []error{radar.ErrRange}},
		{name: "range word", line: "set range forty", wantErrs: []error{radar.ErrRange}},
		{name: settingTheme, line: "set theme Night", wantErrs: []error{theme.ErrUnknown}},
		{name: settingLook, line: "set look neon", wantErrs: []error{theme.ErrUnknownLook}},
		{name: settingColour, line: "set colour red", wantErrs: []error{radar.ErrColour}},
		{name: "recenter word", line: "set recenter often", wantErrs: []error{radar.ErrRecentre}},
		{name: "recenter too short", line: "set recenter 9s", wantErrs: []error{radar.ErrRecentre}},
		{name: "recenter too long", line: "set recenter 61m", wantErrs: []error{radar.ErrRecentre}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := control.Parse(testCase.line)
			if !errors.Is(err, control.ErrValue) {
				t.Fatalf("Parse(%q) error = %v, want ErrValue", testCase.line, err)
			}

			for _, want := range testCase.wantErrs {
				if !errors.Is(err, want) {
					t.Fatalf("Parse(%q) error = %v, want it to match %v", testCase.line, err, want)
				}
			}
		})
	}
}

// TestParseSetApplies pins which State field each setting writes, including
// the pairs shore and airports set together.
func TestParseSetApplies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		line  string
		check func(state control.State) bool
	}{
		{settingView, "set view 3d", func(state control.State) bool { return state.Radar.View == radar.View3D }},
		{"shore off", "set shore off", func(state control.State) bool {
			return state.Radar.Shore == radar.ToggleOff && !state.Radar.MinimalShore
		}},
		{"shore on", "set shore on", func(state control.State) bool {
			return state.Radar.Shore == radar.ToggleOn && state.Radar.MinimalShore
		}},
		{"airports off", "set airports off", func(state control.State) bool {
			return state.Radar.Airports == radar.ToggleOff && !state.Radar.MinimalAirports
		}},
		{"airports on", "set airports on", func(state control.State) bool {
			return state.Radar.Airports == radar.ToggleOn && state.Radar.MinimalAirports
		}},
		{"bar on", "set bar on", func(state control.State) bool { return state.Radar.MinimalBar }},
		{"bar off", "set bar off", func(state control.State) bool { return !state.Radar.MinimalBar }},
		{"board on", "set board on", func(state control.State) bool { return state.Radar.Board }},
		{"board off", "set board off", func(state control.State) bool { return !state.Radar.Board }},
		{"lights off", "set lights off", func(state control.State) bool { return !state.Radar.Lights }},
		{"lights on", "set lights on", func(state control.State) bool { return state.Radar.Lights }},
		{"stars off", "set stars off", func(state control.State) bool { return !state.Radar.Stars.On() }},
		{"stars on", "set stars on", func(state control.State) bool { return state.Radar.Stars == radar.ToggleOn }},
		{"corridors off", "set corridors off", func(state control.State) bool { return !state.Radar.Corridors.On() }},
		{"corridors on", "set corridors on", func(state control.State) bool {
			return state.Radar.Corridors == radar.ToggleOn
		}},
		{"filter below-high", "set filter below-high", func(state control.State) bool {
			return state.Radar.Filter == radar.FilterBelowHigh
		}},
		{"filter in", "set filter in", func(state control.State) bool {
			return state.Radar.Filter == radar.FilterIn
		}},
		{"filter other", "set filter other", func(state control.State) bool {
			return state.Radar.Filter == radar.FilterOther
		}},
		{"colour corridor", "set colour corridor", func(state control.State) bool {
			return state.Radar.Colour == radar.ColourCorridor
		}},
		{"filter all", "set filter all", func(state control.State) bool {
			return state.Radar.Filter == radar.FilterAll
		}},
		{"zoom", "set zoom -2", func(state control.State) bool { return state.Radar.Zoom == -2 }},
		{"tilt", "set tilt 25", func(state control.State) bool { return state.Radar.Tilt == 25 }},
		{"orbit", "set orbit off", func(state control.State) bool { return state.Radar.Orbit == radar.ToggleOff }},
		{"azimuth", "set azimuth 90", func(state control.State) bool { return state.Radar.Azimuth == 90 }},
		{"exaggerate", "set exaggerate 5", func(state control.State) bool { return state.Radar.Exaggerate == 5 }},
		{"range auto", "set range auto", func(state control.State) bool { return state.Radar.RangeNm == 0 }},
		{"range number", "set range 60", func(state control.State) bool { return state.Radar.RangeNm == 60 }},
		{"theme", "set theme day", func(state control.State) bool { return state.Theme == theme.KindDay }},
		{"look", "set look mono", func(state control.State) bool { return state.Look == theme.LookMono }},
		{"look vector", "set look vector", func(state control.State) bool {
			return state.Look == theme.LookVector
		}},
		{settingColour, "set colour airline", func(state control.State) bool {
			return state.Radar.Colour == radar.ColourAirline
		}},
		{"recenter off", "set recenter 0", func(state control.State) bool { return state.Radar.Recentre == 0 }},
		{"recenter shortest", "set recenter 10s", func(state control.State) bool {
			return state.Radar.Recentre == radar.MinRecentre
		}},
		{"recenter longest", "set recenter 1h", func(state control.State) bool {
			return state.Radar.Recentre == radar.MaxRecentre
		}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd, err := control.Parse(testCase.line)
			if err != nil {
				t.Fatalf("Parse(%q): %v", testCase.line, err)
			}

			if cmd.Verb != control.VerbSet {
				t.Fatalf("Parse(%q) verb = %d, want VerbSet", testCase.line, cmd.Verb)
			}

			var state control.State

			cmd.Change(&state)

			if !testCase.check(state) {
				t.Fatalf("Parse(%q) change left the wrong state: %+v", testCase.line, state)
			}
		})
	}
}

// TestParseKey pins the key allow list: named keys, one printable rune, and
// no way to quit.
func TestParseKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		want    input.Key
		wantErr error
	}{
		{name: "letter", line: "key m", want: input.Key{Kind: input.Rune, Rune: 'm'}},
		{name: "bracket", line: "key ]", want: input.Key{Kind: input.Rune, Rune: ']'}},
		{name: "multi-byte rune", line: "key é", want: input.Key{Kind: input.Rune, Rune: 'é'}},
		{name: "up", line: "key up", want: input.Key{Kind: input.Up}},
		{name: "down", line: "key down", want: input.Key{Kind: input.Down}},
		{name: "left", line: "key left", want: input.Key{Kind: input.Left}},
		{name: "right", line: "key right", want: input.Key{Kind: input.Right}},
		{name: "enter", line: "key enter", want: input.Key{Kind: input.Enter}},
		{name: "quit lower", line: "key q", wantErr: control.ErrQuit},
		{name: "bias-tee lower", line: "key b", wantErr: control.ErrBiasTee},
		{name: "bias-tee upper", line: "key B", wantErr: control.ErrBiasTee},
		{name: "quit upper", line: "key Q", wantErr: control.ErrQuit},
		{name: "escape", line: "key esc", wantErr: control.ErrQuit},
		{name: "no key", line: "key", wantErr: control.ErrUsage},
		{name: "two keys", line: "key a b", wantErr: control.ErrUsage},
		{name: "two characters", line: "key ab", wantErr: control.ErrKey},
		{name: "control character", line: "key \x01", wantErr: control.ErrKey},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd, err := control.Parse(testCase.line)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Parse(%q) error = %v, want %v", testCase.line, err, testCase.wantErr)
			}

			if testCase.wantErr != nil {
				return
			}

			if cmd.Verb != control.VerbKey || cmd.Key != testCase.want {
				t.Fatalf("Parse(%q) = verb %d key %+v, want VerbKey %+v",
					testCase.line, cmd.Verb, cmd.Key, testCase.want)
			}
		})
	}
}

// TestStateStringEndsWithTheFilter pins the newest field where scripts that
// cut the reply by position expect it: last.
func TestStateStringEndsWithTheFilter(t *testing.T) {
	t.Parallel()

	state := control.State{Radar: radar.Settings{Filter: radar.FilterBelowHigh}}
	if got := state.String(); !strings.HasSuffix(got, " corridors=on filter=below-high board=off") {
		t.Errorf("String() = %q, want it to end with filter=below-high board=off", got)
	}
}

// TestHelp pins that help names every setting, so the list cannot drift from
// the allow list it describes.
func TestHelp(t *testing.T) {
	t.Parallel()

	text := control.Help()

	words := []string{
		settingView, "shore", "airports", "zoom", settingTilt, settingOrbit, settingAzimuth, settingExaggerate,
		"range", settingTheme, settingLook, settingColour, "bar", "lights", "stars", "recenter",
		"corridors", "filter", wordGet, "set", "key", wordHelp,
	}

	for _, word := range words {
		t.Run(word, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(text, word) {
				t.Fatalf("Help() does not mention %q: %s", word, text)
			}
		})
	}
}

// TestStateStringReportsTheBoard pins the board as the last field, on.
func TestStateStringReportsTheBoard(t *testing.T) {
	t.Parallel()

	state := control.State{Radar: radar.Settings{Board: true}}
	if got := state.String(); !strings.HasSuffix(got, " filter=all board=on") {
		t.Errorf("String() = %q, want it to end with filter=all board=on", got)
	}
}

// TestStateString pins the reply line's field order and its rounding, which
// scripts cut fields out of.
func TestStateString(t *testing.T) {
	t.Parallel()

	const zero = "view=scope shore=on airports=on zoom=0 tilt=0 orbit=on azimuth=0 exaggerate=0 range=auto " +
		"theme=night look=glass colour=altitude bar=off lights=off stars=on recenter=0 corridors=on " +
		"filter=all board=off"

	tests := []struct {
		name  string
		state control.State
		want  string
	}{
		{name: "zero state", state: control.State{}, want: zero},
		{
			name: "bare view reports the minimal pair",
			state: control.State{Radar: radar.Settings{
				View: radar.ViewMinimal3D, Shore: radar.ToggleOn, Airports: radar.ToggleOn,
			}},
			want: "view=minimal3d shore=off airports=off zoom=0 tilt=0 orbit=on azimuth=0 exaggerate=0 " +
				"range=auto theme=night look=glass colour=altitude bar=off lights=off stars=on recenter=0 " +
				"corridors=on filter=all board=off",
		},
		{
			name:  "fixed range",
			state: control.State{Radar: radar.Settings{RangeNm: 60}},
			want:  strings.Replace(zero, "range=auto", "range=60", 1),
		},
		{
			name:  "azimuth wraps",
			state: control.State{Radar: radar.Settings{Azimuth: 359.96}},
			want:  zero,
		},
		{
			name:  "tilt rounds",
			state: control.State{Radar: radar.Settings{Tilt: 22.54}},
			want:  strings.Replace(zero, "tilt=0", "tilt=22.5", 1),
		},
		{
			name:  "orbit off",
			state: control.State{Radar: radar.Settings{Orbit: radar.ToggleOff}},
			want:  strings.Replace(zero, "orbit=on", "orbit=off", 1),
		},
		{
			// One bar whatever the view, unlike the shore and airport pairs.
			name:  "bar on in the scope",
			state: control.State{Radar: radar.Settings{MinimalBar: true}},
			want:  strings.Replace(zero, "bar=off", "bar=on", 1),
		},
		{
			name:  "lights on",
			state: control.State{Radar: radar.Settings{Lights: true}},
			want:  strings.Replace(zero, "lights=off", "lights=on", 1),
		},
		{
			name:  "stars off",
			state: control.State{Radar: radar.Settings{Stars: radar.ToggleOff}},
			want:  strings.Replace(zero, "stars=on", "stars=off", 1),
		},
		{
			name:  "corridors off",
			state: control.State{Radar: radar.Settings{Corridors: radar.ToggleOff}},
			want:  strings.Replace(zero, "corridors=on", "corridors=off", 1),
		},
		{
			name:  "recenter in whole hours",
			state: control.State{Radar: radar.Settings{Recentre: time.Hour}},
			want:  strings.Replace(zero, "recenter=0", "recenter=1h", 1),
		},
		{
			name:  "recenter in whole minutes",
			state: control.State{Radar: radar.Settings{Recentre: radar.DefaultRecentre}},
			want:  strings.Replace(zero, "recenter=0", "recenter=3m", 1),
		},
		{
			// Ninety minutes is past the flag's hour, but Fields spells whatever
			// it is handed; the hour test has to come before the minute one.
			name:  "recenter in minutes past the hour",
			state: control.State{Radar: radar.Settings{Recentre: 90 * time.Minute}},
			want:  strings.Replace(zero, "recenter=0", "recenter=90m", 1),
		},
		{
			name:  "recenter in seconds",
			state: control.State{Radar: radar.Settings{Recentre: 90 * time.Second}},
			want:  strings.Replace(zero, "recenter=0", "recenter=1m30s", 1),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.state.String(); got != testCase.want {
				t.Fatalf("String() =\n %s\nwant\n %s", got, testCase.want)
			}
		})
	}
}

// TestCheckAddress pins which listen addresses the flag may hand the server.
func TestCheckAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		address string
		wantErr error
	}{
		{name: "loopback", address: "127.0.0.1:7777"},
		{name: "every interface", address: ":7777"},
		{name: "hostname", address: "localhost:1"},
		{name: "ipv6", address: "[::1]:65535"},
		{name: "no colon", address: "7777", wantErr: control.ErrAddress},
		{name: "port zero", address: "127.0.0.1:0", wantErr: control.ErrAddress},
		{name: "port too high", address: "127.0.0.1:65536", wantErr: control.ErrAddress},
		{name: "named port", address: "127.0.0.1:http", wantErr: control.ErrAddress},
		{name: "empty", address: "", wantErr: control.ErrAddress},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if err := control.CheckAddress(testCase.address); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("CheckAddress(%q) = %v, want %v", testCase.address, err, testCase.wantErr)
			}
		})
	}
}

// loopMode says whether the harness runs a fake run loop.
type loopMode int

const (
	loopAnswers loopMode = iota
	loopAbsent
)

// harness is a running server plus the bookkeeping to stop it.
type harness struct {
	address  string
	cancel   context.CancelFunc
	serveErr chan error
	group    sync.WaitGroup

	// asked counts the requests the fake run loop has answered, which is how
	// a test proves a line never became a command.
	asked atomic.Int32
}

// startServer runs a server on a loopback port and, unless mode is loopAbsent, a fake
// run loop that answers every request with a tilt of 25. It is stopped and
// joined when the test ends.
func startServer(tb testing.TB, mode loopMode, opts ...control.Option) *harness {
	tb.Helper()

	ctx, cancel := context.WithCancel(tb.Context())

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		tb.Fatalf("listen: %v", err)
	}

	requests := make(chan control.Request)
	srv := control.New(listener, requests, opts...)
	hrn := &harness{address: listener.Addr().String(), cancel: cancel, serveErr: make(chan error, 1)}

	hrn.group.Go(func() { hrn.serveErr <- srv.Serve(ctx) })

	if mode == loopAnswers {
		hrn.group.Go(func() {
			for {
				select {
				case req := <-requests:
					hrn.asked.Add(1)
					req.Reply(control.State{Radar: radar.Settings{Tilt: 25}})
				case <-ctx.Done():
					return
				}
			}
		})
	}

	tb.Cleanup(func() {
		cancel()
		hrn.group.Wait()
	})

	return hrn
}

// client is one connection with a buffered reader on it.
type client struct {
	conn   net.Conn
	reader *bufio.Reader
}

// dial connects to the harness and closes the connection when the test ends.
func dial(tb testing.TB, hrn *harness) *client {
	tb.Helper()

	conn, err := (&net.Dialer{}).DialContext(tb.Context(), "tcp", hrn.address)
	if err != nil {
		tb.Fatalf("dial: %v", err)
	}

	tb.Cleanup(func() { _ = conn.Close() })

	return &client{conn: conn, reader: bufio.NewReader(conn)}
}

// send writes raw text to the server.
func (c *client) send(tb testing.TB, text string) {
	tb.Helper()

	_ = c.conn.SetWriteDeadline(time.Now().Add(readLimit))

	if _, err := io.WriteString(c.conn, text); err != nil {
		tb.Fatalf("write %q: %v", text, err)
	}
}

// line reads one reply line without its newline.
func (c *client) line(tb testing.TB) string {
	tb.Helper()

	_ = c.conn.SetReadDeadline(time.Now().Add(readLimit))

	text, err := c.reader.ReadString('\n')
	if err != nil {
		tb.Fatalf("read reply: %v", err)
	}

	return strings.TrimSuffix(text, "\n")
}

// expectEOF fails unless the server closes the connection before the limit.
func (c *client) expectEOF(tb testing.TB) {
	tb.Helper()

	_ = c.conn.SetReadDeadline(time.Now().Add(readLimit))

	if _, err := c.reader.ReadByte(); !errors.Is(err, io.EOF) {
		tb.Fatalf("read error = %v, want io.EOF", err)
	}
}

// roundTrip sends one line and returns the reply.
func (c *client) roundTrip(tb testing.TB, text string) string {
	tb.Helper()

	c.send(tb, text+"\n")

	return c.line(tb)
}

// TestServerReplies pins the wire text of the replies a client sees, which is
// the protocol itself.
func TestServerReplies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		send       string
		wantExact  string
		wantPrefix string
		wantHas    string
	}{
		{name: "get", send: "get\n", wantPrefix: "ok view=", wantHas: "tilt=25"},
		{name: "get with crlf", send: "get\r\n", wantPrefix: "ok view=", wantHas: "tilt=25"},
		{name: "help", send: "help\n", wantPrefix: "ok commands:"},
		{name: "unknown command", send: "bogus\n", wantExact: "err unknown command"},
		{name: "quit refused", send: "key q\n", wantExact: "err quitting is not available over the control port"},
		{name: "not utf-8", send: "get \xff\n", wantExact: "err line is not UTF-8"},
		{name: "line too long", send: strings.Repeat("a", 300) + "\n", wantPrefix: "err line too long"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			conn := dial(t, startServer(t, loopAnswers))
			conn.send(t, testCase.send)

			got := conn.line(t)

			if testCase.wantExact != "" && got != testCase.wantExact {
				t.Fatalf("reply = %q, want %q", got, testCase.wantExact)
			}

			if !strings.HasPrefix(got, testCase.wantPrefix) {
				t.Fatalf("reply = %q, want prefix %q", got, testCase.wantPrefix)
			}

			if !strings.Contains(got, testCase.wantHas) {
				t.Fatalf("reply = %q, want it to contain %q", got, testCase.wantHas)
			}

			// A refused line must not cost the client its connection.
			if again := conn.roundTrip(t, "get"); !strings.HasPrefix(again, "ok view=") {
				t.Fatalf("get after %q = %q", testCase.name, again)
			}
		})
	}
}

// TestServerHelpSkipsRunLoop pins that help is answered by the server itself,
// so it works while the run loop is stuck.
func TestServerHelpSkipsRunLoop(t *testing.T) {
	t.Parallel()

	conn := dial(t, startServer(t, loopAbsent, control.WithReplyTimeout(30*time.Millisecond)))

	if got := conn.roundTrip(t, "help"); !strings.HasPrefix(got, "ok commands:") {
		t.Fatalf("help reply = %q", got)
	}
}

// TestServerEvictsTheQuietest pins that a connection over the limit is let
// in by closing the one that has been silent longest, so clients that stay
// connected cannot lock the operator out.
func TestServerEvictsTheQuietest(t *testing.T) {
	t.Parallel()

	hrn := startServer(t, loopAnswers, control.WithMaxConnections(2))

	quiet := dial(t, hrn)
	if got := quiet.roundTrip(t, "get"); !strings.HasPrefix(got, "ok view=") {
		t.Fatalf("first get = %q", got)
	}

	busy := dial(t, hrn)
	if got := busy.roundTrip(t, "get"); !strings.HasPrefix(got, "ok view=") {
		t.Fatalf("second get = %q", got)
	}

	newcomer := dial(t, hrn)
	if got := newcomer.roundTrip(t, "get"); !strings.HasPrefix(got, "ok view=") {
		t.Fatalf("newcomer get = %q", got)
	}

	quiet.expectEOF(t)

	if got := busy.roundTrip(t, "get"); !strings.HasPrefix(got, "ok view=") {
		t.Fatalf("the more recent client lost its connection: %q", got)
	}
}

// TestServerHangsUpOnHTTP sends what a browser sends when a web page POSTs to
// the port: the connection is closed without a reply, and no line of the
// body reaches the run loop.
func TestServerHangsUpOnHTTP(t *testing.T) {
	t.Parallel()

	hrn := startServer(t, loopAnswers)
	conn := dial(t, hrn)

	body := "set view 3d\nget\n"
	conn.send(t, "POST / HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Type: text/plain\r\n"+
		"Content-Length: "+strconv.Itoa(len(body))+"\r\n\r\n"+body)

	conn.expectEOF(t)

	if got := hrn.asked.Load(); got != 0 {
		t.Errorf("the run loop was asked %d times, want none", got)
	}

	// A typo is not a request line and keeps the connection, which is what
	// an operator at nc needs.
	other := dial(t, hrn)
	if got := other.roundTrip(t, "POST / HTTP"); got != "err unknown command" {
		t.Errorf("a near miss = %q, want an err reply", got)
	}
}

// TestServerIdleTimeout pins that a silent client is dropped, so forgotten
// sessions do not hold a slot forever.
func TestServerIdleTimeout(t *testing.T) {
	t.Parallel()

	conn := dial(t, startServer(t, loopAnswers, control.WithIdleTimeout(50*time.Millisecond)))

	conn.expectEOF(t)
}

// TestServerReplyTimeout pins that a stuck run loop becomes an error reply
// instead of a hung connection.
func TestServerReplyTimeout(t *testing.T) {
	t.Parallel()

	conn := dial(t, startServer(t, loopAbsent, control.WithReplyTimeout(30*time.Millisecond)))

	want := "err the run loop did not answer in time"
	if got := conn.roundTrip(t, "get"); got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}

// TestServerShutdown pins that cancelling the context ends Serve cleanly,
// closes connected clients and stops accepting new ones.
func TestServerShutdown(t *testing.T) {
	t.Parallel()

	hrn := startServer(t, loopAnswers)
	conn := dial(t, hrn)

	if got := conn.roundTrip(t, "get"); !strings.HasPrefix(got, "ok view=") {
		t.Fatalf("get = %q", got)
	}

	hrn.cancel()

	conn.expectEOF(t)

	select {
	case err := <-hrn.serveErr:
		if err != nil {
			t.Fatalf("Serve = %v, want nil after cancel", err)
		}
	case <-time.After(readLimit):
		t.Fatal("Serve did not return after cancel")
	}

	dialer := net.Dialer{Timeout: readLimit}

	again, err := dialer.DialContext(t.Context(), "tcp", hrn.address)
	if err == nil {
		_ = again.Close()

		t.Fatal("dial succeeded after shutdown")
	}
}

// TestServerIgnoresBadOptions pins that a zero or negative limit leaves the
// default in place instead of making the port unusable.
func TestServerIgnoresBadOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opt  control.Option
	}{
		{name: "max connections zero", opt: control.WithMaxConnections(0)},
		{name: "idle timeout zero", opt: control.WithIdleTimeout(0)},
		{name: "reply timeout negative", opt: control.WithReplyTimeout(-1)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			conn := dial(t, startServer(t, loopAnswers, testCase.opt))

			if got := conn.roundTrip(t, "get"); !strings.HasPrefix(got, "ok view=") {
				t.Fatalf("get = %q", got)
			}
		})
	}
}

// TestParseSettingErrors checks set's parser without the verb refuses the
// same things set does, with the same sentinels.
func TestParseSettingErrors(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, setting, value string
		wantErr              error
	}{
		{name: "unknown name", setting: "trails", value: "long", wantErr: control.ErrUnknownSetting},
		{name: "value out of range", setting: settingTilt, value: "400", wantErr: control.ErrValue},
		{name: "value not on the allow list", setting: settingView, value: "4d", wantErr: control.ErrValue},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			change, err := control.ParseSetting(testCase.setting, testCase.value)
			if change != nil || !errors.Is(err, testCase.wantErr) {
				t.Errorf("ParseSetting(%q, %q) = %t, %v; want errors.Is(%v)",
					testCase.setting, testCase.value, change != nil, err, testCase.wantErr)
			}
		})
	}
}

// TestFieldsParseBack is the property a state file depends on: every field
// Fields reports goes back through ParseSetting and lands on a state that
// reports the same fields.
func TestFieldsParseBack(t *testing.T) {
	t.Parallel()

	want := control.State{
		Radar: radar.Settings{
			View: radar.ViewMinimal3D, MinimalShore: true, Zoom: -2, Tilt: 22.5, Orbit: radar.ToggleOff,
			Azimuth: 359.99, Exaggerate: 4, RangeNm: 60, Colour: radar.ColourAirline, MinimalBar: true,
			Board: true, Stars: radar.ToggleOff, Recentre: 90 * time.Second,
		},
		Theme: theme.KindDay, Look: theme.LookMono,
	}

	var got control.State

	for _, field := range want.Fields() {
		change, err := control.ParseSetting(field.Name, field.Value)
		if err != nil {
			t.Fatalf("ParseSetting(%q, %q): %v", field.Name, field.Value, err)
		}

		change(&got)
	}

	if got.String() != want.String() {
		t.Errorf("read back:\n %s\nwant\n %s", got, want)
	}
}
