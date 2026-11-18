package canvas

import (
	"image"
	"image/color"
	"math"
)

// quadEdges is the most edges a four-sided ring has.
const quadEdges = 4

// BlendQuad is BlendPolygon for a single ring of four points, the same
// pixels at the same bytes, without the general sweep's scratch, sorting and
// active list.
//
// A perspective scene fills thousands of small quadrilaterals a frame, and
// for a shape that size the bookkeeping of the general fill cost as much as
// the blending. The edges are built by the same appendEdge and stepped by
// the same addition, so every crossing is the same float64 the sweep would
// have found, and the crossings on a row are taken in pairs the same way.
// It allocates nothing and touches none of the canvas's scratch.
func (c *Canvas) BlendQuad(quad [4]image.Point, clip image.Rectangle, col color.RGBA, alpha float64) {
	if math.IsNaN(alpha) || alpha <= 0 {
		return
	}

	area := clip.Intersect(c.img.Rect)
	if area.Empty() {
		return
	}

	var store [quadEdges]polyEdge

	edges := store[:0]
	for index, point := range quad {
		edges = appendEdge(edges, point, quad[(index+1)%quadEdges], area)
	}

	if len(edges) == 0 {
		return
	}

	top, bottom := edges[0].top, edges[0].bottom
	for _, edge := range edges[1:] {
		top, bottom = min(top, edge.top), max(bottom, edge.bottom)
	}

	//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
	for y := top; y < bottom; y++ {
		c.quadRow(edges, y, area, col, alpha)
	}
}

// quadRow fills one row of BlendQuad: the crossings of the edges active on
// it, sorted, and the spans between pairs of them, then every active edge
// stepped on to the next row.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout this package.
func (c *Canvas) quadRow(edges []polyEdge, y int, area image.Rectangle, col color.RGBA, alpha float64) {
	var crossings [quadEdges]float64

	count := 0

	for index := range edges {
		edge := &edges[index]
		if y < edge.top || y >= edge.bottom {
			continue
		}

		// Insertion into the sorted few, which is all slices.Sort does for
		// a handful of values anyway.
		at := count
		for at > 0 && crossings[at-1] > edge.x {
			crossings[at] = crossings[at-1]
			at--
		}

		crossings[at] = edge.x
		count++
		edge.x += edge.slope
	}

	for index := 0; index+1 < count; index += 2 {
		from := max(pixelAt(crossings[index]), area.Min.X)
		to := min(pixelAt(crossings[index+1])-1, area.Max.X-1)

		if alpha >= 1 {
			c.hline(from, to, y, col)

			continue
		}

		c.blendSpan(from, to, y, col, alpha)
	}
}
