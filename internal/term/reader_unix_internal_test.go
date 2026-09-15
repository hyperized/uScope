//go:build linux || darwin

package term

import (
	"errors"
	"syscall"
	"testing"
)

// fakeRead scripts the results of successive read calls.
type fakeRead struct {
	steps []readStep
	calls int
}

type readStep struct {
	data []byte
	err  error
}

func (f *fakeRead) read(_ int, p []byte) (int, error) {
	step := f.steps[f.calls]
	f.calls++

	return copy(p, step.data), step.err
}

// errBoom stands in for any failure the read call might return.
var errBoom = errors.New("boom")

func TestReaderRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		steps     []readStep
		wantCount int
		wantData  string
		wantErr   error
		wantCalls int
	}{
		{name: "bytes pass through", steps: []readStep{{data: []byte("q")}}, wantCount: 1, wantData: "q", wantCalls: 1},
		{name: "timeout is zero not EOF", steps: []readStep{{}}, wantCount: 0, wantCalls: 1},
		{
			name:      "interrupted call is retried",
			steps:     []readStep{{err: syscall.EINTR}, {data: []byte("n")}},
			wantCount: 1, wantData: "n", wantCalls: 2,
		},
		{name: "other errors are wrapped", steps: []readStep{{err: errBoom}}, wantErr: errBoom, wantCalls: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeRead{steps: test.steps}
			reader := &Reader{fd: 7, read: fake.read}
			buf := make([]byte, 8)

			count, err := reader.Read(buf)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("err = %v, want %v", err, test.wantErr)
			}

			if count != test.wantCount || string(buf[:count]) != test.wantData {
				t.Fatalf("read %d %q, want %d %q", count, buf[:count], test.wantCount, test.wantData)
			}

			if fake.calls != test.wantCalls {
				t.Fatalf("calls = %d, want %d", fake.calls, test.wantCalls)
			}
		})
	}
}

func TestNewReaderUsesSyscallRead(t *testing.T) {
	t.Parallel()

	reader := NewReader(3)
	if reader.fd != 3 || reader.read == nil {
		t.Fatalf("NewReader(3) = %+v, want fd 3 with a read function", reader)
	}
}
