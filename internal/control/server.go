package control

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The server's limits. Each has an option, so a test can shrink a timeout
// rather than wait ten minutes for one.
const (
	// DefaultMaxConnections is how many operators can be connected at once.
	// One is the usual number; four leaves room for a forgotten session in
	// another terminal without letting a loop of reconnects pile up.
	DefaultMaxConnections = 4

	// DefaultIdleTimeout is how long a connection may sit without sending a
	// line before it is closed. Long enough to read the picture and think.
	DefaultIdleTimeout = 10 * time.Minute

	// DefaultReplyTimeout is how long a command waits for the run loop. The
	// loop answers between two frames, so anything past a second means it is
	// stuck, and a connection should not hang with it.
	DefaultReplyTimeout = 5 * time.Second

	// writeTimeout bounds every reply, so a client that stops reading cannot
	// hold its session open.
	writeTimeout = time.Second

	// readBuffer holds a full-length line with room to spare for its line
	// ending, so ReadSlice only reports a full buffer for a line that really
	// is too long.
	readBuffer = 2 * MaxLine
)

// Errors the server answers with or returns.
var (
	ErrTimeout = errors.New("control: the run loop did not answer in time")
	ErrClosed  = errors.New("control: shutting down")
	ErrAddress = errors.New("control: address must be [HOST]:PORT")

	// ErrHTTP ends a connection whose line is an HTTP request line. It is
	// never sent: the client is a browser, and nothing it sends is a command.
	ErrHTTP = errors.New("control: HTTP request")
)

// CheckAddress refuses a listen address that is not [HOST]:PORT with a port
// from 1 to 65535. The host may be empty, which listens on every interface;
// the caller is the one who has to decide that is safe.
func CheckAddress(address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %q: %w", ErrAddress, address, err)
	}

	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return fmt.Errorf("%w: %q has no port from 1 to 65535", ErrAddress, address)
	}

	return nil
}

// Request is a validated command on its way to the run loop.
type Request struct {
	Command Command

	reply chan State
}

// Reply answers the request with the state after the command ran. The reply
// channel holds one answer, so this never blocks, and a second call or a reply
// to a connection that stopped waiting is dropped.
func (r Request) Reply(state State) {
	select {
	case r.reply <- state:
	default:
	}
}

// Server accepts operator connections and turns their lines into Requests.
//
// It never touches a scene: Serve's goroutines parse and validate, and the
// run loop that receives from the requests channel does the rest. See the
// package doc for why.
type Server struct {
	listener net.Listener
	requests chan<- Request

	maxConns     int
	idleTimeout  time.Duration
	replyTimeout time.Duration

	// now is the clock idle time is measured on, a field so a test can order
	// the connections without sleeping between them.
	now func() time.Time

	// idle holds when each open connection last sent a line, so a full house
	// can make room by closing the one that has been quiet longest.
	mu   sync.Mutex
	idle map[net.Conn]time.Time
}

// Option changes one of the server's limits.
type Option func(*Server)

// WithMaxConnections sets how many connections may be open at once. Anything
// under one is ignored, since a port nobody can use is not a setting.
func WithMaxConnections(count int) Option {
	return func(s *Server) {
		if count > 0 {
			s.maxConns = count
		}
	}
}

// WithIdleTimeout sets how long a connection may stay silent. Zero or less is
// ignored.
func WithIdleTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		if timeout > 0 {
			s.idleTimeout = timeout
		}
	}
}

// WithReplyTimeout sets how long a command waits for the run loop. Zero or
// less is ignored.
func WithReplyTimeout(timeout time.Duration) Option {
	return func(s *Server) {
		if timeout > 0 {
			s.replyTimeout = timeout
		}
	}
}

// New builds a server on a listener it takes ownership of. Requests go out on
// requests; the run loop receives them and calls Reply once on each.
func New(listener net.Listener, requests chan<- Request, opts ...Option) *Server {
	server := &Server{
		listener:     listener,
		requests:     requests,
		maxConns:     DefaultMaxConnections,
		idleTimeout:  DefaultIdleTimeout,
		replyTimeout: DefaultReplyTimeout,
		now:          time.Now,
		idle:         make(map[net.Conn]time.Time),
	}

	for _, opt := range opts {
		opt(server)
	}

	return server
}

// Serve accepts connections until ctx is cancelled or the listener fails, then
// closes the listener and every connection and waits for all of their
// goroutines before it returns.
//
// It returns nil after a cancelled context. Any other accept error ends the
// server and is returned, and the program goes on without a control port
// rather than with one that has stopped answering.
func (s *Server) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)

	var group sync.WaitGroup

	// Cancel first, then wait: cancelling is what closes the connections the
	// goroutines are blocked reading.
	defer group.Wait()
	defer cancel()
	defer func() { _ = s.listener.Close() }()

	stop := context.AfterFunc(ctx, func() { _ = s.listener.Close() })
	defer stop()

	slots := make(chan struct{}, s.maxConns)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return fmt.Errorf("control: accept: %w", err)
		}

		if !s.admit(ctx, slots) {
			_ = conn.Close()

			return nil
		}

		s.touch(conn)

		group.Go(func() {
			defer func() { <-slots }()
			defer s.forget(conn)

			s.session(ctx, conn)
		})
	}
}

