package main

import (
	"bytes"
	"errors"
	"strings"
	"syscall"
	"testing"

	"github.com/hyperized/uScope/internal/affinity"
)

// flagCPUs is the pinning flag as typed.
const (
	flagCPUs = "--cpus"
	bigCores = "4-7"
)

// TestParseFlagsCPUs checks --cpus is empty by default, takes CPU lists, and
// refuses anything else with its own sentinel wrapped around the affinity
// package's.
func TestParseFlagsCPUs(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, value, want string
		err               error
	}{
		{name: "empty leaves them alone", value: "", want: ""},
		{name: "big cores", value: bigCores, want: bigCores},
		{name: "mixed", value: "0,2,4-7", want: "0,2,4-7"},
		{name: "folded", value: "4,5,6,7", want: bigCores},
		{name: "backwards", value: "7-4", err: affinity.ErrOrder},
		{name: "too high", value: "4096", err: affinity.ErrRange},
		{name: "five digits", value: "10000", err: affinity.ErrSyntax},
		{name: "taskset mask", value: "0xf0", err: affinity.ErrSyntax},
		{name: "spaces", value: "4 - 7", err: affinity.ErrSyntax},
		{name: "shell", value: "4-7;reboot", err: affinity.ErrSyntax},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			args := []string{flagCPUs, testCase.value}

			cfg, err := parseFlags(args)
			if testCase.err != nil {
				if !errors.Is(err, errCPUs) || !errors.Is(err, testCase.err) {
					t.Fatalf("parseFlags(%v) error = %v, want errCPUs wrapping %v", args, err, testCase.err)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseFlags(%v): %v", args, err)
			}

			if got := cfg.cpus.String(); got != testCase.want {
				t.Errorf("cfg.cpus = %q, want %q", got, testCase.want)
			}
		})
	}

	t.Run("default", func(t *testing.T) {
		t.Parallel()

		cfg, err := parseFlags(nil)
		if err != nil {
			t.Fatalf("parseFlags(nil): %v", err)
		}

		if !cfg.cpus.IsEmpty() {
			t.Errorf("default cfg.cpus = %s, want empty: no pinning unless asked", cfg.cpus)
		}
	})
}

// fakePin swaps pinCPUs for the length of the test and records what it was
// asked for. Callers must not be parallel.
func fakePin(t *testing.T, threads int, err error) *affinity.Set {
	t.Helper()

	asked := &affinity.Set{}
	saved := pinCPUs

	t.Cleanup(func() { pinCPUs = saved })

	pinCPUs = func(set affinity.Set) (int, error) {
		*asked = set

		return threads, err
	}

	return asked
}

// TestPinProcessEmpty checks no --cpus pins nothing and says nothing.
func TestPinProcessEmpty(t *testing.T) { //nolint:paralleltest // swaps pinCPUs.
	asked := fakePin(t, 0, syscall.EPERM)

	var stderr bytes.Buffer

	if err := pinProcess(affinity.Set{}, &stderr); err != nil || stderr.Len() != 0 || !asked.IsEmpty() {
		t.Fatalf("pinProcess(empty) = %v, stderr %q, asked %s; want nil, nothing, nothing", err, stderr.String(), asked)
	}
}

// TestPinProcessReports checks a pin is announced in one line naming the
// thread count and the canonical list.
func TestPinProcessReports(t *testing.T) { //nolint:paralleltest // swaps pinCPUs.
	asked := fakePin(t, 9, nil)

	set, err := affinity.Parse("4,5,6,7")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var stderr bytes.Buffer

	if err := pinProcess(set, &stderr); err != nil {
		t.Fatalf("pinProcess: %v", err)
	}

	if *asked != set {
		t.Errorf("pinned %s, want %s", asked, set)
	}

	if want := "uScope: cpus: pinned 9 threads to 4-7\n"; stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

// TestRunStopsWhenPinningFails checks a refused --cpus ends the run before
// anything opens, with the usual failure code and the reason on stderr.
func TestRunStopsWhenPinningFails(t *testing.T) { //nolint:paralleltest // swaps pinCPUs.
	fakePin(t, 0, affinity.ErrNotHeld)

	var stdout, stderr bytes.Buffer

	code := run([]string{flagCPUs, bigCores, flagDemo, "--test-pattern", "--backend", "blocks"}, &stdout, &stderr)
	if code != exitFailure {
		t.Fatalf("run = %d, want %d", code, exitFailure)
	}

	if !strings.Contains(stderr.String(), "--cpus 4-7") || stdout.Len() != 0 {
		t.Errorf("stderr = %q, stdout %d bytes; want the flag named and no frame drawn", stderr.String(), stdout.Len())
	}
}

// TestRunRefusesABadCPUList checks a malformed --cpus is a flag error.
func TestRunRefusesABadCPUList(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	if code := run([]string{flagCPUs, "4-7,"}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("run = %d, want %d", code, exitFailure)
	}

	if !strings.Contains(stderr.String(), "--cpus must be") {
		t.Errorf("stderr = %q, want the flag's rule", stderr.String())
	}
}
