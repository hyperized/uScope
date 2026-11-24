//go:build integration && linux

package affinity_test

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperized/uScope/internal/affinity"
)

// ownSet reads the CPUs this process may use from /proc.
func ownSet(t *testing.T) affinity.Set {
	t.Helper()

	text, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}

	for line := range strings.Lines(string(text)) {
		if value, ok := strings.CutPrefix(line, "Cpus_allowed_list:"); ok {
			set, err := affinity.Parse(strings.TrimSpace(value))
			if err != nil {
				t.Fatalf("Cpus_allowed_list %q: %v", value, err)
			}

			return set
		}
	}

	t.Fatal("no Cpus_allowed_list in /proc/self/status")

	return affinity.Set{}
}

// TestPinFollowsGOMAXPROCS pins this test process to its first four CPUs
// and watches GOMAXPROCS. Since Go 1.25 the runtime re-reads the affinity
// mask and the cgroup limit about once a second and takes the smaller, so
// the figure it settles on is what a pinned uScope runs with. The test puts
// the old mask back when it ends.
//
//nolint:paralleltest // moves every thread of the test binary.
func TestPinFollowsGOMAXPROCS(t *testing.T) {
	own := ownSet(t)
	if own.Count() < 4 {
		t.Skipf("only %s here; the test pins to four", own)
	}

	var first []string

	for cpu := 0; cpu <= affinity.MaxCPU && len(first) < 4; cpu++ {
		if own.Has(cpu) {
			first = append(first, strconv.Itoa(cpu))
		}
	}

	four, err := affinity.Parse(strings.Join(first, ","))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	before := runtime.GOMAXPROCS(0)

	threads, err := affinity.Pin(four)
	if err != nil {
		t.Fatalf("Pin(%s): %v", four, err)
	}

	t.Cleanup(func() {
		if _, err := affinity.Pin(own); err != nil {
			t.Errorf("restoring %s: %v", own, err)
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for runtime.GOMAXPROCS(0) > four.Count() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}

	after := runtime.GOMAXPROCS(0)

	t.Logf("allowed %s (%d CPUs), GOMAXPROCS %d; pinned %d threads to %s, GOMAXPROCS now %d",
		own, own.Count(), before, threads, four, after)

	if after > four.Count() {
		t.Errorf("GOMAXPROCS %d after pinning to %d CPUs", after, four.Count())
	}
}
