package canvas

import "image/color"

// ClearRows paints the whole canvas one colour per row: the first row takes
// rows[0], the next rows[1], and every row past the end of rows takes rest.
// Entries past the bottom of the canvas are ignored, so a caller can hand
// over a gradient measured against a picture taller than the canvas.
//
// It is Clear for a picture whose background changes down the frame, such as
// a sky, and it costs the same: each row is written once, by doubling copies
// along it, and the rows that share rest are one row copied down. Painting a
// gradient over a cleared frame instead would write every pixel of it twice.
//
// rows is only read, never kept, so the caller may reuse it.
func (c *Canvas) ClearRows(rows []color.RGBA, rest color.RGBA) {
	rect := c.img.Rect

	//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
	y := rect.Min.Y

	for ; y < rect.Max.Y && y-rect.Min.Y < len(rows); y++ {
		c.hline(rect.Min.X, rect.Max.X-1, y, rows[y-rect.Min.Y])
	}

	if y >= rect.Max.Y {
		return
	}

	c.hline(rect.Min.X, rect.Max.X-1, y, rest)

	width := rect.Dx() * bytesPerPixel
	first := c.img.PixOffset(rect.Min.X, y)
	row := c.img.Pix[first : first+width]

	for y++; y < rect.Max.Y; y++ {
		off := c.img.PixOffset(rect.Min.X, y)
		copy(c.img.Pix[off:off+width], row)
	}
}
