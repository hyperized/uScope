package radar_test

import (
	"errors"
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/radar"
)

// TestParseBounds covers the four parsers the flags and the control port
// share: both edges of every range, the first value past each edge, NaN, the
// infinities, and text that is not a number at all.
func TestParseBounds(t *testing.T) {
	t.Parallel()

	floats := []struct {
		name    string
		parse   func(string) (float64, error)
		input   string
		want    float64
		wantErr error
	}{
		{name: "tilt at the floor", parse: radar.ParseTilt, input: "10", want: radar.MinTilt},
		{name: "tilt at the ceiling", parse: radar.ParseTilt, input: "80", want: radar.MaxTilt},
		{name: "tilt with a fraction", parse: radar.ParseTilt, input: "22.5", want: 22.5},
		{name: "tilt under the floor", parse: radar.ParseTilt, input: "9.9", wantErr: radar.ErrTilt},
		{name: "tilt over the ceiling", parse: radar.ParseTilt, input: "80.1", wantErr: radar.ErrTilt},
		{name: "tilt NaN", parse: radar.ParseTilt, input: "NaN", wantErr: radar.ErrTilt},
		{name: "tilt text", parse: radar.ParseTilt, input: "steep", wantErr: radar.ErrTilt},
		{name: "exaggerate at the floor", parse: radar.ParseExaggerate, input: "1", want: radar.MinExaggerate},
		{name: "exaggerate at the ceiling", parse: radar.ParseExaggerate, input: "20", want: radar.MaxExaggerate},
		{name: "exaggerate past the ceiling", parse: radar.ParseExaggerate, input: "21", wantErr: radar.ErrExaggerate},
		{name: "exaggerate infinite", parse: radar.ParseExaggerate, input: "Inf", wantErr: radar.ErrExaggerate},
		{name: "azimuth north", parse: radar.ParseAzimuth, input: "0", want: 0},
		{name: "azimuth just short of north", parse: radar.ParseAzimuth, input: "359.9", want: 359.9},
		{name: "azimuth 360 is refused", parse: radar.ParseAzimuth, input: "360", wantErr: radar.ErrAzimuth},
		{name: "azimuth negative", parse: radar.ParseAzimuth, input: "-1", wantErr: radar.ErrAzimuth},
		{name: "azimuth NaN", parse: radar.ParseAzimuth, input: "NaN", wantErr: radar.ErrAzimuth},
		{name: "azimuth text", parse: radar.ParseAzimuth, input: "east", wantErr: radar.ErrAzimuth},
	}

	for _, testCase := range floats {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := testCase.parse(testCase.input)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("parse(%q) error = %v, want %v", testCase.input, err, testCase.wantErr)
			}

			if got != testCase.want {
				t.Errorf("parse(%q) = %g, want %g", testCase.input, got, testCase.want)
			}
		})
	}
}

// TestParseZoom covers the integer parser on the same terms.
func TestParseZoom(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		input   string
		want    int
		wantErr error
	}{
		{name: "the fit", input: "0", want: 0},
		{name: "tightest", input: "5", want: radar.MaxZoom},
		{name: "widest", input: "-5", want: radar.MinZoom},
		{name: "too tight", input: "6", wantErr: radar.ErrZoom},
		{name: "too wide", input: "-6", wantErr: radar.ErrZoom},
		{name: "a fraction", input: "1.5", wantErr: radar.ErrZoom},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := radar.ParseZoom(testCase.input)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("ParseZoom(%q) error = %v, want %v", testCase.input, err, testCase.wantErr)
			}

			if got != testCase.want {
				t.Errorf("ParseZoom(%q) = %d, want %d", testCase.input, got, testCase.want)
			}
		})
	}
}

// TestCheckBounds covers the checks on their own, the half the flag parser
// calls with a number the flag package already read: both edges of every
// range, the first value past each edge, NaN and the infinities.
func TestCheckBounds(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "zoom the fit", err: radar.CheckZoom(0)},
		{name: "zoom tightest", err: radar.CheckZoom(radar.MaxZoom)},
		{name: "zoom widest", err: radar.CheckZoom(radar.MinZoom)},
		{name: "zoom too tight", err: radar.CheckZoom(radar.MaxZoom + 1), wantErr: radar.ErrZoom},
		{name: "zoom too wide", err: radar.CheckZoom(radar.MinZoom - 1), wantErr: radar.ErrZoom},
		{name: "tilt at the floor", err: radar.CheckTilt(radar.MinTilt)},
		{name: "tilt at the ceiling", err: radar.CheckTilt(radar.MaxTilt)},
		{name: "tilt with a fraction", err: radar.CheckTilt(22.5)},
		{name: "tilt under the floor", err: radar.CheckTilt(9.9), wantErr: radar.ErrTilt},
		{name: "tilt over the ceiling", err: radar.CheckTilt(80.1), wantErr: radar.ErrTilt},
		{name: "tilt NaN", err: radar.CheckTilt(math.NaN()), wantErr: radar.ErrTilt},
		{name: "exaggerate at the floor", err: radar.CheckExaggerate(radar.MinExaggerate)},
		{name: "exaggerate at the ceiling", err: radar.CheckExaggerate(radar.MaxExaggerate)},
		{name: "exaggerate under", err: radar.CheckExaggerate(0.5), wantErr: radar.ErrExaggerate},
		{name: "exaggerate infinite", err: radar.CheckExaggerate(math.Inf(1)), wantErr: radar.ErrExaggerate},
		{name: "azimuth north", err: radar.CheckAzimuth(radar.MinAzimuth)},
		{name: "azimuth just short of north", err: radar.CheckAzimuth(359.9)},
		{name: "azimuth at the open end", err: radar.CheckAzimuth(radar.MaxAzimuth), wantErr: radar.ErrAzimuth},
		{name: "azimuth negative", err: radar.CheckAzimuth(-1), wantErr: radar.ErrAzimuth},
		{name: "azimuth NaN", err: radar.CheckAzimuth(math.NaN()), wantErr: radar.ErrAzimuth},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if !errors.Is(testCase.err, testCase.wantErr) {
				t.Errorf("error = %v, want %v", testCase.err, testCase.wantErr)
			}
		})
	}
}
