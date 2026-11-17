package radar

import (
	"hash/fnv"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/hyperized/uAirwaves/pkg/airplane"
	"github.com/hyperized/uScope/internal/source"
	"github.com/hyperized/uScope/internal/theme"
	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/sprite"
	"github.com/hyperized/uScope/pkg/sun"
)

// The two addresses the phase cases compare, and the frame intervals the
// timing cases run at: the uConsole's worst and 30 fps.
const (
	lightICAO      = "484AC1"
	otherLightICAO = "3C6745"

	slowFrame = time.Second / 8
	fastFrame = time.Second / 30
)

// lightScene is a scene with the lights on over pal, the way New leaves it,
// with nothing else set.
func lightScene(pal theme.Palette) *Scene {
	scene := &Scene{lights: LightsOn}
	scene.SetPalette(pal)

	return scene
}

// momentFor finds a render clock on the 10 ms grid at which lightICAO's flashes
// are exactly what want says, sampled as a still (no interval since the last
// frame), so a case can draw a frame whose flashes it knows in advance.
func momentFor(tb testing.TB, want flashes) time.Duration {
	tb.Helper()

	const grid = 10 * time.Millisecond

	scene := &Scene{}

	for at := time.Duration(0); at < 10*strobePeriod; at += grid {
		scene.elapsed, scene.flashFrom = at, at
		if scene.flashesFor(lightICAO) == want {
			return at
		}
	}

	tb.Fatalf("no moment on the grid gives %s flashes %+v", lightICAO, want)

	return 0
}

// TestLightHashIsFNV1a checks the hash against the standard library's FNV-1a,
// which is the reference the constants were copied from.
func TestLightHashIsFNV1a(t *testing.T) {
	t.Parallel()

	for _, icao := range []string{"", lightICAO, otherLightICAO, "~000001"} {
		t.Run(icao, func(t *testing.T) {
			t.Parallel()

			want := fnv.New64a()
			_, _ = want.Write([]byte(icao))

			if got := lightHash(icao); got != want.Sum64() {
				t.Errorf("lightHash(%q) = %#x, want %#x", icao, got, want.Sum64())
			}
		})
	}
}

// TestLightPhaseDiffersBetweenAircraft checks that two aircraft do not blink
// in unison: on a clock sampled every 10 ms over two strobe periods, the
// frames on which each one's strobe and beacon are lit are not the same set.
func TestLightPhaseDiffersBetweenAircraft(t *testing.T) {
	t.Parallel()

	const grid = 10 * time.Millisecond

	scene := &Scene{}
	strobeApart, beaconApart := false, false

	for at := time.Duration(0); at < 2*beaconPeriod; at += grid {
		scene.elapsed, scene.flashFrom = at, at-grid

		one, other := scene.flashesFor(lightICAO), scene.flashesFor(otherLightICAO)
		strobeApart = strobeApart || one.strobe != other.strobe
		beaconApart = beaconApart || one.beacon != other.beacon
	}

	if !strobeApart || !beaconApart {
		t.Errorf("%s and %s flash together: strobes apart %v, beacons apart %v",
			lightICAO, otherLightICAO, strobeApart, beaconApart)
	}
}

// TestFlashLitCatchesAFlashBetweenFrames checks the case the rule exists for:
// at 8 fps a strobe that starts and ends between two frames still lights the
// second of them, and only that one.
func TestFlashLitCatchesAFlashBetweenFrames(t *testing.T) {
	t.Parallel()

	// The flash runs from 20 ms to about 120 ms, wholly between the frames
	// at 0 and at 125 ms.
	const start = 20 * time.Millisecond

	for _, testCase := range []struct {
		name string
		now  time.Duration
		want bool
	}{
		{name: "the frame before it", now: 0},
		{name: "the frame after it", now: slowFrame, want: true},
		{name: "the frame after that", now: 2 * slowFrame},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := flashLit(testCase.now, slowFrame, start, strobePeriod, strobeFlash); got != testCase.want {
				t.Errorf("flashLit at %v = %v, want %v", testCase.now, got, testCase.want)
			}
		})
	}

	// An instant sampled at either frame misses it, which is the bug the
	// interval rule fixes.
	for _, now := range []time.Duration{0, slowFrame} {
		if flashLit(now, 0, start, strobePeriod, strobeFlash) {
			t.Errorf("a still at %v caught a flash that ended before it, want the instant rule to miss it", now)
		}
	}
}

