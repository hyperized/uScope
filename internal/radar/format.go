package radar

import (
	"image/color"
	"math"
	"strconv"

	"github.com/hyperized/uScope/pkg/canvas"
	"github.com/hyperized/uScope/pkg/psf"
	"github.com/hyperized/uScope/pkg/text"
)

// Number formatting, reusing the scene's scratch arrays.
//
// Every formatter here returns a byte slice into a buffer the Scene owns, and
// the three helpers at the bottom turn that into the string pkg/text wants.
// The split is not cosmetic. A method that returns a string has to assume the
// string escapes, which puts it on the heap; a method that returns bytes into
// an array the Scene already owns allocates nothing, and the conversion in a
// one-line helper is inlined into the caller, where the compiler can see the
// string does not escape and keep it on the stack. That is the difference
// between zero and about thirty allocations per frame.
//
// A returned slice is valid until the next call that uses the same buffer, so
// format one number, draw it, then format the next.

// Compass arithmetic.
const (
	degreesPerCircle = 360.0
	halfCircle       = 180.0

	// groupSize is how many digits go between two thousands separators.
	groupSize = 3

	// decimalBase is the base every number here is written in.
	decimalBase = 10

	// floatBits is the precision strconv is told the value has.
	floatBits = 64
)

// fixed writes value with exactly decimals digits after the point.
func (s *Scene) fixed(value float64, decimals int) []byte {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return append(s.digits[:0], '-')
	}

	return strconv.AppendFloat(s.digits[:0], value, 'f', decimals, floatBits)
}

// whole writes a rounded whole number.
func (s *Scene) whole(value float64) []byte {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return append(s.digits[:0], '-')
	}

	return strconv.AppendInt(s.digits[:0], int64(math.Round(value)), decimalBase)
}

// speed writes an airspeed, or three dashes when nobody has decoded one.
//
// uAirwaves marks an undecoded velocity with -1, which is airplane's own
// defaultVelocity and not a reading. Passing that straight to whole is what
// put "-1" in the panel's speed figure and in the SPD column on the live
// scope. A negative figure is never a speed, so the whole half-line is the
// guard rather than an equality test against the sentinel.
func (s *Scene) speed(value float64) []byte {
	if math.IsNaN(value) || value < 0 {
		return append(s.digits[:0], '-', '-', '-')
	}

	return s.whole(value)
}

// count writes a count. It is separate from whole because a count is already
// an integer and rounding one through float64 would be a lie about where it
// came from.
func (s *Scene) count(value int) []byte {
	return strconv.AppendInt(s.digits[:0], int64(value), decimalBase)
}

// shownCount writes a count into the grouped buffer instead of into digits.
//
// The strips' count line draws two numbers side by side while a filter is
// on, and both widths have to be known before either is set. One of them
// therefore needs a buffer of its own, and grouped is free: thousands is its
// only other user and nothing calls that while the title line is being drawn.
func (s *Scene) shownCount(value int) []byte {
	return strconv.AppendInt(s.grouped[:0], int64(value), decimalBase)
}

// counter writes an unsigned counter. Frame counts arrive as uint64 and
// pushing one through float64 would start rounding after fifteen digits, so
// it gets its own formatter.
func (s *Scene) counter(value uint64) []byte {
	return strconv.AppendUint(s.digits[:0], value, decimalBase)
}

// degrees writes a heading as three digits, the way a course is written on a
// flight strip, so 41 reads as 041 and the column never jumps a character
// wider.
func (s *Scene) degrees(value float64) []byte {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return append(s.digits[:0], '0', '0', '0')
	}

	rounded := int(math.Round(value)) % int(degreesPerCircle)
	if rounded < 0 {
		rounded += int(degreesPerCircle)
	}

	out := s.digits[:0]
	if rounded < hundred {
		out = append(out, '0')
	}

	if rounded < decimalBase {
		out = append(out, '0')
	}

	return strconv.AppendInt(out, int64(rounded), decimalBase)
}

// hundred is where a heading stops needing a second leading zero.
const hundred = 100

