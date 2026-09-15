// Package airlines maps ADS-B callsign prefixes to the operator that flew
// them and the colour the radar paints it in.
//
// A callsign heard over the air starts with the ICAO three-letter operator
// designator: KLM123 is KLM, EZY45AB is easyJet. The database backing the
// lookup is a small CSV embedded at build time (airlines.csv, in this
// directory) and parsed once, on first use, into a map keyed on the
// designator's three bytes, so the render path costs nothing more than a map
// lookup per aircraft per frame.
package airlines

import (
	"bytes"
	"cmp"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"slices"
	"sync"
)

const (
	codeLength      = 3
	expectedFields  = 6
	hexLength       = 6
	hexLetterOffset = 10
	hexNibbleShift  = 4
	maxChannel      = 255
)

const (
	// darkFloorLuminance is the WCAG relative luminance a colour must clear
	// before it reads as a coloured shape rather than a dark smear against
	// the radar's near-black field. It isn't a value out of the WCAG spec;
	// it's picked for this display, a small transflective panel read at
	// arm's length, roughly where a colour stops looking like a dark blob
	// and starts looking like the operator's actual brand colour.
	darkFloorLuminance = 0.18

	// lightCeilingLuminance is the mirror of darkFloorLuminance for the paper
	// theme. The paper field sits at a relative luminance of about 0.88, so a
	// colour has to come down well below that before it reads as a shape on the
	// page rather than as a pale wash. 0.45 is where that happens on this panel.
	lightCeilingLuminance = 0.45

	// bisectionIterations is fixed rather than convergence-checked so OnDark
	// produces the exact same bytes on every platform it runs on.
	bisectionIterations = 24

	srgbGammaThreshold = 0.04045
	srgbLinearDivisor  = 12.92
	srgbGammaOffset    = 0.055
	srgbGammaDivisor   = 1.055
	srgbGammaExponent  = 2.4

	luminanceRedWeight   = 0.2126
	luminanceGreenWeight = 0.7152
	luminanceBlueWeight  = 0.0722

	hueSixth      = 1.0 / 6.0
	hueThird      = 1.0 / 3.0
	hueHalf       = 0.5
	hueTwoThirds  = 2.0 / 3.0
	hueSextantSix = 6.0
	hueBlueOffset = 4.0
)

// The sentinel errors parse can return. Every one of them is wrapped with
// fmt.Errorf and a line number before it leaves this package, so a caller
// still gets errors.Is against a fixed value plus enough context to find the
// bad row in the CSV.
var (
	// ErrEmpty is returned for an embedded file with no data rows: nothing
	// after the header, or nothing at all.
	ErrEmpty = errors.New("airlines: embedded database has no rows")

	// ErrHeader is returned when the first line does not match the expected
	// header exactly.
	ErrHeader = errors.New("airlines: unexpected header line")

	// ErrFieldCount is returned for a row with more or fewer than six
	// fields.
	ErrFieldCount = errors.New("airlines: row has the wrong number of fields")

	// ErrCode is returned for an icao column that is not three uppercase
	// A-Z letters.
	ErrCode = errors.New("airlines: icao code must be three uppercase letters")

	// ErrDuplicate is returned when the same icao code appears twice.
	ErrDuplicate = errors.New("airlines: duplicate icao code")

	// ErrHex is returned for a hex column that is not six uppercase hex
	// digits.
	ErrHex = errors.New("airlines: hex must be six uppercase hex digits")

	// ErrSource is returned for a source column outside the allow list.
	ErrSource = errors.New("airlines: source must be simple-icons, brand or livery")

	// ErrName is returned for an empty name column.
	ErrName = errors.New("airlines: name must not be empty")

	// ErrGroup is returned when a group column names an icao code that does
	// not exist in the file.
	ErrGroup = errors.New("airlines: group names an icao code that does not exist")
)

// Airline is one operator: its ICAO designator, its name, the brand colour
// the radar paints it, where that colour came from, and its parent brand's
// designator if it flies under one.
type Airline struct {
	ICAO   string
	Name   string
	Color  color.RGBA
	Source string
	Group  string
}

// The CSV embedded at build time. See database.load for where it actually
// gets parsed; embedding it doesn't parse it.
//
//nolint:gochecknoglobals // go:embed needs a package-level variable.
//go:embed airlines.csv
var embedded []byte

// catalog is the package's one embedded database, parsed at most once and
// shared by every exported lookup function below.
//
//nolint:gochecknoglobals // the cache has to outlive the call that fills it.
var catalog = &database{raw: embedded}

// database is one CSV source plus the result of parsing it, done at most
// once through once. The internal test builds one directly with a synthetic
// raw, which is how every parse-failure branch gets driven without touching
// the real embedded file.
type database struct {
	once   sync.Once
	raw    []byte
	byCode map[[3]byte]Airline
	err    error
}