// TestFlashLitRunLengths runs a train of flashes through a frame clock at
// several rates and phases, and checks how many frames in a row each flash
// lights: every flash is seen, and none lasts longer on screen than it does
// in the air beyond the one frame a flash needs to be seen at all.
func TestFlashLitRunLengths(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		frame   time.Duration
		period  time.Duration
		length  time.Duration
		wantMin int
		wantMax int
	}{
		{name: "strobe at 8 fps", frame: slowFrame, period: strobePeriod, length: strobeFlash, wantMin: 1, wantMax: 1},
		{name: "strobe at 30 fps", frame: fastFrame, period: strobePeriod, length: strobeFlash, wantMin: 2, wantMax: 2},
		{name: "beacon at 8 fps", frame: slowFrame, period: beaconPeriod, length: beaconFlash, wantMin: 1, wantMax: 1},
		{name: "beacon at 30 fps", frame: fastFrame, period: beaconPeriod, length: beaconFlash, wantMin: 3, wantMax: 4},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const (
				periods = 5
				phases  = 7
			)

			for phase := range phases {
				start := time.Duration(phase) * testCase.frame / phases
				span := testCase.period * periods
				runs := flashRuns(testCase.frame, span, start, testCase.period, testCase.length)

				if want := flashesIn(span, testCase.frame, start, testCase.period); len(runs) != want {
					t.Fatalf("phase %v: %d flashes seen, want every one of the %d", start, len(runs), want)
				}

				for _, run := range runs {
					if run < testCase.wantMin || run > testCase.wantMax {
						t.Errorf("phase %v: a flash lit %d frames in a row, want %d to %d",
							start, run, testCase.wantMin, testCase.wantMax)
					}
				}
			}
		})
	}
}

// flashRuns draws frames every frame apart from zero up to span, the first
// one standing for the interval just before zero, and returns how many
// frames in a row each flash it saw stayed lit.
func flashRuns(frame, span, start, period, length time.Duration) []int {
	var runs []int

	lit := 0

	for now := time.Duration(0); now <= span; now += frame {
		if flashLit(now, frame, start, period, length) {
			lit++

			continue
		}

		if lit > 0 {
			runs = append(runs, lit)
			lit = 0
		}
	}

	if lit > 0 {
		runs = append(runs, lit)
	}

	return runs
}

// flashesIn counts the flashes from start that begin no later than the last
// frame flashRuns draws, which are the ones it can see.
func flashesIn(span, frame, start, period time.Duration) int {
	frames := int64(span / frame)
	last := time.Duration(frames) * frame
	count := 0

	for at := start; at <= last; at += period {
		count++
	}

	return count
}

// TestFlashLitWrapsBeforeTheStart checks the remainder is folded onto the
// period when the clock is earlier than the train's own start, the way it is
// for an aircraft whose phase lands after the first frame.
func TestFlashLitWrapsBeforeTheStart(t *testing.T) {
	t.Parallel()

	// The train starts 100 ms before the end of its first period, so it also
	// started at -100 ms, and a still at -80 ms is 20 ms into that flash.
	const start = strobePeriod - 100*time.Millisecond

	if !flashLit(-80*time.Millisecond, 0, start, strobePeriod, strobeFlash) {
		t.Error("a still 20 ms into the flash before the start was not lit")
	}
}

// TestFlashesForIgnoresAGapThatIsNotAFrame checks that a frame drawn long
// after the last one, or before it, is sampled as a still rather than as one
// enormous interval that every flash in the fleet started in.
func TestFlashesForIgnoresAGapThatIsNotAFrame(t *testing.T) {
	t.Parallel()

	dark := momentFor(t, flashes{})

	for _, testCase := range []struct {
		name string
		from time.Duration
	}{
		{name: "a scene that was off screen for a minute", from: dark - time.Minute},
		{name: "a clock that went backwards", from: dark + time.Second},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{elapsed: dark, flashFrom: testCase.from}

			if got := scene.flashesFor(lightICAO); got != (flashes{}) {
				t.Errorf("flashesFor = %+v, want nothing lit at a dark moment", got)
			}
		})
	}
}

