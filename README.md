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

`--scene radar` is the default. The left half is the scope: a square field with
the sea tinted and the coastline drawn over it, three dashed range rings, the
cardinal letters, a marker where the receiver is, and the airfields that fall
inside the current range as small hollow squares with their ICAO codes beside
them. Each ring carries its range in nautical miles, and an airfield that would
land under that label is left off rather than drawn through it. `a` turns the
airfields off when the field is busy enough that they are in the way, and
`--airports off` starts without them.

None of that is redrawn every frame. It goes onto a background layer of its
own and gets copied under each frame, and the layer is rebuilt only when
something it depends on moves: the canvas size, the range, the receiver's
position, the palette, or one of the two overlay toggles. Drawing a few thousand
coastline segments thirty times a second would otherwise be the most expensive
thing in the frame, and none of it changes between two frames that agree on
all of that.

The home marker's ring says where the receiver's own position came from, so
the fix state is one glance rather than a line to read:

| Ring | Means |
|---|---|
| muted grey | nothing known, so nothing can be plotted |
| ink | a position given with `--lat` and `--lon` |
| green | a GPS fix, with or without altitude |
| amber | a self-locate estimate, or a GPS fix that has gone with its last position still on screen |

The centre dot stays ink whatever the ring is doing, so the marker is the same
size and in the same place at any fix state. The header's mode word takes the
same colour as the ring, so the two are one signal read twice rather than two
facts to reconcile. A guess and a stale fix share the same amber, because both
are the same caution to the eye: the position under them might not be where
the receiver actually is right now. Red is kept for `EMERGENCY` alone, so a
squawk in anger is the one thing on the whole scope that reads as a fault.

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
who is turning and who came from where.

`t` cycles four trail modes, and the key cap names the one you are in rather
than the setting, so the bar answers the question instead of restating it.

| Cap | Draws |
|---|---|
| `T OFF` | nothing |
| `T SHORT` | the last twelve fixes, fading to nothing at the tail |
| `T LONG` | the whole history, fading to a quarter strength at the tail |
| `T ALL` | the whole history at full strength, and the ghosts with it |

`LONG` is where a run starts and it is what the scope has always drawn. `SHORT`
is twelve fixes because uAirwaves samples one position every ten seconds, so
it is about the last two minutes: it answers "where is this one going" and not
"where has it been", which is what you want on a field of eighty aircraft where
the trails cross each other more than they tell you anything.
`ALL` goes the other way. Nothing fades, so the whole track reads as one line
rather than as a line arriving from nowhere, which is the mode for looking at
the shapes the traffic makes rather than at the traffic.

`ALL` is also the only mode that draws ghosts. When a contact goes quiet and
the store drops it, its track stays as a ghost with nothing at the head of it:
no silhouette, no label, no selection ring, because there is no aeroplane there
to mark and no heading to point one at. Ghosts go under the live trails, so an
aeroplane is never hidden by the track of one that has gone. They take no part
in the auto range or in minimal mode's centring, both of which are about where
the receiver can hear right now.

The tracking runs on every session whether or not the mode that draws them is
on. It used to be tied to a flag, which made it useless to anyone who did not
know in advance that they would want it: switching to `ALL` an hour in is a way
of asking what you have missed, and the answer is nothing at all unless
something was already keeping it. Ghosts are kept for the life of the process,
up to two thousand tracks or a million fixes between them, oldest given up
first, which is what bounds the cost of leaving it on.

That is also the one place the demo fleet behaves like a real feed. Ninety
seconds in, the aircraft with no callsign stops transmitting and never comes
back, so `--demo` and three presses of `t` leave a ghost on the field to look
at.

The right column runs the status line, the selected aircraft's panel, the
flight strip board, then the legend, top to bottom.

The status line sits above the lot, right-aligned and set in the data colour
because it is a reading about the machine rather than about any one aeroplane:
`SEL 07 · 81 AIRCRAFT`, or `SEL 07 · 12 OF 81 AIRCRAFT` while a filter is
narrowing the field. The first figure is where the selection is standing in the
list, so `n` and `p` move a number as well as a mark. It reads `SEL --` when
nothing on the board is selected, and adds `(FILTERED)` in the caution colour
while the filter is hiding the aeroplane that was pinned.

Under it is the panel: the radar data block the scope's own tag is built from,
set large. A `SELECTED` label, a 1 pixel accent rule down the left edge, the
callsign in the large face at double scale with the ICAO hex and `SQ nnnn`
stacked beside it, then five figures in the large face with a small cyan word
under each. `LEVEL` is hundreds of feet with the trend arrow after it, set in
the aircraft's own altitude band because that is the one figure the scope
beside it also says. `GS` and `TRK` follow, then `DIST` and `BRG` on a second
row with the bearing needle turned to it. `TRK` carries no needle of its own:
the block already has one, and a second eight pixels away would read as a pair
of directions to reconcile rather than as one to fly.

Under the block, one muted line spells the shorthand out:
`2,400 FT CLIMBING 1,800 FT/MIN · 52.30 N / 4.81 E · SEEN JUST NOW`. Everything
above it is controller abbreviation, and 024 is two thousand four hundred feet
to anyone who has worked a radar and a three-digit number to everyone else. The
panel gives things up as the column shortens, in this order: the plain line
first, then the callsign's second scale, then the `DIST` and `BRG` row. An
empty sky leaves the block its shape and puts `NO TRAFFIC` where the callsign
goes, so the column does not change height when the last aircraft leaves range.
A pinned aircraft the filter is hiding keeps the panel, with `· FILTERED` after
the label in the caution colour, until the filter widens enough to let it back
in.

