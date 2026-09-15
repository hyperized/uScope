//go:build linux || darwin

package termbackend

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyResize subscribes ch to SIGWINCH, which is how a terminal tells a
// program its window changed size.
//
// Coverage note: this is two library calls with no branch in them, and a
// test that installed a real handler would fight the test binary's own
// signal state. The seam in Open exists so everything above it is tested
// with a channel the test owns.
func notifyResize(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGWINCH)
}

// stopResize ends the subscription so the signal package stops holding the
// channel.
func stopResize(ch chan<- os.Signal) {
	signal.Stop(ch)
}
