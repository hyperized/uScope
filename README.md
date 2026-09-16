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
heading, each with the trail it flew in on, coloured either by altitude or by
the operator whose callsign they are flying under.

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

## The radar

`--scene radar` is the default. The left half is the scope: a square field
with the coastline under it, three dashed range rings, the cardinal letters, a
marker where the receiver is, and the airfields that fall inside the current
range as small hollow squares with their ICAO codes beside them. Each ring
carries its range in nautical miles, and an airfield that would land under
that label is left off rather than drawn through it. `a` turns the airfields
off when the field is busy enough that they are in the way, and `--airports
off` starts without them.

None of that is redrawn every frame. It goes onto a background layer of its
own and gets copied under each frame, and the layer is rebuilt only when
something it depends on moves: the canvas size, the range, the receiver's
position, the theme, or one of the two overlay toggles. Drawing a few thousand
coastline segments thirty times a second would otherwise be the most expensive
thing in the frame, and none of it changes between two frames that agree on
all of that.

The home marker's ring says where the receiver's own position came from, so
the fix state is one glance rather than a line to read:

| Ring | Means |
|---|---|
| muted grey | nothing known, so nothing can be plotted |
| ink | a position given with `--lat` and `--lon` |
| accent | a self-locate estimate, with its radius in the header |
| red | a GPS that is connected and has not locked yet |
| amber | a GPS fix without altitude |
| green | a full GPS fix |

The centre dot stays ink whatever the ring is doing, so the marker is the same
size and in the same place at any fix state. The header's mode word takes the
same colour as the ring, so the two are one signal read twice rather than two
facts to reconcile. Only the first three happen today: uScope has no GPS, and
the three GPS colours are mapped so that wiring gpsd in later is a change in
one function rather than a change in the scene as well.

Aircraft are 15 pixel silhouettes rotated to their heading. An aircraft whose
heading nobody has decoded is drawn as a bare circle, because a silhouette
would be claiming to know which way it is facing. uAirwaves marks an undecoded
heading and an undecoded velocity with -1, so a heading of zero is due north
and gets a silhouette like any other course, and a speed the receiver has not
heard reads `---` rather than a negative number.

Behind each aircraft is its trail, drawn as an anti-aliased polyline from the
oldest fix it still holds to the newest, brightening towards the head. The
trails are the reason this project exists. A character cell cannot draw one,
and a scope full of them says in one glance what a scope full of dots cannot:
who is turning and who came from where. `--no-decay` draws every segment at
full strength instead, which trades "which end is the aeroplane" for a track
that is easier to follow across a busy field.

The right column runs panel, rows, legend, top to bottom.

The panel is the selected aircraft, and it is one block rather than two. Its
callsign is set at 64 pixels, with the ICAO hex and the squawk beside it, its
track in degrees and compass points under that, three figures across the
middle (distance in nautical miles, altitude in feet, speed in knots), and a
line of smaller values under those: vertical rate with a climb or descent
triangle, position to two decimals, and how long ago the aircraft was heard.
`SEEN` reports in coarse buckets rather than in seconds, because a figure
counting up is movement the eye keeps going back to. A squawk with the
emergency flag set says `EMERGENCY` after it in the accent colour, which is the
one place on the scope the accent means something other than the selection.
With nothing in the sky the panel reads `NO TRAFFIC` and keeps its height, so
the column does not change shape when the last aircraft leaves range.

An aircraft with no callsign has its ICAO hex set in the large face, so the
corner drops the hex rather than printing the same six characters twice.

Under it is the row table, nearest aircraft first, with a small header line
naming its columns: number, callsign, ICAO hex, altitude, speed, distance and
bearing from the receiver, and the aircraft count right-aligned at the end of
that same line. The selected row carries the accent bar. Altitude
gets a small triangle beside it when the aircraft is climbing or descending,
and both the figure and the triangle are set in that aircraft's altitude band
whichever colour mode is on. That is the one column where a number and a
colour say the same thing, so the band survives airline mode instead of being
the price of turning it on: the list still answers "how high" while the scope
answers "who". The panel's altitude figure is set the same way.

Every column, and the panel's three figures, are sized from the widest value
they could hold rather than from the values on screen, so a table full of
moving numbers stays still and a value climbing through a digit never nudges
its neighbour. On a narrow right column the panel gives its figures up units
first, then a smaller face, then the speed and distance figures in that
order, keeping altitude to the last; narrower still and the line of values
under them wraps and then goes. The rows take whatever height is left under
the panel, up to twenty-four of them, and a longer list ends on a muted
`+N MORE` that counts everything not on screen.

Under that, the legend.

Units are nautical miles, feet and knots throughout, because that is what
aviation uses and converting would only make the numbers harder to check
against anything else.

