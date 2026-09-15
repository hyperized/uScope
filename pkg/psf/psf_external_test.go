package psf_test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/hyperized/uScope/pkg/fonts"
	"github.com/hyperized/uScope/pkg/psf"
)

// Mirrors of the unexported layout constants in psf2.go and psf1.go. A
// black-box test cannot see the real ones, so it keeps its own copies to
// build synthetic font files byte by byte.
const (
	psf2MagicBytes  = "\x72\xb5\x4a\x86"
	psf2HeaderLen   = 32
	psf2FlagUnicode = 1

	psf2Terminator byte = 0xFF
	psf2SeqStart   byte = 0xFE

	off2HeaderSize = 8
	off2Flags      = 12
	off2Length     = 16
	off2Charsize   = 20
	off2Height     = 24
	off2Width      = 28

	psf1MagicBytes    = "\x36\x04"
	psf1HeaderLen     = 4
	psf1Mode512Flag   = 0x01
	psf1TableModeFlag = 0x06

	psf1Terminator uint16 = 0xFFFF
	psf1SeqStart   uint16 = 0xFFFE

	glyphCount256  = 256
	glyphCount512  = 512
	psf1GlyphWidth = 8

	// maxCharsizeCap mirrors psf2.go's unexported maxCharsize: the widest
	// glyph row (MaxWidth/8 bytes) times the tallest glyph (MaxHeight rows).
	maxCharsizeCap = psf.MaxWidth / 8 * psf.MaxHeight

	blankFill = 0x00
	inkFill   = 0xFF
)

// psf2Fields holds every PSF2 header field a test may want to set, including
// ones deliberately wrong. headerLen is how many bytes are physically
// allocated for the header; declaredSize is what gets written into the
// headerSize field, which need not match headerLen: the format allows a
// header that claims to be longer, or shorter, than the file backing it.
type psf2Fields struct {
	headerLen    int
	declaredSize int
	flags        int
	count        int
	charsize     int
	height       int
	width        int
}

// buildPSF2Raw assembles a raw PSF2 file: a header of headerLen bytes (its
// fields are written only when there is room for all of them), followed by
// glyphData and table verbatim. It does nothing to keep the result
// well-formed, which is the point: a malformed-header test builds one
// directly with the one field it wants broken.
func buildPSF2Raw(fields psf2Fields, glyphData, table []byte) []byte {
	buf := make([]byte, fields.headerLen)
	copy(buf, psf2MagicBytes)

	// Test fixtures deliberately write out-of-range field values (that is
	// what the TooLarge cases are for), so these conversions are exercised
	// outside uint32's normal domain on purpose.
	if fields.headerLen >= psf2HeaderLen {
		binary.LittleEndian.PutUint32(buf[off2HeaderSize:], uint32(fields.declaredSize)) //nolint:gosec
		binary.LittleEndian.PutUint32(buf[off2Flags:], uint32(fields.flags))             //nolint:gosec
		binary.LittleEndian.PutUint32(buf[off2Length:], uint32(fields.count))            //nolint:gosec
		binary.LittleEndian.PutUint32(buf[off2Charsize:], uint32(fields.charsize))       //nolint:gosec
		binary.LittleEndian.PutUint32(buf[off2Height:], uint32(fields.height))           //nolint:gosec
		binary.LittleEndian.PutUint32(buf[off2Width:], uint32(fields.width))             //nolint:gosec
	}

	return concatBytes(buf, glyphData, table)
}

// buildPSF2 assembles a well-formed PSF2 file: a header sized headerLen bytes
// with its declared size matching, glyph data, and an optional unicode
// table.
func buildPSF2(fields psf2Fields, glyphData, table []byte) []byte {
	fields.declaredSize = fields.headerLen

	return buildPSF2Raw(fields, glyphData, table)
}

// psf1Fields holds the PSF1 header fields (beyond the fixed magic) a test may
// want to set.
type psf1Fields struct {
	mode     int
	charsize int
}

