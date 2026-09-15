# uScope

A terminal program for the ClockworkPi uConsole that draws pixels instead of
characters.

It is a sister project of [uAirwaves](https://github.com/hyperized/uAirwaves),
which renders a radar scope through tview and tcell in character cells.
That works, but a cell grid is a coarse thing to draw a radar on. uScope
takes the other route: it opens `/dev/fb0`, maps it, and writes pixels into
it directly. No X, no Wayland, no DRM master, no terminal emulator.

It still behaves like a console program. One static binary, started from the
shell on tty1, keyboard driven, `q` to get back to the shell.

There is no radar in it yet. What there is: a drawing surface, three ways of
getting that surface onto a screen, raw keyboard input, a test pattern, and
a clean exit that puts things back the way it found them.

## The three backends

The same canvas code feeds all three. Which one runs decides only how the
pixels leave the process.

| Backend | What it does | Where it works |
|---|---|---|
| `fb` | mmaps `/dev/fb0` and writes packed pixels, rotated to match the panel | Linux with a framebuffer, so the uConsole |
| `kitty` | sends the frame as zlib-compressed RGBA in Kitty graphics escape sequences | Ghostty, kitty, WezTerm, including over ssh |
| `blocks` | one `▀` per cell, top pixel as foreground and bottom as background | any terminal with 24-bit colour |

The resolution is not comparable between them. `fb` and `kitty` draw at real
pixel sizes. `blocks` gets one pixel per half cell, so an 80x24 window is an
80x48 canvas, which is coarse enough to count the pixels. It is the fallback
that always works.

That coarseness used to show up in the test scene, whose shapes are sized in
pixels for the panel. Below a 720 pixel short edge the scene now scales those
sizes down to fit; at 1280x720 and above, on the panel and in `kitty` mode, it
draws exactly as before.

With `--backend auto`, which is the default, uScope tries in this order:

1. The framebuffer, if this is Linux and `--fb` opens.
2. Kitty graphics, if the terminal is one that has them. That means `TERM`
   is `xterm-ghostty` or `xterm-kitty`, or `KITTY_WINDOW_ID` is set, or
   `TERM_PROGRAM` is `ghostty` or `WezTerm`.
3. Half blocks.

The Kitty check is an allow list rather than a probe. Asking a terminal what
it supports means writing a query and waiting for an answer that never comes
if it does not understand the question, and half a second of nothing at
startup is a worse trade than falling back to blocks.

## The device

Everything below was checked on the machine rather than assumed.

| | |
|---|---|
| Hardware | ClockworkPi uConsole, Raspberry Pi CM4 |
| Kernel | 6.12.62-v8+, linux/arm64 |
| Session | no desktop, user shell on tty1, `TERM=linux` |
| Device | `/dev/fb0`, driver `vc4drmfb`, group `video` |
| Geometry | 720x1280 portrait, 16 bpp RGB565, stride 1440 bytes |
| Rotation | `/sys/class/graphics/fbcon/rotate` reads `1` |

The panel is mounted portrait, so the framebuffer is 720 wide and 1280 tall.
fbcon turns the console a quarter turn, which is why the user sees a 1280x720
landscape screen. uScope reads the same sysfs file and turns its canvas the
same way.

The operator is in the `video`, `render` and `input` groups, so none of this
needs root. Writing to `/dev/fb0` works over ssh too, which is how the test
pattern gets checked without sitting at the machine. Only the ioctl that
switches the console into graphics mode needs the controlling terminal, and
that one is allowed to fail.

### Rotation

Autodetect is correct by construction, not by luck. `pkg/rotate` maps a
logical pixel for rotation 1 to `(pw-1-y, x)`, which is the transform the
kernel's own `fbcon_cw.c` applies:

```c
area.sx = vxres - (sy + height) * font.height;
area.sy = sx * font.width;
```

Rotation 3 matches `fbcon_ccw.c` the same way. The console text on the
uConsole is drawn through that transform and reads the right way up, so a
canvas that reads fbcon's number and applies the same mapping lands where
the console already is. `--rotate` overrides it if a future panel disagrees.

## Running it

### On the uConsole

Put your device in a `.env` next to the Makefile. It is gitignored, because
it is yours:

```
DEVICE = user@uconsole-host
```

Then:

```sh
make pattern
```

That cross-compiles for arm64, ships the binary, and paints the test pattern
on the panel.

Red square top-left and the cyan triangle at the top means the rotation is
right. The triangle points up, so it tells you which way up the frame landed.
If the pattern is on the wrong edge, run it again with `--rotate 3`.

`--test-pattern` changes no console or terminal state, so it is safe over ssh
and the pattern stays on screen until something else repaints.

### On a Mac

Ghostty implements the Kitty graphics protocol, so the live scene runs in a
terminal window:

```sh
make run
```

That is `go run .`, and auto detection lands on the kitty backend. Press `q`
to quit. To see the half-block renderer instead:

```sh
make run-blocks
```

Both leave the shell exactly as they found it: uScope draws on the alternate
screen and hands the normal one back on exit.

To render a still frame to a file rather than a screen:

```sh
go run . --png pattern.png
go run . --png portrait.png --size 720x1280
```

### Over ssh

From Ghostty on the Mac, into the uConsole:

```sh
ssh -t user@uconsole-host './uScope --backend kitty'
```

The frames travel back over the ssh connection as escape sequences and
appear in the Mac window. The uConsole's own screen is untouched. Swap in
`--backend fb` on the same connection and it draws on the panel instead,
with nothing coming back to the Mac.

The `-t` matters. `ssh host 'cmd'` runs without a pty, so the remote stdout
is a pipe: uScope falls back to an assumed 80x24, skips the alternate
screen, and never sees a keypress, which means no `q`. With `-t` there is a
real terminal on the far end, ssh forwards the window size and every resize,
and all of it behaves as if it were local.

`--backend blocks` is what to reach for on a terminal that has neither, and
on a slow link, since a frame of half blocks is a fraction of the bytes.

## Keys

| Key | Does |
|---|---|
| `q`, `Q` | quit |
| `Esc` | quit |
| `Ctrl-C` | quit |

Arrow keys are decoded but nothing is bound to them yet.

## Flags

| Flag | Default | Does |
|---|---|---|
| `--backend` | `auto` | `auto`, `fb`, `kitty`, `blocks` or `png` |
| `--fb` | `/dev/fb0` | framebuffer device |
| `--rotate` | `auto` | `auto` reads sysfs, or force `0`, `1`, `2`, `3` |
| `--fps` | `30` | frames per second in live mode, 1 to 120 |
| `--frames` | `0` | stop after this many frames, 0 runs until quit |
| `--test-pattern` | off | paint one frame and exit |
| `--png` | | render to a PNG instead of a screen |
| `--size` | `1280x720` | canvas size for `--png` and for the kitty backend |

`--png PATH` implies `--backend png`. Asking for both a file and a screen in
one run is refused rather than guessed at.

`--frames` is what makes uScope testable from a shell:

```sh
go run . --backend blocks --frames 3 | wc -c
```

Exit status is 0 on a clean quit and 1 on any failure.

## Building and testing

```sh
make build          # for the machine you're on
make build-aarch64  # for the uConsole
make run            # go run .
make run-blocks     # go run . --backend blocks
make test           # go test -race -cover ./...
make lint           # golangci-lint run ./...
make test-device    # cross-compile the integration tests and run them on the device
```

Tests that touch a real framebuffer or a real terminal sit behind the
`integration` build tag, so `make test` never opens a device. `make
test-device` is what runs them, on the hardware where they mean something.

## Layout

```
main.go, flags.go     the command line
pkg/canvas            drawing surface
pkg/rotate            fbcon rotation numbering and pixel mapping
pkg/backend           the Backend interface and the --backend allow list
pkg/fbdev             framebuffer blitter (Linux; stub elsewhere)
pkg/vt                console graphics mode (Linux; stub elsewhere)
pkg/winsize           TIOCGWINSZ (Linux and macOS; stub elsewhere)
pkg/kitty             Kitty graphics protocol encoder
pkg/blocks            half-block renderer
pkg/termbackend       owns the terminal, drives kitty or blocks
internal/term         raw tty mode
internal/input        bytes to key events
internal/pattern      the test scene
internal/app          the run loop
```

`pkg/kitty` and `pkg/blocks` are pure encoders: they take an image and an
`io.Writer` and know nothing about terminals. `pkg/termbackend` is the one
that owns the alternate screen, the cursor and the window size.

Standard library only. No third-party modules, and no `golang.org/x` either:
the termios, ioctl and signal work is done with `syscall` behind build tags.

## What comes next

Slice 3 adds PSF font rendering, since a radar needs labels and there is no
text at all yet. After that, the radar itself: aircraft, tracks and range
rings, sharing uAirwaves' decoding work but drawing it at pixel resolution.

## Licence

Business Source License 1.1. See [LICENSE](LICENSE).
