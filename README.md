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

The radar is in it now. Aircraft come in over the uConsole's own radio, from a
BEAST feed on another machine, from a captured IQ file played back, or from a
fleet of twelve invented ones so the thing can be worked on at a desk with no
receiver anywhere near it. They are drawn as silhouettes turned to their
heading and coloured by altitude, each with the trail it flew in on.

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

That coarseness used to show up in the pattern scene, whose shapes are sized
in pixels for the panel. Below a 720 pixel short edge the scene scales those
sizes down to fit; at 1280x720 and above, on the panel and in `kitty` mode, it
draws exactly as before. The specimen scene handles the same problem
differently, by dropping whole blocks that will not fit rather than shrinking
them. A bitmap face has one design size, and scaling it down does not make
small text, it makes unreadable text.

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

## The radar

`--scene radar` is the default. The left half is the scope: a square field
with three dashed range rings, the cardinal letters, a marker where the
receiver is, and the airports that fall inside the current range as small
hollow squares. Aircraft are 15 pixel silhouettes rotated to their heading.
The colour is the altitude band: green below 10,000 feet, amber below 25,000,
red above. An aircraft whose altitude nobody has decoded yet is grey. One
whose heading nobody has decoded is drawn as a bare circle, because a
silhouette would be claiming to know which way it is facing.

Behind each aircraft is its trail, drawn as an anti-aliased polyline from the
oldest fix it still holds to the newest, brightening towards the head. The
trails are the reason this project exists. A character cell cannot draw one,
and a scope full of them says in one glance what a scope full of dots cannot:
who is turning and who came from where.

The right column is the card. The selected aircraft's callsign is set at 64
pixels with its ICAO hex and squawk beside it, its track in degrees and
compass points under it, and three figures along the bottom: distance in
nautical miles, altitude in feet, speed in knots. Under the card is one
compact row per aircraft, nearest first, and the row for the selected one
carries the accent bar. Under that, the altitude legend and a line saying how
many aircraft are being tracked and where from.

Units are nautical miles, feet and knots throughout, because that is what
aviation uses and converting would only make the numbers harder to check
against anything else.

### Where the aircraft come from

| Flag | Source |
|---|---|
| `--replay-iq PATH` | a captured IQ file, played back through the demodulator |
| `--beast HOST:PORT` | Mode S frames from a remote demodulator over TCP |
| `--demo` | twelve invented aircraft on straight tracks |
| none | the local RTL-SDR on Linux, the demo fleet anywhere else |

They are listed in the order they beat each other. A capture wins over a feed
so a recorded problem can always be replayed on a machine that also has a feed
configured. Giving two of them is not an error; the more specific one is
obviously what was meant.

With nothing given at all the answer depends on the machine. On Linux that is
the radio, which is the point of the uConsole. On a Mac there is no receiver to
open, so uScope flies the demo fleet and says so once on stderr rather than
refusing to start.

`--lat` and `--lon` pin the receiver's own position. Both or neither: a
latitude with no longitude is half an answer. Without them uScope works its
own position out from the aircraft it can hear, by intersecting their radio
horizons, which takes about thirty position reports and lands within tens of
nautical miles. The header says which of the three it is showing: coordinates
for a known position, `EST ±22 NM` for an estimate, `NO FIX` for neither.

A known position is worth giving if you have one. With a reference nearby a
single CPR frame resolves to a position; without one the decoder waits for the
matching half of the pair, which takes up to ten seconds per aircraft.

### Range

The scope starts in auto range, which fits the farthest aircraft that has a
position, rounded up to a whole 20 nautical mile step and clamped between 20
and 500. A frame where nothing has a position leaves the range alone, so the
scope does not snap back to its minimum every time the feed goes quiet. `a`
turns auto off and `+` and `-` step the range by hand, which also turns auto
off: asking for a range and having it overridden on the next frame is not what
pressing the key meant.

## The other two scenes

`--scene` picks what gets drawn. In live mode `s` steps through all three
without restarting.

`pattern` is the orientation check from slice 1: four coloured corner
squares, a triangle pointing up, a circle and a sweeping line. Red square
top-left and cyan triangle at the top means the frame landed the right way
up, and the sweep moving means the loop is running.