// buildPSF1Raw assembles a raw PSF1 file: a header of headerLen bytes (mode
// and charsize are written only when there is room for both), followed by
// glyphData and table verbatim.
func buildPSF1Raw(headerLen int, fields psf1Fields, glyphData, table []byte) []byte {
	buf := make([]byte, headerLen)
	copy(buf, psf1MagicBytes)

	if headerLen >= psf1HeaderLen {
		buf[2] = byte(fields.mode)     //nolint:gosec // test fixture, mode fits a byte by construction
		buf[3] = byte(fields.charsize) //nolint:gosec // test fixture, charsize fits a byte by construction
	}

	return concatBytes(buf, glyphData, table)
}

// buildPSF1 assembles a well-formed PSF1 file: the 4-byte header, glyph data,
// and an optional unicode table.
func buildPSF1(fields psf1Fields, glyphData, table []byte) []byte {
	return buildPSF1Raw(psf1HeaderLen, fields, glyphData, table)
}

// concatBytes joins byte slices in order. Assembling a file out of named
// pieces (header, glyphs, table) reads better than one long hand-indexed
// slice.
func concatBytes(parts ...[]byte) []byte {
	total := 0
	for _, part := range parts {
		total += len(part)
	}

	joined := make([]byte, 0, total)
	for _, part := range parts {
		joined = append(joined, part...)
	}

	return joined
}

// makeGlyphBytes returns count one-byte glyphs, every byte set to fill. The
// bit pattern rarely matters for a truncation or header test; what matters
// there is having the right number of bytes.
func makeGlyphBytes(count int, fill byte) []byte {
	data := make([]byte, count)
	for index := range data {
		data[index] = fill
	}

	return data
}

// utf8Entry returns one glyph's PSF2 unicode table entry: the UTF-8 bytes of
// codepoints, terminated by 0xFF.
func utf8Entry(codepoints string) []byte {
	return concatBytes([]byte(codepoints), []byte{psf2Terminator})
}

// utf8EntryWithSequence is utf8Entry plus a 0xFE-prefixed sequence tail that
// the parser must drop.
func utf8EntryWithSequence(codepoints, sequence string) []byte {
	return concatBytes([]byte(codepoints), []byte{psf2SeqStart}, []byte(sequence), []byte{psf2Terminator})
}

// buildUTF8Table concatenates one PSF2 unicode table entry per glyph, in
// glyph order.
func buildUTF8Table(entries ...[]byte) []byte {
	return concatBytes(entries...)
}

// ucs2Entry returns one glyph's PSF1 unicode table entry: codepoints as
// little-endian uint16s, optionally followed by 0xFFFE and a sequence the
// parser must drop, terminated by 0xFFFF.
func ucs2Entry(codepoints, sequence []rune) []byte {
	const uint16Size = 2

	entry := make([]byte, 0, (len(codepoints)+len(sequence)+2)*uint16Size)

	for _, value := range codepoints {
		entry = binary.LittleEndian.AppendUint16(entry, uint16(value)) //nolint:gosec // ASCII test values
	}

	if len(sequence) > 0 {
		entry = binary.LittleEndian.AppendUint16(entry, psf1SeqStart)

		for _, value := range sequence {
			entry = binary.LittleEndian.AppendUint16(entry, uint16(value)) //nolint:gosec // ASCII test values
		}
	}

	return binary.LittleEndian.AppendUint16(entry, psf1Terminator)
}

// fullPSF1Table returns a complete PSF1 unicode table for count glyphs: the
// entries named in overrides for the glyphs they key, and an empty
// terminated entry (no mapping) for every glyph overrides does not mention.
// PSF1 needs exactly count table entries to match its count glyphs, so this
// always builds the full run rather than leaving gaps for a test to trip
// over by accident.
func fullPSF1Table(count int, overrides map[int][]byte) []byte {
	entries := make([][]byte, count)

	for index := range count {
		if entry, ok := overrides[index]; ok {
			entries[index] = entry

			continue
		}

		entries[index] = ucs2Entry(nil, nil)
	}

	return concatBytes(entries...)
}

