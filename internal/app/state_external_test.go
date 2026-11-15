package app_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperized/uScope/internal/app"
	"github.com/hyperized/uScope/internal/radar"
)

// TestRunStateFileWinsOverTheFlags starts a run on a state file that holds
// two fields: those two come from the file, everything else from the flags.
// A set then lands in the file by the time Run returns, without waiting out
// the saver's delay, because cancelling is what flushes it.
func TestRunStateFileWinsOverTheFlags(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings")
	if err := os.WriteFile(path, []byte("tilt 40\nlook mono\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	cfg := controlConfig(radar.Settings{View: radar.ViewMinimal3D, MinimalShore: true, Tilt: 10, Azimuth: 30})
	cfg.State = path

	status := make(statusLines, 4)
	done := runControl(ctx, cfg, io.Discard, app.WithStderr(status), app.WithTicker(newFakeTicker().new))

	client := dialControl(t, status)

	const want = "ok view=minimal3d shore=on airports=off zoom=0 tilt=40 orbit=on azimuth=30 exaggerate=8 " +
		"range=auto theme=night look=mono colour=altitude bar=off lights=off stars=on recenter=0 " +
		"corridors=on filter=all board=off"

	if got := client.send(t, "get"); got != want {
		t.Errorf("get:\n got %q\nwant %q", got, want)
	}

	if got := client.send(t, "set zoom 2"); !strings.Contains(got, "zoom=2") {
		t.Errorf("set zoom 2 = %q", got)
	}

	cancel()

	if err := recvOrTimeout(t, done, testTimeout, "Run to return"); err != nil {
		t.Fatalf("Run error = %v, want nil after cancel", err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own temp dir.
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range []string{"view minimal3d\n", "zoom 2\n", "tilt 40\n", "look mono\n"} {
		if !strings.Contains(string(body), line) {
			t.Errorf("state file does not carry %q:\n%s", line, body)
		}
	}
}
