package control

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/radar"
)

const testGet = "get"

var errAcceptBoom = errors.New("scripted accept failure")

// failingListener fails every Accept at once and counts the Close calls, so a
// test can see that Serve gave the listener back.
type failingListener struct {
	closed atomic.Int32
}

func (*failingListener) Accept() (net.Conn, error) { return nil, errAcceptBoom }

func (listener *failingListener) Close() error {
	listener.closed.Add(1)

	return nil
}

func (*failingListener) Addr() net.Addr { return &net.TCPAddr{} }

// newLineReader wraps text in a reader sized the way the session sizes its own.
func newLineReader(tb testing.TB, text string) *bufio.Reader {
	tb.Helper()

	return bufio.NewReaderSize(strings.NewReader(text), readBuffer)
}

// TestReadLine pins how one line is cut out of the connection: line endings
// removed, the length limit exact, and an EOF never mistaken for a line.
func TestReadLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "plain line", input: "get\n", want: testGet},
		{name: "crlf", input: "get\r\n", want: testGet},
		{name: "empty line", input: "\n", want: ""},
		{name: "exactly max line", input: strings.Repeat("a", MaxLine) + "\n", want: strings.Repeat("a", MaxLine)},
		{name: "max line with crlf", input: strings.Repeat("a", MaxLine) + "\r\n", want: strings.Repeat("a", MaxLine)},
		{name: "one over max line", input: strings.Repeat("a", MaxLine+1) + "\n", wantErr: ErrLineTooLong},
		{name: "longer than the buffer", input: strings.Repeat("a", readBuffer+10) + "\n", wantErr: ErrLineTooLong},
		{name: "eof mid overlong line", input: strings.Repeat("a", readBuffer+10), wantErr: io.EOF},
		{name: "eof without newline", input: "get", wantErr: io.EOF},
		{name: "empty input", input: "", wantErr: io.EOF},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := readLine(newLineReader(t, testCase.input))
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("readLine error = %v, want %v", err, testCase.wantErr)
			}

			if got != testCase.want {
				t.Fatalf("readLine = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestReadLineRecoversAfterOverlongLine pins that an overlong line is
// swallowed whole, so the next command is not read from the middle of it.
func TestReadLineRecoversAfterOverlongLine(t *testing.T) {
	t.Parallel()

	reader := newLineReader(t, strings.Repeat("a", 3*readBuffer)+"\nget\n")

	if _, err := readLine(reader); !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("first readLine error = %v, want ErrLineTooLong", err)
	}

	got, err := readLine(reader)
	if err != nil || got != testGet {
		t.Fatalf("second readLine = %q, %v, want \"get\", nil", got, err)
	}
}

// TestDiscardLine pins the three ways skipping the rest of a line can end.
func TestDiscardLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "rest fits the buffer", input: "tail\n", wantErr: ErrLineTooLong},
		{name: "rest spans several buffers", input: strings.Repeat("a", 3*readBuffer) + "\n", wantErr: ErrLineTooLong},
		{name: "connection ends first", input: strings.Repeat("a", 3*readBuffer), wantErr: io.EOF},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if err := discardLine(newLineReader(t, testCase.input)); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("discardLine error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

// TestFailure pins that the reply drops the package prefix, which is the wire
// format scripts match on.
func TestFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "prefixed", err: ErrTimeout, want: "err the run loop did not answer in time"},
		{name: "wrapped keeps its tail", err: errors.Join(ErrUsage), want: "err usage"},
		{name: "foreign error is left alone", err: errAcceptBoom, want: "err scripted accept failure"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := failure(testCase.err); got != testCase.want {
				t.Fatalf("failure = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestNumber pins the one-decimal rendering of camera values with no trailing
// zero.
func TestNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value float64
		want  string
	}{
		{name: "integer", value: 10, want: "10"},
		{name: "rounds down", value: 123.44, want: "123.4"},
		{name: "rounds up", value: 22.55, want: "22.6"},
		{name: "zero", value: 0, want: "0"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := number(testCase.value); got != testCase.want {
				t.Fatalf("number(%v) = %q, want %q", testCase.value, got, testCase.want)
			}
		})
	}
}