// mustParse parses data and fails the test immediately if Parse returns an
// error. Most success-path cases treat a parse failure as a setup mistake
// rather than the behaviour under test.
func mustParse(t *testing.T, data []byte) *psf.Font {
	t.Helper()

	font, err := psf.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	return font
}

// wantParseError fails the test unless parsing data returns an error
// satisfying errors.Is(err, want).
func wantParseError(t *testing.T, data []byte, want error) {
	t.Helper()

	_, err := psf.Parse(data)
	if !errors.Is(err, want) {
		t.Errorf("Parse() error = %v, want %v", err, want)
	}
}

// assertGlyphMapped fails the test unless font has a glyph for value. why
// names the reason this mapping should exist.
func assertGlyphMapped(t *testing.T, font *psf.Font, value rune, why string) {
	t.Helper()

	if _, ok := font.Glyph(value); !ok {
		t.Errorf("Glyph(%q) ok = false, want true (%s)", value, why)
	}
}

// assertGlyphNotMapped fails the test if font has a glyph for value. why
// names the reason this mapping should not exist.
func assertGlyphNotMapped(t *testing.T, font *psf.Font, value rune, why string) {
	t.Helper()

	if _, ok := font.Glyph(value); ok {
		t.Errorf("Glyph(%q) ok = true, want false (%s)", value, why)
	}
}

// assertHasInk fails the test unless font has a glyph for value with at
// least one lit pixel.
func assertHasInk(t *testing.T, font *psf.Font, value rune, label string) {
	t.Helper()

	glyph, ok := font.Glyph(value)
	if !ok {
		t.Fatalf("%s: Glyph(%q) not found", label, value)
	}

	if !glyphHasInk(glyph, font) {
		t.Errorf("%s: Glyph(%q) has no ink pixels, want at least one", label, value)
	}
}

// assertNoInk fails the test unless font's glyph for value is entirely
// blank.
func assertNoInk(t *testing.T, font *psf.Font, value rune, label string) {
	t.Helper()

	glyph, ok := font.Glyph(value)
	if !ok {
		t.Fatalf("%s: Glyph(%q) not found", label, value)
	}

	if glyphHasInk(glyph, font) {
		t.Errorf("%s: Glyph(%q) has ink pixels, want none", label, value)
	}
}

// glyphHasInk reports whether any pixel in glyph is set, scanning the full
// width and height of font.
func glyphHasInk(glyph psf.Glyph, font *psf.Font) bool {
	for y := range font.Height() {
		for x := range font.Width() {
			if glyph.Set(x, y) {
				return true
			}
		}
	}

	return false
}

func TestParseMagic(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		data []byte
	}{
		{name: "empty input", data: nil},
		{name: "one byte", data: []byte{0x72}},
		{name: "four bytes wrong magic", data: []byte{0x00, 0x01, 0x02, 0x03}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wantParseError(t, testCase.data, psf.ErrMagic)
		})
	}
}

func TestParsePSF2Minimal(t *testing.T) {
	t.Parallel()

	const (
		width  = psf1GlyphWidth
		height = 1
		count  = 1
	)

	data := buildPSF2(psf2Fields{
		headerLen: psf2HeaderLen,
		count:     count, charsize: height, height: height, width: width,
	}, makeGlyphBytes(count, blankFill), nil)

	font := mustParse(t, data)

	if got := font.Width(); got != width {
		t.Errorf("Width() = %d, want %d", got, width)
	}

	if got := font.Height(); got != height {
		t.Errorf("Height() = %d, want %d", got, height)
	}

	if got := font.Len(); got != count {
		t.Errorf("Len() = %d, want %d", got, count)
	}
}

