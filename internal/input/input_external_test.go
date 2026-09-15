package input_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/input"
)

var errBoom = errors.New("scripted reader failure")

// readerStep is one scripted return from scriptedReader.Read.
type readerStep struct {
	data []byte
	err  error
}

// scriptedReader plays back a fixed list of (bytes, error) reads, the shape
// a raw tty read loop actually sees: data, timeouts, and a final error.
type scriptedReader struct {
	steps []readerStep
	idx   int
}

func (reader *scriptedReader) Read(buf []byte) (int, error) {
	if reader.idx >= len(reader.steps) {
		return 0, io.EOF
	}

	step := reader.steps[reader.idx]
	reader.idx++

	n := copy(buf, step.data)

	return n, step.err
}

// gatedReader blocks its Read call until the test releases it, and closes
// reached right beforehand so the test knows Read has moved past its
// ctx.Err() check for that iteration and is sitting inside src.Read.
type gatedReader struct {
	reached chan struct{}
	proceed chan struct{}
	data    []byte
}

func (reader *gatedReader) Read(buf []byte) (int, error) {
	close(reader.reached)
	<-reader.proceed

	n := copy(buf, reader.data)

	return n, nil
}

// recvKey waits for one key on out, failing the test instead of hanging if
// none arrives in time.
func recvKey(t *testing.T, out <-chan input.Key) input.Key {
	t.Helper()

	const recvTimeout = time.Second

	select {
	case key := <-out:
		return key
	case <-time.After(recvTimeout):
		t.Fatal("timed out waiting for a key")

		return input.Key{}
	}
}

func TestDecoder_PlainRunes(t *testing.T) {
	t.Parallel()

	const highByte = 0xC3

	tests := []struct {
		name string
		char byte
		want rune
	}{
		{name: "lowercase q", char: 'q', want: 'q'},
		{name: "uppercase Q", char: 'Q', want: 'Q'},
		{name: "lowercase a", char: 'a', want: 'a'},
		{name: "byte above ascii kept whole", char: highByte, want: highByte},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			var dec input.Decoder

			got := dec.Feed([]byte{tcase.char})
			want := []input.Key{{Kind: input.Rune, Rune: tcase.want}}

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Feed(%q) = %#v, want %#v", tcase.char, got, want)
			}
		})
	}
}

func TestDecoder_CtrlAndEnter(t *testing.T) {
	t.Parallel()

	const (
		ctrlC  = 0x03
		crByte = 0x0D
		lfByte = 0x0A
	)

	tests := []struct {
		name string
		char byte
		want input.Kind
	}{
		{name: "ctrl-c", char: ctrlC, want: input.CtrlC},
		{name: "carriage return", char: crByte, want: input.Enter},
		{name: "line feed", char: lfByte, want: input.Enter},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			var dec input.Decoder

			got := dec.Feed([]byte{tcase.char})
			want := []input.Key{{Kind: tcase.want}}

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Feed(%q) = %#v, want %#v", tcase.char, got, want)
			}
		})
	}
}

func TestDecoder_ArrowSequences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		chunk string
		want  input.Kind
	}{
		{name: "csi up", chunk: "\x1b[A", want: input.Up},
		{name: "csi down", chunk: "\x1b[B", want: input.Down},
		{name: "csi right", chunk: "\x1b[C", want: input.Right},
		{name: "csi left", chunk: "\x1b[D", want: input.Left},
		{name: "ss3 up", chunk: "\x1bOA", want: input.Up},
		{name: "ss3 down", chunk: "\x1bOB", want: input.Down},
		{name: "ss3 right", chunk: "\x1bOC", want: input.Right},
		{name: "ss3 left", chunk: "\x1bOD", want: input.Left},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			var dec input.Decoder

			got := dec.Feed([]byte(tcase.chunk))
			want := []input.Key{{Kind: tcase.want}}

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Feed(%q) = %#v, want %#v", tcase.chunk, got, want)
			}
		})
	}
}

