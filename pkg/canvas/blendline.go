package canvas

import (
	"image/color"
	"math"
)

// BlendLineAA draws StrokeLineAA's line composited over what is under it at
// alpha, rather than painted solid. Alpha is held to one, and an alpha of
// zero or less draws nothing. A width under one is drawn one pixel wide, as
// the two-pixel band's thin sibling rather than through LineAA, so the same
// line drawn at widths one and two sits in the same place.
//
// A faded colour drawn solid is the cheap way to draw a faint line and is
// right over a near-uniform field, which is what the flat scope is. It goes
// wrong over the tilted views' sky and lit ground, where a faint line comes
// out darker than what is around it.
//
//nolint:varnamelen // x0, y0, x1, y1 is how Line spells the same four values.
func (c *Canvas) BlendLineAA(x0, y0, x1, y1 float64, width int, col color.RGBA, alpha float64) {
	if alpha <= 0 || invalidCoordinate(x0) || invalidCoordinate(y0) || invalidCoordinate(x1) || invalidCoordinate(y1) {
		return
	}

	band := float64(max(width, 1))
	if c.missesCanvas(x0, y0, x1, y1, band) {
		return
	}

	run := orientRun(x0, y0, x1, y1)
	lift := (band - 1) / 2
	interY := run.startMinor + run.gradient*(run.firstCol-run.startMajor)
	lanes := c.lanes(run.steep)
	ink := levelsOf(col)
	alpha = min(alpha, 1)

	for pixelX := run.firstCol; pixelX <= run.lastCol; pixelX++ {
		if major := int(pixelX); major >= lanes.majorFrom && major < lanes.majorTo {
			c.blendBand(&lanes, major, interY-lift, band, &ink, alpha)
		} else if major >= lanes.majorTo {
			return
		}

		interY += run.gradient
	}
}

// blendBand is coverBand with every pixel's cover scaled by alpha.
func (c *Canvas) blendBand(lanes *lanes, major int, top, height float64, ink *inkLevels, alpha float64) {
	bottom := top + height
	row := math.Floor(top)

	if first := float64(lanes.minorFrom); row < first {
		row = first
	}

	pix := c.img.Pix
	off := c.offset(lanes, major, int(row))

	for ; row < bottom; row++ {
		if int(row) >= lanes.minorTo {
			return
		}

		if cover := (min(row+1, bottom) - max(row, top)) * alpha; cover > 0 {
			dst := pix[off : off+bytesPerPixel : off+bytesPerPixel]
			dst[0] = blendToward(dst[0], ink.red, cover)
			dst[1] = blendToward(dst[1], ink.green, cover)
			dst[2] = blendToward(dst[2], ink.blue, cover)
			dst[3] = opaque
		}

		off += lanes.minorStep
	}
}
