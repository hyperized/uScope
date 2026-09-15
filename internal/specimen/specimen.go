// Package specimen draws the slice 3 type specimen.
//
// It exists to answer, on the panel rather than in a test, whether the PSF
// parser and the text drawer produce something a person can read at arm's
// length on a 5 inch screen. So it is half a font sample and half a mock of
// the radar's furniture: the header band, the selected-flight card, the
// compact rows and the key bar that DESIGN.md calls for, with invented
// aircraft in them. When the radar lands it inherits this layout and swaps
// the mock data for real decodes.
//
// Every block sizes itself from the canvas bounds and the metrics of the face
// it is set in, never from a number that assumes 1280x720. A block that does
// not fit is dropped rather than drawn over its neighbour, so the same scene
// renders on the panel and in an 80x24 terminal of half blocks, where almost
// nothing fits and the little that does is still legible.
//
// A Scene is read-only once built, so one instance draws every frame and any
// number of goroutines may share it, as long as no two of them are drawing on
// the same canvas.
package specimen

import (
	"time"

	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
)

// Spacing, in pixels at the panel's resolution. These are the only fixed
// numbers in the scene; everything else comes from a font metric.
const (
	// baseMargin is the border around the whole frame, cut down on a canvas
	// too small to spare it by marginDivisor.
	baseMargin    = 16
	marginDivisor = 8

	// blockGap separates the stacked blocks, rowGap the lines inside one
	// block, and labelLead a small label from the thing it labels.
	blockGap  = 16
	rowGap    = 12
	rowLead   = 6
	labelLead = 4

	// headerPadY is the air above and below the header band's tallest face;
	// ruleHeight is the hairline under it.
	headerPadY = 6
	ruleHeight = 1

	// The card: padding inside the border, the accent bar down its left
	// edge, the scale the callsign is set at, and the gap before a unit
	// suffix.
	cardPadX       = 12
	cardPadY       = 14
	accentWidth    = 3
	cardTitleScale = 2
	unitGap        = 6

	// columnGap is the air between the compact rows' measured columns, and
	// figureGap the air between the card's three figures.
	columnGap = 16
	figureGap = 40

	// The key caps: padding inside the filled box, the gap from a cap to its
	// own label, and the gap from that label to the next cap.
	capPadX  = 5
	capPadY  = 3
	capGap   = 6
	entryGap = 22

	// Letter spacing for the small all-caps labels. Terminus is tight at 6
	// pixels wide and a tracked-out label reads as a heading rather than as
	// text someone forgot to finish.
	labelTracking  = 1
	headerTracking = 2
)

// The content. It is invented, but it is shaped exactly like what the radar
// will put here, so a layout that holds together for this holds together for
// real decodes.
const (
	appTitle    = "USCOPE"
	clockFormat = "15:04"

	cardLabel    = "01 / SELECTED FLIGHT"
	cardCallsign = "UAE35Q"
	cardType     = "BOEING 777-300ER"
	cardTrack    = "TRACK 264 / W"

	sampleText = "ABCDEFGHIJKLMNOPQRSTUVWXYZ 0123456789 .,:/-"

	// creditText is the attribution the font licence asks for. It is also
	// the one string in the scene with a non-ASCII character in it, so a
	// unicode table that failed to parse shows up here as a fallback glyph
	// rather than silently.
	//nolint:gosec // G101 reads "LICENSE" as a credential; it is an attribution line.
	creditText = "TERMINUS FONT © 2010 DIMITAR TOSHKOV ZHEKOV / SIL OPEN FONT LICENSE 1.1"
)

// callsignColumn is the one column of a compact row set in ink; the rest are
// muted, so the eye lands on the callsign first when scanning down.
const callsignColumn = 1

// columns is how many cells a compact row has.
const columns = 5

// figure is one of the three numbers across the bottom of the card: the value
// set large, the unit set small beside it.
type figure struct {
	value string
	unit  string
}

// namedFace pairs a face with the label the specimen block prints above it.
type namedFace struct {
	name string
	face *psf.Font
}

// keyCap is one entry in the bottom bar: the cap, then what the key does.
type keyCap struct {
	key   string
	label string
}

