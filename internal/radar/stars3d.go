package radar

import (
	"image"
	"image/color"
	"math"

	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/sun"
)

// The star field and how it is drawn.
const (
	// starCount is how many stars the whole circle of sky holds. The camera's
	// lens takes in about 53 degrees of it, and at a tilt of ten
	// degrees under six of those are sky, so this comes to about a hundred and
	// fifty on screen at once. The first cut had seventy, and once the
	// faint ones had gone on a dim panel, a count by eye came to a handful.
	starCount = 1800

	// starTopDeg is how high the field reaches. Twice the sky's own depth
	// covers what a camera at the bottom of its tilt shows in a box as wide
	// as a 4K canvas's; a tall box shows more sky than that, and an empty band
	// over the stars there is less wrong than a field spread thin everywhere.
	starTopDeg = 2 * skyDepthDeg

	// starHorizonDeg is the band over the horizon the stars fade in through.
	// Real stars dim towards the horizon through the extra air, and a star at
	// full strength sitting on the ground's far edge reads as a light on the
	// land.
	starHorizonDeg = 1.5

	// starSeed fixes the field. Any value would do; a fixed one is what makes
	// two frames, two runs and the tests agree about where every star is.
	starSeed = 0x75536F70655374

	// starInk is the colour a star is let off the field towards, a slightly
	// blue white, before the palette's look and share have had their say.
	starInk = 0xE8EEFF

	// starMinDepth is how far in front of the camera a star's direction has
	// to point, as the cosine of its angle off the view axis, before it is
	// projected. Anything past eighty degrees off axis is far outside a lens
	// of 53, and dividing by a depth near zero would only make a huge number
	// to throw away.
	starMinDepth = 0.17
)

// The splitmix64 constants. See Steele, Lea and Flood, "Fast Splittable
// Pseudorandom Number Generators", OOPSLA 2014; the mixer is the variant
// Java's SplittableRandom ships.
const (
	splitGamma  = 0x9E3779B97F4A7C15
	splitMulOne = 0xBF58476D1CE4E5B9
	splitMulTwo = 0x94D049BB133111EB
	splitShiftA = 30
	splitShiftB = 27
	splitShiftC = 31

	// unitBits is how many of the 64 bits make a float64 in [0, 1): its
	// mantissa, so every value is exact and evenly spaced.
	unitBits  = 53
	unitShift = 64 - unitBits
)

// The star classes' strengths and shares, brightest first.
const (
	brightStrength = 1.0
	clearStrength  = 0.8
	plainStrength  = 0.6
	faintStrength  = 0.55

	brightPerMille = 60
	clearPerMille  = 200
	plainPerMille  = 340
	faintPerMille  = 400
)

// starClass is one brightness of star: how far it is let off the sky under
// it, how many layout pixels across it is, and how many stars in a thousand
// are this bright. Most stars are faint and few are bright, which is how the
// night sky is.
type starClass struct {
	strength float64
	size     int
	perMille int
}

// starClasses are the brightnesses, brightest first. Only the brightest is
// two pixels across: a field of squares reads as snow. The faintest is still
// a luma near forty on the night field, because single pixels much dimmer
// than that vanish on a dim panel.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var starClasses = [...]starClass{
	{strength: brightStrength, size: 2, perMille: brightPerMille},
	{strength: clearStrength, size: 1, perMille: clearPerMille},
	{strength: plainStrength, size: 1, perMille: plainPerMille},
	{strength: faintStrength, size: 1, perMille: faintPerMille},
}

// star is one star's direction, kept as the sines and cosines the projection
// multiplies by so a frame needs no trigonometry per star.
type star struct {
	sinAz, cosAz   float64
	sinAlt, cosAlt float64

	// fade is how much of its class strength the star keeps for its height
	// over the horizon. See starHorizonDeg.
	fade  float64
	class uint8
}

// starField is the sky's stars, worked out once from starSeed.
//
//nolint:gochecknoglobals // scene content, read-only after init.
var starField = makeStarField(starSeed)

// splitmix is the splitmix64 generator: a few lines, no allocation, and the
// same sequence on every platform, which math/rand does not promise across Go
// releases.
type splitmix struct {
	state uint64
}

// next is the generator's next 64 bits.
func (r *splitmix) next() uint64 {
	r.state += splitGamma

	mixed := r.state
	mixed = (mixed ^ (mixed >> splitShiftA)) * splitMulOne
	mixed = (mixed ^ (mixed >> splitShiftB)) * splitMulTwo

	return mixed ^ (mixed >> splitShiftC)
}

// unit is the next value in [0, 1).
func (r *splitmix) unit() float64 {
	return float64(r.next()>>unitShift) / (1 << unitBits)
}

// makeStarField scatters starCount stars evenly in azimuth and in height up
// to starTopDeg. Even in height is even on the sphere this close to the
// horizon, where the cosine of the altitude is within two percent of one.
func makeStarField(seed uint64) [starCount]star {
	gen := splitmix{state: seed}

	var field [starCount]star
	for index := range field {
		azimuth := gen.unit() * 2 * math.Pi
		altitude := gen.unit() * starTopDeg

		entry := &field[index]
		entry.sinAz, entry.cosAz = math.Sincos(azimuth)
		entry.sinAlt, entry.cosAlt = math.Sincos(altitude * math.Pi / halfCircle)
		entry.fade = min(altitude/starHorizonDeg, 1)
		entry.class = starClassAt(gen.unit())
	}

	return field
}