// TestLightInks checks the colours on each kind of palette: the glass night
// set is the palette's own red and green, the day set keeps less of its
// colour than the night one and the flashes keep all of theirs, and the two
// single-hue looks stay on their Data hue.
func TestLightInks(t *testing.T) {
	t.Parallel()

	t.Run("glass night is the palette's own red and green", func(t *testing.T) {
		t.Parallel()

		inks := lightScene(theme.Night).lightInks()

		if inks.port != theme.Night.Warn || inks.starboard != theme.Night.OK || inks.beacon != theme.Night.Warn {
			t.Errorf("port %v, starboard %v, beacon %v, want Warn, OK and Warn", inks.port, inks.starboard, inks.beacon)
		}

		if inks.strobe != theme.Night.Ink {
			t.Errorf("strobe %v, want Ink %v", inks.strobe, theme.Night.Ink)
		}

		if want := lightScene(theme.Night).fade(theme.Night.Ink, beaconCoreAlpha); inks.beaconCore != want {
			t.Errorf("beacon core %v, want Ink faded to %v, %v", inks.beaconCore, beaconCoreAlpha, want)
		}
	})

	t.Run("by day the steady lights fade and the flashes do not", checkDayFade)

	for _, pal := range []theme.Palette{theme.MonoNight, theme.MonoDay, theme.PhosphorNight, theme.PhosphorDay} {
		t.Run(string(pal.Look)+" stays on the Data hue", func(t *testing.T) {
			t.Parallel()
			checkSingleHue(t, pal)
		})
	}
}

// checkDayFade is TestLightInks' day case: the same day palette with and
// without the day rule, so the only difference between the two is the fade.
func checkDayFade(t *testing.T) {
	t.Parallel()

	night, day := lightScene(theme.Day), lightScene(theme.Day)
	night.light = false

	dim, full := day.lightInks(), night.lightInks()

	for _, pair := range []struct {
		name      string
		day, full color.RGBA
	}{
		{name: "port", day: dim.port, full: full.port},
		{name: "starboard", day: dim.starboard, full: full.starboard},
		{name: "tail", day: dim.tail, full: full.tail},
	} {
		if distance(pair.day, theme.Day.Field) >= distance(pair.full, theme.Day.Field) {
			t.Errorf("%s by day %v is no nearer the field than %v, want it faded", pair.name, pair.day, pair.full)
		}
	}

	if dim.strobe != full.strobe || dim.beacon != full.beacon || dim.beaconCore != full.beaconCore {
		t.Errorf("flashes by day %v %v, want the night strength %v %v",
			dim.strobe, dim.beacon, full.strobe, full.beacon)
	}
}

// checkSingleHue is TestLightInks' case for one single-hue palette.
func checkSingleHue(tb testing.TB, pal theme.Palette) {
	tb.Helper()

	inks := lightScene(pal).lightInks()

	for _, ink := range []color.RGBA{inks.port, inks.starboard, inks.tail, inks.strobe, inks.beacon, inks.beaconCore} {
		if !onLine(ink, pal.Field, pal.Data) {
			tb.Errorf("light %v is not between field %v and data %v", ink, pal.Field, pal.Data)
		}
	}

	if inks.port == inks.starboard {
		tb.Errorf("port and starboard are both %v, want two strengths", inks.port)
	}

	if inks.beacon == inks.beaconCore {
		tb.Errorf("the beacon and its core are both %v, want the core to stand out", inks.beacon)
	}
}

// absDiff is the absolute difference of two channel values.
func absDiff(first, second uint8) int {
	return int(math.Abs(float64(first) - float64(second)))
}

// onLine reports whether col is a mix of from and to, within a channel's
// rounding: one share of to explains all three channels at once.
func onLine(col, from, to color.RGBA) bool {
	share := -1.0

	for _, channel := range [...][3]uint8{{col.R, from.R, to.R}, {col.G, from.G, to.G}, {col.B, from.B, to.B}} {
		if channel[1] == channel[2] {
			if absDiff(channel[0], channel[1]) > 1 {
				return false
			}

			continue
		}

		along := (float64(channel[0]) - float64(channel[1])) / (float64(channel[2]) - float64(channel[1]))
		if share < 0 {
			share = along
		}

		if math.Abs(along-share)*math.Abs(float64(channel[2])-float64(channel[1])) > 1.5 {
			return false
		}
	}

	return true
}