// Load parses the embedded database and returns any parse error.
//
// Calling it is optional: Lookup, ByICAO, Count and All all trigger the same
// parse on first use. Load exists for a caller that wants to fail at startup
// instead of on the first radar frame.
func Load() error {
	return catalog.load()
}

// Lookup finds the airline whose ICAO designator prefixes callsign.
//
// Only the first three bytes of callsign are read, and each one must be an
// uppercase A-Z letter; anything shorter, or with a lowercase letter, digit
// or punctuation in the first three bytes, returns false. Case is
// deliberately not folded: callsigns heard over the air are always
// uppercase, so a lowercase match would mean something upstream mis-decoded
// the frame, and quietly matching it anyway would bury that bug instead of
// surfacing it.
func Lookup(callsign string) (Airline, bool) {
	return catalog.lookup(callsign)
}

// ByICAO finds the airline for a three-letter designator, applying the same
// uppercase A-Z rule as Lookup.
func ByICAO(code string) (Airline, bool) {
	return catalog.byICAO(code)
}

// Count returns the number of airlines in the database, or zero if the
// embedded file failed to parse.
func Count() int {
	return catalog.count()
}

// All returns every airline, sorted ascending by ICAO. Each call returns a
// fresh slice, so mutating the result never affects the package's own
// state.
func All() []Airline {
	return catalog.all()
}

// OnDark returns the colour the radar should actually draw: the same
// hue and saturation, with the lightness raised just enough that the colour
// clears darkFloorLuminance against the field's near-black background. A
// colour already above the floor comes back unchanged. Alpha is always 255.
func (a Airline) OnDark() color.RGBA {
	if relativeLuminance(a.Color) >= darkFloorLuminance {
		return color.RGBA{R: a.Color.R, G: a.Color.G, B: a.Color.B, A: maxChannel}
	}

	hue, saturation, lightness := rgbToHSL(a.Color)
	low, high := lightness, 1.0

	for range bisectionIterations {
		mid := (low + high) / 2

		red, green, blue := hslToRGB(hue, saturation, mid)
		if relativeLuminance(color.RGBA{R: red, G: green, B: blue, A: maxChannel}) < darkFloorLuminance {
			low = mid

			continue
		}

		high = mid
	}

	red, green, blue := hslToRGB(hue, saturation, high)

	return color.RGBA{R: red, G: green, B: blue, A: maxChannel}
}

// OnLight returns the colour the radar should draw on the paper theme's light
// field: the same hue and saturation, with the lightness lowered just enough
// that the colour comes down to lightCeilingLuminance. A colour already at or
// under the ceiling comes back unchanged. Alpha is always 255.
func (a Airline) OnLight() color.RGBA {
	if relativeLuminance(a.Color) <= lightCeilingLuminance {
		return color.RGBA{R: a.Color.R, G: a.Color.G, B: a.Color.B, A: maxChannel}
	}

	hue, saturation, lightness := rgbToHSL(a.Color)
	low, high := 0.0, lightness

	for range bisectionIterations {
		mid := (low + high) / 2

		red, green, blue := hslToRGB(hue, saturation, mid)
		if relativeLuminance(color.RGBA{R: red, G: green, B: blue, A: maxChannel}) > lightCeilingLuminance {
			high = mid

			continue
		}

		low = mid
	}

	red, green, blue := hslToRGB(hue, saturation, low)

	return color.RGBA{R: red, G: green, B: blue, A: maxChannel}
}

// load parses raw once and replays the same outcome to every later caller.
func (d *database) load() error {
	d.once.Do(func() {
		byCode, err := parse(bytes.NewReader(d.raw))
		d.byCode, d.err = byCode, err
	})

	return d.err
}

// lookup is Lookup's implementation, kept on database so the internal test
// can drive it against a malformed raw instead of the real embedded file.
func (d *database) lookup(callsign string) (Airline, bool) {
	if d.load() != nil {
		return Airline{}, false
	}

	if len(callsign) < codeLength {
		return Airline{}, false
	}

	key, valid := codeKey(callsign[:codeLength])
	if !valid {
		return Airline{}, false
	}

	airline, ok := d.byCode[key]

	return airline, ok
}

// byICAO is ByICAO's implementation, kept on database for the same reason as
// lookup.
func (d *database) byICAO(code string) (Airline, bool) {
	if d.load() != nil {
		return Airline{}, false
	}

	key, valid := codeKey(code)
	if !valid {
		return Airline{}, false
	}

	airline, ok := d.byCode[key]

	return airline, ok
}

// count is Count's implementation.
func (d *database) count() int {
	if d.load() != nil {
		return 0
	}

	return len(d.byCode)
}