func TestParsePSF2UnicodeTable(t *testing.T) {
	t.Parallel()

	const (
		width    = psf1GlyphWidth
		height   = 1
		charsize = 1
		count    = 2
	)

	glyphs := makeGlyphBytes(count, blankFill)
	table := buildUTF8Table(utf8Entry("A"), utf8Entry("B"))

	data := buildPSF2(psf2Fields{
		headerLen: psf2HeaderLen, flags: psf2FlagUnicode,
		count: count, charsize: charsize, height: height, width: width,
	}, glyphs, table)

	font := mustParse(t, data)

	assertGlyphMapped(t, font, 'A', "mapped by the table")
	assertGlyphNotMapped(t, font, 'z', "not mapped by the table")
}

func TestParsePSF2DuplicateCodepoint(t *testing.T) {
	t.Parallel()

	const (
		width    = psf1GlyphWidth
		height   = 1
		charsize = 1
		count    = 2
	)

	glyphs := concatBytes(makeGlyphBytes(1, inkFill), makeGlyphBytes(1, blankFill))
	table := buildUTF8Table(utf8Entry("A"), utf8Entry("A"))

	data := buildPSF2(psf2Fields{
		headerLen: psf2HeaderLen, flags: psf2FlagUnicode,
		count: count, charsize: charsize, height: height, width: width,
	}, glyphs, table)

	font := mustParse(t, data)

	glyph, ok := font.Glyph('A')
	if !ok {
		t.Fatal("Glyph('A') ok = false, want true")
	}

	if !glyph.Set(0, 0) {
		t.Error("Glyph('A') read glyph 1's bitmap, want glyph 0's (the first mapping should win)")
	}
}

func TestParsePSF2SequenceEntry(t *testing.T) {
	t.Parallel()

	const (
		width    = psf1GlyphWidth
		height   = 1
		charsize = 1
		count    = 1
	)

	glyphs := makeGlyphBytes(count, blankFill)
	table := utf8EntryWithSequence("A", "xy")

	data := buildPSF2(psf2Fields{
		headerLen: psf2HeaderLen, flags: psf2FlagUnicode,
		count: count, charsize: charsize, height: height, width: width,
	}, glyphs, table)

	font := mustParse(t, data)

	assertGlyphMapped(t, font, 'A', "codepoint before the sequence marker")
	assertGlyphNotMapped(t, font, 'x', "inside the dropped sequence")
	assertGlyphNotMapped(t, font, 'y', "inside the dropped sequence")
}

func TestParsePSF2InvalidUTF8Entry(t *testing.T) {
	t.Parallel()

	const (
		width    = psf1GlyphWidth
		height   = 1
		charsize = 1
		count    = 2
		badByte  = 0x80
	)

	glyphs := makeGlyphBytes(count, blankFill)
	table := concatBytes(
		[]byte{badByte, psf2Terminator}, // glyph 0: not valid UTF-8, the entry stops immediately
		utf8Entry("B"),                  // glyph 1: table stays readable afterwards
	)

	data := buildPSF2(psf2Fields{
		headerLen: psf2HeaderLen, flags: psf2FlagUnicode,
		count: count, charsize: charsize, height: height, width: width,
	}, glyphs, table)

	font := mustParse(t, data)

	assertGlyphMapped(t, font, 'B', "the glyph after a bad entry should still parse")
}

func TestParsePSF1GlyphCounts(t *testing.T) {
	t.Parallel()

	const charsize = 1

	for _, testCase := range []struct {
		name      string
		mode      int
		wantCount int
	}{
		{name: "256 glyphs, no table", mode: 0, wantCount: glyphCount256},
		{name: "512 glyphs, no table", mode: psf1Mode512Flag, wantCount: glyphCount512},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			glyphs := makeGlyphBytes(testCase.wantCount, blankFill)
			data := buildPSF1(psf1Fields{mode: testCase.mode, charsize: charsize}, glyphs, nil)

			font := mustParse(t, data)

			if got := font.Len(); got != testCase.wantCount {
				t.Errorf("Len() = %d, want %d", got, testCase.wantCount)
			}

			if got := font.Width(); got != psf1GlyphWidth {
				t.Errorf("Width() = %d, want %d", got, psf1GlyphWidth)
			}
		})
	}
}

