package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/hyperized/uScope/internal/affinity"
)

// errCPUs refuses a --cpus value that is not a CPU list.
var errCPUs = errors.New(appName + ": --cpus must be a CPU list such as 4-7 or 0,2,4-7, " +
	"CPUs 0 to 1023, ranges counting up")

// bindCPUs registers --cpus. Empty, the default, leaves the threads wherever
// the kernel and the container put them.
func bindCPUs(set *flag.FlagSet, raw *string) {
	set.StringVar(raw, "cpus", "",
		"pin every thread to these CPUs at start, as a list such as 4-7 or 0,2,4-7, in place of taskset; "+
			"Linux only, and the run stops if the kernel will not keep them. Empty leaves them alone")
}

// parseCPUs reads --cpus, the empty set for empty.
func parseCPUs(text string) (affinity.Set, error) {
	if text == "" {
		return affinity.Set{}, nil
	}

	set, err := affinity.Parse(text)
	if err != nil {
		return affinity.Set{}, fmt.Errorf("%w: %w", errCPUs, err)
	}

	return set, nil
}
