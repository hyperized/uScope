package canvas

import (
	"image"
	"image/color"
	"slices"
)

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
	c.ClearRowsExcept(rows, rest, nil)
}

// ClearRowsExcept is ClearRows that leaves alone every row whose entry in
// skip is true, counted from the canvas's first row like rows. Rows past the
// end of skip are cleared.
//
// It is for a picture something opaque is about to cover edge to edge on
// some rows, such as the ground under a sky: clearing those rows only to
// paint over every pixel of them was a whole write of the frame's lower half
// thrown away. CoveredRows says which rows a fill will cover.
func (c *Canvas) ClearRowsExcept(rows []color.RGBA, rest color.RGBA, skip []bool) {
	rect := c.img.Rect
	width := rect.Dx() * bytesPerPixel
	painted := -1

	//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		index := y - rect.Min.Y
		if index < len(skip) && skip[index] {
			continue
		}

		if index < len(rows) {
			c.hline(rect.Min.X, rect.Max.X-1, y, rows[index])

			continue
		}

		// The rows that share rest are one row painted and then copied
		// down, which is cheaper than doubling copies along each of them.
		off := c.img.PixOffset(rect.Min.X, y)
		if painted < 0 {
			c.hline(rect.Min.X, rect.Max.X-1, y, rest)

			painted = off

			continue
		}

		copy(c.img.Pix[off:off+width], c.img.Pix[painted:painted+width])
	}
}

// CoveredRows marks, for each row of the canvas, whether FillPolygon of rings
// clipped to clip would paint every pixel of clip on that row, and returns
// the marks, one per row from the canvas's first, in dst grown as needed.
//
// It runs the fill's own sweep without painting, so its answer is the fill's
// to the pixel: a row it marks is one the fill, or any FillLayersZoned whose
// lowest layer is these rings, writes edge to edge. That is what makes it
// safe to skip clearing a row it marks before such a fill.
func (c *Canvas) CoveredRows(rings [][]image.Point, clip image.Rectangle, dst []bool) []bool {
	rect := c.img.Rect
	dst = slices.Grow(dst[:0], rect.Dy())[:rect.Dy()]
	clear(dst)

	area := clip.Intersect(rect)
	if area.Empty() {
		return dst
	}

	c.edges = buildEdges(c.edges[:0], rings, area)
	if len(c.edges) == 0 {
		return dst
	}

	c.edges, c.spare = sortByTop(c.edges, c.spare, &c.tops)
	c.active = c.active[:0]
	next := 0

	//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
	for y := c.edges[0].top; y < area.Max.Y; y++ {
		c.active = retire(c.active, y)
		c.active, next = admit(c.active, c.edges, next, y)

		dst[y-rect.Min.Y] = c.rowCovered(area)

		advance(c.active)
	}

	return dst
}

// rowCovered reports whether the active edges' even-odd spans on the current
// row take in the whole width of area, by fillRow's own pairing. Spans that
// meet end to end, as two rings side by side give, cover the join between
// them.
func (c *Canvas) rowCovered(area image.Rectangle) bool {
	c.sortCrossings()

	reach := area.Min.X

	for index := 0; index+1 < len(c.crossings) && reach < area.Max.X; index += 2 {
		if pixelAt(c.crossings[index]) > reach {
			return false
		}

		reach = max(reach, pixelAt(c.crossings[index+1]))
	}

	return reach >= area.Max.X
}