// TestCompass pins that an azimuth which rounds up to 360 reads as north, so
// a reply never reports a value set would refuse.
func TestCompass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		degrees float64
		want    float64
	}{
		{name: "wraps onto zero", degrees: 359.96, want: 0},
		{name: "rounds to a tenth", degrees: 123.44, want: 123.4},
		{name: "plain value", degrees: 10, want: 10},
		{name: "just under the wrap", degrees: 359.9, want: 359.9},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := compass(testCase.degrees); got != testCase.want {
				t.Fatalf("compass(%v) = %v, want %v", testCase.degrees, got, testCase.want)
			}
		})
	}
}

// TestRequestReplyTwice pins that a second answer is dropped instead of
// blocking the run loop, and that the first one wins.
func TestRequestReplyTwice(t *testing.T) {
	t.Parallel()

	req := Request{reply: make(chan State, 1)}

	done := make(chan struct{})

	go func() {
		defer close(done)

		req.Reply(State{Radar: radar.Settings{Zoom: 1}})
		req.Reply(State{Radar: radar.Settings{Zoom: 2}})
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Reply blocked on its second call")
	}

	if got := (<-req.reply).Radar.Zoom; got != 1 {
		t.Fatalf("first reply lost: zoom = %d, want 1", got)
	}
}

// TestWriteClosedConn pins that a failed write is reported, which is what
// ends a session whose client has gone.
func TestWriteClosedConn(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()

	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}

	defer func() { _ = server.Close() }()

	if err := write(server, "ok"); err == nil {
		t.Fatal("write to a closed peer returned nil")
	}
}

// TestServeAcceptError pins that a dead listener ends Serve with the cause
// wrapped, and that the listener is released rather than left open.
func TestServeAcceptError(t *testing.T) {
	t.Parallel()

	listener := &failingListener{}
	srv := New(listener, make(chan Request))

	err := srv.Serve(t.Context())
	if !errors.Is(err, errAcceptBoom) {
		t.Fatalf("Serve error = %v, want it to wrap the accept failure", err)
	}

	if listener.closed.Load() == 0 {
		t.Fatal("Serve returned without closing the listener")
	}
}