### Shore

The coastlines and lake shores are drawn under everything else, in the
quietest colour either theme has, so the surroundings are recognisable without
turning the scope into a map with aircraft on it. `m` turns them off and
`--shore off` starts without them.

The data is compiled into the binary, about 2 MB of it, covering the whole
world. uScope runs on a handheld with no network and is used outside the
Netherlands as well as in it, so there was never a version of this
that asked a tile server or an Overpass endpoint for anything while it drew.

> Made with Natural Earth. Free vector and raster map data at
> [naturalearthdata.com](https://www.naturalearthdata.com).

Natural Earth is public domain. `pkg/shore/README.md` has the provenance, the
packed format and how to rebuild it; `make shore-data` is the one command.

### Minimal

`v` while the radar is running strips the scene back to the aircraft sprites
and their trails on the bare field, edge to edge. No header, no key bar, no
right column, no rings, cardinals, range labels or home marker. A run always
starts on the full scope; `v` is what gets you to minimal and what gets you
back.

The picture fills the canvas with the range mapped to half the short edge, and
nothing is clipped to a ring, so the corners show traffic that a ring would
have cut off. Nothing is drawn for the
selected aircraft either: no ring, no leader line, no label. There is no panel
here for a ring to refer to, and on an otherwise bare field a ring around one
contact reads as another contact.

Pressing `v` also takes the key bar with it, `V VIEW` included: there is
nothing left on screen for the cap to sit on.

#### Following the traffic

A directional antenna hears one part of the sky, so a scope centred on the
receiver spends half its canvas on a half of the sky with nothing in it.

Every `--recenter` interval, three minutes unless you say otherwise, minimal
mode takes the mean position of every aircraft that has one and makes that the
centre of the picture. The range is refitted at the same moment, around that
centre rather than around the antenna, so the scope is sized by how far the
traffic is spread rather than by how far away it is.

The first centring happens as soon as one aircraft has a position, and it
snaps: there is nothing on screen yet for a slide to keep continuous, and a
still frame is drawn once. Every centring after it glides over two seconds on
an ease-in-out curve, so the sprites and their trails move together and the eye
follows the picture across instead of losing it and finding it again.

`+` and `-` still step the range and pin it, and `r` hands it back to
automatic. `--recenter 0` turns the following off and leaves the picture
centred on the receiver, which is what an omnidirectional aerial wants and what
anyone comparing two renders wants. The flag takes that `0` or anything from
`10s` to `1h`, and refuses the rest rather than clamping it.

Because the picture is no longer centred on the antenna, minimal mode marks
where the antenna actually is: a small ring with a dot in it, in the quietest
colour the palette has so it does not read as a contact. The ring carries the
fix state the same way the full scope's home marker does. Following the traffic
can push it off the canvas, and then nothing is drawn for it.

#### The two overlays

`a` and `m` work in minimal mode and draw there, on a pair of toggles of
minimal's own. Both start off, so minimal opens bare, and pressing either one
leaves the full scope's pair alone. The scope is a map with
aircraft on it and minimal is aircraft with nothing behind them: one shared
pair would have meant two key presses on the way in and two more on the way
back out.

Turned on, the coastline and the airfield markers are drawn the way the full
scope draws them, around minimal's own centre and range, ICAO codes included.
There are no range labels in minimal mode for a code to collide with, so
nothing is dropped for want of room beside one.

### Colour modes

`--colour` picks what an aircraft's colour means, and `c` cycles it while the
radar is up.

`altitude` is the default: green below 10,000 feet, amber below 25,000, red
above, and grey for an aircraft whose altitude nobody has decoded yet. That is
the one thing a top-down scope cannot show by position, which is why it is
what the colours carry until asked otherwise.

`airline` paints each aircraft in its operator's own colour instead, taken from
the 409 designators in [pkg/airlines](pkg/airlines/README.md). The silhouette,
the trail, the callsign on the card and the callsign in its row all match, so
one glance ties the dot to the row. An aircraft with no callsign, or one whose
three-letter prefix is not in the database, is drawn muted. The legend then
names the four operators with the most aircraft on the field, and adds `OTHER`
when anything on it has no colour.

Brand colours are picked for print, so they are adapted to the field before
they are drawn: anything too dark to read against night's near-black field is
lifted, and anything too light for paper is brought down. The scope decides
which way round from the palette it is drawing with.

### The header

The band bleeds to the top, left and right edges rather than sitting inside the
page margin, and the hairline under it runs the full width. Inside the margin
it reads as a navy rectangle on a page rather than as a masthead, which is
visible only on the paper theme: night's band is the field colour. The type
keeps its inset, so the margin is the band's inner padding and nothing else on
the frame moves for it.

The wordmark and the ingest source on the left, with a filled dot when the
source is connected and a hollow one when it is not. The receiver's position
under them, or `EST ±22 NM` when it was worked out from the aircraft, or
`NO FIX`.

On the right, two clocks and the battery. Local time keeps the 32 pixel face;
UTC sits beside it in the 16 pixel one with a `Z` after it, because aviation
runs on UTC and a handheld in the field wants both without a key press. The
battery is drawn as a glyph filled in proportion to the charge, amber under
twenty percent and red under ten, with a lightning mark instead of a level when
it is on power. A machine with no battery shows nothing there at all: an empty
glyph would read as a flat one. On a Mac it reads the laptop battery through
the same package that reads `/sys/class/power_supply` on the uConsole, so
`--demo` on a desk shows a real figure.

### Where the aircraft come from

| Flag | Source |
|---|---|
| `--replay-iq PATH` | a captured IQ file, played back through the demodulator |
| `--beast HOST:PORT` | Mode S frames from a remote demodulator over TCP |
| `--demo` | twelve invented aircraft on straight tracks |
| none | the local RTL-SDR on Linux, the demo fleet anywhere else |

`--demo-sector` moves the invented fleet into the north-west quadrant and
leaves everything else about it alone. A real antenna on a mast hears one
sector and the demo fleet is scattered evenly all round, which is the one way
the demo is unlike every live feed. It is what to reach for when looking at
minimal mode's recentring, or at anything else that depends on where the
traffic sits rather than on how much of it there is. It is ignored on a run
that is not flying the demo fleet.

They are listed in the order they beat each other. A capture wins over a feed
so a recorded problem can always be replayed on a machine that also has a feed
configured. Giving two of them is not an error; the more specific one is
obviously what was meant.

With nothing given at all the answer depends on the machine. On Linux that is
the radio, which is the point of the uConsole. On a Mac there is no receiver to
open, so uScope flies the demo fleet and says so once on stderr rather than
refusing to start.

`--battery` is the same override uAirwaves has. Without it the battery reader
finds the first power supply of type Battery on its own, which is what happens
on the uConsole. The flag exists for a machine with more than one, or for
pointing at a fixture. The macOS reader takes its figures from `pmset` and
ignores the flag.

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

`--range NM` starts at a range instead, with auto off, which is what to reach
for when rendering a still frame: the demo fleet fits inside 40 nautical miles
and auto range will not show you a coastline three countries wide. It takes
anything from 20 to 500, or `auto`, and refuses the rest rather than clamping
it silently.

## The other scene

`--scene pattern` is a flags-only diagnostic: the orientation check from
slice 1, four coloured corner squares, a triangle pointing up, a circle and a
sweeping line. Red square top-left and cyan triangle at the top means the
frame landed the right way up, and the sweep moving means the loop is
running. It is never reached from a running radar; there is no key that
switches to it, and none of the radar's own keys do anything there either.
`--scene pattern` is the only way to see it.

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
to quit, `v` to flip between the scope and the minimal view. Ghostty draws
the frame at its real pixel size, so that is the closest look at the fonts
available without a uConsole on the desk. To see the half-block renderer:

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
| `v`, `V` | view: flip between the scope and the minimal view |
| `n`, `N`, Down | select the next aircraft |
| `p`, `P`, Up | select the previous one |
| `+`, `=` | widen the range by one step, and turn auto off |
| `-`, `_` | narrow it by one step, and turn auto off |
| `r`, `R` | auto range on or off |
| `t`, `T` | trails on or off |
| `a`, `A` | airfield markers on or off; minimal keeps its own |
| `m`, `M` | coastline on or off; minimal keeps its own |
| `c`, `C` | cycle the colour mode: altitude or airline |
| `l`, `L` | cycle the colour theme |
| `Esc` | in the radar, hand the selection back to the nearest aircraft |
| `Ctrl-C` | quit |

Both cases are bound because caps lock is easy to hit by accident on the
uConsole's keyboard, and the unshifted twins of `+` and `-` are bound for the
same reason.

The letters name what they do rather than where the thing lives: `r` for range,
`a` for airports, `m` for map, `t` for trails, `c` for colour, `l` for look,
`v` for view.

The radar scene gets first refusal on every key and passes on the ones it does
not want, which is what keeps `q` working while it is on screen. `v` is one of
the ones it takes: pressing it never reaches the run loop, because it is the
radar's own toggle between the scope and minimal. The pattern scene binds
nothing, so Esc still quits from it.

Until you choose an aircraft, the selection is the nearest contact and the rows
start at the top. `n`, `p`, Up and Down pin it to whatever they land on, and it
then stays with that aeroplane by ICAO however the distance-sorted list moves
under it. Esc lets go again, and so does the aircraft leaving the list.

The key caps carry their own state. A cap for a toggle that is on is filled,
one that is off is a hollow outline, and the two cycling keys are labelled with
the value they are on rather than with the name of the setting: `C ALT` or
`C AIRLINE`, `L NIGHT` or `L PAPER`. The scope says the same thing from its own
side, writing `AUTO` before the outer ring's range while auto range is on.

## Flags

| Flag | Default | Does |
|---|---|---|
| `--backend` | `auto` | `auto`, `fb`, `kitty`, `blocks` or `png` |
| `--scene` | `radar` | `radar` or `pattern`; `pattern` is a flags-only diagnostic with no key back to it |
| `--theme` | `night` | `night` or `paper` colour theme |
| `--colour` | `altitude` | what an aircraft's colour means: `altitude` or `airline` |
| `--airports` | `on` | draw the airfield markers: `on` or `off` |
| `--shore` | `on` | draw the coastline: `on` or `off` |
| `--range` | `auto` | scope range in nautical miles, 20 to 500, or `auto` |
| `--recenter` | `3m` | how often minimal mode recentres on the traffic, `10s` to `1h`, or `0` to stay on the receiver |
| `--no-decay` | off | draw every trail segment at full strength, no fade to the tail |
| `--battery` | | power-supply uevent file to read the battery from, Linux only |
| `--demo` | off | fly twelve invented aircraft instead of decoding any |
| `--demo-sector` | off | put the whole invented fleet in the north-west quadrant, as a directional antenna would |
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
make run-airline    # go run . --demo --colour airline
make run-beast      # go run . --beast $(BEAST)
make run-blocks     # go run . --backend blocks --demo
make run-pattern    # go run . --scene pattern
make shore-data     # rebuild pkg/shore/shore.bin.gz from Natural Earth
make test           # go test -race -cover ./...
make lint           # golangci-lint run ./...
make radar          # ship, then paint one radar frame on the panel
make pattern        # ship, then paint the test pattern on the panel
make test-device    # cross-compile the integration tests and run them on the device
```

`make ship` puts the binary on the device on its own. Once it is there,
`./uScope` over ssh opens the local radio and every flag above works the same
as it does here.

Tests that touch a real framebuffer or a real terminal sit behind the
`integration` build tag, so `make test` never opens a device. `make
test-device` is what runs them, on the hardware where they mean something.

`make shore-data` is the only target that needs the network. It downloads 15 MB
of GeoJSON to a temporary directory, packs it, and writes the 2 MB result into
`pkg/shore`. Run it when Natural Earth publishes a new release, not as part of
a build: the same two files always produce the same bytes.

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
pkg/shore             the embedded world coastlines and the packed format
internal/term         raw tty mode
internal/input        bytes to key events
internal/theme        the colour palettes
internal/source       where aircraft come from: the radio, a feed, or invented
internal/radar        the radar scene
internal/pattern      the orientation scene
internal/app          the run loop
internal/tools/shoregen  packs Natural Earth into pkg/shore/shore.bin.gz
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
RTL-SDR, demodulates Mode S, resolves CPR positions, tracks aircraft, works out
where the receiver is from what it can hear, and reads the battery. Rewriting that to own
it would have taken longer than the rest of the slice and would have been
wrong in different ways.

Eight packages are imported: `pkg/adsb`, `pkg/airplane`, `pkg/airplanes`,
`pkg/airports`, `pkg/battery`, `pkg/location`, `pkg/scope` and
`pkg/selflocate`. All of them are data, decoding or one poll loop.

`pkg/battery` is the one that is not about aeroplanes. It polls
`/sys/class/power_supply` on Linux and `pmset` on macOS behind one interface,
which is a platform problem already solved once next door and not worth solving
again here. The radar only sees two methods off it, declared in
`internal/radar` where they are consumed, so a test hands over a struct of its
own instead of a poller.

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
next to a version that fades by altitude.

Two more went with it. The paper theme is built (`--theme paper`, `l` at run
time) and airline colouring is built (`--colour airline`, `c` at run time), and
neither has been judged against its alternative by anyone who was holding the
device at the time.

Nobody has looked at the radar on the panel yet. That is `make radar`. The
battery indicator has been checked two ways, neither of them on the device: on
the Mac it reads the laptop through `pmset`, and the Linux reader was pointed
at a hand-written uevent file in an arm64 container with `--battery`. What has
not been tried is autodiscovery under `/sys/class/power_supply` on the
uConsole itself.

## Licence

Business Source License 1.1. See [LICENSE](LICENSE).
