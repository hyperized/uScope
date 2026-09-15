package airlines_test

import (
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/pkg/airlines"
)

// wantKLM is pulled out because goconst flags the same string literal
// repeated across the table-driven cases below.
const wantKLM = "KLM"

func TestLoad(t *testing.T) {
	t.Parallel()

	if err := airlines.Load(); err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		callsign string
		wantICAO string
		wantOK   bool
	}{
		{name: "KLM with flight number", callsign: "KLM123", wantICAO: wantKLM, wantOK: true},
		{name: "easyJet with flight number", callsign: "EZY45AB", wantICAO: "EZY", wantOK: true},
		{name: "Lufthansa with flight number", callsign: "DLH4EA", wantICAO: "DLH", wantOK: true},
		// Deliberate: callsigns heard over the air are always uppercase, so
		// folding case here would silently hide an upstream decoder bug
		// instead of surfacing it.
		{name: "lowercase is not folded", callsign: "klm123", wantOK: false},
		{name: "unknown designator", callsign: "N123AB", wantOK: false},
		{name: "empty string", callsign: "", wantOK: false},
		{name: "shorter than three bytes", callsign: "KL", wantOK: false},
		{name: "bare designator with no flight number", callsign: wantKLM, wantICAO: wantKLM, wantOK: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := airlines.Lookup(testCase.callsign)
			if ok != testCase.wantOK {
				t.Fatalf("Lookup(%q) ok = %v, want %v", testCase.callsign, ok, testCase.wantOK)
			}

			if ok && got.ICAO != testCase.wantICAO {
				t.Errorf("Lookup(%q).ICAO = %q, want %q", testCase.callsign, got.ICAO, testCase.wantICAO)
			}
		})
	}
}

func TestByICAO(t *testing.T) {
	t.Parallel()

	if _, ok := airlines.ByICAO(wantKLM); !ok {
		t.Error("ByICAO(\"KLM\") ok = false, want true")
	}

	if _, ok := airlines.ByICAO("ZZZ"); ok {
		t.Error("ByICAO(\"ZZZ\") ok = true, want false")
	}
}

// TestGrouping checks the two group relationships the brief guarantees, and
// that every group column in the whole file resolves through ByICAO.
func TestGrouping(t *testing.T) {
	t.Parallel()

	klc, found := airlines.ByICAO("KLC")
	if !found {
		t.Fatal("ByICAO(\"KLC\") ok = false, want true")
	}

	if klc.Group != wantKLM {
		t.Errorf("KLC.Group = %q, want KLM", klc.Group)
	}

	eju, ok := airlines.ByICAO("EJU")
	if !ok {
		t.Fatal("ByICAO(\"EJU\") ok = false, want true")
	}

	if eju.Group != "EZY" {
		t.Errorf("EJU.Group = %q, want EZY", eju.Group)
	}

	for _, airline := range airlines.All() {
		if airline.Group == "" {
			continue
		}

		if _, ok := airlines.ByICAO(airline.Group); !ok {
			t.Errorf("%s.Group = %q, which ByICAO does not resolve", airline.ICAO, airline.Group)
		}
	}
}

// TestAllRows checks structural invariants that must hold for every row
// regardless of what the data file currently contains: a well-formed ICAO,
// a name, full alpha, and a source from the allow list.
func TestAllRows(t *testing.T) {
	t.Parallel()

	allowedSources := map[string]bool{
		"simple-icons": true,
		"brand":        true,
		"livery":       true,
	}

	const icaoLength = 3

	for _, airline := range airlines.All() {
		if len(airline.ICAO) != icaoLength {
			t.Errorf("ICAO %q has length %d, want %d", airline.ICAO, len(airline.ICAO), icaoLength)
		}

		for _, b := range []byte(airline.ICAO) {
			if b < 'A' || b > 'Z' {
				t.Errorf("ICAO %q contains a byte outside A-Z", airline.ICAO)

				break
			}
		}

		if airline.Name == "" {
			t.Errorf("%s has an empty Name", airline.ICAO)
		}

		if airline.Color.A != 0xFF {
			t.Errorf("%s.Color.A = %#x, want 0xff", airline.ICAO, airline.Color.A)
		}

		if !allowedSources[airline.Source] {
			t.Errorf("%s.Source = %q, not in the allow list", airline.ICAO, airline.Source)
		}
	}
}

func TestAllSortedAndCounted(t *testing.T) {
	t.Parallel()

	all := airlines.All()

	if len(all) != airlines.Count() {
		t.Fatalf("len(All()) = %d, Count() = %d, want equal", len(all), airlines.Count())
	}

	for i := 1; i < len(all); i++ {
		if all[i-1].ICAO >= all[i].ICAO {
			t.Fatalf("All() not sorted ascending at index %d: %q >= %q", i, all[i-1].ICAO, all[i].ICAO)
		}
	}
}

