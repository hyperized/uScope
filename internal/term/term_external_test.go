package term_test

import (
	"errors"
	"testing"

	"github.com/hyperized/uScope/internal/term"
)

// TestSentinelErrorsAreDistinct checks the two sentinels are non-nil and
// distinguishable, since callers branch on errors.Is against each of them.
func TestSentinelErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	if term.ErrNotTerminal == nil {
		t.Fatal("ErrNotTerminal is nil")
	}

	if term.ErrUnsupported == nil {
		t.Fatal("ErrUnsupported is nil")
	}

	if errors.Is(term.ErrNotTerminal, term.ErrUnsupported) {
		t.Fatal("ErrNotTerminal and ErrUnsupported compare equal, want distinct sentinels")
	}
}