func TestParsePSF1UnicodeTable(t *testing.T) {
	t.Parallel()

	const charsize = 1

	glyphs := makeGlyphBytes(glyphCount256, blankFill)
	table := fullPSF1Table(glyphCount256, map[int][]byte{0: ucs2Entry([]rune("A"), nil)})

	data := buildPSF1(psf1Fields{mode: psf1TableModeFlag, charsize: charsize}, glyphs, table)

	font := mustParse(t, data)

	assertGlyphMapped(t, font, 'A', "mapped by the table")
	assertGlyphNotMapped(t, font, 'Z', "not mapped by the table")
}

func TestParsePSF1SequenceAndDuplicate(t *testing.T) {
	t.Parallel()

	const charsize = 1

	glyphs := concatBytes(makeGlyphBytes(1, inkFill), makeGlyphBytes(glyphCount256-1, blankFill))

	overrides := map[int][]byte{
		0: ucs2Entry([]rune("A"), []rune("xy")), // 'A' maps here; x, y are a dropped sequence
		1: ucs2Entry([]rune("A"), nil),          // duplicate: glyph 0 already claimed 'A'
	}

	table := fullPSF1Table(glyphCount256, overrides)

	data := buildPSF1(psf1Fields{mode: psf1TableModeFlag, charsize: charsize}, glyphs, table)

	font := mustParse(t, data)

	glyph, ok := font.Glyph('A')
	if !ok {
		t.Fatal("Glyph('A') ok = false, want true")
	}

	if !glyph.Set(0, 0) {
		t.Error("Glyph('A') read glyph 1's bitmap, want glyph 0's (the first mapping should win)")
	}

	assertGlyphNotMapped(t, font, 'x', "inside the dropped sequence")
	assertGlyphNotMapped(t, font, 'y', "inside the dropped sequence")
}

func TestGlyphIndexWithoutTable(t *testing.T) {
	t.Parallel()

	const (
		width    = psf1GlyphWidth
		height   = 1
		charsize = 1
		count    = 4
	)

	data := buildPSF2(psf2Fields{
		headerLen: psf2HeaderLen,
		count:     count, charsize: charsize, height: height, width: width,
	}, makeGlyphBytes(count, blankFill), nil)

	font := mustParse(t, data)

	for _, testCase := range []struct {
		name  string
		value rune
		want  bool
	}{
		{name: "negative rune", value: -1, want: false},
		{name: "first glyph", value: 0, want: true},
		{name: "last glyph", value: count - 1, want: true},
		{name: "at glyph count", value: count, want: false},
		{name: "past glyph count", value: count + 1, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, ok := font.Glyph(testCase.value)
			if ok != testCase.want {
				t.Errorf("Glyph(%d) ok = %v, want %v", testCase.value, ok, testCase.want)
			}
		})
	}
}

func TestGlyphSetBitExact(t *testing.T) {
	t.Parallel()

	const (
		glyphWidth  = 5
		glyphHeight = 3
		msb         = 0x80
	)

	// Row bit patterns, MSB first: row 0 lights columns 0, 2 and 3; row 1
	// lights column 1 only; row 2 is blank. Only the top 5 bits of each
	// byte matter because the glyph is 5 pixels wide.
	rows := []byte{0b1011_0000, 0b0100_0000, 0b0000_0000}

	data := buildPSF2(psf2Fields{
		headerLen: psf2HeaderLen,
		count:     1, charsize: len(rows), height: glyphHeight, width: glyphWidth,
	}, rows, nil)

	font := mustParse(t, data)

	glyph, ok := font.Glyph(0)
	if !ok {
		t.Fatal("Glyph(0) ok = false, want true")
	}

	type pixelCase struct {
		name string
		x    int
		y    int
		want bool
	}

	cases := make([]pixelCase, 0, (glyphHeight+2)*(glyphWidth+2))

	//nolint:varnamelen // x, y is the pixel-addressing idiom used throughout uScope.
	for y := -1; y <= glyphHeight; y++ {
		for x := -1; x <= glyphWidth; x++ {
			want := false
			if x >= 0 && x < glyphWidth && y >= 0 && y < glyphHeight {
				want = rows[y]&(msb>>x) != 0 //nolint:gosec // guarded above, y is always in range here
			}

			cases = append(cases, pixelCase{name: fmt.Sprintf("x=%d,y=%d", x, y), x: x, y: y, want: want})
		}
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := glyph.Set(testCase.x, testCase.y); got != testCase.want {
				t.Errorf("Set(%d, %d) = %v, want %v", testCase.x, testCase.y, got, testCase.want)
			}
		})
	}
}

