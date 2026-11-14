// Package sprite draws small monochrome bitmaps onto a canvas, rotated to a
// heading.
//
// A sprite is a square grid of set and clear pixels, defined as string art
// so the shape it draws can be read straight out of the source next to the
// code that builds it. The one built in here is a top-down airplane
// silhouette, used to draw aircraft on the radar scope turned to face the
// heading they are actually flying.
//
// There used to be a second, a nine-pixel arrow for the scene's bearing and
// track figures. It was withdrawn because nearest-neighbour rotation is a
// poor way to turn a shape that small: the arrow broke up at most angles, and
// internal/radar now computes that triangle from the angle instead. A sprite
// earns its keep at fifteen pixels with a silhouette to carry; below that,
// work the shape out in floating point.
package sprite

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"

	"github.com/hyperized/uScope/pkg/canvas"
)

// MaxSide is the largest side length New accepts. A sprite is a small icon,
// and a request for anything bigger than that is a bug in the caller rather
// than a real sprite.
const MaxSide = 64

// degreesPerHalfTurn converts a heading in degrees to radians: radians =
// degrees * math.Pi / degreesPerHalfTurn.
const degreesPerHalfTurn = 180.0

// ErrEmpty is returned by New when rows is nil or has no rows.
var ErrEmpty = errors.New("sprite: rows is empty")

// ErrRagged is returned by New when a row's length does not match the first
// row's length.
var ErrRagged = errors.New("sprite: rows are ragged")

// ErrNotSquare is returned by New when the row count does not equal the row
// length. Draw rotates a sprite around the centre of a square, so a
// non-square sprite is rejected here instead of being rotated wrong later.
var ErrNotSquare = errors.New("sprite: not square")

// ErrTooLarge is returned by New when the side exceeds MaxSide.
var ErrTooLarge = errors.New("sprite: side too large")

// Bitmap is a monochrome sprite: a square grid of set and clear pixels.
//
// A Bitmap is read-only after New returns, so any number of goroutines may
// call At or Draw on the same one at the same time. What they must not do is
// call Draw against the same canvas at once, because the canvas itself is
// not safe for concurrent use.
type Bitmap struct {
	side   int
	pixels []bool

	// lights is where the silhouette carries its exterior lights, and only
	// means anything while hasLights is set. Art handed to New has no idea
	// which of its pixels is a wingtip, so only the built-in silhouette has
	// them.
	lights    Lights
	hasLights bool
}

// Lights are the pixels of a silhouette that carry its exterior lights, in
// the bitmap's own coordinates with the nose up: the port wingtip on the left,
// the starboard wingtip on the right, the tail at the bottom and the
// anti-collision beacon on the fuselage.
type Lights struct {
	Port      image.Point
	Starboard image.Point
	Tail      image.Point
	Beacon    image.Point
}

// New builds a bitmap from string art. Each string is one row; a space or a
// '.' is clear, and any other character is set.
//
// New validates rows because it has no way to know where they came from: a
// literal in this file today, perhaps a file loaded at runtime tomorrow.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func New(rows []string) (*Bitmap, error) {
	if len(rows) == 0 {
		return nil, ErrEmpty
	}

	side := len(rows[0])

	for y, row := range rows {
		if len(row) != side {
			return nil, fmt.Errorf("%w: row %d has length %d, want %d", ErrRagged, y, len(row), side)
		}
	}

	if len(rows) != side {
		return nil, fmt.Errorf("%w: %d rows of %d characters each", ErrNotSquare, len(rows), side)
	}

	if side > MaxSide {
		return nil, fmt.Errorf("%w: side %d exceeds %d", ErrTooLarge, side, MaxSide)
	}

	pixels := make([]bool, side*side)

	for y, row := range rows {
		for x, char := range row {
			if char != ' ' && char != '.' {
				pixels[y*side+x] = true
			}
		}
	}

	return &Bitmap{side: side, pixels: pixels}, nil
}

// mustBitmap builds a bitmap from rows and panics if the art does not parse.
// It exists so the sprites built into this package go through exactly the
// same validation as any other caller's rows, while still treating a bad
// literal in this file as the programmer error it would be.
func mustBitmap(rows []string) *Bitmap {
	bmp, err := New(rows)
	if err != nil {
		panic(fmt.Sprintf("sprite: built-in art does not parse: %v", err))
	}

	return bmp
}

// Side returns the sprite's width, which is also its height.
func (b *Bitmap) Side() int {
	return b.side
}