// all is All's implementation.
func (d *database) all() []Airline {
	if d.load() != nil {
		return []Airline{}
	}

	out := make([]Airline, 0, len(d.byCode))

	for _, airline := range d.byCode {
		out = append(out, airline)
	}

	slices.SortFunc(out, func(a, b Airline) int {
		return cmp.Compare(a.ICAO, b.ICAO)
	})

	return out
}

// parse reads a whole CSV database from source and builds the lookup map,
// or fails with one of the sentinel errors declared above.
func parse(source io.Reader) (map[[3]byte]Airline, error) {
	reader := csv.NewReader(source)
	reader.FieldsPerRecord = expectedFields

	if err := readHeader(reader); err != nil {
		return nil, err
	}

	byCode, err := readRows(reader)
	if err != nil {
		return nil, err
	}

	if len(byCode) == 0 {
		return nil, fmt.Errorf("airlines: %w", ErrEmpty)
	}

	if err := validateGroups(byCode); err != nil {
		return nil, err
	}

	return byCode, nil
}

// readHeader consumes the first line and checks it matches the CSV header
// exactly.
func readHeader(reader *csv.Reader) error {
	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("airlines: %w", ErrEmpty)
	}

	if err != nil {
		return fmt.Errorf("airlines: line 1: %w", ErrFieldCount)
	}

	want := []string{"icao", "name", "hex", "source", "group", "note"}

	for i, col := range want {
		if header[i] != col {
			return fmt.Errorf("airlines: line 1: %w", ErrHeader)
		}
	}

	return nil
}

// readRows consumes every row after the header and builds the lookup map,
// checking each row's icao code for shape and uniqueness as it goes.
func readRows(reader *csv.Reader) (map[[3]byte]Airline, error) {
	byCode := make(map[[3]byte]Airline)
	line := 1

	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return byCode, nil
		}

		line++

		if err != nil {
			return nil, fmt.Errorf("airlines: line %d: %w", line, ErrFieldCount)
		}

		airline, code, err := parseRow(line, record, byCode)
		if err != nil {
			return nil, err
		}

		byCode[code] = airline
	}
}

// parseRow validates one already-split CSV row against byCode (the rows
// seen so far, for the uniqueness check) and builds the Airline it
// describes. The checks run in the order the package doc promises: code
// shape, code uniqueness, hex shape, source allow-list, then name.
func parseRow(line int, record []string, byCode map[[3]byte]Airline) (Airline, [3]byte, error) {
	code, valid := codeKey(record[0])
	if !valid {
		return Airline{}, code, fmt.Errorf("airlines: line %d: %w", line, ErrCode)
	}

	if _, exists := byCode[code]; exists {
		return Airline{}, code, fmt.Errorf("airlines: line %d: %w", line, ErrDuplicate)
	}

	col, ok := parseHex(record[2])
	if !ok {
		return Airline{}, code, fmt.Errorf("airlines: line %d: %w", line, ErrHex)
	}

	if !validSource(record[3]) {
		return Airline{}, code, fmt.Errorf("airlines: line %d: %w", line, ErrSource)
	}

	if record[1] == "" {
		return Airline{}, code, fmt.Errorf("airlines: line %d: %w", line, ErrName)
	}

	airline := Airline{
		ICAO:   record[0],
		Name:   record[1],
		Color:  col,
		Source: record[3],
		Group:  record[4],
	}

	return airline, code, nil
}

// codeKey converts value into the three-letter map key, reporting false
// unless value is exactly three uppercase A-Z bytes. Lookup, ByICAO, the CSV
// parser and the group cross-check all funnel through this one check, so
// "three uppercase letters" is defined in exactly one place.
func codeKey(value string) ([3]byte, bool) {
	var key [3]byte

	if len(value) != codeLength {
		return key, false
	}

	for i := range codeLength {
		if value[i] < 'A' || value[i] > 'Z' {
			return key, false
		}

		key[i] = value[i]
	}

	return key, true
}

// parseHex converts a six hex-digit column into an opaque colour, reporting
// false unless value is exactly six uppercase 0-9A-F characters.
func parseHex(value string) (color.RGBA, bool) {
	if len(value) != hexLength {
		return color.RGBA{}, false
	}

	var channel [3]uint8

	for idx := range channel {
		high, valid := hexDigit(value[idx*2])
		if !valid {
			return color.RGBA{}, false
		}

		low, ok := hexDigit(value[idx*2+1])
		if !ok {
			return color.RGBA{}, false
		}

		channel[idx] = high<<hexNibbleShift | low
	}

	return color.RGBA{R: channel[0], G: channel[1], B: channel[2], A: maxChannel}, true
}

