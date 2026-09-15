package winsize_test

import (
	"errors"
	"os"
	"testing"

	"github.com/hyperized/uScope/pkg/winsize"
)

// TestSentinelErrorsAreDistinct checks the two sentinels are non-nil and
// distinguishable, since callers branch on errors.Is against each of them.
func TestSentinelErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	if winsize.ErrNotTerminal == nil {
		t.Fatal("ErrNotTerminal is nil")
	}

	if winsize.ErrUnsupported == nil {
		t.Fatal("ErrUnsupported is nil")
	}

	if errors.Is(winsize.ErrNotTerminal, winsize.ErrUnsupported) {
		t.Fatal("ErrNotTerminal and ErrUnsupported compare equal, want distinct sentinels")
	}
}

// TestGetOnPipe exercises Get against a descriptor that is definitely not a
// terminal. It uses the read end of a pipe rather than relying on what the
// test runner happens to hand the process for stdin, so it covers the real
// ioctl's failure path on any machine.
func TestGetOnPipe(t *testing.T) {
	t.Parallel()

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}

	t.Cleanup(func() {
		if err := readEnd.Close(); err != nil {
			t.Fatalf("Close read end: %v", err)
		}
	})

	t.Cleanup(func() {
		if err := writeEnd.Close(); err != nil {
			t.Fatalf("Close write end: %v", err)
		}
	})

	_, err = winsize.Get(readEnd.Fd())
	if !errors.Is(err, winsize.ErrNotTerminal) {
		t.Fatalf("Get() error = %v, want ErrNotTerminal", err)
	}
}
