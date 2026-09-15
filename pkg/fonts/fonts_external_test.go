package fonts_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/hyperized/uScope/pkg/fonts"
	"github.com/hyperized/uScope/pkg/psf"
)

// loaderCase names one embedded face loader alongside the glyph box it is
// supposed to produce.
type loaderCase struct {
	name       string
	load       func() (*psf.Font, error)
	wantWidth  int
	wantHeight int
}

// loaderCases lists the four embedded faces. Every test that needs to drive
// all of them shares this table instead of repeating the face names, which
// keeps a name like "Small" from turning into a goconst violation.
func loaderCases() []loaderCase {
	const (
		smallWidth  = 6
		smallHeight = 12
		bodyWidth   = 8
		bodyHeight  = 16
		largeWidth  = 16
		largeHeight = 32
	)

	return []loaderCase{
		{name: "Small", load: fonts.Small, wantWidth: smallWidth, wantHeight: smallHeight},
		{name: "Body", load: fonts.Body, wantWidth: bodyWidth, wantHeight: bodyHeight},
		{name: "BodyBold", load: fonts.BodyBold, wantWidth: bodyWidth, wantHeight: bodyHeight},
		{name: "Large", load: fonts.Large, wantWidth: largeWidth, wantHeight: largeHeight},
	}
}

func TestLoaders(t *testing.T) {
	t.Parallel()

	for _, testCase := range loaderCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			font, err := testCase.load()
			if err != nil {
				t.Fatalf("%s() error = %v, want nil", testCase.name, err)
			}

			if got := font.Width(); got != testCase.wantWidth {
				t.Errorf("%s() Width() = %d, want %d", testCase.name, got, testCase.wantWidth)
			}

			if got := font.Height(); got != testCase.wantHeight {
				t.Errorf("%s() Height() = %d, want %d", testCase.name, got, testCase.wantHeight)
			}

			if got := font.Len(); got <= 0 {
				t.Errorf("%s() Len() = %d, want > 0", testCase.name, got)
			}

			const letterA = 'A'

			if _, ok := font.Glyph(letterA); !ok {
				t.Errorf("%s() Glyph(%q) not found", testCase.name, letterA)
			}
		})
	}
}

// TestLoaderCaching checks that calling a loader twice hands back the exact
// same *psf.Font pointer, which is the observable proof that sync.Once
// skipped a second parse rather than doing it silently again.
func TestLoaderCaching(t *testing.T) {
	t.Parallel()

	for _, testCase := range loaderCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			first, err := testCase.load()
			if err != nil {
				t.Fatalf("%s() error = %v, want nil", testCase.name, err)
			}

			second, err := testCase.load()
			if err != nil {
				t.Fatalf("%s() second call error = %v, want nil", testCase.name, err)
			}

			if first != second {
				t.Errorf("%s() returned different pointers across calls, want the cached one", testCase.name)
			}
		})
	}
}

// TestLoadersAreDistinct checks every pair of faces, which is what catches
// BodyBold quietly aliasing Body: both are 8x16, so only the pointers tell
// them apart.
func TestLoadersAreDistinct(t *testing.T) {
	t.Parallel()

	cases := loaderCases()
	byName := make(map[string]*psf.Font, len(cases))

	for _, testCase := range cases {
		font, err := testCase.load()
		if err != nil {
			t.Fatalf("%s() error = %v, want nil", testCase.name, err)
		}

		byName[testCase.name] = font
	}

	for outerIndex, outer := range cases {
		for _, inner := range cases[outerIndex+1:] {
			pairName := outer.name + " vs " + inner.name

			t.Run(pairName, func(t *testing.T) {
				t.Parallel()

				if byName[outer.name] == byName[inner.name] {
					t.Errorf("%s: both loaders returned the same pointer, want distinct fonts", pairName)
				}
			})
		}
	}
}

// TestLoaderConcurrency is the reason go test -race is run over this
// package: every goroutine calls the same loader at once, and sync.Once
// only earns its keep if they all land on one font with no error.
func TestLoaderConcurrency(t *testing.T) {
	t.Parallel()

	const goroutineCount = 20

	for _, testCase := range loaderCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			results := make([]*psf.Font, goroutineCount)
			errs := make([]error, goroutineCount)

			var waitGroup sync.WaitGroup

			for index := range goroutineCount {
				waitGroup.Go(func() {
					results[index], errs[index] = testCase.load()
				})
			}

			waitGroup.Wait()

			for index, err := range errs {
				if err != nil {
					t.Errorf("goroutine %d: error = %v, want nil", index, err)
				}

				if results[index] != results[0] {
					t.Errorf("goroutine %d returned a different font pointer than goroutine 0", index)
				}
			}
		})
	}
}

// TestLicence checks the attribution the SIL Open Font License actually
// requires: the license name, the author, and the reserved font name.
func TestLicence(t *testing.T) {
	t.Parallel()

	licence := fonts.Licence()
	if licence == "" {
		t.Fatal("Licence() is empty, want the OFL text")
	}

	for _, testCase := range []struct {
		name string
		want string
	}{
		{name: "license name", want: "SIL Open Font License"},
		{name: "author", want: "Dimitar Toshkov Zhekov"},
		{name: "reserved font name", want: "Terminus Font"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(licence, testCase.want) {
				t.Errorf("Licence() does not contain %q", testCase.want)
			}
		})
	}
}
