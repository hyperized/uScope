package airlines

import (
	"errors"
	"image/color"
	"strings"
	"testing"
)

const testHeader = "icao,name,hex,source,group,note\n"

// wantKLM and caseTooShort are pulled out because goconst flags the same
// string literal repeated across the table-driven cases below.
const (
	wantKLM      = "KLM"
	caseTooShort = "too short"
)

// runParseCases is the shared body for every parse error table below: parse
// testCase.csv and require errors.Is against testCase.wantErr.
func runParseCases(t *testing.T, cases []struct {
	name    string
	csv     string
	wantErr error
},
) {
	t.Helper()

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := parse(strings.NewReader(testCase.csv))
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("parse() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestParseHeaderErrors(t *testing.T) {
	t.Parallel()

	runParseCases(t, []struct {
		name    string
		csv     string
		wantErr error
	}{
		{
			name:    "wrong header",
			csv:     "icao,name,hex,source,grp,note\n" + "KLM,KLM,00A1DE,livery,,\n",
			wantErr: ErrHeader,
		},
		{
			name:    "missing header",
			csv:     "KLM,KLM,00A1DE,livery,,\n",
			wantErr: ErrHeader,
		},
		{
			name:    "header row has the wrong number of fields",
			csv:     "icao,name,hex,source,group\n",
			wantErr: ErrFieldCount,
		},
		{
			name:    "empty file",
			csv:     "",
			wantErr: ErrEmpty,
		},
		{
			name:    "header only",
			csv:     testHeader,
			wantErr: ErrEmpty,
		},
	})
}

func TestParseRowShapeErrors(t *testing.T) {
	t.Parallel()

	runParseCases(t, []struct {
		name    string
		csv     string
		wantErr error
	}{
		{
			name:    "short row",
			csv:     testHeader + "KLM,KLM,00A1DE,livery,\n",
			wantErr: ErrFieldCount,
		},
		{
			name:    "long row",
			csv:     testHeader + "KLM,KLM,00A1DE,livery,,,extra\n",
			wantErr: ErrFieldCount,
		},
		{
			name:    "two-letter code",
			csv:     testHeader + "KL,KLM,00A1DE,livery,,\n",
			wantErr: ErrCode,
		},
		{
			name:    "lowercase code",
			csv:     testHeader + "klm,KLM,00A1DE,livery,,\n",
			wantErr: ErrCode,
		},
		{
			name:    "code with a digit",
			csv:     testHeader + "KL1,KLM,00A1DE,livery,,\n",
			wantErr: ErrCode,
		},
		{
			name: "duplicate code",
			csv: testHeader +
				"KLM,KLM,00A1DE,livery,,\n" +
				"KLM,KLM Cityhopper,00A1DE,livery,,\n",
			wantErr: ErrDuplicate,
		},
	})
}

func TestParseColumnErrors(t *testing.T) {
	t.Parallel()

	runParseCases(t, []struct {
		name    string
		csv     string
		wantErr error
	}{
		{
			name:    "five-digit hex",
			csv:     testHeader + "KLM,KLM,0A1DE,livery,,\n",
			wantErr: ErrHex,
		},
		{
			name:    "seven-digit hex",
			csv:     testHeader + "KLM,KLM,00A1DEE,livery,,\n",
			wantErr: ErrHex,
		},
		{
			name:    "lowercase hex",
			csv:     testHeader + "KLM,KLM,00a1de,livery,,\n",
			wantErr: ErrHex,
		},
		{
			name:    "hex with a non-hex character",
			csv:     testHeader + "KLM,KLM,00A1DG,livery,,\n",
			wantErr: ErrHex,
		},
		{
			name:    "unknown source",
			csv:     testHeader + "KLM,KLM,00A1DE,unknown,,\n",
			wantErr: ErrSource,
		},
		{
			name:    "empty name",
			csv:     testHeader + "KLM,,00A1DE,livery,,\n",
			wantErr: ErrName,
		},
	})
}

func TestParseGroupErrors(t *testing.T) {
	t.Parallel()

	runParseCases(t, []struct {
		name    string
		csv     string
		wantErr error
	}{
		{
			name:    "group points at a code that does not exist",
			csv:     testHeader + "KLC,KLM Cityhopper,00A1DE,livery,ZZZ,\n",
			wantErr: ErrGroup,
		},
		{
			name:    "group is not a well-formed code",
			csv:     testHeader + "KLC,KLM Cityhopper,00A1DE,livery,zz,\n",
			wantErr: ErrGroup,
		},
	})
}

// TestParseHappyPath checks the one case that must not fail: a well-formed
// file including a row whose group resolves to another row in the same
// file.
func TestParseHappyPath(t *testing.T) {
	t.Parallel()

	const csvData = testHeader +
		"KLM,KLM Royal Dutch Airlines,00A1DE,livery,,KLM light blue fuselage\n" +
		"KLC,KLM Cityhopper,00A1DE,livery,KLM,KLM group livery\n"

	byCode, err := parse(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("parse() unexpected error: %v", err)
	}

	if len(byCode) != 2 {
		t.Fatalf("parse() returned %d airlines, want 2", len(byCode))
	}

	klc, ok := byCode[[3]byte{'K', 'L', 'C'}]
	if !ok {
		t.Fatal("parse() result is missing KLC")
	}

	if klc.Group != wantKLM {
		t.Errorf("KLC.Group = %q, want KLM", klc.Group)
	}

	want := color.RGBA{R: 0x00, G: 0xA1, B: 0xDE, A: maxChannel}
	if klc.Color != want {
		t.Errorf("KLC.Color = %v, want %v", klc.Color, want)
	}
}

// TestDatabaseLoad drives load against a malformed raw. The second call
// proves sync.Once replays the first outcome instead of reparsing.
func TestDatabaseLoad(t *testing.T) {
	t.Parallel()

	store := &database{raw: []byte("a,b,c,d,e,f\n")}

	if err := store.load(); !errors.Is(err, ErrHeader) {
		t.Fatalf("load() error = %v, want ErrHeader", err)
	}

	if err := store.load(); !errors.Is(err, ErrHeader) {
		t.Fatalf("second load() error = %v, want ErrHeader", err)
	}
}

// TestDatabaseDegradesOnFailure checks that every accessor behaves as if the
// database were empty once load has failed, rather than panicking on a nil
// byCode map.
func TestDatabaseDegradesOnFailure(t *testing.T) {
	t.Parallel()

	store := &database{raw: []byte("")}

	if _, ok := store.lookup("KLM123"); ok {
		t.Error("lookup() on a failed database returned ok = true")
	}

	if _, ok := store.byICAO(wantKLM); ok {
		t.Error("byICAO() on a failed database returned ok = true")
	}

	if got := store.count(); got != 0 {
		t.Errorf("count() = %d, want 0", got)
	}

	if got := store.all(); len(got) != 0 {
		t.Errorf("all() = %v, want empty", got)
	}
}

// TestDatabaseLookup drives every branch of lookup against a small
// synthetic database: too short, a lowercase prefix, an unknown code and a
// match.
func TestDatabaseLookup(t *testing.T) {
	t.Parallel()

	store := &database{raw: []byte(testHeader + "KLM,KLM Royal Dutch Airlines,00A1DE,livery,,\n")}

	for _, testCase := range []struct {
		name     string
		callsign string
		wantOK   bool
	}{
		{name: caseTooShort, callsign: "KL", wantOK: false},
		{name: "lowercase prefix", callsign: "klm123", wantOK: false},
		{name: "unknown code", callsign: "XXX123", wantOK: false},
		{name: "match with flight number", callsign: "KLM123", wantOK: true},
		{name: "bare designator", callsign: wantKLM, wantOK: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			airline, ok := store.lookup(testCase.callsign)
			if ok != testCase.wantOK {
				t.Fatalf("lookup(%q) ok = %v, want %v", testCase.callsign, ok, testCase.wantOK)
			}

			if ok && airline.ICAO != wantKLM {
				t.Errorf("lookup(%q).ICAO = %q, want KLM", testCase.callsign, airline.ICAO)
			}
		})
	}
}

