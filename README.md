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

Slice 1 is the shell around that idea. There is no radar in it yet. What
there is: a drawing surface, a framebuffer blitter that understands how the
panel is rotated, raw keyboard input, a test pattern, and a clean exit that
puts the console back the way it found it.

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

## Running it

Put your device in a `.env` next to the Makefile. It is gitignored, because
it is yours:

```
DEVICE = user@uconsole-host
```

Then:

```sh
make pattern
```

That cross-compiles for arm64, ships the binary, and paints the test pattern.

### Reading the test pattern

Red square top-left and the cyan triangle at the top means the rotation is
right. The triangle points up, so it tells you which way up the frame landed.

If the pattern is on the wrong edge, the rotation guess was wrong. Run it
again forcing the other quarter turn:

```sh
ssh user@uconsole-host './uScope --test-pattern --rotate 3'
```

The two quarter turns are mirror images. Getting the wrong one puts the image
on the wrong edge rather than producing garbage, which makes it easy to miss
if you are not looking for it.

`--test-pattern` changes no console or terminal state, so it is safe over ssh
and the pattern stays on screen until something else repaints.

### On a Mac

There is no framebuffer, so render the scene to a file instead:

```sh
go run . --png pattern.png
go run . --png portrait.png --size 720x1280
```

It is the same scene through the same canvas code, which is how the layout
gets checked without a uConsole on the desk.

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
| `--fb` | `/dev/fb0` | framebuffer device |
| `--rotate` | `auto` | `auto` reads sysfs, or force `0`, `1`, `2`, `3` |
| `--fps` | `30` | frames per second in live mode, 1 to 120 |
| `--test-pattern` | off | paint one frame and exit |
| `--png` | | render to a PNG instead of a device |
| `--size` | `1280x720` | canvas size for `--png` |

Exit status is 0 on a clean quit and 1 on any failure.

## Building and testing

```sh
make build          # for the machine you're on
make build-aarch64  # for the uConsole
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
pkg/fbdev             framebuffer blitter (Linux; stub elsewhere)
pkg/vt                console graphics mode (Linux; stub elsewhere)
internal/term         raw tty mode
internal/input        bytes to key events
internal/pattern      the test scene
internal/app          the run loop
```

Standard library only. No third-party modules, and no `golang.org/x` either:
the termios and ioctl work is done with `syscall` behind build tags.

## What comes next

Slice 2 is a second blitter that paints into a terminal instead of a
framebuffer, using Kitty graphics where the terminal supports it and
half-block characters where it does not, so the same scene works over ssh
without a device. Slice 3 adds PSF font rendering, since a radar needs
labels and there is no text at all yet. After that, the radar itself:
aircraft, tracks and range rings, sharing uAirwaves' decoding work but
drawing it at pixel resolution.

## Licence

Business Source License 1.1. See [LICENSE](LICENSE).
