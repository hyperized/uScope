package app

import (
	"io"
	"sync"
	"sync/atomic"
)

// biasTeeSource is the part of a source the bias-tee toggle drives.
//
// It is two methods rather than the whole source.Source because that is all
// this reads, and because a test then hands over a struct of its own instead
// of a live ingest. It is declared here, where it is consumed, for the same
// reason radar.BatteryReader is declared in internal/radar.
type biasTeeSource interface {
	// BiasTee is the cached state: no device transfer, safe anywhere.
	BiasTee() (supported, enabled bool)

	// SetBiasTee is the control transfer, and the reason this whole file
	// exists.
	SetBiasTee(enable bool) error
}

// biasToggler runs the b key's bias-tee flip off the goroutine that draws.
//
// Setting a bias-tee is a USB control transfer. A dongle wedged by an unplug
// or a bus reset part way through one can take seconds to answer, and the run
// loop is single-threaded: doing the write there would freeze every frame and
// every keypress until the kernel gave up. uAirwaves reached the same
// conclusion about its event loop and put the flip behind a worker as well.
//
// The read-modify-write runs on the worker, off the cached state rather than
// a live poll, so a press costs no transfer either. The trade-off is that a
// flip made outside this process, with rtl_biast say, is not noticed: the
// cache re-syncs when the receiver next opens and on the next toggle here.
//
// It is safe for concurrent use. inFlight is the only mutable state and it is
// atomic; the source behind it does its own locking.
type biasToggler struct {
	src    biasTeeSource
	stderr io.Writer

	// group holds the outstanding toggle so shutdown can wait for it. The
	// dongle is let go by the source's Close, and returning from Run with a
	// control transfer still in the air would race the two.
	group sync.WaitGroup

	// inFlight collapses a burst of presses to one running toggle. A GPIO
	// flip is not worth queueing: by the time a queued second press ran, the
	// operator would have watched the cap change twice for one decision.
	inFlight atomic.Bool
}

// newBiasToggler wires a toggler to a source and somewhere to complain.
func newBiasToggler(src biasTeeSource, stderr io.Writer) *biasToggler {
	return &biasToggler{src: src, stderr: stderr}
}

// Toggle implements radar.Toggler. It runs on the goroutine that draws, so it
// does no device work itself: it claims the guard and hands the flip to a
// worker. A press while one is already running is dropped on the floor.
func (t *biasToggler) Toggle() {
	if !t.inFlight.CompareAndSwap(false, true) {
		return
	}

	t.group.Go(t.flip)
}

// wait blocks until any outstanding toggle has finished.
//
// Run calls it as the last thing it does, by which time the loop has returned
// and nothing can start another one, so there is no Wait-during-Add race to
// worry about.
func (t *biasToggler) wait() { t.group.Wait() }

// flip is the worker body: read the cached state, drive the transfer, report
// whatever went wrong, and clear the guard.
//
// Nothing here is fatal. A bias-tee that will not flip is a powered LNA that
// stays powered, or an unpowered one that stays unpowered, and neither is a
// reason to take a working scope off the screen. The deferred clear runs on
// every exit path, a recovered panic included, so the next press starts a
// fresh worker rather than finding the guard stuck.
func (t *biasToggler) flip() {
	defer t.inFlight.Store(false)

	// Written inline rather than as a named method because revive's defer
	// rule only recognises recover inside a function literal.
	defer func() {
		if value := recover(); value != nil {
			sayf(t.stderr, "warning: bias-tee toggle panicked: %v\n", value)
		}
	}()

	supported, enabled := t.src.BiasTee()
	if !supported {
		sayf(t.stderr, "warning: this source has no bias-tee to switch\n")

		return
	}

	if err := t.src.SetBiasTee(!enabled); err != nil {
		sayf(t.stderr, "warning: bias-tee: %v\n", err)
	}
}