// TestDatabaseByICAO drives every branch of byICAO the same way TestDatabaseLookup
// does for lookup.
func TestDatabaseByICAO(t *testing.T) {
	t.Parallel()

	store := &database{raw: []byte(testHeader + "KLM,KLM Royal Dutch Airlines,00A1DE,livery,,\n")}

	for _, testCase := range []struct {
		name   string
		code   string
		wantOK bool
	}{
		{name: caseTooShort, code: "KL", wantOK: false},
		{name: "unknown code", code: "XXX", wantOK: false},
		{name: "present code", code: wantKLM, wantOK: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			airline, ok := store.byICAO(testCase.code)
			if ok != testCase.wantOK {
				t.Fatalf("byICAO(%q) ok = %v, want %v", testCase.code, ok, testCase.wantOK)
			}

			if ok && airline.ICAO != wantKLM {
				t.Errorf("byICAO(%q).ICAO = %q, want KLM", testCase.code, airline.ICAO)
			}
		})
	}
}

func TestCodeKey(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		value  string
		wantOK bool
	}{
		{name: caseTooShort, value: "KL", wantOK: false},
		{name: "too long", value: "KLMX", wantOK: false},
		{name: "lowercase", value: "klm", wantOK: false},
		{name: "contains a digit", value: "KL1", wantOK: false},
		{name: "valid", value: wantKLM, wantOK: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			key, ok := codeKey(testCase.value)
			if ok != testCase.wantOK {
				t.Fatalf("codeKey(%q) ok = %v, want %v", testCase.value, ok, testCase.wantOK)
			}

			if ok && key != ([3]byte{testCase.value[0], testCase.value[1], testCase.value[2]}) {
				t.Errorf("codeKey(%q) = %v, want the three input bytes", testCase.value, key)
			}
		})
	}
}