// At reports whether the pixel at (x, y) is set. Coordinates outside the
// bitmap report false.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (b *Bitmap) At(x, y int) bool {
	if x < 0 || x >= b.side || y < 0 || y >= b.side {
		return false
	}

	return b.pixels[y*b.side+x]
}

// airplane is the package's Airplane singleton, built once on first use.
// sync.OnceValue is used rather than an init function so the bitmap is built
// lazily, and rather than a plain package variable so there is no mutable
// global sitting around after the first call.
//
//nolint:gochecknoglobals // memoization idiom, not mutable state; see the comment above.
var airplane = sync.OnceValue(func() *Bitmap {
	// Nose up, 15x15, read top row first. The wings rake back rather than
	// standing square, because a swept wing is what tells a reader which end
	// is the front once the sprite is rotated to a heading and there are only
	// fifteen pixels to say it in.
	//
	//	.......#.......
	//	......###......
	//	......###......
	//	......###......
	//	......###......
	//	.....#####.....
	//	....#######....
	//	..###########..
	//	.#############.
	//	......###......
	//	......###......
	//	......###......
	//	.....#####.....
	//	....#######....
	//	......###......
	//
	//nolint:goconst // ascii art rows repeat on purpose; naming them would hide the shape.
	rows := []string{
		".......#.......",
		"......###......",
		"......###......",
		"......###......",
		"......###......",
		".....#####.....",
		"....#######....",
		"..###########..",
		".#############.",
		"......###......",
		"......###......",
		"......###......",
		".....#####.....",
		"....#######....",
		"......###......",
	}

	bmp := mustBitmap(rows)

	// The two ends of the widest row and the middle of the bottom one. Port is
	// on the left because the art is drawn from above with the nose up, the
	// way the aircraft would be seen from a camera over it. The beacon is the
	// centre pixel, the one Draw turns the sprite about, so it stays on the
	// fuselage at every heading.
	bmp.lights = Lights{
		Port:      image.Pt(1, 8),  //nolint:mnd // a pixel of the art above.
		Starboard: image.Pt(13, 8), //nolint:mnd // a pixel of the art above.
		Tail:      image.Pt(7, 14), //nolint:mnd // a pixel of the art above.
		Beacon:    image.Pt(7, 7),  //nolint:mnd // a pixel of the art above.
	}
	bmp.hasLights = true

	return bmp
})

// Airplane is a 15x15 top-down airplane silhouette, nose up.
func Airplane() *Bitmap {
	return airplane()
}

// Lights reports where the bitmap carries its exterior lights, and false for a
// bitmap that has none, which is every one New builds.
func (b *Bitmap) Lights() (Lights, bool) {
	return b.lights, b.hasLights
}

// Turn is a heading resolved once into the sine and cosine Draw turns a sprite
// by, so a caller placing more than one pixel of the same turned sprite pays
// for the trigonometry once.
type Turn struct {
	sin float64
	cos float64
}

// TurnTo resolves a compass heading in degrees, treating NaN and the
// infinities as zero the way Draw does.
func TurnTo(headingDeg float64) Turn {
	if math.IsNaN(headingDeg) || math.IsInf(headingDeg, 0) {
		headingDeg = 0
	}

	sin, cos := math.Sincos(headingDeg * math.Pi / degreesPerHalfTurn)

	return Turn{sin: sin, cos: cos}
}

// Landing is where Draw puts the bitmap's pixel at pixel once the sprite is
// turned by turn, as an offset from the centre Draw is handed and before the
// scale enlarges it: multiply by the scale and add the centre to get the block
// Draw stamped.
//
// Draw walks the destination and rounds the inverse turn, so a pixel lands
// wherever a destination happened to sample it, which is not always the
// forward turn rounded. The three by three box around the rounded point holds
// every destination that can sample it, so Landing searches that box, nine
// inverse turns at most. At some angles nothing samples the pixel at all,
// because two source pixels rounded onto one; the tip of a wing is the usual
// case. Then Landing answers with the nearest destination in the box that
// Draw did paint, so a light put there still sits on the shape, and with the
// rounded point only when nothing in the box is painted.
//
//nolint:varnamelen // dx, dy are offsets from the centre, the coordinate idiom used throughout uScope.
func (b *Bitmap) Landing(pixel image.Point, turn Turn) image.Point {
	half := (b.side - 1) / 2
	srcX, srcY := pixel.X-half, pixel.Y-half

	near := image.Pt(
		int(math.Round(float64(srcX)*turn.cos-float64(srcY)*turn.sin)),
		int(math.Round(float64(srcX)*turn.sin+float64(srcY)*turn.cos)),
	)

	fallback, painted := near, false

	for _, step := range landingSearch {
		dx, dy := near.X+step.X, near.Y+step.Y
		gotX, gotY := invRotate(dx, dy, turn.sin, turn.cos)

		if gotX == srcX && gotY == srcY {
			return image.Pt(dx, dy)
		}

		if !painted && b.At(half+gotX, half+gotY) {
			fallback, painted = image.Pt(dx, dy), true
		}
	}

	return fallback
}

