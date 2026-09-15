// Package text draws strings onto a canvas with a PSF console font.
//
// There is no layout engine here and there is not going to be one. A console
// font is a fixed grid: every glyph is the same box, so a run of text is the
// glyph width times the number of runes, and that is the whole of it. What
// the package does add is what a scene actually needs on a 1280x720 panel:
// integer scaling so one face covers three sizes, optional letter spacing so
// a small label can be tracked out, an optional painted background so a key
// cap can be knocked out of a filled box, and right and centre alignment
// computed from the same measurement the drawing uses.
//
// Drawing is transparent by default: only the ink pixels of a glyph are
// touched, so text sits on top of whatever is already on the canvas.
// Everything is clipped by canvas.Set, so a string may start off the left
// edge or run off the right.
//
// Nothing here holds state, so the package is safe to use from any goroutine
// as long as no two of them are drawing on the same canvas.
package text

import (
	"image"
	"image/color"
	"unicode/utf8"

	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
)

// Limits on the two numeric options. Both clamp rather than reject: a scale
// or a spacing is a continuous knob, not a choice between named things, so
// the nearest usable value is what the caller meant. That is the opposite of
// how uScope treats a flag, where an unrecognised value means the operator
// asked for something that does not exist.
const (
	// MinScale is the smallest glyph scale, which is the font's own size.
	MinScale = 1

	// MaxScale is the largest. Eight times a 32 pixel face is 256 pixels
	// tall, which is already a third of the panel.
	MaxScale = 8

	// MinSpacing is no extra gap between glyphs.
	MinSpacing = 0

	// MaxSpacing is the widest gap. Past a glyph's own width the string
	// stops reading as a word.
	MaxSpacing = 64
)

// options is the settled configuration for one call.
type options struct {
	background color.RGBA
	filled     bool
	scale      int
	spacing    int
}

// Option adjusts one drawing or measuring call.
//
// It takes a value and returns one rather than taking a pointer, which is not
// how the rest of uScope spells a functional option. The reason is that this
// one is applied per call on the render path rather than once at
// construction: handing an unknown function the address of a local forces the
// compiler to assume the pointer escapes, and that costs a heap allocation on
// every Draw, including the ones that pass no options at all. Passing the
// settings by value keeps them on the stack.
type Option func(options) options

// WithBackground paints the run's box in col before the glyphs go down.
//
// The box covers the whole measured run rather than each glyph's own cell, so
// a tracked-out label comes out as one solid block instead of a row of
// separate ones.
func WithBackground(col color.RGBA) Option {
	return func(set options) options {
		set.background = col
		set.filled = true

		return set
	}
}

// WithScale draws each glyph pixel as a scale by scale block, clamped to
// MinScale and MaxScale. Nearest neighbour is the right answer here: the
// glyphs are one-bit bitmaps designed for a pixel grid, and interpolating
// them would only make them blurry.
func WithScale(scale int) Option {
	return func(set options) options {
		set.scale = clamp(scale, MinScale, MaxScale)

		return set
	}
}

// WithSpacing adds pixels between glyphs, clamped to MinSpacing and
// MaxSpacing. The gap goes between glyphs and not after the last one, so a
// tracked string still measures and right-aligns exactly.
func WithSpacing(pixels int) Option {
	return func(set options) options {
		set.spacing = clamp(pixels, MinSpacing, MaxSpacing)

		return set
	}
}

// Draw paints value at (x, y), which is the top-left corner of the first
// glyph's cell, and returns the x just past the last glyph.
//
// The return value is x plus the width Measure reports for the same
// arguments, so a caller can set a run of pieces in different faces by
// feeding one call's answer into the next.
//
// A rune the font has no glyph for is drawn as the font's fallback, and so is
// each byte of invalid UTF-8.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func Draw(dst *canvas.Canvas, font *psf.Font, x, y int, value string, ink color.RGBA, opts ...Option) int {
	set := settle(opts)

	width, height := runSize(font, value, set)
	if width == 0 {
		return x
	}

	if set.filled {
		dst.FillRect(image.Rect(x, y, x+width, y+height), set.background)
	}

	pen{dst: dst, font: font, ink: ink, set: set}.run(x, y, value)

	return x + width
}

// DrawRight paints value so that it ends at rightX, and returns rightX.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout uScope.
func DrawRight(dst *canvas.Canvas, font *psf.Font, rightX, y int, value string, ink color.RGBA, opts ...Option) int {
	width, _ := Measure(font, value, opts...)

	return Draw(dst, font, rightX-width, y, value, ink, opts...)
}

// DrawCentered paints value centred on centerX and returns the x just past
// the last glyph.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout uScope.
func DrawCentered(
	dst *canvas.Canvas, font *psf.Font, centerX, y int, value string, ink color.RGBA, opts ...Option,
) int {
	width, _ := Measure(font, value, opts...)

	return Draw(dst, font, centerX-width/2, y, value, ink, opts...)
}

// Measure reports the pixel box a run would occupy. An empty string, or a nil
// font, measures zero by zero, which is what lets a scene ask whether it has
// a face to draw with at all.
func Measure(font *psf.Font, value string, opts ...Option) (int, int) {
	return runSize(font, value, settle(opts))
}

// pen carries what every glyph in one run needs, so the loops below take
// coordinates and nothing else.
type pen struct {
	dst  *canvas.Canvas
	font *psf.Font
	ink  color.RGBA
	set  options
}

// settle applies the options over the defaults.
func settle(opts []Option) options {
	set := options{scale: MinScale}

	for _, opt := range opts {
		set = opt(set)
	}

	return set
}

// runSize is Measure with the options already settled, so Draw can size the
// background box without walking the option list twice.
func runSize(font *psf.Font, value string, set options) (int, int) {
	if font == nil {
		return 0, 0
	}

	count := utf8.RuneCountInString(value)
	if count == 0 {
		return 0, 0
	}

	return count*font.Width()*set.scale + (count-1)*set.spacing, font.Height() * set.scale
}

// run walks the string, drawing one glyph per rune.
//
// Ranging over a string decodes invalid UTF-8 as one utf8.RuneError per bad
// byte, which is exactly the behaviour wanted here: the text still occupies
// the space it measured, with a fallback glyph standing in for each byte
// nobody can read.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (p pen) run(x, y int, value string) {
	step := p.font.Width()*p.set.scale + p.set.spacing

	for _, code := range value {
		glyph, ok := p.font.Glyph(code)
		if !ok {
			glyph = p.font.Fallback()
		}

		p.glyph(glyph, x, y)
		x += step
	}
}

// glyph paints one glyph's ink pixels with its top-left corner at (originX,
// originY).
func (p pen) glyph(glyph psf.Glyph, originX, originY int) {
	scale := p.set.scale

	for row := range p.font.Height() {
		for col := range p.font.Width() {
			if !glyph.Set(col, row) {
				continue
			}

			if scale == MinScale {
				p.dst.Set(originX+col, originY+row, p.ink)

				continue
			}

			p.dst.FillRect(image.Rect(
				originX+col*scale, originY+row*scale,
				originX+(col+1)*scale, originY+(row+1)*scale,
			), p.ink)
		}
	}
}

// clamp pins value into the inclusive range.
func clamp(value, low, high int) int {
	return min(max(value, low), high)
}