// admit takes a slot for a new connection. With every slot taken it closes
// the connection that has been silent longest and waits for its session to
// give the slot back. Refusing the newcomer instead would let a few clients
// that send a line every few minutes lock the operator out for good. It
// reports false only when the server is stopping.
func (s *Server) admit(ctx context.Context, slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		return true
	default:
	}

	if quietest := s.quietest(); quietest != nil {
		_ = quietest.Close()
	}

	select {
	case slots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// touch records that conn has just been heard from.
func (s *Server) touch(conn net.Conn) {
	s.mu.Lock()
	s.idle[conn] = s.now()
	s.mu.Unlock()
}

// forget drops a connection whose session has ended.
func (s *Server) forget(conn net.Conn) {
	s.mu.Lock()
	delete(s.idle, conn)
	s.mu.Unlock()
}

// quietest is the open connection that has gone longest without a line, or
// nil when a session is just ending and none is left to choose.
func (s *Server) quietest() net.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		oldest net.Conn
		since  time.Time
	)

	for conn, heard := range s.idle {
		if oldest == nil || heard.Before(since) {
			oldest, since = conn, heard
		}
	}

	return oldest
}

// session reads lines from one connection and answers each until the client
// goes away, falls silent for the idle timeout, or the server stops.
func (s *Server) session(ctx context.Context, conn net.Conn) {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReaderSize(conn, readBuffer)

	for {
		_ = conn.SetReadDeadline(time.Now().Add(s.idleTimeout))

		reply, err := s.next(ctx, reader)
		if err != nil {
			return
		}

		s.touch(conn)

		if write(conn, reply) != nil {
			return
		}
	}
}

// next reads one line and works out its reply. The error is only ever the
// connection's own: a bad line is a reply, not a reason to hang up.
func (s *Server) next(ctx context.Context, reader *bufio.Reader) (string, error) {
	line, err := readLine(reader)
	if errors.Is(err, ErrLineTooLong) {
		return failure(err), nil
	}

	if err != nil {
		return "", err
	}

	// A web page can make a browser POST to a loopback port, and the body
	// lines would then run as commands. A browser always sends its request
	// line first, so hanging up on that line means nothing after it runs. A
	// typo still gets an err, which keeps an nc session alive.
	if httpRequestLine(line) {
		return "", ErrHTTP
	}

	return s.answer(ctx, line), nil
}

// httpRequestLine reports whether line has the shape of an HTTP request line:
// a method token, a space, a target, a space and "HTTP/" with a version.
func httpRequestLine(line string) bool {
	method, rest, found := strings.Cut(line, " ")
	if !found || method == "" || strings.ContainsFunc(method, notTokenRune) {
		return false
	}

	space := strings.LastIndexByte(rest, ' ')
	if space <= 0 {
		return false
	}

	version, isHTTP := strings.CutPrefix(rest[space+1:], "HTTP/")

	return isHTTP && version != "" && !strings.ContainsFunc(version, notVersionRune)
}

// notTokenRune is true for anything an HTTP method cannot contain. Methods
// are letters in practice, which is all this needs to recognise.
func notTokenRune(r rune) bool { return (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') }

// notVersionRune is true for anything an HTTP version number cannot contain.
func notVersionRune(r rune) bool { return (r < '0' || r > '9') && r != '.' }

// answer runs one line through the parser and, unless it is help, the run
// loop.
func (s *Server) answer(ctx context.Context, line string) string {
	cmd, err := Parse(line)
	if err != nil {
		return failure(err)
	}

	if cmd.Verb == VerbHelp {
		return "ok " + Help()
	}

	state, err := s.ask(ctx, cmd)
	if err != nil {
		return failure(err)
	}

	return "ok " + state.String()
}

// ask hands a command to the run loop and waits for its answer, for no longer
// than the reply timeout in all.
//
// A command that times out before the loop took it never runs. One that times
// out after may still run, and its answer goes into the request's buffer and
// is dropped; the next get says what happened.
func (s *Server) ask(ctx context.Context, cmd Command) (State, error) {
	timer := time.NewTimer(s.replyTimeout)
	defer timer.Stop()

	req := Request{Command: cmd, reply: make(chan State, 1)}

	select {
	case s.requests <- req:
	case <-timer.C:
		return State{}, ErrTimeout
	case <-ctx.Done():
		return State{}, ErrClosed
	}

	select {
	case state := <-req.reply:
		return state, nil
	case <-timer.C:
		return State{}, ErrTimeout
	case <-ctx.Done():
		return State{}, ErrClosed
	}
}

// readLine reads one line without its line ending. A line longer than MaxLine
// is read to its end and thrown away, so the next line starts clean and the
// connection stays up.
func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return "", discardLine(reader)
	}

	if err != nil {
		return "", fmt.Errorf("control: read: %w", err)
	}

	line = line[:len(line)-1]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	if len(line) > MaxLine {
		return "", ErrLineTooLong
	}

	return string(line), nil
}

// discardLine skips the rest of an overlong line and reports it as one, or
// reports whatever ended the connection part way through it.
func discardLine(reader *bufio.Reader) error {
	for {
		_, err := reader.ReadSlice('\n')

		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err != nil:
			return fmt.Errorf("control: read: %w", err)
		default:
			return ErrLineTooLong
		}
	}
}

// failure is the reply line for an error: "err" and the message without the
// package prefix.
func failure(err error) string {
	return "err " + strings.TrimPrefix(err.Error(), "control: ")
}

// write sends one reply line under the write deadline.
func write(conn net.Conn, line string) error {
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))

	if _, err := io.WriteString(conn, line+"\n"); err != nil {
		return fmt.Errorf("control: write: %w", err)
	}

	return nil
}