func TestDecoder_SplitSequences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		first  string
		second string
	}{
		{name: "split after esc", first: "\x1b", second: "[A"},
		{name: "split after intro", first: "\x1b[", second: "A"},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			var dec input.Decoder

			// A read can land mid escape sequence; the decoder must carry
			// the partial state to the next Feed instead of losing it.
			gotFirst := dec.Feed([]byte(tcase.first))
			if len(gotFirst) != 0 {
				t.Fatalf("first Feed(%q) = %#v, want empty", tcase.first, gotFirst)
			}

			gotSecond := dec.Feed([]byte(tcase.second))
			want := []input.Key{{Kind: input.Up}}

			if !reflect.DeepEqual(gotSecond, want) {
				t.Fatalf("second Feed(%q) = %#v, want %#v", tcase.second, gotSecond, want)
			}
		})
	}
}

func TestDecoder_LoneEscTimeout(t *testing.T) {
	t.Parallel()

	var dec input.Decoder

	gotFirst := dec.Feed([]byte("\x1b"))
	if len(gotFirst) != 0 {
		t.Fatalf("Feed(ESC) = %#v, want empty", gotFirst)
	}

	// An empty Feed models a raw-tty read timeout: it is what tells a
	// pending ESC that no sequence followed, so it has to resolve alone.
	got := dec.Feed(nil)
	want := []input.Key{{Kind: input.Esc}}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Feed(nil) = %#v, want %#v", got, want)
	}
}

func TestDecoder_EscThenNonIntroducer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		chunk string
		want  []input.Key
	}{
		{name: "esc then rune", chunk: "\x1bq", want: []input.Key{{Kind: input.Esc}, {Kind: input.Rune, Rune: 'q'}}},
		{name: "esc then esc", chunk: "\x1b\x1b", want: []input.Key{{Kind: input.Esc}}},
		{name: "esc then enter", chunk: "\x1b\r", want: []input.Key{{Kind: input.Esc}, {Kind: input.Enter}}},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			var dec input.Decoder

			got := dec.Feed([]byte(tcase.chunk))
			if !reflect.DeepEqual(got, tcase.want) {
				t.Fatalf("Feed(%q) = %#v, want %#v", tcase.chunk, got, tcase.want)
			}
		})
	}
}

func TestDecoder_UnknownFinalByte(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		chunk string
	}{
		{name: "csi unknown final byte", chunk: "\x1b[Z"},
		{name: "ss3 unknown final byte", chunk: "\x1bOZ"},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			var dec input.Decoder

			got := dec.Feed([]byte(tcase.chunk))
			want := []input.Key{{Kind: input.Esc}}

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Feed(%q) = %#v, want %#v", tcase.chunk, got, want)
			}
		})
	}
}

func TestDecoder_MultipleKeysOneFeed(t *testing.T) {
	t.Parallel()

	var dec input.Decoder

	got := dec.Feed([]byte("ab\x1b[Cq"))
	want := []input.Key{
		{Kind: input.Rune, Rune: 'a'},
		{Kind: input.Rune, Rune: 'b'},
		{Kind: input.Right},
		{Kind: input.Rune, Rune: 'q'},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Feed(...) = %#v, want %#v", got, want)
	}
}

func TestDecoder_EmptyFeed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		chunk []byte
	}{
		{name: "nil chunk", chunk: nil},
		{name: "zero length chunk", chunk: []byte{}},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			var dec input.Decoder

			got := dec.Feed(tcase.chunk)
			if len(got) != 0 {
				t.Fatalf("Feed(%v) = %#v, want empty", tcase.chunk, got)
			}
		})
	}
}

func TestDecoder_ZeroValueUsable(t *testing.T) {
	t.Parallel()

	var dec input.Decoder

	got := dec.Feed([]byte("a"))
	want := []input.Key{{Kind: input.Rune, Rune: 'a'}}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Feed(%q) = %#v, want %#v", "a", got, want)
	}
}

