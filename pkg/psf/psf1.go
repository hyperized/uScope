package psf

import (
	"encoding/binary"
	"fmt"
)

// parsePSF1 reads the original format: four header bytes, 256 or 512 glyphs
// of charsize bytes each, and an optional UCS-2 unicode table.
//
// Everything about PSF1 is implied rather than stated. The glyphs are always
// 8 pixels wide, so charsize doubles as the height, and there is no field
// saying where the table starts because it starts right after the glyphs.
func parsePSF1(data []byte) (*Font, error) {
	if len(data) < psf1HeaderSize {
		return nil, fmt.Errorf("%w: %d bytes is less than a %d byte PSF1 header",
			ErrTruncated, len(data), psf1HeaderSize)
	}

	mode := data[2]

	charsize := int(data[3])
	if charsize < 1 || charsize > MaxHeight {
		return nil, fmt.Errorf("%w: charsize %d is outside 1 to %d", ErrTooLarge, charsize, MaxHeight)
	}

	count := psf1Glyphs
	if mode&psf1Mode512 != 0 {
		count = psf1Glyphs512
	}

	end := psf1HeaderSize + count*charsize
	if end > len(data) {
		return nil, fmt.Errorf("%w: %d glyphs of %d bytes need %d bytes, file has %d",
			ErrTruncated, count, charsize, end, len(data))
	}

	font := &Font{
		data:     data[psf1HeaderSize:end],
		width:    psf1Width,
		height:   charsize,
		stride:   1,
		charsize: charsize,
		count:    count,
	}

	if mode&psf1ModeTable != 0 {
		lookup, err := parseUCS2Table(data[end:], count)
		if err != nil {
			return nil, err
		}

		font.lookup = lookup
	}

	font.fallback = font.fallbackIndex()

	return font, nil
}

// parseUCS2Table reads PSF1's unicode table: per glyph, a run of
// little-endian 16-bit code points ended by 0xFFFF, where 0xFFFE switches the
// rest of the run over to multi-code-point sequences.
//
// Being UCS-2 rather than UTF-16, a PSF1 table cannot name anything above the
// basic multilingual plane at all, so there are no surrogate pairs to worry
// about.
func parseUCS2Table(table []byte, count int) (map[rune]int, error) {
	lookup := make(map[rune]int, count)
	pos := 0

	for index := range count {
		sequence := false

		for {
			if pos+2 > len(table) {
				return nil, fmt.Errorf("%w: unicode table has no terminator for glyph %d", ErrTruncated, index)
			}

			value := binary.LittleEndian.Uint16(table[pos:])
			pos += 2

			if value == psf1Terminator {
				break
			}

			if value == psf1SeqStart {
				sequence = true

				continue
			}

			if _, taken := lookup[rune(value)]; sequence || taken {
				continue
			}

			lookup[rune(value)] = index
		}
	}

	return lookup, nil
}
