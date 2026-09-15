// Package source is the seam between the radar scene and wherever the
// aircraft come from.
//
// There are two implementations. Live drives the uAirwaves ingest stack: an
// RTL-SDR on the uConsole, a BEAST feed over TCP, or a captured IQ file
// played back. Demo invents twelve aircraft and flies them in straight lines,
// which is what makes the radar developable on a laptop with no receiver in
// it.
//
// Both hand back the same Frame, and the scene never learns which one it is
// talking to. That is the point: the layout work happens against Demo, and
// the same code draws real decodes on the device.
//
// A Frame is a value copy taken under whatever locks the layer below needs,
// so the scene may read it without holding anything. The Planes slice and the
// position histories in it are only valid until the next Frame call on the
// same Source: Live builds a fresh list every time, but Demo hands back its
// own buffers, which is how it stays free of per-frame allocations.
package source

import (
	"time"

	"github.com/hyperized/uAirwaves/pkg/adsb"
	"github.com/hyperized/uAirwaves/pkg/airplanes"
)

// The receiver-position labels. They say where the coordinates came from,
// which matters because a self-locate estimate can be tens of nautical miles
// out and must never be shown as if it were a GPS fix.
const (
	// LabelGPS marks coordinates from a real fix.
	LabelGPS = "GPS"

	// LabelEstimate marks the self-locate fallback, derived by intersecting
	// the horizon circles of aircraft we can hear.
	LabelEstimate = "EST"

	// LabelManual marks coordinates the operator typed in with --lat/--lon.
	LabelManual = "MANUAL"

	// LabelNone means no position is known yet, so nothing can be plotted.
	LabelNone = "none"
)

// Receiver is where the scope is centred and how much that is worth.
//
// ConfidenceNm is the self-locate radius in nautical miles and is zero for
// every other label. HasFix is false for an estimate: an estimate is a guess
// good enough to centre a scope on, not a fix.
type Receiver struct {
	Latitude     float64
	Longitude    float64
	ConfidenceNm float64
	HasFix       bool
	Label        string
}

// Frame is everything the radar needs to draw one frame.
//
// Now is carried in the frame rather than read from time.Now inside the scene
// so a test drives the clock and the data from one place.
type Frame struct {
	Planes   airplanes.List
	Receiver Receiver
	Source   adsb.SourceInfo
	Stats    adsb.Stats
	Now      time.Time
}

// Source hands out frames until it is closed.
//
// Frame is called once per drawn frame, so an implementation does the least
// work it can there, and what it returns is only guaranteed until the next
// call. Close releases whatever the implementation owns and waits for any
// goroutine it started; it is safe to call more than once.
type Source interface {
	Frame() Frame
	Close() error
}

// Empty is a Source with nothing in it. It is the default wherever a source
// has not been wired up yet, so nothing has to nil-check one.
//
// It is not a stand-in for a receiver that has heard nothing: a real source
// reports its label and its connection state even with an empty sky, and this
// one reports neither. Anything drawing an Empty is drawing a scope that was
// never given anywhere to look.
type Empty struct{}

// Frame hands back nothing at all.
func (Empty) Frame() Frame { return Frame{Receiver: Receiver{Label: LabelNone}} }

// Close has nothing to release.
func (Empty) Close() error { return nil }