// TestDrawLights3PutsEachLightOnItsVertex checks the 3D lights land on the
// vertices they belong to, in the colours they belong in, and that the
// flashes go over the steady lights when they are lit.
func TestDrawLights3PutsEachLightOnItsVertex(t *testing.T) {
	t.Parallel()

	cam := testCamera(0, defaultElevation)
	plane := airplane.Snapshot{ICAO: lightICAO}

	core := lightScene(theme.Night).fade(theme.Night.Ink, beaconCoreAlpha)

	for _, testCase := range []struct {
		name  string
		lit   flashes
		wants map[int]color.RGBA

		// rings are colours one radius out to the right of a vertex, which
		// is where a two-colour light shows its outer colour.
		rings map[int]color.RGBA
	}{
		{
			name: "all dark",
			wants: map[int]color.RGBA{
				vertWingTipLeft: theme.Night.Warn, vertWingTipRight: theme.Night.OK,
			},
		},
		{
			name: "strobe lit",
			lit:  flashes{strobe: true},
			wants: map[int]color.RGBA{
				vertWingTipLeft: theme.Night.Ink, vertWingTipRight: theme.Night.Ink, vertTail: theme.Night.Ink,
			},
		},
		{
			name:  "beacon lit",
			lit:   flashes{beacon: true},
			wants: map[int]color.RGBA{vertBeacon: core, vertWingTipRight: theme.Night.OK},
			rings: map[int]color.RGBA{vertBeacon: theme.Night.Warn},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := posedAt(cam, point3{north: modelNearNm}, 0, 0, 0)
			scene.lights = LightsOn
			scene.SetPalette(theme.Night)
			scene.elapsed = momentFor(t, testCase.lit)
			scene.flashFrom = scene.elapsed

			canv := lightCanvas(t)
			scene.drawLights3(canv, cam, plane)

			for vertex, want := range testCase.wants {
				got := canv.Image().RGBAAt(scene.posed.atX[vertex], scene.posed.atY[vertex])
				if got != want {
					t.Errorf("vertex %d is %v, want %v", vertex, got, want)
				}
			}

			radius := int(cam.modelSpan * beaconLightSpan)
			for vertex, want := range testCase.rings {
				got := canv.Image().RGBAAt(scene.posed.atX[vertex]+radius, scene.posed.atY[vertex])
				if got != want {
					t.Errorf("vertex %d's ring is %v, want %v", vertex, got, want)
				}
			}
		})
	}
}

// TestDrawLights3SkipsWhatIsNotThere checks the two ways a light is left
// off: the setting, and a vertex the camera refused.
func TestDrawLights3SkipsWhatIsNotThere(t *testing.T) {
	t.Parallel()

	cam := testCamera(0, defaultElevation)
	plane := airplane.Snapshot{ICAO: lightICAO}

	for _, testCase := range []struct {
		name   string
		lights LightsMode
	}{
		{name: "lights off", lights: LightsOff},
		{name: "every vertex refused", lights: LightsOn},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := posedAt(cam, point3{north: modelNearNm}, 0, 0, 0)
			scene.lights = testCase.lights
			scene.SetPalette(theme.Night)

			if testCase.lights == LightsOn {
				scene.posed.seen = [modelVertexCount]bool{}
			}

			canv := lightCanvas(t)
			scene.drawLights3(canv, cam, plane)

			if painted(canv) {
				t.Error("drawLights3 painted something, want nothing")
			}
		})
	}
}