func TestFallbackMapsReplacementCharacter(t *testing.T) {
	t.Parallel()

	const charsize = 1

	glyphs := concatBytes(makeGlyphBytes(1, inkFill), makeGlyphBytes(1, blankFill))
	table := buildUTF8Table(utf8Entry(string(utf8.RuneError)), utf8Entry("?"))

	data := buildPSF2(psf2Fields{
		headerLen: psf2HeaderLen, flags: psf2FlagUnicode,
		count: 2, charsize: charsize, height: 1, width: psf1GlyphWidth,
	}, glyphs, table)

	font := mustParse(t, data)

	if !font.Fallback().Set(0, 0) {
		t.Error("Fallback().Set(0, 0) = false, want true (the replacement character's glyph)")
	}
}

func TestFallbackMapsQuestionMark(t *testing.T) {
	t.Parallel()

	const charsize = 1

	glyphs := makeGlyphBytes(1, inkFill)
	table := buildUTF8Table(utf8Entry("?"))

	data := buildPSF2(psf2Fields{
		headerLen: psf2HeaderLen, flags: psf2FlagUnicode,
		count: 1, charsize: charsize, height: 1, width: psf1GlyphWidth,
	}, glyphs, table)

	font := mustParse(t, data)

	if !font.Fallback().Set(0, 0) {
		t.Error("Fallback().Set(0, 0) = false, want true (the '?' glyph, since U+FFFD is not mapped)")
	}
}

func TestFallbackMapsNeither(t *testing.T) {
	t.Parallel()

	const charsize = 1

	glyphs := makeGlyphBytes(1, inkFill)
	table := buildUTF8Table(utf8Entry("Q")) // an unrelated rune, so the table exists but names neither fallback

	data := buildPSF2(psf2Fields{
		headerLen: psf2HeaderLen, flags: psf2FlagUnicode,
		count: 1, charsize: charsize, height: 1, width: psf1GlyphWidth,
	}, glyphs, table)

	font := mustParse(t, data)

	if !font.Fallback().Set(0, 0) {
		t.Error("Fallback().Set(0, 0) = false, want true (glyph 0, the last resort)")
	}
}

func TestFallbackZeroGlyphs(t *testing.T) {
	t.Parallel()

	const charsize = 1

	data := buildPSF2(psf2Fields{
		headerLen: psf2HeaderLen,
		count:     0, charsize: charsize, height: 1, width: psf1GlyphWidth,
	}, nil, nil)

	font := mustParse(t, data)
	fallback := font.Fallback()

	for _, testCase := range []struct {
		name string
		x    int
		y    int
	}{
		{name: "origin", x: 0, y: 0},
		{name: "negative x", x: -1, y: 0},
		{name: "negative y", x: 0, y: -1},
		{name: "inside where bounds would be", x: 3, y: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if fallback.Set(testCase.x, testCase.y) {
				t.Errorf("Fallback().Set(%d, %d) = true, want false (font has zero glyphs)", testCase.x, testCase.y)
			}
		})
	}
}

