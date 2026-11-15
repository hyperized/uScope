package radar

import (
	"image"

	"github.com/hyperized/uScope/internal/source"
)

// barShown reports whether the view on screen carries the bar: a bare view
// with h pressed, or --minimal-bar on. The full views have the header and no
// bar, whatever minimalBar says.
func (s *Scene) barShown() bool { return s.bare() && s.minimalBar }

// toggleBar flips the bare views' bar, which is what h does.
//
// It reports false outside the two bare views, so the key falls through to
// the run loop there the way w does in a bare view: the full views already
// have a header, and a press that quietly flipped a flag nobody can see would
// turn up as a surprise the next time v came round to minimal.
func (s *Scene) toggleBar() bool {
	if !s.bare() {
		return false
	}

	s.minimalBar = !s.minimalBar

	return true
}

// barHeight is the room the bar takes off the top of a bare view, its rule
// included, or zero when it is off, when one of its faces is missing, or when
// the canvas is too short to give it up.
//
// The band is one line of the wordmark's face with the header's padding above
// and below it. The wordmark's bold face and the body face the local clock is
// set in are the same height in Terminus, and the small face of the source
// label, the sun block and UTC is shorter, so one line of the bold face holds
// all of them. There is no margin over it the way there is over the header:
// the bar is the whole top edge of a picture that is otherwise edge to edge,
// and a margin there would be a strip of something that is neither.
func (s *Scene) barHeight(lay *layout) int {
	if !s.barShown() {
		return 0
	}

	line := max(lay.lineHeight(s.faces.BodyBold), lay.lineHeight(s.faces.Body))
	if line == 0 || lay.lineHeight(s.faces.Small) == 0 {
		return 0
	}

	total := line + 2*lay.px(headerPadY) + lay.px(ruleHeight)
	if total >= lay.dst.Bounds().Dy() {
		return 0
	}

	return total
}

// belowBar is the bare views' scope box: the canvas less the bar's height off
// the top. A height of zero is the whole canvas, which is what the bare views
// drew on before there was a bar.
func belowBar(bounds image.Rectangle, bar int) image.Rectangle {
	bounds.Min.Y += bar

	return bounds
}

// drawBar draws the bare views' bar across the top of the canvas: the
// wordmark and the source on the left, the sun block and the two clocks on
// the right, on the header's band colour with its rule under it. total is
// what barHeight answered, and zero draws nothing.
//
// It is the header with the second line taken out. The receiver line and the
// battery stay with the full views: a bare view is for watching traffic, and
// what the bar is for is the time and the light, with the feed's dot to say
// the picture is live. The clocks are a size down from the header's, the
// local one in the body face and UTC in the small one, because the bar is a
// line tall and the header's large clock is taller than that.
func (s *Scene) drawBar(lay *layout, frame source.Frame, total int) {
	if total == 0 {
		return
	}

	bounds := lay.dst.Bounds()
	rule := bounds.Min.Y + total - lay.px(ruleHeight)
	middle := (bounds.Min.Y + rule) / 2

	lay.dst.FillRect(image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Max.X, rule), s.pal.Band)

	pen := s.drawClocks(lay, lay.right, middle, frame.Now, s.faces.Body, s.faces.Small)

	limit := lay.left + s.wordmarkWidth(lay, frame)
	if left, drawn := s.drawSunBlock(lay, pen-lay.px(clockGap), middle, limit); drawn {
		pen = left
	}

	s.drawWordmark(lay, middle-lay.lineHeight(s.faces.BodyBold)/2, frame, pen-lay.px(sourceLabelGap))

	lay.dst.FillRect(image.Rect(bounds.Min.X, rule, bounds.Max.X, rule+lay.px(ruleHeight)), s.pal.Rule)
}