The board under that is one strip per aircraft, all of them the same height,
about 30 pixels at the panel's own resolution, in the order the list is sorted
in. The selected aeroplane's strip is marked where it stands rather than
promoted: a 3 pixel accent bar down its left edge, and its index set in the
reading ink where every other index is muted. The board used to lift it to the
top as a full-height card, which meant pressing `n` swapped which aeroplane was
on top and nothing on screen said where in the list the operator had got to.

Every strip opens with that index, at the far left of the ident field: the
aircraft's rank by distance under whatever filter is on, `01`, `02`, and so on.
It is the cursor. Room is reserved for three digits, because the index counts
places in the whole list rather than rows on the board, and a list of a hundred
and twenty aircraft has to be able to write one without the callsigns beside it
stepping sideways.

As many strips are drawn as the column has room for, and the window scrolls so
the selected strip is always fully on screen, riding a third of the way down
once the list is long enough for the window to have a choice about it. Muted
`+3 ABOVE` and `+57 MORE` lines close either end the window has cut, each
taking one strip's room, so the strips on screen and the two numbers always add
up to what the filter is showing.

Seven fields run left to right on every strip, with a hairline rule between
each pair: `CALLSIGN / ICAO`, `LEVEL FT`, `GS KT`, `TRK`, `DIST NM / BRG`,
`POS` and `SEEN`. A small header in the data colour names each one, set once in
a row above the board, because repeating FT and KT over every aircraft would
spend a third of the board saying what it had already shown. The units live in
those headers and nowhere else.

A strip has one line, so the ICAO hex sits beside the callsign in the bold body
face rather than under it. An aircraft with no callsign gets its hex in the
callsign's own place rather than printing the same six characters twice. An
aircraft squawking an emergency draws `EMERGENCY` as a filled red box with
white text, in the hex's place, which has no room for both and no use for a hex
while an aircraft is declaring one. A box rather than a coloured word, because
it is the one thing on the board that has to be seen without being looked for.
The same box follows the squawk on the panel above.

Climb and descent get a small triangle beside the level figure, on every strip.
The rate itself, in feet per minute, is on the panel alone: a column of rate
figures nobody is reading is noise, and the one aircraft whose rate is worth a
number is the one that has been picked. `SEEN` reports in coarse buckets rather
than in seconds, because a figure counting up is movement the eye keeps going
back to. `POS` sets its two halves side by side, which is the shape the field
was sized for in the first place.

The little attitude model sits at the right end of the ident field, in a
24-pixel cell: the same low-polygon aeroplane the 3D view draws, yawed to the
aircraft's heading and pitched and banked by the same rules, seen through a
camera of its own that is fixed north up and looking down from due south, so
every strip reads the same angle down the board. An aircraft with no decoded
heading gets the disc, the same as in the picture. It takes the aircraft's own
colour on every strip, the selected one included, because reading the board and
reading the scope are meant to be the same act of recognition.

Altitude gets its figure and its triangle set in that aircraft's altitude band,
whichever colour mode is on. That is the one field where a number and a colour
say the same thing, so the band survives airline mode instead of being the
price of turning it on.

Every field is sized from the widest value it could hold rather than from what
is on screen, so a board full of moving numbers stays still and a value
climbing through a digit never nudges its neighbour. As the column narrows,
fields are given up whole rather than squeezed, in this order: `POS`, `SEEN`,
`TRK`, the attitude model, then `GS`. The identity, the level and the range are
never given up; a column too narrow even for those draws no board at all.

Under that, the legend.

Units are nautical miles, feet and knots throughout, because that is what
aviation uses and converting would only make the numbers harder to check
against anything else.

### Shore

The coastlines and lake shores are drawn under everything else, in the
quietest colour any of the six palettes has, so the surroundings are
recognisable without turning the scope into a map with aircraft on it. `m`
turns them off and `--shore off` starts without them.

Under the outlines, the sea is tinted. The scope floods the ground inside the
range ring with a colour eight per cent off the field towards the palette's
data hue, then paints the land back out of it in the field colour, so land
reads from sea at a glance the way it does on a glass cockpit map. The tint
comes out cyan-black on Glass night, a deeper green on Phosphor, a lighter grey
on Mono where there is no hue to borrow, and a faint blue-grey on the three day
pages. Lakes are water again: the IJsselmeer is a hole in the land, not a lake
shape drawn on top of one.

The fill is why there is a second data file. A coastline is a line, and nothing
in a line says which side of it is water, so the land has to arrive as
polygons. Every land ring in view is filled in one even-odd pass, which is what
makes a lake inside a landmass come out as sea without anything recording which
rings are lakes.

`m` and `--shore` govern the fill and the outlines together. They are one
picture rather than two overlays: a coastline with no fill behind it says where
a line is, and the fill is what says which side of it is sea. The two tilted
views keep the outlines and no fill; see the open list in DESIGN.md.

The data is compiled into the binary, about 4 MB of it in two files, covering
the whole world. uScope runs on a handheld with no network and is used outside
the Netherlands as well as in it, so there was never a version of this
that asked a tile server or an Overpass endpoint for anything while it drew.