`specimen` is the slice 3 scene. It is half a font sample and half a mock of
what the radar turned out to look like: a header band with a clock, a
selected-flight card with the callsign set large, two compact aircraft rows,
all four embedded faces rendering the alphabet, and a key bar along the
bottom. The aircraft in it are invented and always the same. It stays in
because it is the fastest way to judge a font change, and because a scene with
no moving parts is a useful thing to have when the radar is misbehaving and
you want to know whether the drawing or the data is at fault.

Every block in the specimen sizes itself from the canvas bounds and the
metrics of the font it is set in. A block that does not fit is skipped rather
than drawn over its neighbour, so the same scene renders at 1280x720 on the
panel and on a canvas of a few dozen pixels. An 80x24 terminal of half blocks
is a canvas 80 by 48, and all that fits there is the key bar. Give it a
320x200 window and the header band and the two compact rows come back.

## Themes

uScope has two colour themes. Night is the default: a near-black field, light
ink, and it costs the least on a backlit handheld in the dark. Paper is
modelled on an e-paper flight display: a light field, dark ink, and a navy
header band. `l` cycles between them at run time, on whichever scene is on
screen, and `--theme night` or `--theme paper` picks the one to start on.

## Fonts

uScope draws text with PSF console fonts, the same bitmap format the kernel
loads into a virtual terminal. Four faces are compiled into the binary:

| Face | Size | What it sets |
|---|---|---|
| Terminus | 6x12 | labels, unit suffixes, key caps |
| Terminus | 8x16 | body text and the compact rows |
| Terminus Bold | 8x16 | the wordmark |
| Terminus Bold | 16x32 | the clock and the figures on a card |

They are Debian `console-setup`'s Uni3 builds of Terminus Font, taken byte
for byte and gzipped as that package ships them. Two of the four are PSF1 and
two are PSF2, which is why `pkg/psf` reads both formats.

Terminus Font is licensed under the SIL Open Font License, Version 1.1,
Copyright (c) 2010 Dimitar Toshkov Zhekov, with Reserved Font Name "Terminus
Font". The full text is in `pkg/fonts/OFL-Terminus.txt` and
`fonts.Licence()` returns it at run time. The files keep their original
names because the licence reserves the font name; see
[pkg/fonts/README.md](pkg/fonts/README.md).

There is no font scaling beyond whole numbers. A console font is a grid of
one-bit pixels drawn for a specific size, and interpolating it only makes it
blurry, so `pkg/text` repeats each pixel as a square block instead.

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
on the panel. `make specimen` does the same with the type specimen, which is
the check that matters for the fonts.

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
to quit, `s` to switch scenes. To start on the type specimen instead:

```sh
make run-specimen
```

Ghostty draws the frame at its real pixel size, so that is the closest look
at the fonts available without a uConsole on the desk. To see the half-block
renderer:

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
| `s`, `S` | step to the next scene |
| `l`, `L` | cycle the colour theme |
| `n`, `N`, Down | select the next aircraft |
| `p`, `P`, Up | select the previous one |
| `+`, `=` | widen the range by one step, and turn auto off |
| `-`, `_` | narrow it by one step, and turn auto off |
| `a`, `A` | auto range on or off |
| `t`, `T` | trails on or off |
| `Esc` | quit |
| `Ctrl-C` | quit |

Both cases are bound because caps lock is easy to hit by accident on the
uConsole's keyboard, and the unshifted twins of `+` and `-` are bound for the
same reason.

The radar scene gets first refusal on every key and passes on the ones it does
not want, which is what keeps `q` and `s` working while it is on screen. The
other two scenes bind nothing.

Selection is by ICAO rather than by position in the list, so an aircraft
overtaking another does not move the selection to a different aeroplane. When
the selected one goes out of range the selection falls to the nearest.

## Flags