func TestParseHex(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		value  string
		wantOK bool
		want   color.RGBA
	}{
		{name: caseTooShort, value: "A1DE", wantOK: false},
		{name: "too long", value: "00A1DE00", wantOK: false},
		{name: "lowercase", value: "00a1de", wantOK: false},
		{name: "non-hex character", value: "00A1DG", wantOK: false},
		{
			name: "valid", value: "00A1DE", wantOK: true,
			want: color.RGBA{R: 0x00, G: 0xA1, B: 0xDE, A: maxChannel},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseHex(testCase.value)
			if ok != testCase.wantOK {
				t.Fatalf("parseHex(%q) ok = %v, want %v", testCase.value, ok, testCase.wantOK)
			}

			if ok && got != testCase.want {
				t.Errorf("parseHex(%q) = %v, want %v", testCase.value, got, testCase.want)
			}
		})
	}
}

func TestHexDigit(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		digit  byte
		wantOK bool
		want   uint8
	}{
		{name: "digit", digit: '7', wantOK: true, want: 7},
		{name: "uppercase letter", digit: 'C', wantOK: true, want: 12},
		{name: "lowercase letter", digit: 'c', wantOK: false},
		{name: "punctuation", digit: '#', wantOK: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := hexDigit(testCase.digit)
			if ok != testCase.wantOK {
				t.Fatalf("hexDigit(%q) ok = %v, want %v", testCase.digit, ok, testCase.wantOK)
			}

			if ok && got != testCase.want {
				t.Errorf("hexDigit(%q) = %d, want %d", testCase.digit, got, testCase.want)
			}
		})
	}
}

func TestValidSource(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		source string
		want   bool
	}{
		{name: "simple-icons", source: "simple-icons", want: true},
		{name: "brand", source: "brand", want: true},
		{name: "livery", source: "livery", want: true},
		{name: "unknown", source: "wikipedia", want: false},
		{name: "empty", source: "", want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := validSource(testCase.source); got != testCase.want {
				t.Errorf("validSource(%q) = %v, want %v", testCase.source, got, testCase.want)
			}
		})
	}
}

func TestLinearize(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		channel float64
		want    float64
	}{
		{name: "below the gamma threshold", channel: 0, want: 0},
		{name: "above the gamma threshold", channel: 1, want: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const tolerance = 1e-9

			got := linearize(testCase.channel)
			if diff := got - testCase.want; diff > tolerance || diff < -tolerance {
				t.Errorf("linearize(%v) = %v, want %v", testCase.channel, got, testCase.want)
			}
		})
	}
}