> Made with Natural Earth. Free vector and raster map data at
> [naturalearthdata.com](https://www.naturalearthdata.com).

Natural Earth is public domain. `pkg/shore/README.md` has the provenance, both
packed files, the cell scheme they are cut into and how to rebuild them;
`make shore-data` is the one command and it writes both.

### Minimal

`v` while the radar is running strips the scene back to the aircraft sprites
and their trails on the bare field, edge to edge. No header, no key bar, no
right column, no rings, cardinals, range labels or home marker. A run starts on
the full scope and `v` cycles from there: scope, 3D, minimal, bare 3D, scope.
`--view` picks which one it starts on instead.

The order changes one thing at a time. The first press tilts the picture, the
second takes the furniture away, the third tilts it again and the fourth puts
everything back, so the two flat views and the two perspective ones are each a
single press apart.

The picture fills the canvas with the range mapped to half the short edge, and
nothing is clipped to a ring, so the corners show traffic that a ring would
have cut off. Nothing is drawn for the
selected aircraft either: no ring, no leader line, no label. There is no strip
board here for a ring to refer to, and on an otherwise bare field a ring around
one contact reads as another contact.

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

The bare 3D view reads the same pair. It is one choice about how much furniture
a bare picture carries, and the two bare views are a single press apart.

Turned on, the coastline and the airfield markers are drawn the way the full
scope draws them, around minimal's own centre and range, ICAO codes included.
There are no range labels in minimal mode for a code to collide with, so
nothing is dropped for want of room beside one.

### The 3D view

Press `v` once and the scope box becomes a perspective picture: the same
traffic seen from a camera orbiting the receiver, with altitude drawn as height
instead of as colour. Only that square changes. The header, the right column
and the key bar are the same furniture they were.

The ground is the range rings projected as polylines, the cardinal letters out
at the outer ring, the coastline when `m` is on and the airfields when `a` is
on. `+`, `-` and `r` still move the range, and everything on the ground moves
with it. There are no range numbers on the rings: in perspective a ring is an
ellipse, and a number pinned to one point of it would only be true from one
side of the orbit.

Every aircraft gets a thin stalk from its shadow on the ground up to where it
is flying. The stalk is the only thing in the picture that says how high an
aeroplane is: a shape on its own floats at a height the eye cannot measure, and
two aircraft on the same bearing at different levels draw at nearly the same
place. Trails are drawn in the air at the altitude each fix was reported at, so
a descent reads as a descent.

On the end of each stalk is a model rather than the flat scope's rotated
sprite. It is fourteen vertices in the aircraft's own axes, a thin hull from
nose to tail, swept wings, a tailplane and a fin, filled as triangles with the
fuselage centreline stroked over them so the shape survives at sixteen pixels.
The sprite was a plan view, and this camera is never directly above anything:
turning a plan view to a heading in a perspective scene read as a sticker on
the glass. The model is posed in the world and goes through the same camera as
the rings under it, so it foreshortens with everything else.

The pose is three angles and all three come off what the receiver has decoded.
Yaw is the heading. Pitch is eight degrees nose-up above 300 feet a minute and
eight down below minus 300, level in between: three states and not a
measurement, because at this size there is nothing to draw between five hundred
feet a minute and two thousand.
Bank comes off the last two legs of the trail, up to fifteen degrees, which is
a standard-rate turn at uAirwaves' ten-second sampling. It is taken from two
legs rather than from one leg against the reported heading because heading and
track differ by the drift angle the wind puts on them, and between two legs
flown a minute apart the drift is the same in both and cancels.

The wing on the far side of the camera is mixed forty per cent into the field
and the near one drawn at full strength, which is the only cue a shape this
small has for which way up it is. Within one aircraft the order is far wing,
fin, fuselage, near wing; there is no depth buffer, because an aeroplane is
convex enough that the order is the sort. The whole model is a constant sixteen
pixels across whatever the range, scaled per aircraft from the projected length
of a unit vector at its own depth, and an aircraft with no decoded heading gets
a small filled disc instead: there is no attitude to pose a model in, and a
model drawn pointing north anyway would be the one thing in the picture stating
a fact nobody has.

Altitude is stretched by `--exaggerate`, 1 to 20, and 8 unless you say. At life
size the whole fleet lies in a film on the floor: forty thousand feet is six
and a half nautical miles against a scope tens of miles across. At 8 an
airliner sits about as far above the ground as the outer ring is wide, which is
where two aircraft a flight level apart are visibly a flight level apart.

The camera's distance is set by whichever of two targets needs more room: the
outer range ring across 85 percent of the box's width, which is the rule that
has always held, and the envelope's own top ring inside 85 percent of the
box's height, which is new. At the default exaggeration of 8 the second one
wins, so the ring comes out narrower than it used to and the top of the
envelope is no longer cut off by the edge of the box, in the wide view with
the column hidden as well as the square one with it showing.

The camera turns on its own, one revolution every two minutes. Left and Right
nudge it fifteen degrees and stop it turning. `o` is the switch: it stops the
orbit where the picture has it, and starts it again from there. Stopping is
usually what you want it for, to hold one side of the envelope still while
reading it. `[` and `]` tilt the camera between ten and eighty degrees of
elevation, starting at thirty-five.

The bar lists all four while the view is up: `O ORBIT`, `E ENVELOPE`, a cap
with the two arrows labelled `TURN`, and `[ ]` for `TILT`. The orbit and
envelope caps show the green engaged bar while their setting is on and none
when it is off. None of the four appears in the two flat views, where the
keys do nothing.

#### The envelope

`e` toggles the receiving envelope, which is on when the view opens. It is two
shapes drawn together, from two different sources.

The first is the theoretical one: the radio horizon. A ring at every five
thousand feet up to forty-five thousand, at the distance an aircraft at that
height would come over the horizon for an antenna thirty feet up, drawn dashed
with eight meridians running up the outside of it. The distance is `1.23 * (sqrt(h_antenna) + sqrt(h_aircraft))`
nautical miles, the standard VHF and radar line-of-sight approximation, which
folds atmospheric refraction in by pretending the earth is a third larger than
it is.
It is why an antenna in an attic hears an airliner two hundred and fifty miles
away when the geometric horizon is under seven. Every ring is clamped to the
current range, so at forty miles the bowl reads as a cylinder, which is honest:
at that range every altitude above five thousand feet is over the horizon and
the bowl has nothing left to say. Open the range past a hundred and the curve
appears on its own.

The second is the measured one: where the antenna has actually heard
something. uScope runs uAirwaves' coverage tracker behind every
source, binning each decoded fix by distance, altitude band and bearing sector,
and the wireframe is a ring through the sixteen sectors at each altitude band
with vertical edges joining the bands. Each band's edge is the 98th percentile
of what has actually been observed in it rather than the single farthest bin
ever touched, so one stray mis-decoded position at long range does not drag
the whole band out to it. A sector nothing has ever
been heard in is skipped, so a directional antenna, or one with a chimney on
one side of it, comes out lopsided rather than round. `--demo-sector` shows what
that looks like without a receiver: the invented fleet sits in one quadrant and
so does its envelope.

Two bands with an empty one between them are not bridged. An edge drawn through
a band nothing was heard in would be claiming reception the tracker never saw.

Both shapes are drawn faded into the field rather than at full strength. The
measured mesh is the palette's data colour mixed 35 percent into the field,
and the theoretical bowl is the muted colour mixed in at the same 35 percent.
The bowl used to sit at 60 percent, but even that dim it still outshone the
mesh by brightness, so both were brought to the same fade and the hue alone
now tells them apart. The mesh also moved off the accent, because the accent
belongs to the selected aircraft alone and the mesh is a record of what the
antenna has heard rather than a fact about any one aeroplane. At full strength
either shape used to outshine the aircraft inside it, so the picture read as a
wireframe with some dots caught in it: the envelope is context, and context
that outshines its subject is in the way. The mixing happens once per frame
and the lines are then drawn solid, which on a near-uniform field gives the
same picture as blending every pixel for a fraction of the work.

The 3D view keeps no background layer. The camera moves on every frame the
orbit is running, so a cached picture would be rebuilt each time and cost the
same drawing plus a copy of the canvas on top. It draws straight into the frame
instead, clipped to the scope box so nothing lands on the flight list, and it
still allocates nothing.

#### The bare 3D view

`--view minimal3d`, or the third press of `v`, is what minimal is to the scope,
done to the perspective picture. The models, their trails in the air and the
receiver's own marker, across the whole canvas. No header, no key bar, no
column, and on the ground no rings, no cardinal letters, no range labels and no
envelope.

It draws no stalks either. The full view puts one under each aircraft because
it is the only thing that says how high the aeroplane is; on a bare field with
nothing else on screen, the trail already says where it has been, and a forest
of verticals under the fleet was the one thing worth taking away along with
the rest of the furniture.

It follows the traffic the way minimal does, on the same `--recenter` cadence
and the same two-second glide, and it refits the range around the same centre.
The camera keys are the ones the full view uses: the orbit runs, Left and Right
nudge it, `[` and `]` tilt it. `e` is the one that does not, because there is no
envelope to toggle; like the camera keys outside the 3D views, it falls through
rather than changing a setting nothing on screen could show.

`a` and `m` reach the bare pair of overlay toggles, so the coastline and the
airfields can be put back on the ground a piece at a time. The selection keys
still move the selection and nothing is drawn for it, which is minimal's rule:
there is no strip board anywhere for a ring to refer to.

### Hiding the column

`w` takes the right column off the frame, and the picture gets the whole width
between the header band and the key bar. The status line, the panel, the strips
and the legend go together; the header and the key bar stay exactly where they are,
and `W WIDE` in the bar shows its green engaged bar while the column is away.

The two views behave differently under it, because they measure the box
differently. The flat scope keeps the ring it had, sized by the height, and
moves it to the middle of the frame: the same picture with air either side
instead of a flight list. The perspective view genuinely grows, because its
lens is the box's own width, so whichever of its two framing targets is
binding comes out proportionally larger and everything on the ground grows
with it.

The selection keys keep working with the column away, and so does the filter.
The scope still shows which aircraft is selected, and its tag still says what
that aeroplane is doing; what is gone is the panel and the board under it.

There is no flag for it. Hiding the figures is something you do while looking
at the scope rather than something you decide before the program starts. The
two bare views have no column, so `w` falls through there.

### Colour modes

`--colour` picks what an aircraft's colour means, and `c` cycles it while the
radar is up.

`altitude` is the default: green below 10,000 feet, white between there and
25,000, orange above, and muted grey for an aircraft whose altitude nobody has
decoded yet. The ramp is the cockpit's own, low to high, and altitude is the
one thing a top-down scope cannot show by position, which is why it is what
the colours carry until asked otherwise.

`airline` paints each aircraft in its operator's own colour instead, taken from
the 409 designators in [pkg/airlines](pkg/airlines/README.md). The silhouette,
the trail and the callsign match on the panel and on the strip, so one glance
ties the dot to the list. An aircraft with no
callsign, or one whose
three-letter prefix is not in the database, is drawn muted. The legend then
names the four operators with the most aircraft on the field, and adds `OTHER`
when anything on it has no colour.

Brand colours are picked for print, so they are adapted to the field before
they are drawn: anything too dark to read against night's near-black field is
lifted, and anything too light for the day page is brought down. The scope decides
which way round from the palette it is drawing with.

### Traffic filter

`f` and `F` cycle a filter that narrows the field to one entry of whichever
legend the colour mode is currently drawing, and the key cap between `C` and
`L` names the value it is on, the same way `T` and `C` name theirs.

In altitude mode the cycle runs:

| Cap | Shows |
|---|---|
| `F ALL` | every aircraft |
| `F <10K` | under 10,000 feet |
| `F 10-25K` | 10,000 up to 25,000 feet |
| `F >25K` | 25,000 feet and above |

back to `F ALL`. Those are the same three bands the legend names and the same
boundaries the altitude colours use, so a value under `f` and a colour on the
scope never disagree about which band an aircraft belongs to. An aircraft
whose altitude nobody has decoded yet carries no band at all, so it comes off
the field the moment any band is picked. In airline mode the cycle runs `ALL`,
then each operator the legend is currently naming, most aircraft first, then
`OTHER`, then back to `ALL`. `OTHER` is the aircraft with no callsign or one
the database does not know, the same aircraft the legend's own `OTHER` row
stands for, and the cycle only offers it when the legend is drawing one: a
field with nothing uncoloured has nowhere for that value to point. The filter
holds an operator by its three-letter designator rather than by its rank in
the legend, so an airline that slips down the count keeps the scope pointed at
it instead of quietly handing the field to whichever operator took its place.
Pressing `c` puts the filter back to `ALL` along with the colour mode, because
a filter value is one entry of a legend and `c` is what replaces the legend.

An aircraft the filter is holding back is not drawn anywhere: no silhouette on
the flat scope, no model, stalk or label in the 3D view, no trail in any view,
no strip on the board. Minimal mode's centroid and the auto range both
work from what is left, so narrowing to the low band on a scope that had
widened to 180 nautical miles pulls the range in with it. Ghost trails are the
one exception. A ghost is the track of an aircraft that has stopped
transmitting, so there is no live aircraft left to test against a band or a
callsign, only a last reading that could be any age, and hiding a track on a
reading that old would be a decision nobody watching the scope could check.

The status line above the panel says which state you are in: `SEL 07 · 81
AIRCRAFT` at `ALL`, `SEL 07 · 12 OF 81 AIRCRAFT` while a filter is on, so a
short board under a filter reads as the filter doing its job rather than as a
quiet sky. The
legend marks its own place in this too: whichever entry the filter is on gets
a ring around its swatch, drawn outside it in the reading ink, so the legend
says which of its own rows the scope is showing.

Selection follows the same rule as everything else, with one exception for a
pin. An unpinned selection moves to the nearest aircraft the filter is still
showing, so it follows the field when a value hides the aircraft it was on. A
pinned aircraft the filter hides stays selected: it keeps the panel, with
`· FILTERED` after the label in the caution colour and `SEL -- (FILTERED)` on
the status line, until the filter widens enough to let it back in. It has no
strip on the board while it is hidden, because the board is the list the filter
left. A pinned aircraft that leaves the list
altogether still loses its pin, exactly as it did before the filter existed.
There is no flag for the filter; it is reachable from `f` and nowhere else,
the same as the trail modes.

### The header

The band bleeds to the top, left and right edges rather than sitting inside the
page margin, and the hairline under it runs the full width. Inside the margin
it would read as a rectangle on a page rather than as a masthead. Night's own
band sits a shade above its true-black field rather than flush with it, for
the same reason: flush, the strip would only ever have read as a band on the
day theme. The type keeps its horizontal inset, so the margin is the band's
inner padding on the left and nothing else on the frame moves for it.

Vertically the type is centred in what the fill covers rather than in the room
the layout reserves under the margin. The two are a whole margin apart, and
measured against the second the band sat with twenty-two pixels of air over the
wordmark and six under the receiver line, which on something that reads as a
masthead is the first thing anyone notices. The clocks and the battery share
the same centre, so the band reads level right across.

The wordmark reads `uScope`, in the project's own spelling rather than in the
all-caps every label in the scene uses. A wordmark is a name and not a label.
Set `USCOPE`, it made the band the one place in the project that disagreed with
the binary, the repository and this file about what the thing is called.

The wordmark and the ingest source on the left, with a filled dot when the
source is connected and a hollow one when it is not. The receiver's position
under them, opening with `LOC`: `LOC GPS 3D 52.3100 N / 4.7700 E` for a fix,
`LOC GPS 2D` for one with no altitude in it, `LOC GPS LOST` while the last fix
is being held after the lock went, `LOC MANUAL` for coordinates you typed in,
`LOC EST ±22 NM` when it was worked out from the aircraft, and `LOC NO FIX`
when none of that has happened yet. An estimate the self-locator does not
fully believe gets a `?` after the radius. Without the prefix the line was a
mode word and two numbers with nothing saying what they were of, and next to
the aircraft's position on the selected strip it read as another aeroplane.
`LOC` is drawn in the band's own ink whatever the fix mode is; only the mode
word after it carries the fix colour.

While `--auto-sweep` is walking the gain grid the source label picks up a
`SWEEP` suffix in the caution colour. A sweep decodes nothing for the few
seconds it runs, so without the marker the scope is empty for a reason that
has not gone wrong, which otherwise reads as a broken receiver. The marker
takes its room out of the label's budget rather than being appended after it,
so a long `--beast` address is cut one character shorter instead of pushing
`SWEEP` across the clocks.

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

Where the receiver itself is has its own section, below.

### Where the receiver is

Three ways of knowing, and they beat each other in this order:

| Flag | Position |
|---|---|
| `--lat` and `--lon` | what you typed, and nothing argues with it |
| `--gpsd HOST:PORT` | a real fix from a gpsd daemon |
| neither | worked out from the aircraft you can hear |

`--lat` and `--lon` are both or neither: a latitude with no longitude is half
an answer. They also turn the other two off rather than leaving three opinions
to reconcile, and one line on stderr says so, because an operator who has just
plugged a GPS in would otherwise spend a while wondering why the header never
says GPS.

`--gpsd` defaults to `localhost:2947` on Linux and to `off` everywhere else. A
Mac has no gpsd on it, and a watcher retrying a refused connection would put a
warning on stderr answering a question nobody asked. The daemon is not uScope's
to set up: on the uConsole that is uAirwaves' `make ship`, which installs gpsd,
points `/etc/default/gpsd` at the AIO board's GNSS on `/dev/ttyS0` and enables
the units. `make gps-check` in that repo says what it is seeing right now.

uScope does not wait for the daemon. The watcher reconnects on its own, so a
gpsd that is not up yet, or a receiver that has not locked, leaves the scope on
the estimate until a fix arrives and then switches over without a restart. A
gpsd that stays down warns once rather than once per retry.

When the fix goes, the last one is kept for thirty seconds and the header says
`LOC GPS LOST`. A receiver under a roof or a gantry loses lock for a few
seconds and gets it back, and dropping straight to the estimate would move the
scope's centre by tens of nautical miles and then move it back, which reads as
a fault. Past thirty seconds the dropout is not a dropout any more and the
estimate takes over. Thirty is also how long uAirwaves' own client waits before
it decides a silent socket is dead and reconnects.

With no GPS at all, uScope works its position out from the aircraft it can hear
by intersecting their radio horizons, which takes about thirty position reports
and lands within tens of nautical miles. When those circles cannot all be true
at once the answer is a compromise between them, and the header puts a `?`
after the radius: `LOC EST ±22 NM ?`. The radius is widened to cover that
disagreement already, but a wide radius on its own reads as an estimate that is
merely vague, which is a different and more comfortable thing than an estimate
its own observations argue with.

A known position is worth giving if you have one, whichever way it arrives.
With a reference nearby a single CPR frame resolves to a position; without one
the decoder waits for the matching half of the pair, which takes up to ten
seconds per aircraft.

### The bias-tee and the gain sweep

An external LNA at the antenna is powered up the coax, and the dongle's
bias-tee is what puts 5 V on the centre conductor. `--bias-t` turns it on, and
because rtl2832u pulls the pin high during chip configuration rather than
afterwards, the LNA is already running by the time anything else touches the
radio.

`--auto-sweep` walks the gain grid once before the first frame and keeps the
cell that decoded best. The order matters: a sweep run against an unpowered LNA
measures a chain that is not the one that will be receiving, picks the wrong
cell, and leaves the receiver sitting there deaf. Give `--bias-t` as well when
there is an LNA on the mast, or leave both off.

Both settings only mean something when uScope is driving the radio itself.
Under `--beast` the gain belongs to whoever runs the remote demodulator, a
captured IQ file has no gain at all, and the demo fleet has no antenna in front
of it. In all three cases the flags are ignored rather than refused, which is
what uAirwaves does with the same pair, and one line on stderr says so:

```
uScope: --bias-t and --auto-sweep need the local SDR; ignored under --demo
```

Silence would be the wrong answer there. `--bias-t` is the flag that powers
somebody's LNA, and an operator who believes it is powered when it is not
spends the next hour wondering why the scope is so quiet.

The `b` key flips the bias-tee while uScope is running, so the LNA can be cut
without restarting. The key bar grows a `B BIAS-T` cap while the source has a
bias-tee to flip, filled when the LNA is powered and hollow when it is not, and
the cap is absent entirely under `--demo`, `--beast` and `--replay-iq`.

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

## Looks and themes

uScope draws in one of three looks, and each look has a night and a day theme,
so six palettes in all. The layout does not move with them. The flight strips,
the data block, the tag hanging off the selected aircraft and the row of
softkeys are the same picture whichever palette is on, and only the colours
change. `k` cycles the look and `l` cycles night and day, both on whichever
scene is on screen, and `--look` and `--theme` pick the pair to start on. The
key bar says which of each is on rather than naming the setting, so the two
caps read `K PHOSPHOR` and `L NIGHT` rather than `LOOK` and `THEME`.

Glass is the default, and the only one of the three with a vocabulary behind
it. It is a glass cockpit's colour grammar rather than decoration: magenta is
the one thing that is selected, cyan is a reading about the machine, green is
valid or engaged, amber is a caution, red is kept for a genuine warning, and
every control sits in a grey softkey box. Anyone who has flown
behind a Garmin panel already knows all of it. Night is a true black field with
white ink. Day is the same grammar on a cool light grey, which is the page a
glass panel puts up for its daytime view, with every hue pulled down far enough
to still mean the same thing against the lighter field: the magenta deepens,
the cyan becomes a teal, and the greens and reds darken rather than change.

Phosphor commits to the CRT. Field, ink and rules are one green and the
selection is bright phosphor instead of magenta. Because green is the ground
here rather than a reading, the altitude bands have to move off it: low goes
cyan, the middle band is the pale phosphor itself, and high is an orange burn.
Day is the same world as pale green chart paper under a dark green band. It is
the look nobody mistakes for anything else, and it is also the one that costs
airline mode the most, since brand colours land on a field that was not picked
with them in mind.

Mono has no accent hue at all. Selection is made with contrast instead, which
is the strongest mark a pixel display has and the one thing that survives any
palette. The only colour left on screen is the three altitude bands and, in
airline mode, the operators. Red stays on the warning: an emergency is a
meaning rather than an accent, and it is the one word on the board that has to
be read from across a room.

Night is the default inside every look. A light field on a backlit handheld is
a torch in the face in the dark and costs battery all day.

## Fonts

uScope draws text with PSF console fonts, the same bitmap format the kernel
loads into a virtual terminal. Four faces are compiled into the binary:

| Face | Size | What it sets |
|---|---|---|
| Terminus | 6x12 | labels, unit suffixes, softkey labels |
| Terminus | 8x16 | body text, the scope tag and the panel's codes |
| Terminus Bold | 8x16 | the wordmark, softkey letters and the strips' callsigns |
| Terminus Bold | 16x32 | the clock, the panel's figures, and its callsign at double scale |

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

The uConsole's GNSS is not uScope's to install. uAirwaves' `make ship` is what
sets gpsd up on the device, and once it is running uScope finds it on
`localhost:2947` with no flag at all. There is nothing to add to this Makefile
for it.

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
| `v`, `V` | view: cycle scope, 3D, minimal, bare 3D |
| `n`, `N`, Down | select the next aircraft |
| `p`, `P`, Up | select the previous one |
| `+`, `=` | widen the range by one step, and turn auto off |
| `-`, `_` | narrow it by one step, and turn auto off |
| `r`, `R` | auto range on or off |
| `t`, `T` | cycle the trail mode: off, short, long, all |
| `a`, `A` | airfield markers on or off; minimal keeps its own |
| `m`, `M` | coastline and the water fill on or off; minimal keeps its own |
| `c`, `C` | cycle the colour mode: altitude or airline |
| `f`, `F` | cycle the filter to one entry of the current legend, then back to all |
| `l`, `L` | cycle the colour theme: night or day |
| `k`, `K` | cycle the look: glass, phosphor or mono |
| `w`, `W` | hide the right column and give the picture the whole width |
| `b`, `B` | bias-tee on or off; only bound when the source has one |
| `e`, `E` | full 3D view only: the receiving envelope on or off |
| `o`, `O` | either 3D view: the camera orbit on or off |
| Left, Right | either 3D view: nudge the camera 15 degrees and stop the orbit |
| `[`, `]` | either 3D view: tilt the camera, 10 to 80 degrees |
| `Esc` | in the radar, hand the selection back to the nearest aircraft |
| `Ctrl-C` | quit |

The four camera keys are claimed by the two perspective views and by nothing
else. In the scope and minimal views there is no camera to move and no envelope
to toggle, so they fall through to the run loop rather than quietly changing
state nothing on screen could show. The key bar says the same thing from its
side: `O ORBIT`, `E ENVELOPE`, `TURN` and `TILT` only appear while the full 3D
view is up. `e` follows the same rule one level further in: the bare 3D view
draws no envelope, so it does not take the key either.

`w` is the mirror image. It is claimed by the two views that have a column and
falls through in the two that do not.

Both cases are bound because caps lock is easy to hit by accident on the
uConsole's keyboard, and the unshifted twins of `+` and `-` are bound for the
same reason.

The letters name what they do rather than where the thing lives: `r` for range,
`a` for airports, `m` for map, `t` for trails, `c` for colour, `l` for look,
`v` for view, `w` for wide, `b` for bias-tee.

`b` behaves like the camera keys: it does nothing and falls through to the run
loop on a source with no dongle behind it, and the `B BIAS-T` cap stays off the
bar to say so. Flipping a bias-tee is a USB control transfer, and a dongle
wedged by an unplug mid-write can take seconds to answer, so the press hands
the work to a worker goroutine and returns at once. A second press while one
flip is still running is dropped rather than queued. The cap shows the cached
state that arrived on the last frame, never a fresh read of the chip, so
nothing on the draw path can block on the bus.

The radar scene gets first refusal on every key and passes on the ones it does
not want, which is what keeps `q` working while it is on screen. `v` is one of
the ones it takes: pressing it never reaches the run loop, because it is the
radar's own cycle through its four views. The pattern scene binds
nothing, so Esc still quits from it.

Until you choose an aircraft, the selection is the nearest contact, which is
the first strip on the board. `n`, `p`, Up and Down
pin it to whatever they land on, and it then stays with that aeroplane by ICAO
however the distance-sorted order moves under it. Esc lets go again, and so
does the aircraft leaving the list.

The key caps carry their own state. Every softkey is drawn in the same grey
box; a toggle that is on shows a green bar along the inside of its bottom
edge, and one that is off shows none. `C`, `L`, `T` and `F` never carry a bar,
because they answer with a value rather than an on or off state, and the cap
already says which value that is: `C ALT` or `C AIRLINE`, `L NIGHT` or
`L DAY`. The scope says the same thing from its own side, writing `AUTO`
before the outer ring's range while auto range is on.

## Flags

| Flag | Default | Does |
|---|---|---|
| `--backend` | `auto` | `auto`, `fb`, `kitty`, `blocks` or `png` |
| `--scene` | `radar` | `radar` or `pattern`; `pattern` is a flags-only diagnostic with no key back to it |
| `--theme` | `night` | `night` or `day` colour theme; `l` cycles them while it runs |
| `--look` | `glass` | which palette to wear: `glass`, `phosphor` or `mono`; `k` cycles them while it runs |
| `--view` | `scope` | which view the radar starts on: `scope`, `3d`, `minimal` or `minimal3d`; `v` cycles them while it runs |
| `--exaggerate` | `8` | how far the 3D view stretches altitude into height, 1 to 20 |
| `--colour` | `altitude` | what an aircraft's colour means: `altitude` or `airline` |
| `--airports` | `on` | draw the airfield markers: `on` or `off` |
| `--shore` | `on` | draw the coastline and the water fill: `on` or `off` |
| `--range` | `auto` | scope range in nautical miles, 20 to 500, or `auto` |
| `--recenter` | `3m` | how often minimal mode recentres on the traffic, `10s` to `1h`, or `0` to stay on the receiver |
| `--battery` | | power-supply uevent file to read the battery from, Linux only |
| `--bias-t` | off | power an external LNA over the coax from the dongle's bias-tee; local SDR only |
| `--auto-sweep` | off | walk the gain grid once before the first frame and keep the best cell; local SDR only |
| `--demo` | off | fly twelve invented aircraft instead of decoding any; one of them goes quiet after 90 seconds |
| `--demo-sector` | off | put the whole invented fleet in the north-west quadrant, as a directional antenna would |
| `--beast` | | take Mode S frames from `HOST:PORT` |
| `--replay-iq` | | replay a captured IQ file through the demodulator |
| `--gpsd` | `localhost:2947` on Linux, `off` elsewhere | watch a gpsd daemon at `HOST:PORT` for the receiver's own position, or `off` |
| `--lat` | | receiver latitude in degrees, needs `--lon`; turns gpsd and self-locate off |
| `--lon` | | receiver longitude in degrees, needs `--lat`; turns gpsd and self-locate off |
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
make shore-data     # rebuild pkg/shore/shore.bin.gz and land.bin.gz from Natural Earth
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

`make shore-data` is the only target that needs the network. It downloads 25 MB
of GeoJSON to a temporary directory, packs it, and writes the two results, 4 MB
together, into `pkg/shore`. Run it when Natural Earth publishes a new release,
not as part of a build: the same three files always produce the same bytes, so
a run that changes nothing leaves `git status` clean.

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
pkg/shore             the embedded world coastlines and land, and the packed format
internal/term         raw tty mode
internal/input        bytes to key events
internal/theme        the colour palettes
internal/source       where aircraft come from: the radio, a feed, or invented
internal/radar        the radar scene
internal/pattern      the orientation scene
internal/app          the run loop
internal/tools/shoregen  packs Natural Earth into pkg/shore's two files
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

Ten packages are imported: `pkg/adsb`, `pkg/airplane`, `pkg/airplanes`,
`pkg/airports`, `pkg/battery`, `pkg/coverage`, `pkg/gps`, `pkg/location`,
`pkg/scope` and `pkg/selflocate`. All of them are data, decoding or one poll
loop.

`pkg/gps` is the gpsd client, with the reconnect and the stall watchdog already
in it, and it writes into the same `pkg/location` the self-locator does. That
shared location is the whole reason the two fit together without a coordinator:
`internal/source` decides which of them is allowed to write on the frame it is
about to draw. It brings `github.com/stratoberry/go-gpsd` along as the only
third-party package under uScope at all, which is how uAirwaves talks to gpsd
and not a choice made here.

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
reason: it is where uAirwaves keeps its notification bar and its tview widgets,
and both are answers to a problem a pixel radar does not have.

The dependency is pinned to an exact commit rather than a tag. uAirwaves is a
moving target and its own local work is ahead of what is published; a pin is
the difference between a reproducible build and one that changes under you.

## What comes next

One question [DESIGN.md](DESIGN.md) left open is still open. Trails fade by
age, which was the thing to try first and looks right, but nobody has seen it
next to a version that fades by altitude.

Two more went with it. The day theme is built (`--theme day`, `l` at run time)
and airline colouring is built (`--colour airline`, `c` at run time), and
neither has been judged against its alternative by anyone who was holding the
device at the time. Phosphor and Mono join that list: both are built and both
have only been seen on a monitor, which is not where a palette for a handheld
gets decided.

The 3D view has been looked at on a monitor and on a live feed, and not on the
panel. Its two open questions are whether an exaggeration of 8 still reads at a
third the size, and whether the theoretical bowl is worth drawing at a range
where every altitude it covers is over the horizon anyway.

The aircraft models add a third and the attitude cell a fourth. Sixteen
pixels of span was picked against eighty real aircraft on a 1280-pixel monitor,
where the shapes are distinct without crowding; at a third the size the fin and
the tailplane are a pixel each, and whether what is left still reads as an
aeroplane or as a smear is a question for the panel. The same model appears at
fourteen pixels in the 24-pixel cell at the end of the ident field, so it is
the same question asked where there is even less room to answer it.

Nobody has looked at the radar on the panel yet. That is `make radar`. The
battery indicator has been checked two ways, neither of them on the device: on
the Mac it reads the laptop through `pmset`, and the Linux reader was pointed
at a hand-written uevent file in an arm64 container with `--battery`. What has
not been tried is autodiscovery under `/sys/class/power_supply` on the
uConsole itself.

## Licence

Business Source License 1.1. See [LICENSE](LICENSE).