// The card's three figures, in the order DESIGN.md lists them: distance,
// altitude, speed.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var figures = [...]figure{
	{value: "14.7", unit: "KM"},
	{value: "7,400", unit: "FT"},
	{value: "256", unit: "KT"},
}

// The compact rows under the card. Same cells in the same order every row,
// which is the point of them.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var flightRows = [...][columns]string{
	{"02", "KLM93V", "E290", "39,000 FT", "19.4 KM"},
	{"03", "DLH4EA", "A320", "36,000 FT", "27.1 KM"},
}

// The key bar along the bottom. These are the keys the run loop binds.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var keyCaps = [...]keyCap{
	{key: "Q", label: "QUIT"},
	{key: "L", label: "THEME"},
	{key: "S", label: "SCENE"},
}

// Faces are the four console fonts the scene sets type in. A nil face is not
// an error: the block that would have used it is skipped, the same way a
// block that does not fit is.
type Faces struct {
	Small    *psf.Font
	Body     *psf.Font
	BodyBold *psf.Font
	Large    *psf.Font
}

// Scene is the type specimen.
type Scene struct {
	faces Faces
	pal   theme.Palette
	now   func() time.Time
}

// Option adjusts a Scene at construction.
type Option func(*Scene)

// WithClock replaces the source of the header clock. A nil function leaves
// time.Now in place rather than producing a scene that panics on its first
// frame.
func WithClock(now func() time.Time) Option {
	return func(scene *Scene) {
		if now != nil {
			scene.now = now
		}
	}
}

// WithPalette replaces the colours at construction.
func WithPalette(pal theme.Palette) Option {
	return func(scene *Scene) { scene.pal = pal }
}

// SetPalette replaces the colours on a scene that is already built. This is
// what the l key uses to cycle the theme at run time: internal/app calls it
// on every scene that implements it, not only the one on screen, so
// switching scenes later still shows the theme that was chosen.
func (s *Scene) SetPalette(pal theme.Palette) { s.pal = pal }

// New builds the scene around the four faces it will set type in.
//
// The faces are a parameter rather than an option because a specimen without
// fonts has nothing to show; the options are the things that have a sensible
// default.
func New(faces Faces, opts ...Option) *Scene {
	scene := &Scene{faces: faces, pal: theme.Night, now: time.Now}

	for _, opt := range opts {
		opt(scene)
	}

	return scene
}

// layout is the box the blocks flow down, and the pen's position in it.
//
// Blocks are added top down, except the key bar, which takes its room off the
// bottom before anything else runs. Each block asks fits before it draws, so
// running out of canvas loses the blocks that did not fit rather than
// overlapping the ones that did.
type layout struct {
	dst    *canvas.Canvas
	left   int
	right  int
	bottom int
	y      int
}

// fits reports whether a block of this height still has room.
func (l *layout) fits(height int) bool { return l.y+height <= l.bottom }

// advance moves the pen past a block that has been drawn.
func (l *layout) advance(height int) { l.y += height }

// Draw paints the whole scene. elapsed is unused: nothing here animates, and
// the only thing that changes between frames is the clock.
func (s *Scene) Draw(dst *canvas.Canvas, _ time.Duration) {
	dst.Clear(s.pal.Field)

	bounds := dst.Bounds()

	// The margin comes out of the canvas rather than being a fixed 16, so a
	// small canvas is not all border. Taking an eighth of the short edge
	// also means the box left over is always at least a pixel wide: the
	// margin can never exceed an eighth of the width, so two of them can
	// never exceed a quarter of it, and there is nothing to guard against.
	margin := min(baseMargin, min(bounds.Dx(), bounds.Dy())/marginDivisor)

	lay := &layout{
		dst:    dst,
		left:   bounds.Min.X + margin,
		right:  bounds.Max.X - margin,
		bottom: bounds.Max.Y - margin,
		y:      bounds.Min.Y + margin,
	}

	s.drawKeyBar(lay)
	s.drawHeader(lay)
	s.drawCard(lay)
	s.drawRows(lay)
	s.drawFaces(lay)
}

// lineHeight is how tall one line of a face is, or zero when the face is
// missing, which is how a block decides it has nothing to draw with.
func lineHeight(face *psf.Font) int {
	if face == nil {
		return 0
	}

	return face.Height()
}
