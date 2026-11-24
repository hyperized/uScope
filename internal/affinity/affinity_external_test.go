package affinity_test

import (
	"errors"
	"testing"

	"github.com/hyperized/uScope/internal/affinity"
)

// The lists that come up more than once.
const (
	bigCores = "4-7"
	mixed    = "0,2,4-7"
	allCPUs  = "0-1023"
)

// TestParse checks the lists the flag accepts come back in canonical form,
// and that each kind of garbage is refused by the rule it breaks.
func TestParse(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, in, want string
		count          int
		err            error
	}{
		{name: "one CPU", in: "3", want: "3", count: 1},
		{name: "CPU zero", in: "0", want: "0", count: 1},
		{name: "big cores", in: bigCores, want: bigCores, count: 4},
		{name: "mixed", in: mixed, want: mixed, count: 6},
		{name: "one-wide range", in: "5-5", want: "5", count: 1},
		{name: "adjacent items fold", in: "0,1,2,3", want: "0-3", count: 4},
		{name: "overlap merges", in: "4-6,5-7", want: bigCores, count: 4},
		{name: "repeat merges", in: "2,2", want: "2", count: 1},
		{name: "out of order items", in: "6,1", want: "1,6", count: 2},
		{name: "top CPU", in: "1023", want: "1023", count: 1},
		{name: "across a word", in: "62-65", want: "62-65", count: 4},
		{name: "everything", in: allCPUs, want: allCPUs, count: 1024},
		{name: "empty", in: "", err: affinity.ErrEmpty},
		{name: "space", in: "4, 5", err: affinity.ErrSyntax},
		{name: "trailing space", in: "4 ", err: affinity.ErrSyntax},
		{name: "trailing comma", in: "4,", err: affinity.ErrSyntax},
		{name: "leading comma", in: ",4", err: affinity.ErrSyntax},
		{name: "double comma", in: "4,,5", err: affinity.ErrSyntax},
		{name: "lone comma", in: ",", err: affinity.ErrSyntax},
		{name: "negative", in: "-1", err: affinity.ErrSyntax},
		{name: "open range", in: "4-", err: affinity.ErrSyntax},
		{name: "two dashes", in: "4-5-6", err: affinity.ErrSyntax},
		{name: "stride", in: "0-7:2", err: affinity.ErrSyntax},
		{name: "plus sign", in: "+4", err: affinity.ErrSyntax},
		{name: "leading zero", in: "04", err: affinity.ErrSyntax},
		{name: "hex", in: "0x4", err: affinity.ErrSyntax},
		{name: "letters", in: "all", err: affinity.ErrSyntax},
		{name: "five digits", in: "10000", err: affinity.ErrSyntax},
		{name: "above the top", in: "1024", err: affinity.ErrRange},
		{name: "range above the top", in: "0-2000", err: affinity.ErrRange},
		{name: "backwards", in: "7-4", err: affinity.ErrOrder},
		{name: "too long", in: string(make([]byte, 257)), err: affinity.ErrSyntax},
		{name: "nul", in: "4\x00", err: affinity.ErrSyntax},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			set, err := affinity.Parse(testCase.in)
			if testCase.err != nil {
				if !errors.Is(err, testCase.err) {
					t.Fatalf("Parse(%q) error = %v, want %v", testCase.in, err, testCase.err)
				}

				if !set.IsEmpty() {
					t.Errorf("Parse(%q) = %s alongside an error, want the empty set", testCase.in, set)
				}

				return
			}

			if err != nil {
				t.Fatalf("Parse(%q): %v", testCase.in, err)
			}

			checkParsed(t, set, testCase.want, testCase.count)
		})
	}
}

// checkParsed checks an accepted list prints as want, holds count CPUs, and
// parses back to itself.
func checkParsed(t *testing.T, set affinity.Set, want string, count int) {
	t.Helper()

	if got := set.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	if got := set.Count(); got != count {
		t.Errorf("Count() = %d, want %d", got, count)
	}

	again, err := affinity.Parse(set.String())
	if err != nil || again != set {
		t.Errorf("Parse(%q) does not round-trip: %v, %v", set.String(), again, err)
	}
}

// TestHas checks membership, including the numbers no set can hold.
func TestHas(t *testing.T) {
	t.Parallel()

	set, err := affinity.Parse(bigCores)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	for cpu, want := range map[int]bool{-1: false, 0: false, 3: false, 4: true, 7: true, 8: false, 1024: false} {
		if got := set.Has(cpu); got != want {
			t.Errorf("Has(%d) = %t, want %t", cpu, got, want)
		}
	}
}

// TestEmptySet checks the zero value is empty, prints as nothing and is
// refused by Pin before anything is touched.
func TestEmptySet(t *testing.T) {
	t.Parallel()

	var set affinity.Set

	if !set.IsEmpty() || set.Count() != 0 || set.String() != "" {
		t.Fatalf("zero Set: IsEmpty %t, Count %d, String %q", set.IsEmpty(), set.Count(), set.String())
	}

	if _, err := affinity.Pin(set); !errors.Is(err, affinity.ErrEmpty) {
		t.Fatalf("Pin(empty) error = %v, want ErrEmpty", err)
	}
}

// TestSentinelsAreDistinct checks no two sentinels match each other, since
// callers branch on errors.Is.
func TestSentinelsAreDistinct(t *testing.T) {
	t.Parallel()

	sentinels := []error{
		affinity.ErrEmpty, affinity.ErrSyntax, affinity.ErrRange, affinity.ErrOrder,
		affinity.ErrUnsupported, affinity.ErrRefused, affinity.ErrNotHeld, affinity.ErrUnsettled,
	}

	for i, one := range sentinels {
		for j, other := range sentinels {
			if i != j && errors.Is(one, other) {
				t.Errorf("%v matches %v", one, other)
			}
		}
	}
}

// FuzzParse checks Parse never panics, and that whatever it accepts prints
// back to a list that parses to the same set.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{bigCores, mixed, "", "7-4", "1024", allCPUs, ",", "4-5-6"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		set, err := affinity.Parse(text)
		if err != nil {
			return
		}

		again, err := affinity.Parse(set.String())
		if err != nil || again != set {
			t.Fatalf("Parse(%q) = %s, which parses back as %s, %v", text, set, again, err)
		}
	})
}
