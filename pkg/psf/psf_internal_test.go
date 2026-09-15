package psf

import (
	"errors"
	"testing"
)

func TestStartsWith(t *testing.T) {
	t.Parallel()

	const magic = "\x72\xb5\x4a\x86"

	for _, testCase := range []struct {
		name string
		data []byte
		want bool
	}{
		{name: "empty data", data: nil, want: false},
		{name: "shorter than magic", data: []byte{0x72, 0xb5}, want: false},
		{name: "matches", data: []byte(magic), want: true},
		{name: "same length, wrong bytes", data: []byte{0x00, 0x00, 0x00, 0x00}, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := startsWith(testCase.data, magic); got != testCase.want {
				t.Errorf("startsWith(%v, %q) = %v, want %v", testCase.data, magic, got, testCase.want)
			}
		})
	}
}

func TestBounded(t *testing.T) {
	t.Parallel()

	const (
		low  = 4
		high = 10
	)

	for _, testCase := range []struct {
		name    string
		raw     uint32
		wantErr bool
		want    int
	}{
		{name: "below low bound", raw: low - 1, wantErr: true},
		{name: "above high bound", raw: high + 1, wantErr: true},
		{name: "at low bound", raw: low, want: low},
		{name: "at high bound", raw: high, want: high},
		{name: "in range", raw: low + 1, want: low + 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := bounded(testCase.raw, "value", low, high)

			if testCase.wantErr {
				if !errors.Is(err, ErrTooLarge) {
					t.Errorf("bounded(%d) error = %v, want ErrTooLarge", testCase.raw, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("bounded(%d): %v", testCase.raw, err)
			}

			if got != testCase.want {
				t.Errorf("bounded(%d) = %d, want %d", testCase.raw, got, testCase.want)
			}
		})
	}
}

func TestLe32(t *testing.T) {
	t.Parallel()

	const offset = 4

	data := []byte{0x00, 0x00, 0x00, 0x00, 0x78, 0x56, 0x34, 0x12}

	const want = 0x12345678

	if got := le32(data, offset); got != want {
		t.Errorf("le32(data, %d) = %#x, want %#x", offset, got, want)
	}
}

func TestGlyphAt(t *testing.T) {
	t.Parallel()

	font := &Font{
		data:     []byte{0xaa, 0xbb},
		width:    8,
		height:   1,
		stride:   1,
		charsize: 1,
		count:    2,
	}

	for _, testCase := range []struct {
		name  string
		index int
	}{
		{name: "negative index", index: -1},
		{name: "index at count", index: font.count},
		{name: "index past count", index: font.count + 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := font.glyphAt(testCase.index)
			if got.bits != nil || got.width != 0 || got.height != 0 || got.stride != 0 {
				t.Errorf("glyphAt(%d) = %+v, want the blank Glyph", testCase.index, got)
			}
		})
	}
}

func TestIndexOfWithoutLookup(t *testing.T) {
	t.Parallel()

	font := &Font{count: 4}

	for _, testCase := range []struct {
		name    string
		value   rune
		wantIdx int
		wantOk  bool
	}{
		{name: "negative rune", value: -1, wantOk: false},
		{name: "in range", value: 2, wantIdx: 2, wantOk: true},
		{name: "out of range", value: 4, wantOk: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotIdx, gotOk := font.indexOf(testCase.value)
			if gotOk != testCase.wantOk {
				t.Errorf("indexOf(%d) ok = %v, want %v", testCase.value, gotOk, testCase.wantOk)
			}

			if gotOk && gotIdx != testCase.wantIdx {
				t.Errorf("indexOf(%d) = %d, want %d", testCase.value, gotIdx, testCase.wantIdx)
			}
		})
	}
}

func TestIndexOfWithLookup(t *testing.T) {
	t.Parallel()

	font := &Font{lookup: map[rune]int{'A': 3}}

	for _, testCase := range []struct {
		name    string
		value   rune
		wantIdx int
		wantOk  bool
	}{
		{name: "hit", value: 'A', wantIdx: 3, wantOk: true},
		{name: "miss", value: 'B', wantOk: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotIdx, gotOk := font.indexOf(testCase.value)
			if gotOk != testCase.wantOk {
				t.Errorf("indexOf(%q) ok = %v, want %v", testCase.value, gotOk, testCase.wantOk)
			}

			if gotOk && gotIdx != testCase.wantIdx {
				t.Errorf("indexOf(%q) = %d, want %d", testCase.value, gotIdx, testCase.wantIdx)
			}
		})
	}
}

// TestFallbackIndexEmptyFont covers the one branch external tests cannot
// reach through Parse: a Font with no glyphs at all and no unicode table,
// which only a hand-built zero value can produce.
func TestFallbackIndexEmptyFont(t *testing.T) {
	t.Parallel()

	font := &Font{}

	if got := font.fallbackIndex(); got != -1 {
		t.Errorf("fallbackIndex() = %d, want -1", got)
	}
}

func TestRecordUTF8(t *testing.T) {
	t.Parallel()

	t.Run("duplicate codepoint keeps the first", func(t *testing.T) {
		t.Parallel()

		const firstGlyph = 0

		lookup := map[rune]int{'A': firstGlyph}

		recordUTF8(lookup, []byte("A"), firstGlyph+1)

		if got := lookup['A']; got != firstGlyph {
			t.Errorf("lookup['A'] = %d, want %d (first glyph kept)", got, firstGlyph)
		}
	})

	t.Run("invalid first byte records nothing", func(t *testing.T) {
		t.Parallel()

		const invalidByte = 0x80

		lookup := map[rune]int{}

		recordUTF8(lookup, []byte{invalidByte}, 0)

		if len(lookup) != 0 {
			t.Errorf("lookup = %v, want empty", lookup)
		}
	})
}

func TestParseUTF8TableNoTerminator(t *testing.T) {
	t.Parallel()

	_, err := parseUTF8Table([]byte("A"), 1)
	if !errors.Is(err, ErrTruncated) {
		t.Errorf("parseUTF8Table() error = %v, want ErrTruncated", err)
	}
}

func TestParseUCS2TableNoTerminator(t *testing.T) {
	t.Parallel()

	table := []byte{'A', 0x00} // 'A' as a little-endian uint16, then nothing more

	_, err := parseUCS2Table(table, 1)
	if !errors.Is(err, ErrTruncated) {
		t.Errorf("parseUCS2Table() error = %v, want ErrTruncated", err)
	}
}