func TestParsePSF2Truncated(t *testing.T) {
	t.Parallel()

	const (
		validCount    = 1
		validCharsize = 1
		validHeight   = 1
		validWidth    = psf1GlyphWidth
	)

	validGlyph := makeGlyphBytes(validCount, blankFill)
	shortEntry := utf8Entry("A")

	for _, testCase := range []struct {
		name string
		data []byte
	}{
		{
			name: "shorter than the 32 byte header",
			data: buildPSF2Raw(psf2Fields{headerLen: psf2HeaderLen - 1}, nil, nil),
		},
		{
			name: "headersize below 32",
			data: buildPSF2Raw(psf2Fields{
				headerLen: psf2HeaderLen, declaredSize: psf2HeaderLen - 1,
				count: validCount, charsize: validCharsize, height: validHeight, width: validWidth,
			}, validGlyph, nil),
		},
		{
			name: "headersize past the end of the data",
			data: buildPSF2Raw(psf2Fields{
				headerLen: psf2HeaderLen, declaredSize: psf2HeaderLen + 1,
				count: validCount, charsize: validCharsize, height: validHeight, width: validWidth,
			}, nil, nil),
		},
		{
			name: "glyph data cut short",
			data: buildPSF2(psf2Fields{
				headerLen: psf2HeaderLen,
				count:     2, charsize: validCharsize, height: validHeight, width: validWidth,
			}, makeGlyphBytes(1, blankFill), nil),
		},
		{
			name: "charsize too small for height rows of stride bytes",
			data: buildPSF2(psf2Fields{
				headerLen: psf2HeaderLen,
				count:     validCount, charsize: 1, height: 2, width: validWidth,
			}, makeGlyphBytes(validCount, blankFill), nil),
		},
		{
			name: "unicode table entry never terminates",
			data: buildPSF2(psf2Fields{
				headerLen: psf2HeaderLen, flags: psf2FlagUnicode,
				count: validCount, charsize: validCharsize, height: validHeight, width: validWidth,
			}, validGlyph, shortEntry[:len(shortEntry)-1]),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wantParseError(t, testCase.data, psf.ErrTruncated)
		})
	}
}

func TestParsePSF1Truncated(t *testing.T) {
	t.Parallel()

	const validCharsize = 1

	fullGlyphs := makeGlyphBytes(glyphCount256, blankFill)
	shortGlyphs := makeGlyphBytes(glyphCount256-1, blankFill)
	fullTable := fullPSF1Table(glyphCount256, nil)

	for _, testCase := range []struct {
		name string
		data []byte
	}{
		{
			name: "shorter than the 4 byte header",
			data: buildPSF1Raw(psf1HeaderLen-1, psf1Fields{}, nil, nil),
		},
		{
			name: "glyph data cut short",
			data: buildPSF1(psf1Fields{charsize: validCharsize}, shortGlyphs, nil),
		},
		{
			name: "table ends mid-entry",
			data: buildPSF1(psf1Fields{mode: psf1TableModeFlag, charsize: validCharsize},
				fullGlyphs, fullTable[:len(fullTable)-1]),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wantParseError(t, testCase.data, psf.ErrTruncated)
		})
	}
}