// TestAllReturnsFreshSlice checks that mutating one call's result cannot
// affect another: All must hand back package state's contents, never the
// state itself.
func TestAllReturnsFreshSlice(t *testing.T) {
	t.Parallel()

	first := airlines.All()
	if len(first) == 0 {
		t.Fatal("All() returned no airlines")
	}

	second := airlines.All()

	original := first[0].Name
	first[0].Name = "mutated"

	if second[0].Name != original {
		t.Errorf("mutating the first All() result changed the second: got %q, want %q",
			second[0].Name, original)
	}
}

// onDarkTolerance is how many units per channel the OnDark tests below
// allow for rounding, both against a hand-computed target and between two
// applications of OnDark in the idempotency check.
const onDarkTolerance = 1

func TestOnDarkWhiteIsUnchanged(t *testing.T) {
	t.Parallel()

	white := airlines.Airline{Color: color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}}

	got := white.OnDark()
	if got != white.Color {
		t.Errorf("OnDark(white) = %v, want %v unchanged", got, white.Color)
	}
}

func TestOnDarkBlackLiftsToNeutralGrey(t *testing.T) {
	t.Parallel()

	black := airlines.Airline{Color: color.RGBA{A: 0xFF}}

	got := black.OnDark()
	if got.R != got.G || got.G != got.B {
		t.Fatalf("OnDark(black) = %v, want equal R, G, B", got)
	}

	const wantNear = 118

	if diff := int(got.R) - wantNear; diff > onDarkTolerance || diff < -onDarkTolerance {
		t.Errorf("OnDark(black) channel = %d, want within %d of %d", got.R, onDarkTolerance, wantNear)
	}
}

func TestOnDarkAboveFloorIsUnchanged(t *testing.T) {
	t.Parallel()

	easyJetOrange := airlines.Airline{Color: color.RGBA{R: 0xFF, G: 0x66, B: 0x00, A: 0xFF}}

	got := easyJetOrange.OnDark()
	if got != easyJetOrange.Color {
		t.Errorf("OnDark(easyJet orange) = %v, want %v unchanged", got, easyJetOrange.Color)
	}
}

func TestOnDarkNavyStaysBlue(t *testing.T) {
	t.Parallel()

	navy := airlines.Airline{Color: color.RGBA{R: 0x05, G: 0x16, B: 0x4D, A: 0xFF}}

	got := navy.OnDark()
	if got == navy.Color {
		t.Fatal("OnDark(navy) returned the input unchanged, want it lifted")
	}

	if got.B < got.R || got.B < got.G {
		t.Errorf("OnDark(navy) = %v, want B to remain the largest channel", got)
	}
}

func TestOnDarkIsIdempotent(t *testing.T) {
	t.Parallel()

	navy := airlines.Airline{Color: color.RGBA{R: 0x05, G: 0x16, B: 0x4D, A: 0xFF}}

	once := navy.OnDark()
	twice := airlines.Airline{Color: once}.OnDark()

	if diff := int(once.R) - int(twice.R); diff > onDarkTolerance || diff < -onDarkTolerance {
		t.Errorf("OnDark twice: R %d vs %d, want within %d", once.R, twice.R, onDarkTolerance)
	}

	if diff := int(once.G) - int(twice.G); diff > onDarkTolerance || diff < -onDarkTolerance {
		t.Errorf("OnDark twice: G %d vs %d, want within %d", once.G, twice.G, onDarkTolerance)
	}

	if diff := int(once.B) - int(twice.B); diff > onDarkTolerance || diff < -onDarkTolerance {
		t.Errorf("OnDark twice: B %d vs %d, want within %d", once.B, twice.B, onDarkTolerance)
	}
}

// onLightTolerance is the sibling of onDarkTolerance for the OnLight tests
// below: how many units per channel rounding is allowed, both against a
// hand-computed target and between two applications of OnLight in the
// idempotency check.
const onLightTolerance = 1

// wantLightCeiling mirrors the package's own lightCeilingLuminance, so the
// tests below can check OnLight's promise without reaching into the
// unexported constant.
const wantLightCeiling = 0.45

func TestOnLightWhiteComesDown(t *testing.T) {
	t.Parallel()

	white := airlines.Airline{Color: color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}}

	got := white.OnLight()
	if got.R != got.G || got.G != got.B {
		t.Fatalf("OnLight(white) = %v, want equal R, G, B", got)
	}

	if luminance := wcagRelativeLuminance(got); luminance > wantLightCeiling {
		t.Errorf("OnLight(white) luminance = %v, want at or under %v", luminance, wantLightCeiling)
	}
}

func TestOnLightBlackIsUnchanged(t *testing.T) {
	t.Parallel()

	black := airlines.Airline{Color: color.RGBA{A: 0xFF}}

	got := black.OnLight()
	if got != black.Color {
		t.Errorf("OnLight(black) = %v, want %v unchanged", got, black.Color)
	}
}

func TestOnLightBelowCeilingIsUnchanged(t *testing.T) {
	t.Parallel()

	navy := airlines.Airline{Color: color.RGBA{R: 0x05, G: 0x16, B: 0x4D, A: 0xFF}}

	got := navy.OnLight()
	if got != navy.Color {
		t.Errorf("OnLight(navy) = %v, want %v unchanged", got, navy.Color)
	}
}

