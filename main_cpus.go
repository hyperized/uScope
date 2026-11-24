package main

import (
	"fmt"
	"io"

	"github.com/hyperized/uScope/internal/affinity"
)

// pinCPUs is a seam so a test can pin without touching the test binary's own
// threads, and fail without a kernel that refuses.
//
//nolint:gochecknoglobals // test seam; production always holds affinity.Pin.
var pinCPUs = affinity.Pin

// setUp parses the flags and applies --cpus, before anything in run starts a
// goroutine, so the fewest threads exist to be moved and every one started
// later inherits the mask. A refused pin comes back as an error like a bad
// flag does, and ends the run the same way.
func setUp(args []string, stderr io.Writer) (config, error) {
	cfg, err := parseFlags(args)
	if err != nil {
		return config{}, err
	}

	if err := pinProcess(cfg.cpus, stderr); err != nil {
		return config{}, err
	}

	return cfg, nil
}

// pinProcess applies --cpus, saying so in one line on stderr, and is a
// no-op for the empty set.
//
// A refusal is an error rather than a warning: whoever set --cpus did it
// because the unpinned run was too slow, and a run that quietly carries on
// unpinned is that slow run again with nothing in the log to say why.
func pinProcess(cpus affinity.Set, stderr io.Writer) error {
	if cpus.IsEmpty() {
		return nil
	}

	threads, err := pinCPUs(cpus)
	if err != nil {
		return fmt.Errorf("%s: --cpus %s: %w", appName, cpus, err)
	}

	_, _ = fmt.Fprintf(stderr, "%s: cpus: pinned %d threads to %s\n", appName, threads, cpus)

	return nil
}
