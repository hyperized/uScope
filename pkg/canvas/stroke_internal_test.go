package canvas

import (
	"image/color"
	"math"
	"testing"
)

func TestOrientRun(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name           string
		x0, y0, x1, y1 float64
		want           lineRun
	}{
		{
			name: "shallow keeps its axes",
			x0:   0, y0: 0, x1: 10, y1: 5,
			want: lineRun{startMajor: 0, startMinor: 0, gradient: 0.5, firstCol: 0, lastCol: 10, steep: false},
		},
		{
			name: "steep swaps its axes",
			x0:   0, y0: 0, x1: 5, y1: 10,
			want: lineRun{startMajor: 0, startMinor: 0, gradient: 0.5, firstCol: 0, lastCol: 10, steep: true},
		},
		{
			name: "reversed shallow is put in order",
			x0:   10, y0: 5, x1: 0, y1: 0,
			want: lineRun{startMajor: 0, startMinor: 0, gradient: 0.5, firstCol: 0, lastCol: 10, steep: false},
		},
		{
			name: "reversed steep is swapped then put in order",
			x0:   2, y0: 8, x1: 0, y1: 0,
			want: lineRun{startMajor: 0, startMinor: 0, gradient: 0.25, firstCol: 0, lastCol: 8, steep: true},
		},
		{
			name: "falling line has a negative gradient",
			x0:   0, y0: 4, x1: 8, y1: 0,
			want: lineRun{startMajor: 0, startMinor: 4, gradient: -0.5, firstCol: 0, lastCol: 8, steep: false},
		},
		{
			name: "zero length has gradient zero",
			x0:   4.2, y0: 7.8, x1: 4.2, y1: 7.8,
			want: lineRun{startMajor: 4.2, startMinor: 7.8, gradient: 0, firstCol: 4, lastCol: 4, steep: false},
		},
		{
			name: "columns are rounded",
			x0:   0.4, y0: 0, x1: 9.6, y1: 0,
			want: lineRun{startMajor: 0.4, startMinor: 0, gradient: 0, firstCol: 0, lastCol: 10, steep: false},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := orientRun(testCase.x0, testCase.y0, testCase.x1, testCase.y1); got != testCase.want {
				t.Errorf("orientRun(%v, %v, %v, %v) = %+v, want %+v",
					testCase.x0, testCase.y0, testCase.x1, testCase.y1, got, testCase.want)
			}
		})
	}
}

func TestCoverBand(t *testing.T) {
	t.Parallel()

	const (
		major  = 3
		top    = 2.25
		height = 2.0
		// full is the channel value of a fully covered white pixel.
		full = 255.0
	)

	covers := map[int]float64{2: 0.75, 3: 1, 4: 0.25}

	for _, testCase := range []struct {
		name  string
		steep bool
	}{
		{name: "along rows", steep: false},
		{name: "steep puts the band across columns", steep: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := New(8, 8)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			canv.coverBand(major, top, height, testCase.steep, White)

			sum := 0.0

			for minor := range 8 {
				col, row := major, minor
				if testCase.steep {
					col, row = minor, major
				}

				channel := uint8(math.Round(full * covers[minor]))
				want := color.RGBA{}

				if channel > 0 {
					want = color.RGBA{R: channel, G: channel, B: channel, A: opaque}
				}

				assertPixelAt(t, canv, col, row, want)

				sum += float64(canv.img.RGBAAt(col, row).R) / full
			}

			if math.Abs(sum-height) > 3/full {
				t.Errorf("fractions sum to %v, want %v", sum, height)
			}
		})
	}
}

// inSpans reports whether x lies in any of the inclusive ranges.
func inSpans(x int, spans [][2]int) bool {
	for _, span := range spans {
		if x >= span[0] && x <= span[1] {
			return true
		}
	}

	return false
}

func TestAnnulusRow(t *testing.T) {
	t.Parallel()

	const (
		side   = 21
		centre = 10
	)

	for _, testCase := range []struct {
		name         string
		dy           float64
		inner, outer float64
		// lit lists the x ranges, inclusive, expected to be set in the row.
		lit [][2]int
	}{
		{name: "row beyond the outer edge", dy: 5, inner: 0.5, outer: 4.5},
		{name: "row on the outer edge", dy: 5, inner: 0.5, outer: 5},
		{name: "one span outside the inner disc", dy: 4, inner: 2.5, outer: 4.5, lit: [][2]int{{8, 12}}},
		{name: "two spans through the inner disc", dy: 0, inner: 2.5, outer: 4.5, lit: [][2]int{{6, 7}, {13, 14}}},
		{name: "no pixel centre between the edges", dy: 0, inner: 4.4, outer: 4.5},
		{
			name: "an inner edge inside the centre leaves no hole", dy: 0, inner: -0.5, outer: 2.5,
			lit: [][2]int{{8, 12}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			canv, err := New(side, side)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			canv.annulusRow(centre, centre, testCase.dy, testCase.inner, testCase.outer, White)

			for col := range side {
				for row := range side {
					want := row == centre && inSpans(col, testCase.lit)

					if got := canv.img.RGBAAt(col, row) == White; got != want {
						t.Errorf("pixel (%d, %d): set = %v, want %v", col, row, got, want)
					}
				}
			}
		})
	}
}