// TestAsk pins every way a command can fail to get an answer from the run
// loop, and that each one maps to the right sentinel.
func TestAsk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		run     func(tb testing.TB, requests chan Request, cancel context.CancelFunc, wg *sync.WaitGroup)
		wantErr error
	}{
		{
			name:    "timeout before the loop takes the request",
			run:     func(testing.TB, chan Request, context.CancelFunc, *sync.WaitGroup) {},
			wantErr: ErrTimeout,
		},
		{
			name: "timeout after the loop took it",
			run: func(_ testing.TB, requests chan Request, _ context.CancelFunc, wg *sync.WaitGroup) {
				wg.Go(func() {
					select {
					case <-requests:
					case <-time.After(time.Second):
					}
				})
			},
			wantErr: ErrTimeout,
		},
		{
			name: "cancelled while sending",
			run: func(_ testing.TB, _ chan Request, cancel context.CancelFunc, _ *sync.WaitGroup) {
				cancel()
			},
			wantErr: ErrClosed,
		},
		{
			name: "cancelled while waiting for the reply",
			run: func(_ testing.TB, requests chan Request, cancel context.CancelFunc, wg *sync.WaitGroup) {
				wg.Go(func() {
					select {
					case <-requests:
					case <-time.After(time.Second):
					}

					cancel()
				})
			},
			wantErr: ErrClosed,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			// A long timeout for the cancel cases, so only the context can end them.
			timeout := 20 * time.Millisecond
			if errors.Is(testCase.wantErr, ErrClosed) {
				timeout = time.Minute
			}

			requests := make(chan Request)
			srv := New(nil, requests, WithReplyTimeout(timeout))

			var group sync.WaitGroup

			testCase.run(t, requests, cancel, &group)

			_, err := srv.ask(ctx, Command{Verb: VerbGet})

			group.Wait()

			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("ask error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

// TestSessionEndsWhenReplyCannotBeWritten pins that a client which vanishes
// after sending a command frees its slot instead of leaving the session
// looping on a dead connection.
func TestSessionEndsWhenReplyCannotBeWritten(t *testing.T) {
	t.Parallel()

	serverConn, clientConn := net.Pipe()
	srv := New(nil, make(chan Request))

	var group sync.WaitGroup

	group.Go(func() { srv.session(t.Context(), serverConn) })

	// net.Pipe is unbuffered: the write returns once the session has read the
	// line, and the close then lands before the session can answer.
	if _, err := io.WriteString(clientConn, "help\n"); err != nil {
		t.Fatalf("write command: %v", err)
	}

	if err := clientConn.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}

	group.Wait()
}

// TestHTTPRequestLine pins the shape the server hangs up on: a browser's
// request line in its usual forms, and nothing an operator would type.
func TestHTTPRequestLine(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		line string
		want bool
	}{
		{line: "POST / HTTP/1.1", want: true},
		{line: "GET /favicon.ico?x=1 HTTP/1.0", want: true},
		{line: "OPTIONS * HTTP/2", want: true},
		{line: "set view 3d"},
		{line: "get"},
		{line: "POST / HTTP/"},
		{line: "POST / HTTPS/1.1"},
		{line: "POST / HTTP/1.x"},
		{line: "P0ST / HTTP/1.1"},
		{line: " / HTTP/1.1"},
		{line: "POST HTTP/1.1"},
	} {
		t.Run(testCase.line, func(t *testing.T) {
			t.Parallel()

			if got := httpRequestLine(testCase.line); got != testCase.want {
				t.Errorf("httpRequestLine(%q) = %v, want %v", testCase.line, got, testCase.want)
			}
		})
	}
}

// TestAdmitWithNobodyToEvict covers a full house whose last session is
// already on its way out: there is no one to close, and a server that stops
// meanwhile lets the newcomer go instead of waiting for ever.
func TestAdmitWithNobodyToEvict(t *testing.T) {
	t.Parallel()

	srv := New(&failingListener{}, make(chan Request))
	slots := make(chan struct{}, 1)

	slots <- struct{}{}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if srv.admit(ctx, slots) {
		t.Error("admit = true with every slot taken and the server stopping")
	}
}

// stuckConn is a connection whose Read ignores Close until the test releases
// it, the way a session waiting on a slow run loop holds its slot past being
// evicted. closed is signalled on every Close.
type stuckConn struct {
	net.Conn

	release chan struct{}
	closed  chan struct{}
}

func (c *stuckConn) Read([]byte) (int, error) {
	<-c.release

	return 0, io.EOF
}

func (*stuckConn) Write(data []byte) (int, error)   { return len(data), nil }
func (*stuckConn) SetReadDeadline(time.Time) error  { return nil }
func (*stuckConn) SetWriteDeadline(time.Time) error { return nil }

func (c *stuckConn) Close() error {
	select {
	case c.closed <- struct{}{}:
	default:
	}

	return nil
}

// queueListener hands out the connections a test queues, and fails Accept
// once it is closed.
type queueListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func (l *queueListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *queueListener) Close() error {
	l.once.Do(func() { close(l.done) })

	return nil
}

func (*queueListener) Addr() net.Addr { return &net.TCPAddr{} }

// TestServeStopsWhileMakingRoom covers a newcomer waiting for an evicted
// session to give its slot back when the server is told to stop: Serve
// closes the newcomer and returns once the evicted session ends.
func TestServeStopsWhileMakingRoom(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	held := &stuckConn{release: release, closed: make(chan struct{}, 4)}
	waiting := &stuckConn{release: release, closed: make(chan struct{}, 4)}
	listener := &queueListener{conns: make(chan net.Conn), done: make(chan struct{})}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	srv := New(listener, make(chan Request), WithMaxConnections(1))
	served := make(chan error, 1)

	go func() { served <- srv.Serve(ctx) }()

	listener.conns <- held

	listener.conns <- waiting

	select {
	case <-held.closed:
	case <-time.After(time.Second):
		t.Fatal("the held session was never evicted")
	}

	cancel()

	select {
	case <-waiting.closed:
	case <-time.After(time.Second):
		t.Fatal("the waiting connection was not closed when the server stopped")
	}

	close(release)

	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve = %v, want nil after cancel", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not return")
	}
}