// flightLevel writes an altitude in hundreds of feet as three digits, which is
// how a data block says a level: 024 rather than 2,400.
//
// It is padded the way degrees is and for the same reason. A tag whose middle
// line changed width as an aircraft climbed through a thousand feet would pull
// the ground speed beside it sideways on that one frame.
//
// An altitude of zero is one nobody has decoded rather than sea level, which is
// the reading bandColour and the 3D view's height both give it, so it comes out
// as dashes rather than as a level of 000.
func (s *Scene) flightLevel(altitudeFt float64) []byte {
	if math.IsNaN(altitudeFt) || math.IsInf(altitudeFt, 0) || altitudeFt <= 0 {
		return append(s.digits[:0], '-', '-', '-')
	}

	hundreds := int(math.Round(altitudeFt / tagLevelPerFoot))

	out := s.digits[:0]
	if hundreds < hundred {
		out = append(out, '0')
	}

	if hundreds < decimalBase {
		out = append(out, '0')
	}

	return strconv.AppendInt(out, int64(hundreds), decimalBase)
}

// thousands writes a rounded whole number with separators, so a flight level
// reads as 39,000 rather than as a run of digits to be counted.
func (s *Scene) thousands(value float64) []byte {
	raw := s.whole(value)

	out := s.grouped[:0]
	body := raw

	if len(body) > 0 && body[0] == '-' {
		out = append(out, '-')
		body = body[1:]
	}

	for position, digit := range body {
		if position > 0 && (len(body)-position)%groupSize == 0 {
			out = append(out, ',')
		}

		out = append(out, digit)
	}

	return out
}

// coordinate writes one half of the receiver's position as a magnitude and a
// hemisphere letter, which is how a chart writes it and how it stays readable
// without a minus sign to spot.
func (s *Scene) coordinate(value float64, positive, negative byte) []byte {
	return s.hemisphere(value, positive, negative, coordinateDecimals)
}

// place writes one half of an aircraft's position, to fewer decimals than the
// receiver's own gets.
func (s *Scene) place(value float64, positive, negative byte) []byte {
	return s.hemisphere(value, positive, negative, placeDecimals)
}

// hemisphere is the shared half of the two above.
func (s *Scene) hemisphere(value float64, positive, negative byte, decimals int) []byte {
	sign := positive
	if value < 0 {
		sign = negative
	}

	out := strconv.AppendFloat(s.coords[:0], math.Abs(value), 'f', decimals, floatBits)

	return append(out, ' ', sign)
}

const (
	// coordinateDecimals is four places, which is about ten metres. More would
	// be a claim the receiver's own position cannot back up.
	coordinateDecimals = 4

	// placeDecimals is two, which is about a nautical mile. An aircraft's
	// position on a scope is worth that much and no more: it is a fix a few
	// seconds old, plotted on a projection that rounds to the pixel.
	placeDecimals = 2
)

// drawBytes draws a formatted number and returns the x just past it.
//
// It exists so the byte-to-string conversion happens in a function small
// enough to inline, which is what keeps it off the heap. See the note at the
// top of this file.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func drawBytes(dst *canvas.Canvas, face *psf.Font, x, y int, value []byte, ink color.RGBA) int {
	return text.Draw(dst, face, x, y, string(value), ink)
}

// drawBytesTracked draws a formatted number at the scene's label tracking and
// returns the x just past it, so a number can open a run that a tracked label
// finishes.
//
//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
func drawBytesTracked(dst *canvas.Canvas, face *psf.Font, x, y int, value []byte, ink color.RGBA) int {
	return text.Draw(dst, face, x, y, string(value), ink, text.WithSpacing(labelTracking))
}

// measureBytes reports how wide a formatted number will be.
func measureBytes(face *psf.Font, value []byte) int {
	width, _ := text.Measure(face, string(value))

	return width
}

// trackedWidth is how wide a run of that many glyphs is at labelTracking. It
// is the inverse of fitRunes, and it exists so a right-aligned tracked run can
// be placed from a byte slice without converting it to a string first.
func trackedWidth(face *psf.Font, runes int) int {
	if runes <= 0 {
		return 0
	}

	return runes*glyphWidth(face) + (runes-1)*labelTracking
}