// starClassAt picks a class for a draw in [0, 1) by the classes' shares.
func starClassAt(draw float64) uint8 {
	const perMille = 1000.0

	total := 0
	for index, class := range starClasses {
		total += class.perMille
		if draw*perMille < float64(total) {
			return uint8(index) //nolint:gosec // four classes.
		}
	}

	return uint8(len(starClasses) - 1)
}

// starsVisibility is how much of their strength the stars have for a sun
// elevation: all of it from the end of civil twilight down, none from the
// horizon up, and a straight slide between. The sky gradient goes from deep
// blue to gold over the same six degrees, so the stars thin out as the
// horizon warms rather than switching off at a line.
func starsVisibility(elevation float64) float64 {
	if !(elevation < sun.Sunset) {
		return 0
	}

	return min((sun.Sunset-elevation)/(sun.Sunset-sun.CivilLimit), 1)
}

// starTarget is the brightest a star may be in a palette: the star's colour in
// the palette's look, let off the field by the same share the sky is. A star
// can then be no brighter than a sky of that colour would be, which is what
// keeps the dark palettes under the ceiling nightSkyShare sets.
func starTarget(pal theme.Palette) color.RGBA {
	return skyInk(pal, skyTone{rgb: starInk, lit: true})
}

// starView is what one frame projects the field through: the camera's
// heading and tilt as sines and cosines, the lens and its centre, and the sky
// rows the stars are lightened against.
type starView struct {
	sinAz, cosAz float64
	sinEl, cosEl float64

	focal   float64
	centerX float64
	centerY float64
	unit    int

	rows   []color.RGBA
	top    int
	target color.RGBA
	alpha  float64
}

// drawStars puts the star field over the sky paintSky has just laid down.
//
// It draws only at night with the stars on and a sun to say it is night, and
// only where there is sky: a row under the horizon, or a sky with no rows,
// has nowhere for a star to be.
//
// The projection is newCamera3's for a point at infinity, so the stars turn
// with the ground at exactly the ground's rate as the camera orbits, and the
// horizon they sit on is the row horizonRow works out. Nothing here allocates.
func (s *Scene) drawStars(window *canvas.Canvas, box image.Rectangle, unit int) {
	alpha := starsVisibility(s.sky.elevation)
	if !s.stars || !s.sky.known || alpha <= 0 {
		return
	}

	rows := s.skyRows(box, window.Bounds())
	if len(rows) == 0 {
		return
	}

	sinAz, cosAz := math.Sincos(s.cameraAzimuth(s.elapsed) * math.Pi / halfCircle)
	sinEl, cosEl := math.Sincos(s.elevation * math.Pi / halfCircle)

	view := starView{
		sinAz: sinAz, cosAz: cosAz, sinEl: sinEl, cosEl: cosEl,
		focal:   float64(box.Dx()),
		centerX: float64(box.Min.X + box.Dx()/2),
		centerY: float64(box.Min.Y + box.Dy()/2),
		unit:    max(unit, 1),
		rows:    rows,
		top:     window.Bounds().Min.Y,
		target:  starTarget(s.pal),
		alpha:   alpha,
	}

	for index := range starField {
		view.plot(window, &starField[index])
	}
}

// plot projects one star and paints it, or does nothing when it is behind
// the camera or off the side of the lens.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (v *starView) plot(dst *canvas.Canvas, entry *star) {
	x, y, seen := v.project(entry)
	if !seen {
		return
	}

	class := starClasses[entry.class]
	size := class.size * v.unit
	x, y = x-size/2, y-size/2

	v.paint(dst, image.Rect(x, y, x+size, y+size), class.strength*entry.fade*v.alpha)
}

// project is the pixel a star lands on, and false for one too far off the
// view axis to be anywhere near the picture.
//
// Its offset from the camera's heading comes from the angle-difference
// identities on the sines and cosines both sides already hold, so the per-star
// cost is a dozen multiplications.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (v *starView) project(entry *star) (int, int, bool) {
	cosOff := entry.cosAz*v.cosAz + entry.sinAz*v.sinAz
	sinOff := entry.sinAz*v.cosAz - entry.cosAz*v.sinAz

	depth := entry.cosAlt*v.cosEl*cosOff - entry.sinAlt*v.sinEl
	if depth < starMinDepth {
		return 0, 0, false
	}

	across := entry.cosAlt * sinOff / depth
	up := (entry.cosAlt*v.sinEl*cosOff + entry.sinAlt*v.cosEl) / depth

	return int(math.Round(v.centerX + v.focal*across)), int(math.Round(v.centerY - v.focal*up)), true
}

// paint lightens a square of sky towards the star's target by along, a row at
// a time against that row's own sky colour. A pixel is only ever made
// lighter, so a dim star over a dusk horizon that is already brighter than it
// leaves the horizon alone rather than punching a dark hole in it.
func (v *starView) paint(dst *canvas.Canvas, square image.Rectangle, along float64) {
	for y := max(square.Min.Y, v.top); y < min(square.Max.Y, v.top+len(v.rows)); y++ {
		col := lighten(v.rows[y-v.top], v.target, along)
		for x := square.Min.X; x < square.Max.X; x++ {
			dst.Set(x, y, col)
		}
	}
}

// lighten is from blended towards to by along, but never darker than from in
// any channel.
func lighten(from, to color.RGBA, along float64) color.RGBA {
	return color.RGBA{
		R: max(from.R, mix(from.R, to.R, along)),
		G: max(from.G, mix(from.G, to.G, along)),
		B: max(from.B, mix(from.B, to.B, along)),
		A: opaque,
	}
}
