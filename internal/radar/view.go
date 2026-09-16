package radar

import (
	"errors"
	"fmt"
)

// View is which of the three pictures the radar is showing.
//
// It is a string type for the same reason ColourMode is: it is what --view
// parses into, so the spelling on the command line and the value in the scene
// are one thing and cannot drift apart. The zero value reads as ViewScope,
// which is what "nobody said" has to mean.
type View string

// The three views, in the order the v key cycles them.
const (
	// ViewScope is the full scope: range rings, cardinals, the home marker,
	// the overlays and the column beside it. A run starts here.
	ViewScope View = "scope"

	// ViewMinimal is the aircraft and their trails on the bare field, edge to
	// edge, with no furniture at all.
	ViewMinimal View = "minimal"

	// View3D is the same traffic in perspective, seen from a camera orbiting
	// the receiver: altitude drawn as height, and the antenna's receiving
	// envelope around it.
	View3D View = "3d"
)

// ErrView is returned for a --view value that is none of the three.
var ErrView = errors.New("radar: unknown view")

// ParseView turns a --view value into a View.
//
// The match is case sensitive, the same way ParseColour is: --view is an allow
// list rather than free text, so "3D" is refused exactly as "perspective"
// would be.
func ParseView(text string) (View, error) {
	switch View(text) {
	case ViewScope:
		return ViewScope, nil
	case ViewMinimal:
		return ViewMinimal, nil
	case View3D:
		return View3D, nil
	default:
		return ViewScope, fmt.Errorf("%w: %q", ErrView, text)
	}
}

// Next cycles to the view after this one: scope, minimal, 3D, scope.
//
// Anything that is not one of the other two moves to minimal, so the zero
// value cycles the way ViewScope does and never has to be special-cased where
// one is read.
func (v View) Next() View {
	switch v {
	case ViewMinimal:
		return View3D
	case View3D:
		return ViewScope
	case ViewScope:
		fallthrough
	default:
		return ViewMinimal
	}
}

// minimal reports whether the bare field is on screen.
//
// Everything that used to read a bool field asks this instead, so the three
// views live in one value rather than in a pair of flags that could both be
// set.
func (s *Scene) minimal() bool { return s.shown == ViewMinimal }

// perspective reports whether the 3D view is on screen.
func (s *Scene) perspective() bool { return s.shown == View3D }
