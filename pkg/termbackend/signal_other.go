//go:build !linux && !darwin

package termbackend

import "os"

// notifyResize does nothing on a platform with no SIGWINCH. The terminal
// size is then read once at startup and never changes, which is the best
// that can be done without the signal.
func notifyResize(chan<- os.Signal) {}

// stopResize does nothing, matching notifyResize.
func stopResize(chan<- os.Signal) {}
