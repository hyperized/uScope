package input

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestArrow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		char byte
		want Kind
		ok   bool
	}{
		{name: "up", char: 'A', want: Up, ok: true},
		{name: "down", char: 'B', want: Down, ok: true},
		{name: "right", char: 'C', want: Right, ok: true},
		{name: "left", char: 'D', want: Left, ok: true},
		{name: "unknown final byte", char: 'Z', want: Rune, ok: false},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := arrow(tcase.char)
			if got != tcase.want || ok != tcase.ok {
				t.Fatalf("arrow(%q) = (%v, %v), want (%v, %v)", tcase.char, got, ok, tcase.want, tcase.ok)
			}
		})
	}
}

func TestStepGround(t *testing.T) {
	t.Parallel()

	const highByte = 0xC3

	tests := []struct {
		name      string
		char      byte
		wantKeys  []Key
		wantState state
	}{
		{name: "esc starts a sequence", char: byteEsc, wantKeys: nil, wantState: afterEsc},
		{name: "ctrl-c", char: byteCtrlC, wantKeys: []Key{{Kind: CtrlC}}, wantState: ground},
		{name: "carriage return", char: byteCR, wantKeys: []Key{{Kind: Enter}}, wantState: ground},
		{name: "line feed", char: byteLF, wantKeys: []Key{{Kind: Enter}}, wantState: ground},
		{name: "plain rune", char: 'q', wantKeys: []Key{{Kind: Rune, Rune: 'q'}}, wantState: ground},
		{
			name: "byte above ascii kept as one rune", char: highByte,
			wantKeys: []Key{{Kind: Rune, Rune: highByte}}, wantState: ground,
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			dec := &Decoder{}
			dec.stepGround(tcase.char)

			if !reflect.DeepEqual(dec.keys, tcase.wantKeys) {
				t.Fatalf("keys = %#v, want %#v", dec.keys, tcase.wantKeys)
			}

			if dec.state != tcase.wantState {
				t.Fatalf("state = %v, want %v", dec.state, tcase.wantState)
			}
		})
	}
}

func TestStepAfterEsc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		char      byte
		wantKeys  []Key
		wantState state
	}{
		{name: "csi introducer", char: '[', wantKeys: nil, wantState: afterIntro},
		{name: "ss3 introducer", char: 'O', wantKeys: nil, wantState: afterIntro},
		{
			name: "esc stands alone before a rune", char: 'q',
			wantKeys: []Key{{Kind: Esc}, {Kind: Rune, Rune: 'q'}}, wantState: ground,
		},
		{
			// The second ESC restarts the state machine rather than being
			// swallowed, so it leaves a fresh pending ESC of its own.
			name: "esc stands alone before another esc", char: byteEsc,
			wantKeys: []Key{{Kind: Esc}}, wantState: afterEsc,
		},
		{
			name: "esc stands alone before enter", char: byteCR,
			wantKeys: []Key{{Kind: Esc}, {Kind: Enter}}, wantState: ground,
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			dec := &Decoder{state: afterEsc}
			dec.stepAfterEsc(tcase.char)

			if !reflect.DeepEqual(dec.keys, tcase.wantKeys) {
				t.Fatalf("keys = %#v, want %#v", dec.keys, tcase.wantKeys)
			}

			if dec.state != tcase.wantState {
				t.Fatalf("state = %v, want %v", dec.state, tcase.wantState)
			}
		})
	}
}

func TestStepAfterIntro(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		char     byte
		wantKeys []Key
	}{
		{name: "up", char: 'A', wantKeys: []Key{{Kind: Up}}},
		{name: "down", char: 'B', wantKeys: []Key{{Kind: Down}}},
		{name: "right", char: 'C', wantKeys: []Key{{Kind: Right}}},
		{name: "left", char: 'D', wantKeys: []Key{{Kind: Left}}},
		{name: "unknown final byte is dropped", char: 'Z', wantKeys: []Key{{Kind: Esc}}},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			dec := &Decoder{state: afterIntro}
			dec.stepAfterIntro(tcase.char)

			if !reflect.DeepEqual(dec.keys, tcase.wantKeys) {
				t.Fatalf("keys = %#v, want %#v", dec.keys, tcase.wantKeys)
			}

			if dec.state != ground {
				t.Fatalf("state = %v, want ground", dec.state)
			}
		})
	}
}

func TestStep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		startState state
		char       byte
		wantKeys   []Key
	}{
		{
			name: "ground dispatches to stepGround", startState: ground,
			char: 'q', wantKeys: []Key{{Kind: Rune, Rune: 'q'}},
		},
		{name: "afterEsc dispatches to stepAfterEsc", startState: afterEsc, char: '[', wantKeys: nil},
		{
			name: "afterIntro dispatches to stepAfterIntro", startState: afterIntro,
			char: 'A', wantKeys: []Key{{Kind: Up}},
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			dec := &Decoder{state: tcase.startState}
			dec.step(tcase.char)

			if !reflect.DeepEqual(dec.keys, tcase.wantKeys) {
				t.Fatalf("keys = %#v, want %#v", dec.keys, tcase.wantKeys)
			}
		})
	}
}

func TestFlush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		startState state
		wantKeys   []Key
	}{
		{name: "ground stays quiet", startState: ground, wantKeys: nil},
		{name: "pending esc resolves", startState: afterEsc, wantKeys: []Key{{Kind: Esc}}},
		{name: "pending intro resolves", startState: afterIntro, wantKeys: []Key{{Kind: Esc}}},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			dec := &Decoder{state: tcase.startState}

			got := dec.flush()
			if !reflect.DeepEqual(got, tcase.wantKeys) {
				t.Fatalf("flush() = %#v, want %#v", got, tcase.wantKeys)
			}

			if dec.state != ground {
				t.Fatalf("state after flush = %v, want ground", dec.state)
			}
		})
	}
}

func TestSend(t *testing.T) {
	t.Parallel()

	t.Run("delivers every key in order", func(t *testing.T) {
		t.Parallel()

		keys := []Key{{Kind: Rune, Rune: 'a'}, {Kind: Rune, Rune: 'b'}}
		out := make(chan Key, len(keys))

		if err := send(context.Background(), keys, out); err != nil {
			t.Fatalf("send() error = %v, want nil", err)
		}

		close(out)

		got := make([]Key, 0, len(keys))
		for key := range out {
			got = append(got, key)
		}

		if !reflect.DeepEqual(got, keys) {
			t.Fatalf("received %#v, want %#v", got, keys)
		}
	})

	t.Run("cancelled context stops a blocked send", func(t *testing.T) {
		t.Parallel()

		// out is unbuffered and nothing ever reads from it, so the only way
		// this can return is through the ctx.Done() branch of the select.
		out := make(chan Key)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := send(ctx, []Key{{Kind: Rune, Rune: 'a'}}, out)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("send() error = %v, want wrapped context.Canceled", err)
		}
	})
}