// landingSearch is the order Landing tries the pixels around the rounded
// point in: the point itself first, so an exact landing costs one inverse
// turn, then the four sides before the four corners.
//
//nolint:gochecknoglobals // a fixed table, read-only after init.
var landingSearch = [...]image.Point{
	{}, {X: 1}, {X: -1}, {Y: 1}, {Y: -1},
	{X: 1, Y: 1}, {X: -1, Y: 1}, {X: 1, Y: -1}, {X: -1, Y: -1},
}

// invRotate returns the source offset that lands at destination offset (dx,
// dy) once the sprite is turned by the angle whose sine and cosine are sin
// and cos.
//
// Draw needs the inverse of the rotation, not the rotation itself: it walks
// the destination pixels and asks where each one's colour comes from, so
// every destination pixel gets an answer. Walking the source forward and
// rotating each of its pixels out would miss destination pixels that no
// source pixel happens to land on exactly, leaving holes in the shape.
//
//nolint:varnamelen // dx, dy, sx, sy are offsets from the centre, the coordinate idiom used throughout uScope.
func invRotate(dx, dy int, sin, cos float64) (int, int) {
	sx := float64(dx)*cos + float64(dy)*sin
	sy := float64(dy)*cos - float64(dx)*sin

	return int(math.Round(sx)), int(math.Round(sy))
}

// Draw stamps the sprite onto dst centred at (cx, cy), rotated so that the
// nose points along headingDeg.
//
// Heading 0 points towards the top of the canvas, decreasing y. Increasing
// heading turns clockwise: 90 points right, 180 points down, 270 points
// left. That is compass convention, not the mathematical one.
//
// A heading that is NaN or infinite is treated as zero. A scope that has
// lost a reliable heading should still draw the sprite upright, not drop it
// off the screen.
//
// scale enlarges the turned sprite nearest-neighbour, each of its pixels
// stamped as a scale by scale block with the block's top-left corner where the
// pixel would have been at scale one, measured out from (cx, cy). The turn is
// worked out at the bitmap's own size and enlarged afterwards rather than
// worked out at the larger size: a fifteen-pixel silhouette rotated at thirty
// pixels samples between its own pixels and comes out with ragged edges, where
// the enlarged one is the same shape the panel shows, only bigger. A scale
// below one is drawn at one.
//
// A nil receiver or a nil dst draws nothing.
//
//nolint:varnamelen // cx, cy name the sprite's centre, the same short-coordinate idiom pkg/canvas uses for x, y.
func (b *Bitmap) Draw(dst *canvas.Canvas, cx, cy int, headingDeg float64, scale int, col color.RGBA) {
	if b == nil || dst == nil {
		return
	}

	turn := TurnTo(headingDeg)
	half := (b.side - 1) / 2

	for dy := -(half + 1); dy <= half+1; dy++ {
		for dx := -(half + 1); dx <= half+1; dx++ {
			srcX, srcY := invRotate(dx, dy, turn.sin, turn.cos)

			if !b.At(half+srcX, half+srcY) {
				continue
			}

			b.stamp(dst, cx, cy, dx, dy, scale, col)
		}
	}
}

// stamp sets one pixel of the turned sprite, enlarged to a block at any scale
// above one. Scale one stays a single Set, so the size the sprite was drawn at
// before it could be enlarged costs what it always did.
//
//nolint:varnamelen // cx, cy, dx, dy are the centre and the offset from it, uScope's coordinate idiom.
func (*Bitmap) stamp(dst *canvas.Canvas, cx, cy, dx, dy, scale int, col color.RGBA) {
	if scale <= 1 {
		dst.Set(cx+dx, cy+dy, col)

		return
	}

	x, y := cx+dx*scale, cy+dy*scale
	dst.FillRect(image.Rect(x, y, x+scale, y+scale), col)
}