// TestDrawLightsFlatSprite checks the flat scope's lights land on the
// silhouette's own wingtip pixels, port red on the left of a northbound
// sprite, and that a sprite with no light positions draws none.
func TestDrawLightsFlatSprite(t *testing.T) {
	t.Parallel()

	const centre = 20

	const row = "###"

	blob, err := sprite.New([]string{row, row, row})
	if err != nil {
		t.Fatalf("sprite.New: %v", err)
	}

	for _, testCase := range []struct {
		name      string
		icon      *sprite.Bitmap
		lights    LightsMode
		lit       flashes
		wantPort  color.RGBA
		wantStarb color.RGBA
	}{
		{
			name: "dark", icon: sprite.Airplane(), lights: LightsOn,
			wantPort: theme.Night.Warn, wantStarb: theme.Night.OK,
		},
		{
			name: "strobe lit", icon: sprite.Airplane(), lights: LightsOn, lit: flashes{strobe: true},
			wantPort: theme.Night.Ink, wantStarb: theme.Night.Ink,
		},
		{
			name: "beacon lit", icon: sprite.Airplane(), lights: LightsOn, lit: flashes{beacon: true},
			wantPort: theme.Night.Warn, wantStarb: theme.Night.OK,
		},
		{name: "lights off", icon: sprite.Airplane()},
		{name: "a sprite with no lights", icon: blob, lights: LightsOn},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := lightScene(theme.Night)
			scene.icon = testCase.icon
			scene.lights = testCase.lights
			scene.elapsed = momentFor(t, testCase.lit)
			scene.flashFrom = scene.elapsed

			canv := lightCanvas(t)
			lay := layout{dst: canv, scale: 1}
			scene.drawLights(&lay, centre, centre, airplane.Snapshot{ICAO: lightICAO})

			// The art's widest row is row 8, one pixel below the centre at row
			// 7, and its ends are six pixels either side of it.
			port := canv.Image().RGBAAt(centre-6, centre+1)
			starboard := canv.Image().RGBAAt(centre+6, centre+1)

			if port != testCase.wantPort || starboard != testCase.wantStarb {
				t.Errorf("port %v, starboard %v, want %v and %v",
					port, starboard, testCase.wantPort, testCase.wantStarb)
			}
		})
	}
}

// TestLampScales checks a flat light is the block the sprite stamped at the
// layout's scale, not a single pixel, and that one off the edge of the
// canvas is clipped rather than wrapped.
func TestLampScales(t *testing.T) {
	t.Parallel()

	canv := lightCanvas(t)
	(&Scene{}).lamp(canv, 10, 10, image.Pt(-6, 1), 2, theme.Night.Warn, 1)

	if got := countRGBA(canv, theme.Night.Warn); got != 0 {
		t.Errorf("a lamp off the left edge painted %d pixels, want 0", got)
	}

	(&Scene{}).lamp(canv, 20, 20, image.Pt(-6, 1), 2, theme.Night.Warn, 1)

	if got := countRGBA(canv, theme.Night.Warn); got != 4 {
		t.Errorf("a scale-two lamp painted %d pixels, want a 2x2 block", got)
	}

	if got := canv.Image().RGBAAt(20-12, 20+2); got != theme.Night.Warn {
		t.Errorf("the block's corner is %v, want it where the sprite stamped the tip", got)
	}
}

// lightCanvas is a blank canvas the size of the test camera's box.
func lightCanvas(tb testing.TB) *canvas.Canvas {
	tb.Helper()

	canv, err := canvas.New(cameraSide, cameraSide)
	if err != nil {
		tb.Fatalf("canvas.New: %v", err)
	}

	return canv
}

// painted reports whether anything on canv differs from a fresh canvas.
func painted(canv *canvas.Canvas) bool {
	for _, value := range canv.Image().Pix {
		if value != 0 {
			return true
		}
	}

	return false
}

// countRGBA counts the pixels of canv that are exactly col.
func countRGBA(canv *canvas.Canvas, col color.RGBA) int {
	count := 0
	bounds := canv.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if canv.Image().RGBAAt(x, y) == col {
				count++
			}
		}
	}

	return count
}

