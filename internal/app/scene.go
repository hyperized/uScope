package app

import (
	"errors"
	"fmt"

	"github.com/hyperized/uAirwaves/pkg/scope"
	"github.com/hyperized/uScope/internal/pattern"
	"github.com/hyperized/uScope/internal/radar"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/specimen"
	"github.com/hyperized/uScope/pkg/fonts"
	"github.com/hyperized/uScope/pkg/psf"
)

// The --scene spellings, kept next to the Kind they parse into so the flag
// help and the parser cannot drift apart.
const (
	sceneRadar    = "radar"
	scenePattern  = "pattern"
	sceneSpecimen = "specimen"
)

// Scene selection errors.
var (
	// ErrScene is returned for a --scene value that is not one of the two.
	ErrScene = errors.New("app: unknown scene")

	// ErrNoScene means the run loop was handed a scene set that does not
	// contain the scene it was asked to start on.
	ErrNoScene = errors.New("app: no such scene to draw")
)

// SceneKind names the scenes uScope can draw. It is what --scene parses into.
//
// The set is closed for the same reason backend.Kind's is: a typo that fell
// through to a default would draw something nobody asked for.
type SceneKind uint8

// The scenes, in the order the s key steps through them and in the order
// buildScenes returns them.
const (
	// Radar is the slice 4 scope: aircraft, trails, range rings and the
	// selected-flight card. It is first because it is what uScope is for;
	// the other two are diagnostics.
	Radar SceneKind = iota

	// Pattern is the slice 1 orientation pattern: corner squares, a
	// triangle and a sweep. It is the one that answers "is the frame the
	// right way up".
	Pattern

	// Specimen is the slice 3 type specimen: the four faces and a mock of
	// the radar's furniture.
	Specimen
)

// ParseScene turns a --scene value into a SceneKind.
func ParseScene(text string) (SceneKind, error) {
	switch text {
	case sceneRadar:
		return Radar, nil
	case scenePattern:
		return Pattern, nil
	case sceneSpecimen:
		return Specimen, nil
	default:
		return Radar, fmt.Errorf("%w: %q", ErrScene, text)
	}
}

// String renders the flag spelling, so a SceneKind round-trips through
// ParseScene and prints as itself.
func (k SceneKind) String() string {
	switch k {
	case Radar:
		return sceneRadar
	case Pattern:
		return scenePattern
	case Specimen:
		return sceneSpecimen
	default:
		return "invalid"
	}
}

// fontLoader is one of pkg/fonts' accessors. It is a named type so the
// builder below can take four of them and a test can hand it one that fails.
type fontLoader func() (*psf.Font, error)

// defaultScenes is the production scene set.
func (r *runner) defaultScenes() ([]Drawer, error) {
	return buildScenes(fonts.Small, fonts.Body, fonts.BodyBold, fonts.Large, r.source, r.scopeRange)
}

// buildScenes loads the fonts and builds all three scenes, in SceneKind order.
//
// The fonts are loaded here, at startup, rather than when a scene first draws.
// A font that will not parse is then a clear error before anything takes the
// screen over, instead of an empty block ten frames into a run on a device
// with no other diagnostics.
//
// All three are built even when --scene picks one of them, because s switches
// between them while the loop is running and a set that was only half built
// would fail at the moment someone pressed a key. A nil source or range gets
// an empty stand-in for the same reason: the radar has to exist even when
// nothing has been wired to it.
func buildScenes(
	small, body, bodyBold, large fontLoader, src source.Source, scopeRange *scope.Scope,
) ([]Drawer, error) {
	if src == nil {
		src = source.Empty{}
	}

	faces, err := loadFaces(small, body, bodyBold, large)
	if err != nil {
		return nil, err
	}

	if scopeRange == nil {
		scopeRange = scope.New()
	}

	return []Drawer{
		radar.New(radar.Faces(faces), src, scopeRange),
		pattern.New(),
		specimen.New(specimen.Faces(faces)),
	}, nil
}

// faceSet is the four loaded faces. It is converted to each scene's own Faces
// type rather than shared, so internal/radar and internal/specimen do not
// have to import one another.
type faceSet struct {
	Small    *psf.Font
	Body     *psf.Font
	BodyBold *psf.Font
	Large    *psf.Font
}

// loadFaces calls the four loaders, naming whichever one fails.
func loadFaces(small, body, bodyBold, large fontLoader) (faceSet, error) {
	var (
		faces faceSet
		err   error
	)

	if faces.Small, err = small(); err != nil {
		return faces, fmt.Errorf("app: small font: %w", err)
	}

	if faces.Body, err = body(); err != nil {
		return faces, fmt.Errorf("app: body font: %w", err)
	}

	if faces.BodyBold, err = bodyBold(); err != nil {
		return faces, fmt.Errorf("app: bold font: %w", err)
	}

	if faces.Large, err = large(); err != nil {
		return faces, fmt.Errorf("app: large font: %w", err)
	}

	return faces, nil
}