func TestRelativeLuminance(t *testing.T) {
	t.Parallel()

	white := relativeLuminance(color.RGBA{R: maxChannel, G: maxChannel, B: maxChannel, A: maxChannel})

	const tolerance = 1e-9
	if diff := white - 1; diff > tolerance || diff < -tolerance {
		t.Errorf("relativeLuminance(white) = %v, want 1", white)
	}

	black := relativeLuminance(color.RGBA{A: maxChannel})
	if black != 0 {
		t.Errorf("relativeLuminance(black) = %v, want 0", black)
	}
}

// TestRGBToHSL covers the grayscale shortcut and all three hue branches
// (red, green and blue each the largest channel).
func TestRGBToHSL(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		col  color.RGBA
	}{
		{name: "grayscale", col: color.RGBA{R: 128, G: 128, B: 128, A: maxChannel}},
		{name: "red dominant, light", col: color.RGBA{R: 220, G: 40, B: 40, A: maxChannel}},
		{name: "red dominant, blue above green", col: color.RGBA{R: 200, G: 20, B: 60, A: maxChannel}},
		{name: "green dominant", col: color.RGBA{R: 20, G: 200, B: 40, A: maxChannel}},
		{name: "blue dominant", col: color.RGBA{R: 20, G: 40, B: 200, A: maxChannel}},
		{name: "dark, saturation branch above 0.5", col: color.RGBA{R: 10, G: 200, B: 10, A: maxChannel}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			hue, saturation, lightness := rgbToHSL(testCase.col)

			for _, v := range []float64{hue, saturation, lightness} {
				if v < 0 || v > 1 {
					t.Errorf("rgbToHSL(%v) = (%v, %v, %v), want all three in [0, 1]",
						testCase.col, hue, saturation, lightness)
				}
			}
		})
	}
}

// TestHSLToRGB covers the zero-saturation shortcut and both branches of the
// lightness split (below and at/above the midpoint).
func TestHSLToRGB(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name                       string
		hue, saturation, lightness float64
	}{
		{name: "zero saturation", hue: 0, saturation: 0, lightness: 0.5},
		{name: "lightness below midpoint", hue: 0.6, saturation: 0.5, lightness: 0.3},
		{name: "lightness at or above midpoint", hue: 0.6, saturation: 0.5, lightness: 0.7},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			red, green, blue := hslToRGB(testCase.hue, testCase.saturation, testCase.lightness)
			if testCase.saturation == 0 && (red != green || green != blue) {
				t.Errorf("hslToRGB(%v, %v, %v) = (%d, %d, %d), want equal channels for zero saturation",
					testCase.hue, testCase.saturation, testCase.lightness, red, green, blue)
			}
		})
	}
}

// TestHueToChannel drives every branch: both wraparound adjustments and all
// four segments of the piecewise formula.
func TestHueToChannel(t *testing.T) {
	t.Parallel()

	const (
		low  = 0.2
		high = 0.8
	)

	for _, testCase := range []struct {
		name      string
		hueOffset float64
	}{
		{name: "wraps up from negative", hueOffset: -0.1},
		{name: "wraps down from above one", hueOffset: 1.1},
		{name: "first sixth", hueOffset: 0.1},
		{name: "second segment, returns high", hueOffset: 0.3},
		{name: "third segment", hueOffset: 0.6},
		{name: "final segment, returns low", hueOffset: 0.9},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := hueToChannel(low, high, testCase.hueOffset)
			if got < low-1e-9 || got > high+1e-9 {
				t.Errorf("hueToChannel(%v, %v, %v) = %v, want within [%v, %v]",
					low, high, testCase.hueOffset, got, low, high)
			}
		})
	}
}

func TestClampChannel(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		fraction float64
		want     uint8
	}{
		{name: "below zero clamps to zero", fraction: -0.5, want: 0},
		{name: "above one clamps to max", fraction: 1.5, want: maxChannel},
		{name: "midpoint rounds", fraction: 0.5, want: 128},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := clampChannel(testCase.fraction); got != testCase.want {
				t.Errorf("clampChannel(%v) = %d, want %d", testCase.fraction, got, testCase.want)
			}
		})
	}
}