// hexDigit converts one uppercase hex digit to its numeric value.
func hexDigit(digit byte) (uint8, bool) {
	switch {
	case digit >= '0' && digit <= '9':
		return digit - '0', true
	case digit >= 'A' && digit <= 'F':
		return digit - 'A' + hexLetterOffset, true
	default:
		return 0, false
	}
}

// validSource reports whether source is one of the three allowed values.
func validSource(source string) bool {
	switch source {
	case "simple-icons", "brand", "livery":
		return true
	default:
		return false
	}
}

// validateGroups is the second pass parse runs once every row is in: every
// non-empty group column has to name an icao code that actually exists in
// the file.
func validateGroups(byCode map[[3]byte]Airline) error {
	for _, airline := range byCode {
		if airline.Group == "" {
			continue
		}

		code, ok := codeKey(airline.Group)
		if !ok {
			return fmt.Errorf("airlines: %s: %w", airline.ICAO, ErrGroup)
		}

		if _, exists := byCode[code]; !exists {
			return fmt.Errorf("airlines: %s: %w", airline.ICAO, ErrGroup)
		}
	}

	return nil
}

// relativeLuminance computes WCAG relative luminance for an opaque colour.
func relativeLuminance(col color.RGBA) float64 {
	red := linearize(float64(col.R) / maxChannel)
	green := linearize(float64(col.G) / maxChannel)
	blue := linearize(float64(col.B) / maxChannel)

	return luminanceRedWeight*red + luminanceGreenWeight*green + luminanceBlueWeight*blue
}

// linearize converts one sRGB channel, in the 0-1 range, to linear light.
func linearize(channel float64) float64 {
	if channel <= srgbGammaThreshold {
		return channel / srgbLinearDivisor
	}

	return math.Pow((channel+srgbGammaOffset)/srgbGammaDivisor, srgbGammaExponent)
}

// rgbToHSL converts an opaque colour to HSL, with hue as a fraction of a
// full turn (0-1) rather than degrees, and saturation and lightness each in
// 0-1.
func rgbToHSL(col color.RGBA) (float64, float64, float64) {
	red := float64(col.R) / maxChannel
	green := float64(col.G) / maxChannel
	blue := float64(col.B) / maxChannel

	largest := math.Max(red, math.Max(green, blue))
	smallest := math.Min(red, math.Min(green, blue))
	lightness := (largest + smallest) / 2

	if largest == smallest {
		return 0, 0, lightness
	}

	delta := largest - smallest

	var saturation float64
	if lightness > hueHalf {
		saturation = delta / (2 - largest - smallest)
	} else {
		saturation = delta / (largest + smallest)
	}

	var hue float64

	switch largest {
	case red:
		hue = (green - blue) / delta
		if green < blue {
			hue += hueSextantSix
		}
	case green:
		hue = (blue-red)/delta + 2
	default:
		hue = (red-green)/delta + hueBlueOffset
	}

	hue /= hueSextantSix

	return hue, saturation, lightness
}

// hslToRGB is rgbToHSL's inverse, rounding each channel to the nearest sRGB
// byte.
func hslToRGB(hue, saturation, lightness float64) (uint8, uint8, uint8) {
	if saturation == 0 {
		level := clampChannel(lightness)

		return level, level, level
	}

	var high float64
	if lightness < hueHalf {
		high = lightness * (1 + saturation)
	} else {
		high = lightness + saturation - lightness*saturation
	}

	low := 2*lightness - high

	red := clampChannel(hueToChannel(low, high, hue+hueThird))
	green := clampChannel(hueToChannel(low, high, hue))
	blue := clampChannel(hueToChannel(low, high, hue-hueThird))

	return red, green, blue
}

// hueToChannel is the classic piecewise HSL-to-channel helper: it turns one
// hue offset (hue shifted by +1/3, 0 or -1/3 turns, one call per channel)
// into that channel's fraction, given the low and high anchors hslToRGB
// already worked out.
func hueToChannel(low, high, hueOffset float64) float64 {
	switch {
	case hueOffset < 0:
		hueOffset++
	case hueOffset > 1:
		hueOffset--
	default:
	}

	switch {
	case hueOffset < hueSixth:
		return low + (high-low)*hueSextantSix*hueOffset
	case hueOffset < hueHalf:
		return high
	case hueOffset < hueTwoThirds:
		return low + (high-low)*(hueTwoThirds-hueOffset)*hueSextantSix
	default:
		return low
	}
}

// clampChannel rounds a channel fraction to the nearest sRGB byte, clamping
// defensively in case float error ever pushes it just outside 0-1.
func clampChannel(fraction float64) uint8 {
	scaled := math.Round(fraction * maxChannel)

	switch {
	case scaled <= 0:
		return 0
	case scaled >= maxChannel:
		return maxChannel
	default:
		return uint8(scaled)
	}
}
