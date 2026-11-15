package radar

import (
	"image"
	"image/color"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/sprite"
)

// The exterior lights' timing, brightness and size.
const (
	// strobePeriod is how often the strobes fire: a double flash every 3 s,
	// twenty a minute. That is below the forty to a hundred the certification
	// rules allow (FAA 14 CFR 25.1401), and on purpose. On a screen the real
	// cadence pulls the eye away from the traffic, and half of the slowest
	// legal rate still looks like a strobe without the distraction.
	strobePeriod = 3000 * time.Millisecond

	// strobeGap is how far the second flash of the pair trails the first.
	// It is longer than a frame at 8 fps (125 ms), so on the uConsole the two
	// flashes land on two frames rather than merging into one.
	strobeGap = 200 * time.Millisecond

	// strobeFlash is how long one strobe flash lasts: three frames at
	// 30 fps, just under 100 ms. flashLit keeps a flash lit for the
	// frame it started in and then for every frame a whole further frame
	// still fits inside, which for exactly three frames' length is two lit
	// frames at any phase. A round 100 ms lights a third frame when the flash
	// starts exactly on one, and 80 ms lights two only when it starts early
	// in a frame. At 8 fps it is one frame either way.
	strobeFlash = 3 * refFrame

	// refFrame is one frame at 30 fps, the reference rate strobeFlash is
	// sized against.
	refFrame = time.Second / 30

	// beaconPeriod is the red anti-collision beacon's rhythm, 46 flashes a
	// minute. It sits inside the certified forty to a hundred and off the
	// strobes' period, so the beacon and the strobes of one aircraft drift
	// through each other instead of firing in a fixed order.
	beaconPeriod = 1300 * time.Millisecond

	// beaconFlash is how long the beacon stays lit. A beacon pulse is longer
	// than a strobe's, and at 30 fps this one lights three or four frames
	// where a strobe lights two.
	beaconFlash = 150 * time.Millisecond

	// maxFlashStep is the longest gap between two frames that the lights
	// treat as one interval. --fps goes down to one, so a second is the
	// slowest real cadence. A longer gap is a scene that was off screen, and
	// treating that as one interval would fire every strobe in the fleet on
	// the frame it comes back.
	maxFlashStep = time.Second

	// navDayAlpha is how much of their colour the steady lights keep by day.
	// Navigation lights are hard to see in sunlight and the strobes are not,
	// which is the reason strobes exist; the day palette shows that rather
	// than painting both at night strength.
	navDayAlpha = 0.5

	// tailAlpha dims the steady white tail light against the strobe, which
	// is the same colour. Without it a strobe firing at the tail of a flat
	// sprite would change nothing on screen.
	tailAlpha = 0.8

	// The single-hue looks have no red or green to spend, so the three steady
	// lights are the Data hue at three strengths: port the dimmest, the tail
	// the brightest short of a flash. Far enough apart to tell the two wings
	// apart at a pixel each, close enough to the field to stay quieter than
	// a strobe.
	monoPortAlpha      = 0.4
	monoStarboardAlpha = 0.6
	monoTailAlpha      = 0.8

	// navLightSpan and flashLightSpan size the 3D lights as a share of the
	// model's span on screen. A sixteen-pixel model on the panel gets a
	// one-pixel steady light and a five-pixel flash; a
	// thirty-two-pixel model gets a five-pixel light and a thirteen-pixel
	// flash. Anything larger would hide the wingtip it sits on.
	navLightSpan   = 1.0 / 24
	flashLightSpan = 1.0 / 12

	// beaconLightSpan is one step up from a strobe, a radius of two pixels on
	// the panel's model. At a radius of one the red sat inside a fuselage
	// filled in the altitude or airline colour and was lost against orange
	// and amber; the wingtips have the field around them, the beacon does not.
	beaconLightSpan = 1.0 / 8

	// beaconCoreAlpha is the strength of the beacon's bright centre, Ink
	// pulled a little towards the field. The core is what makes the beacon
	// read on every fill colour, red ones included; at full Ink it looked
	// like a strobe that had wandered onto the fuselage.
	beaconCoreAlpha = 0.8

	// monoBeaconAlpha is the beacon's halo in the single-hue looks, where
	// the core is the full Data hue. A halo at full strength would leave the
	// core nothing to stand out against.
	monoBeaconAlpha = 0.5
)

// FNV-1a's 64-bit offset basis and prime, from the reference at
// http://www.isthe.com/chongo/tech/comp/fnv/.
const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
)

// beaconShift is how far the hash is shifted before the beacon's phase is
// taken from it, so the beacon and the strobes of one aircraft come from
// different bits and do not fire in step.
const beaconShift = 32