// Vueling's yellow, unlike easyJet's orange, actually clears
// lightCeilingLuminance (0.61 against a 0.45 ceiling), which is what this
// case needs: a bright, saturated colour the panel would otherwise wash out
// against the paper field.
func TestOnLightVuelingStaysYellow(t *testing.T) {
	t.Parallel()

	vuelingYellow := airlines.Airline{Color: color.RGBA{R: 0xFF, G: 0xCC, B: 0x00, A: 0xFF}}

	got := vuelingYellow.OnLight()
	if got == vuelingYellow.Color {
		t.Fatal("OnLight(Vueling yellow) returned the input unchanged, want it lowered")
	}

	if got.R < got.G || got.R < got.B {
		t.Errorf("OnLight(Vueling yellow) = %v, want R to remain the largest channel", got)
	}

	if got.B > got.R || got.B > got.G {
		t.Errorf("OnLight(Vueling yellow) = %v, want B to remain the smallest channel", got)
	}
}

func TestOnLightIsIdempotent(t *testing.T) {
	t.Parallel()

	vuelingYellow := airlines.Airline{Color: color.RGBA{R: 0xFF, G: 0xCC, B: 0x00, A: 0xFF}}

	once := vuelingYellow.OnLight()
	twice := airlines.Airline{Color: once}.OnLight()

	for _, channel := range []struct {
		name        string
		once, twice uint8
	}{
		{name: "R", once: once.R, twice: twice.R},
		{name: "G", once: once.G, twice: twice.G},
		{name: "B", once: once.B, twice: twice.B},
	} {
		diff := int(channel.once) - int(channel.twice)
		if diff > onLightTolerance || diff < -onLightTolerance {
			t.Errorf("OnLight twice: %s %d vs %d, want within %d",
				channel.name, channel.once, channel.twice, onLightTolerance)
		}
	}
}

// The constants below mirror the sRGB gamma correction and luminance weights
// airlines.go's own linearize and relativeLuminance use. They are redeclared
// here rather than shared, because wcagRelativeLuminance is deliberately a
// second implementation: TestOnLightSweep checks the promise OnLight makes,
// not the code that makes it.
const (
	wcagGammaThreshold = 0.04045
	wcagLinearDivisor  = 12.92
	wcagGammaOffset    = 0.055
	wcagGammaDivisor   = 1.055
	wcagGammaExponent  = 2.4

	wcagRedWeight   = 0.2126
	wcagGreenWeight = 0.7152
	wcagBlueWeight  = 0.0722

	wcagByteMax = 255.0
)

// wcagRelativeLuminance computes WCAG relative luminance independently of the
// package's own relativeLuminance. See the constant block above for why it
// duplicates the formula instead of importing it.
func wcagRelativeLuminance(col color.RGBA) float64 {
	red := wcagLinearize(float64(col.R) / wcagByteMax)
	green := wcagLinearize(float64(col.G) / wcagByteMax)
	blue := wcagLinearize(float64(col.B) / wcagByteMax)

	return wcagRedWeight*red + wcagGreenWeight*green + wcagBlueWeight*blue
}

// wcagLinearize converts one sRGB channel, in the 0-1 range, to linear light.
func wcagLinearize(channel float64) float64 {
	if channel <= wcagGammaThreshold {
		return channel / wcagLinearDivisor
	}

	return math.Pow((channel+wcagGammaOffset)/wcagGammaDivisor, wcagGammaExponent)
}

// TestOnLightSweep runs OnLight over every airline in the database and checks
// the two promises its doc comment makes: the result never measures above the
// ceiling, and alpha stays opaque. It also logs how many rows OnLight
// actually lowered, since that number is the only way to tell how much of the
// catalog sits above the paper theme's ceiling.
func TestOnLightSweep(t *testing.T) {
	t.Parallel()

	all := airlines.All()
	changed := 0

	for _, airline := range all {
		got := airline.OnLight()

		if got.A != 0xFF {
			t.Errorf("%s.OnLight().A = %#x, want 0xff", airline.ICAO, got.A)
		}

		if luminance := wcagRelativeLuminance(got); luminance > wantLightCeiling {
			t.Errorf("%s.OnLight() luminance = %v, want at or under %v", airline.ICAO, luminance, wantLightCeiling)
		}

		if got != airline.Color {
			changed++
		}
	}

	t.Logf("OnLight changed %d of %d airlines", changed, len(all))
}

// TestLookupAllocs must not be parallel: AllocsPerRun needs the runtime's
// undivided attention to count allocations accurately, and running
// alongside other tests would make the count unreliable.
//
//nolint:paralleltest // AllocsPerRun measures the whole process and must not race other tests.
func TestLookupAllocs(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		airlines.Lookup("KLM123")
	})

	if allocs != 0 {
		t.Errorf("Lookup allocated %v times per call, want 0", allocs)
	}
}
