package radar

import (
	"image"
	"image/color"

	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// designHeight is the canvas height every fixed size in the scene was drawn
// for. A canvas this tall or shorter is drawn at those sizes, and one a whole
// multiple of it taller has every one of them multiplied by that multiple.
//
// It is 1080 rather than the panel's 720 so that both of the sizes the scene
// is normally drawn at stay at one: 720 is the uConsole's panel and 1080 the
// ordinary desktop window, and neither should move because a larger canvas
// exists. With 1080 as the base, 2160 is exactly two.
const designHeight = 1080

// uiScale is how many canvas pixels one design pixel is on a canvas of these
// bounds: the height over designHeight, rounded down, and never less than one.
//
// It is the height and not the width because the height is what every block
// is stacked in. The header, the card, the rows and the key bar all take their
// room off it one after another, and the scope is a square as tall as what is
// left, so a canvas that is wide for its height gains column and keeps the
// same picture beside it. Scaling from the width would grow the type on a wide
// canvas until the rows no longer fitted under the card.
//
// It is a whole number because the faces are bitmaps. pkg/text scales a glyph
// nearest-neighbour by an integer, and a face drawn at one and a half would
// have some of its pixels doubled and some not, which reads as a damaged font
// rather than a larger one. Every fixed size follows the faces so the type and
// the furniture around it stay in proportion.
func uiScale(bounds image.Rectangle) int {
	return max(1, bounds.Dy()/designHeight)
}

// unit is the layout's scale, with the zero value read as one.
func (l *layout) unit() int { return max(l.scale, 1) }

// px is a design size in canvas pixels.
func (l *layout) px(size int) int { return size * l.unit() }

// pxf is px for the sizes worked out in floating point: the arrow's corners
// and the aircraft model's span.
func (l *layout) pxf(size float64) float64 { return size * float64(l.unit()) }

// glyphs is the text option every run is drawn and measured with, so a face
// comes out at the layout's scale.
func (l *layout) glyphs() text.Option { return text.WithScale(l.unit()) }

// glyphsAt is glyphs for a run that is already enlarged at the design size,
// which is the card's callsign.
func (l *layout) glyphsAt(scale int) text.Option { return text.WithScale(scale * l.unit()) }

// tracking is letter spacing in design pixels. It is multiplied with the
// glyphs rather than left at its design value, because a tracked label at
// twice the size with the old one-pixel gap reads tighter than the same label
// at the panel's size.
func (l *layout) tracking(pixels int) text.Option { return text.WithSpacing(pixels * l.unit()) }

// lineHeight is how tall one line of a face is at the layout's scale, or zero
// when the face is missing, which is how a block decides it has nothing to
// draw with.
func (l *layout) lineHeight(face *psf.Font) int {
	if face == nil {
		return 0
	}

	return face.Height() * l.unit()
}

// glyphWidth is how wide one glyph of a face is at the layout's scale, or
// zero when the face is missing. The flight strips size their fields from it.
func (l *layout) glyphWidth(face *psf.Font) int {
	if face == nil {
		return 0
	}

	return face.Width() * l.unit()
}

// text draws a run at the layout's scale and returns the x just past it.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (l *layout) text(face *psf.Font, x, y int, value string, ink color.RGBA) int {
	return text.Draw(l.dst, face, x, y, value, ink, l.glyphs())
}

// tracked draws a run at the layout's scale and the scene's label tracking,
// and returns the x just past it.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (l *layout) tracked(face *psf.Font, x, y int, value string, ink color.RGBA) int {
	return text.Draw(l.dst, face, x, y, value, ink, l.glyphs(), l.tracking(labelTracking))
}

// measure is how wide a run is at the layout's scale.
func (l *layout) measure(face *psf.Font, value string) int {
	width, _ := text.Measure(face, value, l.glyphs())

	return width
}

// measureTracked is how wide a run is at the layout's scale and the scene's
// label tracking.
func (l *layout) measureTracked(face *psf.Font, value string) int {
	width, _ := text.Measure(face, value, l.glyphs(), l.tracking(labelTracking))

	return width
}
