package canvas

import (
	"image/color"
	"math"
)

// BlendStrokeCircle mixes col at alpha into the ring StrokeCircle would draw.
//
// An alpha of one or more is StrokeCircle, byte for byte, and one that is
// NaN, zero or negative draws nothing, the contract BlendCircle keeps. In
// between, the ring is always the annulus StrokeCircle fills for a thick
// width, at least one pixel wide, even where StrokeCircle would draw width
// one as the midpoint ring: the midpoint ring sets some pixels twice where
// its octants meet, and a blend applied twice there would show as darker
// beads around the ring. Pixels off the canvas are skipped, as they are for
// every other shape.
func (c *Canvas) BlendStrokeCircle(centerX, centerY, radius, width int, col color.RGBA, alpha float64) {
	if math.IsNaN(alpha) || alpha <= 0 || radius < 0 {
		return
	}

	if alpha >= 1 {
		c.StrokeCircle(centerX, centerY, radius, width, col)

		return
	}

	width = max(width, 1)
	inner := float64(max(radius-width/2, 0)) - halfPixel
	outer := float64(max(radius-width/2, 0)+width-1) + halfPixel
	reach := int(math.Ceil(outer))

	for offset := -reach; offset <= reach; offset++ {
		first, last, lit := annulusExtent(float64(offset), inner, outer)
		if !lit {
			continue
		}

		row := centerY + offset
		if first == 0 {
			c.blendSpan(centerX-last, centerX+last, row, col, alpha)

			continue
		}

		c.blendSpan(centerX-last, centerX-first, row, col, alpha)
		c.blendSpan(centerX+first, centerX+last, row, col, alpha)
	}
}