// TestDrawLightsFlatBeacon checks the flat beacon's shape: a five-pixel plus
// with a one-pixel core at scale one, centred on the sprite's own centre,
// and a disc of twice the scale around a core of the scale at two, centred on
// the middle of the block the centre pixel was stamped as. With the beacon
// dark nothing is drawn there at all.
func TestDrawLightsFlatBeacon(t *testing.T) {
	t.Parallel()

	const centre = 30

	for _, testCase := range []struct {
		name   string
		scale  int
		lit    bool
		middle int
		reds   int
	}{
		{name: "scale one, lit", scale: 1, lit: true, middle: centre, reds: 4},
		{name: "scale two, lit", scale: 2, lit: true, middle: centre + 1, reds: discArea(4) - discArea(2)},
		{name: "scale one, dark", scale: 1, middle: centre},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := lightScene(theme.Night)
			scene.icon = sprite.Airplane()
			scene.elapsed = momentFor(t, flashes{beacon: testCase.lit})
			scene.flashFrom = scene.elapsed

			canv := lightCanvas(t)
			lay := layout{dst: canv, scale: testCase.scale}
			scene.drawLights(&lay, centre, centre, airplane.Snapshot{ICAO: lightICAO})

			core := scene.lightInks().beaconCore
			want := core

			if !testCase.lit {
				want = color.RGBA{}
			}

			if got := canv.Image().RGBAAt(testCase.middle, testCase.middle); got != want {
				t.Errorf("the beacon's middle is %v, want %v", got, want)
			}

			// The port tip is red too, one block of it, so the count is the
			// beacon's ring plus that block.
			tip := testCase.scale * testCase.scale
			if got := countRGBA(canv, theme.Night.Warn) - tip; got != testCase.reds {
				t.Errorf("%d red pixels besides the port tip, want %d", got, testCase.reds)
			}
		})
	}
}

// discArea is how many pixels canvas.FillCircle fills at radius.
func discArea(radius int) int {
	area := 0

	for dy := -radius; dy <= radius; dy++ {
		area += 2*int(math.Sqrt(float64(radius*radius-dy*dy))) + 1
	}

	return area
}

// TestStrobePattern checks the timing a real installation has, sampled as
// stills on a 1 ms grid over three strobe periods: the strobes fire twenty
// times a minute as two flashes strobeGap apart, the second flash is over
// before the next pair, and the beacon fires once, on a phase of its own that
// the strobes do not share.
func TestStrobePattern(t *testing.T) {
	t.Parallel()

	const grid = time.Millisecond

	if perMinute := time.Minute / strobePeriod; perMinute != 20 {
		t.Errorf("strobes fire %d times a minute, want 20", perMinute)
	}

	scene := &Scene{}

	var strobeStarts, strobeEnds, beaconStarts []time.Duration

	was := flashes{}

	for clock := time.Duration(0); clock < 3*strobePeriod; clock += grid {
		scene.elapsed, scene.flashFrom = clock, clock
		lit := scene.flashesFor(lightICAO)

		if lit.strobe && !was.strobe {
			strobeStarts = append(strobeStarts, clock)
		}

		if !lit.strobe && was.strobe {
			strobeEnds = append(strobeEnds, clock)
		}

		if lit.beacon && !was.beacon {
			beaconStarts = append(beaconStarts, clock)
		}

		was = lit
	}

	checkStrobeStarts(t, strobeStarts, strobeEnds)

	if len(beaconStarts) < 2 || beaconStarts[1]-beaconStarts[0] != beaconPeriod {
		t.Errorf("beacon flashes start at %v, want one every %v", beaconStarts, beaconPeriod)
	}
}

// checkStrobeStarts is TestStrobePattern's check on the strobes: the starts
// come in pairs strobeGap apart, one pair per period, and the second flash of
// each pair has gone dark before the next pair starts.
func checkStrobeStarts(tb testing.TB, starts, ends []time.Duration) {
	tb.Helper()

	// A pair can straddle the start of the sampled span, so line up on the
	// first start whose partner is strobeGap later.
	for len(starts) > 1 && starts[1]-starts[0] != strobeGap {
		starts = starts[1:]
	}

	if len(starts) < 4 {
		tb.Fatalf("strobe flashes start at %v, want at least two pairs", starts)
	}

	for index := 0; index+3 < len(starts); index += 2 {
		if gap := starts[index+1] - starts[index]; gap != strobeGap {
			tb.Errorf("pair %d is %v apart, want %v", index/2, gap, strobeGap)
		}

		if period := starts[index+2] - starts[index]; period != strobePeriod {
			tb.Errorf("pair %d repeats after %v, want %v", index/2, period, strobePeriod)
		}

		if end := endAfter(ends, starts[index+1]); end == 0 || end >= starts[index+2] {
			tb.Errorf("pair %d's second flash ends at %v, want before the next pair at %v",
				index/2, end, starts[index+2])
		}
	}
}

