package app

import (
	"errors"
	"fmt"

	"github.com/hyperized/uScope/internal/pattern"
	"github.com/hyperized/uScope/internal/specimen"
	"github.com/hyperized/uScope/pkg/fonts"
	"github.com/hyperized/uScope/pkg/psf"
)

// The --scene spellings, kept next to the Kind they parse into so the flag
// help and the parser cannot drift apart.
const (
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
	// Pattern is the slice 1 orientation pattern: corner squares, a
	// triangle and a sweep. It is the default because it is the one that
	// answers "is the frame the right way up".
	Pattern SceneKind = iota

	// Specimen is the slice 3 type specimen: the four faces and a mock of
	// the radar's furniture.
	Specimen
)

// ParseScene turns a --scene value into a SceneKind.
func ParseScene(text string) (SceneKind, error) {
	switch text {
	case scenePattern:
		return Pattern, nil
	case sceneSpecimen:
		return Specimen, nil
	default:
		return Pattern, fmt.Errorf("%w: %q", ErrScene, text)
	}
}

// String renders the flag spelling, so a SceneKind round-trips through
// ParseScene and prints as itself.
func (k SceneKind) String() string {
	switch k {
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
func defaultScenes() ([]Drawer, error) {
	return buildScenes(fonts.Small, fonts.Body, fonts.BodyBold, fonts.Large)
}

// buildScenes loads the fonts and builds both scenes, in SceneKind order.
//
// The fonts are loaded here, at startup, rather than when the specimen scene
// first draws. A font that will not parse is then a clear error before
// anything takes the screen over, instead of an empty block ten frames into a
// run on a device with no other diagnostics.
func buildScenes(small, body, bodyBold, large fontLoader) ([]Drawer, error) {
	faces, err := loadFaces(small, body, bodyBold, large)
	if err != nil {
		return nil, err
	}

	return []Drawer{pattern.New(), specimen.New(faces)}, nil
}

// loadFaces calls the four loaders, naming whichever one fails.
func loadFaces(small, body, bodyBold, large fontLoader) (specimen.Faces, error) {
	var (
		faces specimen.Faces
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
