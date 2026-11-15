package radar

import (
	"math"

	"github.com/hyperized/uScope/pkg/shore"
)

// The shore the tilted ground keeps between frames.
const (
	// landSlack is the headroom the cache is collected with, as a multiple of
	// the radius the frame asked for. The radius follows the camera's
	// distance, which moves with the tilt, the range and the exaggeration;
	// collecting a quarter more than asked, and keeping it until the frame
	// asks for less than two thirds of that, means a press of a bracket
	// reuses the cache instead of walking the cells again.
	landSlack = 1.25

	// cullMarginPx widens the window the land cull tests against, in pixels.
	// The cull is exact in the camera's own arithmetic and the fill rounds
	// every vertex to a pixel, so a ring whose corner falls half a pixel
	// outside the window can still put an edge on its last column.
	cullMarginPx = 2
)

// boxedLine3 is one polyline from the shore set, a land ring or a stretch of
// coastline, together with its box on the floor of the world, which is what
// the cull reads instead of the line.
type boxedLine3 struct {
	line shore.Polyline
	low  point3
	high point3
}

// lineCache3 is every polyline of one kind around one origin out to one
// radius, with the shore set it came from. The Scene keeps two, the land
// rings the ground is filled from and the coastlines drawn over it, and the
// two are invalidated by the same rules on their own.
//
// The lines are the set's own slices and are not copied: a Set is immutable
// once loaded, so holding on to its polylines costs the cache a header per
// line and nothing per point.
type lineCache3 struct {
	set      *shore.Set
	origin   geo
	radiusNm float64
	lines    []boxedLine3
}

// shoreKind is which of the shore set's two halves a cache holds.
type shoreKind int

const (
	// coastLines is the outlines, from Within.
	coastLines shoreKind = iota

	// landRings is the closed land rings, from LandWithin.
	landRings
)

// cacheLines3 makes sure cache holds every polyline of kind within needNm of
// the view's origin, walking the shore set again only when it does not.
//
// It walks again when the set or the origin has changed, when the frame needs
// further than the cache reaches, or when it needs so much less that the cache
// is mostly lines nobody will look at. The bare view moves its origin on the
// recentring cadence, so that is the rate it walks at; the full view never
// moves its origin and walks when the range does.
//
// The walk itself was never the cost, which is microseconds even nine hundred
// miles out. What the cache buys is the box beside each line, which is what
// lets the frame throw a line away without converting a point of it.
func (s *Scene) cacheLines3(cache *lineCache3, kind shoreKind, view scene3, needNm float64) {
	if cache.set == s.shoreSet && cache.origin == view.origin &&
		needNm <= cache.radiusNm && needNm*landSlack*landSlack >= cache.radiusNm {
		return
	}

	s.walkLines3(cache, kind, view, needNm)
}

// walkLines3 refills cache from the shore set, with landSlack of headroom
// over needNm.
//
// The two queries are called by name rather than handed in as a function
// value. Through a value the compiler cannot see that the visitor does not
// outlive the call, and moves it and the view it closes over to the heap.
func (s *Scene) walkLines3(cache *lineCache3, kind shoreKind, view scene3, needNm float64) {
	cache.set, cache.origin, cache.radiusNm = s.shoreSet, view.origin, needNm*landSlack
	cache.lines = cache.lines[:0]

	latSpan := cache.radiusNm / nmPerDegree
	lonSpan := latSpan / max(view.cosLat0, minCosLat)

	south, north := view.origin.lat-latSpan, view.origin.lat+latSpan
	west, east := view.origin.lon-lonSpan, view.origin.lon+lonSpan
	visit := func(line shore.Polyline) { cache.lines = append(cache.lines, view.boxed3(line)) }

	if kind == landRings {
		s.shoreSet.LandWithin(south, north, west, east, visit)

		return
	}

	s.shoreSet.Within(south, north, west, east, visit)
}

// boxed3 boxes one polyline on the floor of the world.
//
// The box is taken in degrees and converted at the two corners. ground is the
// local equirectangular projection, which is linear and increasing in both
// coordinates, so the corners of the box in degrees land on the corners of the
// box on the floor and nothing has to be projected per point.
func (v scene3) boxed3(line shore.Polyline) boxedLine3 {
	south, north := math.Inf(1), math.Inf(-1)
	west, east := math.Inf(1), math.Inf(-1)

	for _, point := range line {
		south, north = min(south, point.Lat), max(north, point.Lat)
		west, east = min(west, point.Lon), max(east, point.Lon)
	}

	return boxedLine3{line: line, low: v.ground(south, west), high: v.ground(north, east)}
}
