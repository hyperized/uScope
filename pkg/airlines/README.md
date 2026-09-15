# Airline colours

`airlines.csv` maps an ICAO three-letter operator designator to one colour, so
the radar can paint an aircraft in its operator's colours. A callsign heard over
the air starts with that designator: KLM123 is KLM, EZY45AB is easyJet, DLH4EA
is Lufthansa.

409 designators, collected on 2026-09-15.

## What the colours are for

One colour per airline, picked so that operators are told apart at a glance on a
5 inch panel. It is not a brand-compliance exercise. Where an airline's logo
colour and the colour on the side of the aircraft disagree, the aircraft wins:
Aurignys are yellow and Air New Zealand's are black, whatever the logo does.

Dark colours are not dropped. `Airline.OnDark` raises anything below a relative
luminance of 0.18 until it clears that floor, which is what keeps Lufthansa's
`05164D` from disappearing into the near-black field.

## Where the data came from

Designators and airline names come from the OpenFlights airline database,
<https://github.com/jpatokal/openflights>, file `data/airlines.dat`. It is
licensed under the [Open Database
License](https://opendatacommons.org/licenses/odbl/1.0/): the database is made
available by OpenFlights under ODbL and any individual contents under the
Database Contents License. OpenFlights' own `Active` flag comes from an older
snapshot, so it was treated as a hint rather than as truth. Where the two
disagreed, the designator in the airline's Wikipedia infobox won.

Colours came from four places. The first is simple-icons,
<https://github.com/simple-icons/simple-icons>, file `data/simple-icons.json`,
licensed CC0, joined to the airline list on normalised name. That join produced
61 candidates, of which 23 were software brands that happen to share a name with
an airline: Spring, Zoom, Ruby, Apache, AliExpress and the like. Those were
thrown out by hand and 38 real matches remain.

The second is the airline's own website: a `theme-color` meta tag, a `mask-icon`
colour, a web app manifest `theme_color`, or a `--brand` or `--primary` CSS
custom property. Anything white, black, or a framework default such as
Bootstrap's `#0d6efd` was discarded.

The third is the airline's logo file, the SVG named in its Wikipedia infobox,
fetched from Wikipedia or Commons, with the dominant non-grey fill taken as the
brand colour. Each one was checked against the aircraft before it was kept. Where
the logo disagreed with the paint, the row moved to the `livery` source instead.
The `note` column carries the file URL.

The fourth is the aircraft itself, for the airlines the first three missed. The
`note` then says which part of the aircraft the colour describes.

## The source column

`simple-icons` means the hex is the simple-icons entry for that brand.

`brand` means the hex came from something the airline publishes, either its
website or its own logo file. The `note` says which, with the URL.

`livery` means no published value turned up, so the hex is the dominant colour
of the aircraft. Good enough to tell operators apart, not a brand-exact value.
The `note` says what it describes, for example "green tail".

## The group column

Empty, or the designator of the parent brand. Subsidiaries that fly in the
parent's paint carry the parent's colour and say so in the note: KLM Cityhopper
under KLM, Malta Air and Buzz under Ryanair, Envoy and PSA under American.
Subsidiaries with their own paint keep their own colour and still name the
parent, which is why Air Nostrum is blue while its parent Iberia is red.

## File format

The parser is strict and a malformed file is a build-time mistake, not a runtime
condition. Comment lines are not allowed.

| Column   | Rule |
|----------|------|
| `icao`   | exactly three uppercase A-Z letters, unique in the file |
| `name`   | not empty |
| `hex`    | exactly six uppercase hex digits, no leading `#` |
| `source` | `simple-icons`, `brand` or `livery` |
| `group`  | empty, or an `icao` that also appears in this file |
| `note`   | free text, may be empty |

Rows are sorted by `icao`. The header line must match exactly.

## Adding a row

1. Confirm the designator. It is the three-letter ICAO code, not the two-letter
   IATA code: KL is IATA, KLM is what shows up in a callsign. The airline's
   Wikipedia infobox and `airlines.dat` both carry it.
2. Find a colour you can point at. A `theme-color` in the page source, a brand
   guideline, or the airline's logo file. Failing that, use the dominant livery
   colour and set the source to `livery`.
3. Insert the row in `icao` order, with the hex in uppercase and no `#`.
4. If the airline flies under a parent brand, fill in `group` with the parent's
   designator. The parent must be a row in this file.
5. Run `go test ./pkg/airlines/`. The tests parse the embedded file and will
   reject a bad code, a duplicate, a bad hex, an unknown source, or a group that
   points at nothing.

Do not guess a hex. A wrong colour is worse than a missing airline, because a
missing airline is obvious on the screen and a wrong one is not.

## Known gaps

Twelve flag carriers have no colour here, because no logo file, website value or
reliable livery description turned up for them: Cyprus Airways, TAAG Angola,
Nepal Airlines, Mauritania Airlines, Eritrean Airlines, Cubana, Air Kiribati,
Congo Airways, CEIBA Intercontinental, Air Marshall Islands, Air Koryo and
Libyan Airlines. They are left out rather than filled in with a guess.