func TestParsePSF2TooLarge(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		fields psf2Fields
	}{
		{
			name: "glyph count above MaxGlyphs",
			fields: psf2Fields{
				headerLen: psf2HeaderLen,
				count:     psf.MaxGlyphs + 1, charsize: 1, height: 1, width: psf1GlyphWidth,
			},
		},
		{
			name: "charsize 0",
			fields: psf2Fields{
				headerLen: psf2HeaderLen,
				count:     0, charsize: 0, height: 1, width: psf1GlyphWidth,
			},
		},
		{
			name: "charsize above the internal cap",
			fields: psf2Fields{
				headerLen: psf2HeaderLen,
				count:     0, charsize: maxCharsizeCap + 1, height: 1, width: psf1GlyphWidth,
			},
		},
		{
			name: "height 0",
			fields: psf2Fields{
				headerLen: psf2HeaderLen,
				count:     0, charsize: 1, height: 0, width: psf1GlyphWidth,
			},
		},
		{
			name: "height above MaxHeight",
			fields: psf2Fields{
				headerLen: psf2HeaderLen,
				count:     0, charsize: 1, height: psf.MaxHeight + 1, width: psf1GlyphWidth,
			},
		},
		{
			name: "width 0",
			fields: psf2Fields{
				headerLen: psf2HeaderLen,
				count:     0, charsize: 1, height: 1, width: 0,
			},
		},
		{
			name: "width above MaxWidth",
			fields: psf2Fields{
				headerLen: psf2HeaderLen,
				count:     0, charsize: 1, height: 1, width: psf.MaxWidth + 1,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wantParseError(t, buildPSF2(testCase.fields, nil, nil), psf.ErrTooLarge)
		})
	}
}

func TestParsePSF1TooLarge(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		charsize int
	}{
		{name: "charsize 0", charsize: 0},
		{name: "charsize above MaxHeight", charsize: psf.MaxHeight + 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			data := buildPSF1(psf1Fields{charsize: testCase.charsize}, nil, nil)
			wantParseError(t, data, psf.ErrTooLarge)
		})
	}
}

// TestParsePSF2LongerHeader is the reason headerSize is honoured rather than
// assumed: this file claims a 40 byte header, 8 bytes longer than the real
// PSF2 header, and the glyph really does start at byte 40, not byte 32.
func TestParsePSF2LongerHeader(t *testing.T) {
	t.Parallel()

	const (
		headerLen = psf2HeaderLen + 8
		charsize  = 1
		height    = 1
		width     = psf1GlyphWidth
	)

	data := buildPSF2(psf2Fields{
		headerLen: headerLen,
		count:     1, charsize: charsize, height: height, width: width,
	}, makeGlyphBytes(1, inkFill), nil)

	font := mustParse(t, data)

	if got := font.Width(); got != width {
		t.Errorf("Width() = %d, want %d", got, width)
	}

	if got := font.Height(); got != height {
		t.Errorf("Height() = %d, want %d", got, height)
	}

	glyph, ok := font.Glyph(0)
	if !ok {
		t.Fatal("Glyph(0) ok = false, want true")
	}

	if !glyph.Set(0, 0) {
		t.Error("Set(0, 0) = false, want true (glyph should be read from byte 40, not byte 32)")
	}
}

func TestEmbeddedFonts(t *testing.T) {
	t.Parallel()

	const (
		smallWidth  = 6
		smallHeight = 12
		bodyWidth   = psf1GlyphWidth
		bodyHeight  = 16
		largeWidth  = 16
		largeHeight = 32
	)

	for _, testCase := range []struct {
		name       string
		load       func() (*psf.Font, error)
		wantWidth  int
		wantHeight int
	}{
		{name: "Small", load: fonts.Small, wantWidth: smallWidth, wantHeight: smallHeight},
		{name: "Body", load: fonts.Body, wantWidth: bodyWidth, wantHeight: bodyHeight},
		{name: "BodyBold", load: fonts.BodyBold, wantWidth: bodyWidth, wantHeight: bodyHeight},
		{name: "Large", load: fonts.Large, wantWidth: largeWidth, wantHeight: largeHeight},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			font, err := testCase.load()
			if err != nil {
				t.Fatalf("%s: %v", testCase.name, err)
			}

			if got := font.Width(); got != testCase.wantWidth {
				t.Errorf("%s: Width() = %d, want %d", testCase.name, got, testCase.wantWidth)
			}

			if got := font.Height(); got != testCase.wantHeight {
				t.Errorf("%s: Height() = %d, want %d", testCase.name, got, testCase.wantHeight)
			}

			assertHasInk(t, font, 'A', testCase.name)
			assertHasInk(t, font, '0', testCase.name)
			assertNoInk(t, font, ' ', testCase.name)
		})
	}
}
