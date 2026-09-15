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

	// pointWidth is how many degrees each of the eight compass points covers,
	// and halfPoint is the offset that centres a point on its letter rather
	// than starting it there.
	pointWidth = 45.0
	halfPoint  = 22.5

	// groupSize is how many digits go between two thousands separators.
	groupSize = 3

	// decimalBase is the base every number here is written in.
	decimalBase = 10

	// floatBits is the precision strconv is told the value has.
	floatBits = 64
)

// compassPoints are the eight points in the order an increasing heading
// passes them.
//
//nolint:gochecknoglobals // a compass is data, and an array cannot be const.
var compassPoints = [...]string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}

// compass turns a heading in degrees into its eight-point letter.
//
// A heading that is not a number reads as north. It arrives from the air and
// is never trusted; a scope that panicked on a corrupt velocity message would
// be worse than one that points the wrong way for a frame.
func compass(heading float64) string {
	if math.IsNaN(heading) || math.IsInf(heading, 0) {
		return compassPoints[0]
	}

	normalised := math.Mod(heading, degreesPerCircle)
	if normalised < 0 {
		normalised += degreesPerCircle
	}

	return compassPoints[int((normalised+halfPoint)/pointWidth)%len(compassPoints)]
}

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

// count writes a count. It is separate from whole because a count is already
// an integer and rounding one through float64 would be a lie about where it
// came from.
func (s *Scene) count(value int) []byte {
	return strconv.AppendInt(s.digits[:0], int64(value), decimalBase)
}

// index writes a one-based position with a leading zero under ten, which is
// what keeps the compact rows' first column the same width all the way down.
//
// A negative value is written as it is. Nothing here passes one, because the
// card guards on its notSelected sentinel before it formats anything, but
// padding a minus sign into "0-3" would be a strange thing to leave lying
// about for the next caller.
func (s *Scene) index(value int) []byte {
	out := s.digits[:0]
	if value >= 0 && value < decimalBase {
		out = append(out, '0')
	}

	return strconv.AppendInt(out, int64(value), decimalBase)
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

// bearing writes a bearing as its three digits, a separator and its compass
// point.
//
// It is one field rather than two because the compact rows right-align it as a
// unit, and two right-aligned pieces would need two column edges to align
// against for no gain.
func (s *Scene) bearing(value float64) []byte {
	out := append(s.degrees(value), ' ', '/', ' ')

	return append(out, compass(value)...)
}

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

// drawBytesRight draws a formatted number ending at rightX.
//
// Unlike drawBytes it returns nothing. A right-aligned run always ends where
// it was told to, so there is no pen position worth passing on.
//
//nolint:varnamelen // y is the pixel-addressing idiom used throughout uScope.
func drawBytesRight(dst *canvas.Canvas, face *psf.Font, rightX, y int, value []byte, ink color.RGBA) {
	text.DrawRight(dst, face, rightX, y, string(value), ink)
}

// measureBytes reports how wide a formatted number will be.
func measureBytes(face *psf.Font, value []byte) int {
	width, _ := text.Measure(face, string(value))

	return width
}