// endAfter returns the first end later than start, or zero when the sampled
// span stopped with the flash still lit.
func endAfter(ends []time.Duration, start time.Duration) time.Duration {
	for _, end := range ends {
		if end > start {
			return end
		}
	}

	return 0
}

// distance is how far apart two colours are, summed over the channels.
func distance(first, second color.RGBA) int {
	diff := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }

	return diff(first.R, second.R) + diff(first.G, second.G) + diff(first.B, second.B)
}

// unspelledLights is a LightsMode outside the allow list, which only a caller
// building Settings by hand can hand the scene. It is untyped so the
// exhaustive linter does not count it as a fourth mode.
const unspelledLights = "dim"

// TestLightsModeNext checks s walks on, off, auto and back to on, and that a
// mode nobody spelled starts from off the way the scene reads it.
func TestLightsModeNext(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		from, want LightsMode
	}{
		{from: LightsOn, want: LightsOff},
		{from: LightsOff, want: LightsAuto},
		{from: LightsAuto, want: LightsOn},
		{from: "", want: LightsAuto},
		{from: unspelledLights, want: LightsAuto},
	} {
		t.Run(string(testCase.from), func(t *testing.T) {
			t.Parallel()

			if got := testCase.from.next(); got != testCase.want {
				t.Errorf("%q.next() = %q, want %q", testCase.from, got, testCase.want)
			}
		})
	}
}

// TestApplyLightsReadsUnknownAsOff checks Apply keeps the scene on one of the
// three spellings, so the cap and the cycle never meet a fourth.
func TestApplyLightsReadsUnknownAsOff(t *testing.T) {
	t.Parallel()

	for _, mode := range []LightsMode{"", unspelledLights, LightsOff, LightsOn, LightsAuto} {
		scene := &Scene{}
		scene.Apply(Settings{Lights: mode})

		if want := mode.orDefault(); scene.lights != want {
			t.Errorf("Apply(%q) left %q, want %q", mode, scene.lights, want)
		}
	}

	if (&Scene{lights: unspelledLights}).capEngaged(capLights) {
		t.Error("an unspelled mode lit the S cap, want it read as off")
	}
}

// TestLightsVisibility pins the fade's two ends and the floor under it:
// nothing from ten degrees up, all of it from the horizon down, a straight
// slide between, and nothing below lightsFloor.
func TestLightsVisibility(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		elevation float64
		want      float64
	}{
		{name: "noon", elevation: 45, want: 0},
		{name: "the sky's last keyframe", elevation: lowSunElevation, want: 0},
		{name: "under the floor", elevation: 9.5, want: 0},
		{name: "at the floor", elevation: 9, want: lightsFloor},
		{name: "half way", elevation: 5, want: 0.5},
		{name: "sunset", elevation: sun.Sunset, want: 1},
		{name: "civil twilight", elevation: -3, want: 1},
		{name: "night", elevation: -35, want: 1},
		{name: "NaN", elevation: math.NaN(), want: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := lightsVisibility(testCase.elevation); math.Abs(got-testCase.want) > 1e-9 {
				t.Errorf("lightsVisibility(%v) = %v, want %v", testCase.elevation, got, testCase.want)
			}
		})
	}
}

// TestLightShare checks each mode against what the sun block knows: on and
// off ignore it, auto reads its elevation, and auto with no receiver position
// is night, the auto theme's answer for the same case.
func TestLightShare(t *testing.T) {
	t.Parallel()

	noon := daylight{known: true, elevation: 40}
	dusk := daylight{known: true, elevation: 5}
	nowhere := daylight{elevation: 40}

	for _, testCase := range []struct {
		name  string
		mode  LightsMode
		sky   daylight
		share float64
	}{
		{name: "on at noon", mode: LightsOn, sky: noon, share: 1},
		{name: "off at night", mode: LightsOff, sky: daylight{known: true, elevation: -20}},
		{name: "unset", mode: "", sky: dusk},
		{name: "auto at noon", mode: LightsAuto, sky: noon},
		{name: "auto at dusk", mode: LightsAuto, sky: dusk, share: 0.5},
		{name: "auto with no position", mode: LightsAuto, sky: nowhere, share: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{lights: testCase.mode, daylight: testCase.sky}
			if got := scene.lightShare(); got != testCase.share {
				t.Errorf("lightShare() = %v, want %v", got, testCase.share)
			}
		})
	}
}