// lightHash is FNV-1a over an ICAO address's bytes.
//
// A Snapshot carries no integer id, and the address is the one thing that
// stays the same for an aircraft from frame to frame, so it is what the phase
// hangs off: an aircraft keeps its own rhythm for as long as it is on screen,
// and two aircraft rarely share one. Indexing the string rather than
// converting it to a byte slice is what keeps this off the heap.
func lightHash(icao string) uint64 {
	hash := uint64(fnvOffset)

	for index := range len(icao) {
		hash ^= uint64(icao[index])
		hash *= fnvPrime
	}

	return hash
}

// flashLit reports whether a train of flashes, each length long, starting at
// start and repeating every period, lights the frame drawn at now when the
// one before it was drawn step earlier.
//
// It does not sample the instant. A 60 ms flash falls entirely between two
// frames at 8 fps, and an instant sampled on either side of it misses it, so
// on the uConsole most strobes would never be seen. The rule is instead that
// a flash lights the frame it started in, whatever the frame rate, and stays
// lit only while a whole further frame still fits inside it. At 8 fps that
// is one frame for a strobe and one for the beacon. At 30 fps it is two
// frames for a strobe, whose hundred milliseconds are three frames, and three
// or four for the beacon.
//
// A step of zero is a frame drawn at the same reading as the last one, a
// still or the first frame. Then only the second test can pass and the rule
// comes down to sampling the instant, which is all a single frame can do.
func flashLit(now, step, start, period, length time.Duration) bool {
	since := (now - start) % period
	if since < 0 {
		since += period
	}

	return since < step || since+step < length
}

// flashes says which of an aircraft's flashing lights are lit this frame.
type flashes struct {
	strobe bool
	beacon bool
}

// flashesFor works out an aircraft's strobes and beacon for the frame being
// drawn, from its own phase and the interval since the last frame.
func (s *Scene) flashesFor(icao string) flashes {
	step := s.elapsed - s.flashFrom
	if step < 0 || step > maxFlashStep {
		step = 0
	}

	hash := lightHash(icao)

	//nolint:gosec // both periods are positive and far below the int64 range, so the remainders fit.
	strobeAt := time.Duration(hash % uint64(strobePeriod))
	//nolint:gosec // as above.
	beaconAt := time.Duration((hash >> beaconShift) % uint64(beaconPeriod))

	return flashes{
		strobe: flashLit(s.elapsed, step, strobeAt, strobePeriod, strobeFlash) ||
			flashLit(s.elapsed, step, strobeAt+strobeGap, strobePeriod, strobeFlash),
		beacon: flashLit(s.elapsed, step, beaconAt, beaconPeriod, beaconFlash),
	}
}

// lightInks are the six colours the lights are drawn in on this palette.
type lightInks struct {
	port       color.RGBA
	starboard  color.RGBA
	tail       color.RGBA
	strobe     color.RGBA
	beacon     color.RGBA
	beaconCore color.RGBA
}

// lightInks picks the lights' colours off the palette, adding none of its own.
//
// Glass takes the meaning the palette already gives its colours: port is
// Warn's red, starboard OK's green. White is Ink, which is the brightest
// neutral at night; by day it is the dark reading colour, because a white
// light on a paper field would be the one light nobody could see. Phosphor
// and mono are one hue by design, so every light there is Data at a strength
// of its own, the way the sky takes their hue in lookTint.
//
// The beacon is two colours, a red disc around a near-white core, because it
// sits on the fuselage rather than on the field. The model is filled in its
// altitude or airline colour, and no single red stands out against all of
// those; a bright centre in a red ring does.
//
// By day the steady lights are mixed half into the field and the flashes are
// not, which is what sunlight does to a real aircraft.
func (s *Scene) lightInks() lightInks {
	inks := lightInks{
		port:       s.pal.Warn,
		starboard:  s.pal.OK,
		tail:       s.fade(s.pal.Ink, tailAlpha),
		strobe:     s.pal.Ink,
		beacon:     s.pal.Warn,
		beaconCore: s.fade(s.pal.Ink, beaconCoreAlpha),
	}

	if s.look == theme.LookPhosphor || s.look == theme.LookMono {
		inks = lightInks{
			port:       s.fade(s.pal.Data, monoPortAlpha),
			starboard:  s.fade(s.pal.Data, monoStarboardAlpha),
			tail:       s.fade(s.pal.Data, monoTailAlpha),
			strobe:     s.pal.Data,
			beacon:     s.fade(s.pal.Data, monoBeaconAlpha),
			beaconCore: s.pal.Data,
		}
	}

	if s.light {
		inks.port = s.fade(inks.port, navDayAlpha)
		inks.starboard = s.fade(inks.starboard, navDayAlpha)
		inks.tail = s.fade(inks.tail, navDayAlpha)
	}

	return inks
}