// TestDecoder_SliceReuse documents Feed's doc comment contract: the returned
// slice is reused on the next call, so its contents are only good until the
// caller feeds again.
func TestDecoder_SliceReuse(t *testing.T) {
	t.Parallel()

	var dec input.Decoder

	first := dec.Feed([]byte("a"))
	if len(first) != 1 {
		t.Fatalf("first Feed = %#v, want 1 key", first)
	}

	second := dec.Feed([]byte("b"))
	if len(second) != 1 {
		t.Fatalf("second Feed = %#v, want 1 key", second)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("first = %#v, second = %#v, want the backing array reused", first, second)
	}
}

func TestKind_ValuesDistinct(t *testing.T) {
	t.Parallel()

	kinds := []input.Kind{
		input.Rune,
		input.Up,
		input.Down,
		input.Left,
		input.Right,
		input.Enter,
		input.Esc,
		input.CtrlC,
	}

	seen := make(map[input.Kind]bool, len(kinds))
	for _, kind := range kinds {
		if seen[kind] {
			t.Fatalf("duplicate Kind value %v", kind)
		}

		seen[kind] = true
	}
}

func TestRead_HappyPath(t *testing.T) {
	t.Parallel()

	reader := strings.NewReader("q")
	out := make(chan input.Key, 1)

	if err := input.Read(context.Background(), reader, out); err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}

	got := recvKey(t, out)
	want := input.Key{Kind: input.Rune, Rune: 'q'}

	if got != want {
		t.Fatalf("received key = %#v, want %#v", got, want)
	}
}

func TestRead_ReaderError(t *testing.T) {
	t.Parallel()

	reader := &scriptedReader{steps: []readerStep{{err: errBoom}}}
	out := make(chan input.Key)

	err := input.Read(context.Background(), reader, out)
	if !errors.Is(err, errBoom) {
		t.Fatalf("Read() error = %v, want wrapped %v", err, errBoom)
	}
}

func TestRead_ContextAlreadyCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reader := strings.NewReader("")
	out := make(chan input.Key)

	err := input.Read(ctx, reader, out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Read() error = %v, want wrapped context.Canceled", err)
	}
}

// TestRead_ContextCancelledWhileSendBlocked protects the shutdown path: out
// is unbuffered and nobody drains it, so Read can only return by way of
// send's ctx.Done() branch, not by finishing the send. The gatedReader
// pins the timing: cancellation lands only after Read is already past its
// ctx.Err() check and sitting in src.Read, so the eventual send is the one
// thing left to unblock it.
func TestRead_ContextCancelledWhileSendBlocked(t *testing.T) {
	t.Parallel()

	const testTimeout = 2 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	reader := &gatedReader{reached: make(chan struct{}), proceed: make(chan struct{}), data: []byte("a")}
	out := make(chan input.Key)

	errCh := make(chan error, 1)

	var group sync.WaitGroup

	group.Go(func() {
		errCh <- input.Read(ctx, reader, out)
	})

	select {
	case <-reader.reached:
	case <-time.After(testTimeout):
		t.Fatal("Read never reached the read call")
	}

	cancel()
	close(reader.proceed)

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Read() error = %v, want wrapped context.Canceled", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("Read did not return after context cancellation")
	}

	group.Wait()
}

// TestRead_TimeoutThenFlush proves a (0, nil) read, the shape of a raw-tty
// VTIME timeout, keeps the loop going and flushes a pending ESC rather than
// stalling or dropping it.
func TestRead_TimeoutThenFlush(t *testing.T) {
	t.Parallel()

	const escByte = 0x1B

	reader := &scriptedReader{steps: []readerStep{
		{data: []byte{escByte}},
		{},
		{err: io.EOF},
	}}
	out := make(chan input.Key, 1)

	if err := input.Read(context.Background(), reader, out); err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}

	got := recvKey(t, out)
	want := input.Key{Kind: input.Esc}

	if got != want {
		t.Fatalf("received key = %#v, want %#v", got, want)
	}
}
