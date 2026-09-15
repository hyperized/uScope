package backend_test

import (
	"errors"
	"image"
	"testing"

	"github.com/hyperized/uScope/pkg/backend"
)

// Flag spellings and environment variable names, named because the linter
// counts repeated literals across a table and these appear in every row.
const (
	nameAuto   = "auto"
	nameFB     = "fb"
	nameKitty  = "kitty"
	nameBlocks = "blocks"
	namePNG    = "png"

	envTerm    = "TERM"
	envProgram = "TERM_PROGRAM"
	envWindow  = "KITTY_WINDOW_ID"
)

// stub proves at compile time that the interface is implementable with the
// signatures the run loop expects. If Backend grows a method, this file
// stops compiling before any real backend does.
type stub struct{}

func (stub) Size() (int, int)       { return 0, 0 }
func (stub) Blit(*image.RGBA) error { return nil }
func (stub) Close() error           { return nil }

var _ backend.Backend = stub{}

func TestParse(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		text    string
		want    backend.Kind
		wantErr bool
	}{
		{name: nameAuto, text: nameAuto, want: backend.Auto},
		{name: nameFB, text: nameFB, want: backend.Framebuffer},
		{name: nameKitty, text: nameKitty, want: backend.Kitty},
		{name: nameBlocks, text: nameBlocks, want: backend.Blocks},
		{name: namePNG, text: namePNG, want: backend.PNG},
		{name: "empty", text: "", wantErr: true},
		{name: "wrong case", text: "FB", wantErr: true},
		{name: "trailing space", text: "fb ", wantErr: true},
		{name: "unknown", text: "sixel", wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := backend.Parse(testCase.text)

			if testCase.wantErr {
				if !errors.Is(err, backend.ErrInvalid) {
					t.Fatalf("Parse(%q) error = %v, want %v", testCase.text, err, backend.ErrInvalid)
				}

				return
			}

			if err != nil {
				t.Fatalf("Parse(%q): %v", testCase.text, err)
			}

			if got != testCase.want {
				t.Errorf("Parse(%q) = %v, want %v", testCase.text, got, testCase.want)
			}
		})
	}
}

func TestKindString(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		kind backend.Kind
		want string
	}{
		{name: nameAuto, kind: backend.Auto, want: nameAuto},
		{name: nameFB, kind: backend.Framebuffer, want: nameFB},
		{name: nameKitty, kind: backend.Kitty, want: nameKitty},
		{name: nameBlocks, kind: backend.Blocks, want: nameBlocks},
		{name: namePNG, kind: backend.PNG, want: namePNG},
		{name: "out of range", kind: backend.Kind(99), want: "invalid"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.kind.String(); got != testCase.want {
				t.Errorf("String() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// envFrom turns a map into the lookup function KittyCapable takes, which is
// os.Getenv in production.
func envFrom(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestKittyCapable(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		vars map[string]string
		want bool
	}{
		{name: "nothing set", vars: map[string]string{}},
		{name: "linux console", vars: map[string]string{envTerm: "linux"}},
		{
			name: "iTerm on xterm-256color",
			vars: map[string]string{envTerm: "xterm-256color", envProgram: "iTerm.app"},
		},
		{name: "empty kitty window id", vars: map[string]string{envWindow: ""}},
		{name: "kitty window id", vars: map[string]string{envWindow: "1"}, want: true},
		{name: "ghostty TERM", vars: map[string]string{envTerm: "xterm-ghostty"}, want: true},
		{name: "kitty TERM", vars: map[string]string{envTerm: "xterm-kitty"}, want: true},
		{name: "ghostty TERM_PROGRAM", vars: map[string]string{envProgram: "ghostty"}, want: true},
		{name: "wezterm TERM_PROGRAM", vars: map[string]string{envProgram: "WezTerm"}, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := backend.KittyCapable(envFrom(testCase.vars)); got != testCase.want {
				t.Errorf("KittyCapable(%v) = %v, want %v", testCase.vars, got, testCase.want)
			}
		})
	}
}