// drawLights3 puts the lights on the model drawLights3's caller has just
// projected into s.posed: the steady ones at the wingtips and the tail, and
// over them whichever flashes are lit this frame.
//
// It draws on whatever posed holds, so it has to come straight after the
// model it belongs to, before the next aircraft is projected. A light whose
// vertex the camera refused is skipped like any face touching it.
func (s *Scene) drawLights3(dst *canvas.Canvas, cam camera3, plane airplane.Snapshot) {
	if !s.lights {
		return
	}

	inks := s.lightInks()
	steady, flash := int(cam.modelSpan*navLightSpan), int(cam.modelSpan*flashLightSpan)
	beacon := int(cam.modelSpan * beaconLightSpan)

	s.lamp3(dst, vertWingTipLeft, steady, inks.port)
	s.lamp3(dst, vertWingTipRight, steady, inks.starboard)
	s.lamp3(dst, vertTail, steady, inks.tail)

	lit := s.flashesFor(plane.ICAO)

	if lit.strobe {
		s.lamp3(dst, vertWingTipLeft, flash, inks.strobe)
		s.lamp3(dst, vertWingTipRight, flash, inks.strobe)
		s.lamp3(dst, vertTail, flash, inks.strobe)
	}

	if lit.beacon {
		s.lamp3(dst, vertBeacon, beacon, inks.beacon)
		s.lamp3(dst, vertBeacon, beacon/2, inks.beaconCore)
	}
}

// lamp3 fills one light as a disc on a projected vertex.
func (s *Scene) lamp3(dst *canvas.Canvas, vertex, radius int, col color.RGBA) {
	if !s.posed.seen[vertex] {
		return
	}

	dst.FillCircle(s.posed.atX[vertex], s.posed.atY[vertex], radius, col)
}

// drawLights puts the lights on the flat scope's silhouette centred at x, y:
// one pixel of the sprite at each wingtip and at the tail, recoloured, white
// at all three while a strobe is lit, and the red beacon over the middle of
// the fuselage while it is lit.
//
// The steady lights replace pixels of the shape rather than adding any, so
// at scale one on the panel the silhouette keeps its outline and only its
// tips change colour. A strobe takes over the wingtip pixel because a real
// strobe sits in the same fairing as the position light, and at scale one
// there is no second pixel to put it in. The beacon is the flash that has to
// read from across the room, so it is larger than the pixel it is centred
// on. A sprite handed in through WithSprite has no light positions and draws
// none.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (s *Scene) drawLights(lay *layout, x, y int, plane airplane.Snapshot) {
	where, carried := s.icon.Lights()
	if !s.lights || !carried {
		return
	}

	inks := s.lightInks()
	lit := s.flashesFor(plane.ICAO)

	if lit.strobe {
		inks.port, inks.starboard, inks.tail = inks.strobe, inks.strobe, inks.strobe
	}

	turn := sprite.TurnTo(plane.Heading)
	scale := lay.unit()

	s.lamp(lay.dst, x, y, s.icon.Landing(where.Port, turn), scale, inks.port)
	s.lamp(lay.dst, x, y, s.icon.Landing(where.Starboard, turn), scale, inks.starboard)
	s.lamp(lay.dst, x, y, s.icon.Landing(where.Tail, turn), scale, inks.tail)

	if lit.beacon {
		s.beacon(lay.dst, x, y, s.icon.Landing(where.Beacon, turn), scale, inks)
	}
}

// beacon fills the flat beacon over the block the sprite stamped at offset
// from x, y: a plus of five pixels at scale one, where a disc of radius two
// would swallow the fuselage and half of each wing root, and a disc of twice
// the scale above it. The core is half the radius, a single pixel at scale
// one.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (*Scene) beacon(dst *canvas.Canvas, x, y int, offset image.Point, scale int, inks lightInks) {
	// The block's own centre, not its top-left corner, so the disc sits on
	// the fuselage at every scale rather than drifting down and right.
	centreX, centreY := x+offset.X*scale+scale/2, y+offset.Y*scale+scale/2

	radius := 1
	if scale > 1 {
		radius = 2 * scale
	}

	dst.FillCircle(centreX, centreY, radius, inks.beacon)
	dst.FillCircle(centreX, centreY, radius/2, inks.beaconCore)
}

// lamp fills the block the sprite stamped at offset from x, y, at the scale it
// was stamped at.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func (*Scene) lamp(dst *canvas.Canvas, x, y int, offset image.Point, scale int, col color.RGBA) {
	left, top := x+offset.X*scale, y+offset.Y*scale
	dst.FillRect(image.Rect(left, top, left+scale, top+scale), col)
}
