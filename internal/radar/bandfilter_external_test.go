package radar_test

import (
	"errors"
	"testing"

	"github.com/hyperized/uScope/internal/radar"
)

// TestParseFilter covers the --filter allow list: the eight spellings, and the
// refusals of a wrong case, a cap's own word and nothing at all.
func TestParseFilter(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"all", "low", "mid", "high", "below-high", "in", "out", "other"} {
		got, err := radar.ParseFilter(text)
		if err != nil || string(got) != text {
			t.Errorf("ParseFilter(%q) = %q, %v, want %q", text, got, err, text)
		}
	}

	for _, text := range []string{"", "Low", "<25K", "below", "IN", "transit"} {
		got, err := radar.ParseFilter(text)
		if !errors.Is(err, radar.ErrFilter) || got != radar.FilterAll {
			t.Errorf("ParseFilter(%q) = %q, %v, want FilterAll and ErrFilter", text, got, err)
		}
	}
}