| Flag | Default | Does |
|---|---|---|
| `--backend` | `auto` | `auto`, `fb`, `kitty`, `blocks` or `png` |
| `--scene` | `radar` | `radar`, `pattern` or `specimen` |
| `--theme` | `night` | `night` or `paper` colour theme |
| `--demo` | off | fly twelve invented aircraft instead of decoding any |
| `--beast` | | take Mode S frames from `HOST:PORT` |
| `--replay-iq` | | replay a captured IQ file through the demodulator |
| `--lat` | | receiver latitude in degrees, needs `--lon` |
| `--lon` | | receiver longitude in degrees, needs `--lat` |
| `--fb` | `/dev/fb0` | framebuffer device |
| `--rotate` | `auto` | `auto` reads sysfs, or force `0`, `1`, `2`, `3` |
| `--fps` | `30` | frames per second in live mode, 1 to 120 |
| `--frames` | `0` | stop after this many frames, 0 runs until quit |
| `--test-pattern` | off | draw one still frame of the selected scene and exit |
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
make run-demo       # go run . --demo
make run-beast      # go run . --beast $(BEAST)
make run-blocks     # go run . --backend blocks --demo
make run-pattern    # go run . --scene pattern
make run-specimen   # go run . --scene specimen
make test           # go test -race -cover ./...
make lint           # golangci-lint run ./...
make radar          # ship, then paint one radar frame on the panel
make pattern        # ship, then paint the test pattern on the panel
make specimen       # ship, then paint the type specimen on the panel
make test-device    # cross-compile the integration tests and run them on the device
```

`make ship` puts the binary on the device on its own. Once it is there,
`./uScope` over ssh opens the local radio and every flag above works the same
as it does here.

Tests that touch a real framebuffer or a real terminal sit behind the
`integration` build tag, so `make test` never opens a device. `make
test-device` is what runs them, on the hardware where they mean something.

## Layout

```
main.go, flags.go     the command line
pkg/canvas            drawing surface
pkg/sprite            monochrome bitmaps, rotated to a heading
pkg/rotate            fbcon rotation numbering and pixel mapping
pkg/backend           the Backend interface and the --backend allow list
pkg/fbdev             framebuffer blitter (Linux; stub elsewhere)
pkg/vt                console graphics mode (Linux; stub elsewhere)
pkg/winsize           TIOCGWINSZ (Linux and macOS; stub elsewhere)
pkg/kitty             Kitty graphics protocol encoder
pkg/blocks            half-block renderer
pkg/termbackend       owns the terminal, drives kitty or blocks
pkg/psf               PSF1 and PSF2 console font parser
pkg/fonts             the four embedded Terminus faces
pkg/text              draws strings with a PSF font
internal/term         raw tty mode
internal/input        bytes to key events
internal/theme        the colour palettes
internal/source       where aircraft come from: the radio, a feed, or invented
internal/radar        the radar scene
internal/pattern      the orientation scene
internal/specimen     the type specimen scene
internal/app          the run loop
```

`pkg/kitty` and `pkg/blocks` are pure encoders: they take an image and an
`io.Writer` and know nothing about terminals. `pkg/termbackend` is the one
that owns the alternate screen, the cursor and the window size.

`pkg/psf` and `pkg/text` are the same shape on the drawing side. The parser
takes bytes and knows nothing about canvases; the drawer takes a font and a
canvas and knows nothing about files. Neither of them has ever heard of a
scene, which is what lets both be tested against fonts built inside a test.

Everything above the data layer is standard library only. No `golang.org/x`
either: the termios, ioctl and signal work is done with `syscall` behind build
tags.

## The uAirwaves dependency

The one thing uScope does not do itself is decode. `internal/source` wraps
[uAirwaves](https://github.com/hyperized/uAirwaves), which already drives the
RTL-SDR, demodulates Mode S, resolves CPR positions, tracks aircraft, and
works out where the receiver is from what it can hear. Rewriting that to own
it would have taken longer than the rest of the slice and would have been
wrong in different ways.

Seven packages are imported: `pkg/adsb`, `pkg/airplane`, `pkg/airplanes`,
`pkg/airports`, `pkg/location`, `pkg/scope` and `pkg/selflocate`. All of them
are data and decoding.

`pkg/radar` is not imported and will not be. That is uAirwaves' own scope,
written against tview and tcell in character cells, which is the thing uScope
exists to do differently. Importing it would drag a terminal UI toolkit into a
program that writes pixels into `/dev/fb0`. `internal/ui` is out for the same
reason, and `pkg/gps`, `pkg/coverage` and `pkg/battery` are out because
nothing here uses them yet.

The dependency is pinned to an exact commit rather than a tag. uAirwaves is a
moving target and its own local work is ahead of what is published; a pin is
the difference between a reproducible build and one that changes under you.

## What comes next

One question [DESIGN.md](DESIGN.md) left open is still open. Trails fade by
age, which was the thing to try first and looks right, but nobody has seen it
next to a version that fades by altitude. The paper theme is built now
(`--theme paper`, `l` to switch at run time); whether it is worth keeping
next to night is still a guess nobody has weighed in on.

Nobody has looked at the radar on the panel yet. That is `make radar`.

## Licence

Business Source License 1.1. See [LICENSE](LICENSE).
