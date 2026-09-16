package radar

import (
	"errors"
	"fmt"
)

// View is which of the four pictures the radar is showing.
//
// It is a string type for the same reason ColourMode is: it is what --view
// parses into, so the spelling on the command line and the value in the scene
// are one thing and cannot drift apart. The zero value reads as ViewScope,
// which is what "nobody said" has to mean.
//
// The four are two pictures times two amounts of furniture: flat or in
// perspective, with the chrome or without it. v walks the grid rather than a
// list, so the two flat views are a press apart from the two perspective ones
// whichever pair you started in. See Next.
type View string

// The four views, in the order the v key cycles them.
const (
	// ViewScope is the full scope: range rings, cardinals, the home marker,
	// the overlays and the column beside it. A run starts here.
	ViewScope View = "scope"

	// View3D is the same traffic in perspective, seen from a camera orbiting
	// the receiver: altitude drawn as height, and the antenna's receiving
	// envelope around it.
	View3D View = "3d"

	// ViewMinimal is the aircraft and their trails on the bare field, edge to
	// edge, with no furniture at all.
	ViewMinimal View = "minimal"

	// ViewMinimal3D is what View3D is to ViewScope, done to ViewMinimal: the
	// perspective picture with nothing around it. Models on their stalks,
	// trails in the air and the receiver's own marker, on a field with no
	// rings, no cardinals, no envelope and no column.
	ViewMinimal3D View = "minimal3d"
)

// ErrView is returned for a --view value that is none of the four.
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
	case ViewMinimal3D:
		return ViewMinimal3D, nil
	default:
		return ViewScope, fmt.Errorf("%w: %q", ErrView, text)
	}
}

// Next cycles to the view after this one: scope, 3D, minimal, bare 3D, scope.
//
// The order changes one thing at a time. The first press tilts the picture,
// the second takes the furniture away, the third tilts it again, and the
// fourth puts everything back. Walking the two flat views first and the two
// perspective ones afterwards would have put a press between the tilted pair,
// which is the comparison anybody cycling the views is actually making.
//
// Anything that is not one of the other three moves to 3D, so the zero value
// cycles the way ViewScope does and never has to be special-cased where one is
// read.
func (v View) Next() View {
	switch v {
	case View3D:
		return ViewMinimal
	case ViewMinimal:
		return ViewMinimal3D
	case ViewMinimal3D:
		return ViewScope
	case ViewScope:
		fallthrough
	default:
		return View3D
	}
}

// minimal reports whether the flat bare field is on screen.
//
// It is the narrower of the two questions about furniture, and the one the
// flat drawing path asks: measureScope has a projection for it, and neither
// perspective view has one at all. Everything that only cares whether there is
// chrome asks bare instead.
func (s *Scene) minimal() bool { return s.shown == ViewMinimal }

// perspective reports whether one of the two 3D views is on screen.
func (s *Scene) perspective() bool { return s.shown == View3D || s.shown == ViewMinimal3D }

// bare reports whether the view on screen has no chrome: no header band, no
// key bar and no column, with the picture running edge to edge.
//
// It is what the two minimal views have in common, and it is the question
// nearly everything asks. The overlay toggles keep their own pair for a bare
// view, the centre follows the traffic in one, and the selection draws no ring
// because there is no type on screen for a ring to refer to. None of those
// three cares whether the picture is flat or tilted.
func (s *Scene) bare() bool { return s.shown == ViewMinimal || s.shown == ViewMinimal3D }