// TestLightsFadeAcrossDuskAndDawn follows a day at the test receiver through
// the sun block's own resolve, ten minutes a step: through dusk the share
// never falls and through dawn it never rises, both run from none to all,
// and every step is the same function of the elevation, so a dawn at the
// height of a dusk draws the lights alike.
func TestLightsFadeAcrossDuskAndDawn(t *testing.T) {
	t.Parallel()

	const step = 10 * time.Minute

	receiver := source.Receiver{Latitude: 52.3105, Longitude: 4.7683, Mode: source.FixManual}
	day := time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC)

	for _, testCase := range []struct {
		name       string
		from, to   time.Duration
		brightens  bool
		first, end float64
	}{
		{name: "dusk", from: 16 * time.Hour, to: 18*time.Hour + 30*time.Minute, brightens: true, first: 0, end: 1},
		{name: "dawn", from: 4*time.Hour + 30*time.Minute, to: 7 * time.Hour, first: 1, end: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scene := &Scene{lights: LightsAuto}
			previous := testCase.first

			for offset := testCase.from; offset <= testCase.to; offset += step {
				scene.resolveDaylight(source.Frame{Now: day.Add(offset), Receiver: receiver})

				share, own := scene.lightShare(), lightsVisibility(scene.daylight.elevation)
				if share != own {
					t.Fatalf("%v: share %v is not the elevation's own %v", offset, share, own)
				}

				if testCase.brightens && share < previous || !testCase.brightens && share > previous {
					t.Errorf("%v: share went from %v to %v", offset, previous, share)
				}

				previous = share
			}

			if previous != testCase.end {
				t.Errorf("the run ended on %v, want %v", previous, testCase.end)
			}
		})
	}
}

// TestDrawLightsFadesTheColourNotTheShape checks a faded light on the flat
// sprite: the port tip moves steadily from the sprite's own colour towards the
// red as the sun goes down, it is the same block at every share, and the
// floor leaves the tip untouched rather than a few levels off.
func TestDrawLightsFadesTheColourNotTheShape(t *testing.T) {
	t.Parallel()

	const centre = 20

	tip := func(elevation float64) (color.RGBA, int) {
		scene := lightScene(theme.Night)
		scene.icon = sprite.Airplane()
		scene.lights = LightsAuto
		scene.daylight = daylight{known: true, elevation: elevation}
		scene.elapsed = momentFor(t, flashes{})
		scene.flashFrom = scene.elapsed

		canv := lightCanvas(t)
		canv.Clear(theme.Night.Ink)
		lay := layout{dst: canv, scale: 1}
		scene.drawLights(&lay, centre, centre, airplane.Snapshot{ICAO: lightICAO})

		return canv.Image().RGBAAt(centre-6, centre+1), countRGBA(canv, theme.Night.Ink)
	}

	untouched, background := tip(lowSunElevation - 0.5)
	if untouched != theme.Night.Ink {
		t.Errorf("under the floor the port tip is %v, want it left at %v", untouched, theme.Night.Ink)
	}

	previous := untouched

	for _, elevation := range []float64{9, 7, 5, 3, 1, 0} {
		got, rest := tip(elevation)
		if rest != background-3 {
			t.Errorf("at %v degrees %d pixels changed, want the three tips", elevation, background-rest)
		}

		if distance(got, theme.Night.Warn) >= distance(previous, theme.Night.Warn) {
			t.Errorf("at %v degrees the port tip %v is no nearer red than %v", elevation, got, previous)
		}

		previous = got
	}

	if previous != theme.Night.Warn {
		t.Errorf("at the horizon the port tip is %v, want the full %v", previous, theme.Night.Warn)
	}
}
