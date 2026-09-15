//go:build integration && (linux || darwin)

package term_test

import (
	"errors"
	"os"
	"testing"

	"github.com/hyperized/uScope/internal/term"
)

// TestMakeRawOnRealStdin exercises MakeRaw against the process's actual
// stdin. It only proves anything when stdin is a real terminal, so it skips
// rather than fails whenever that is not the case.
func TestMakeRawOnRealStdin(t *testing.T) {
	t.Parallel()

	restore, err := term.MakeRaw(os.Stdin.Fd())
	if err != nil {
		if errors.Is(err, term.ErrNotTerminal) {
			t.Skipf("stdin is not a terminal: %v", err)
		}

		t.Fatalf("MakeRaw() error = %v", err)
	}

	if err := restore(); err != nil {
		t.Fatalf("restore() error = %v", err)
	}
}
