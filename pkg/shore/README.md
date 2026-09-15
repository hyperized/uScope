# Embedded shorelines

The world's coastlines and lake shores, compiled into the uScope binary so the
scope can show where the land is on a handheld with no network.

`shore.bin.gz` is about 2.1 MB and holds 5,453 polylines made of 564,386
points. `shore.Load()` decodes it once and hands back a `*Set`; `Set.Within`
gives the drawing code the polylines near the scope without walking the rest.

## Where it came from

Natural Earth, 1:10m physical vectors, in the GeoJSON builds published at
[nvkelso/natural-earth-vector](https://github.com/nvkelso/natural-earth-vector):

| File | What is taken from it |
|---|---|
| `ne_10m_coastline.geojson` | every `LineString` and `MultiLineString`, whole |
| `ne_10m_lakes.geojson` | the outer ring of each `Polygon` and `MultiPolygon` enclosing at least 5 km² |

Natural Earth is public domain. Their request, which this file honours, is:

> Made with Natural Earth. Free vector and raster map data at
> [naturalearthdata.com](https://www.naturalearthdata.com).

Islands inside a lake are dropped. At the ranges uScope draws they are a few
pixels inside an outline that already reads as water.

The 5 km² threshold is measured with a planar shoelace area, with longitude
squeezed by the cosine of the ring's mean latitude. That is the same cheap
projection the scope itself uses and it is accurate enough to sort lakes into
kept and dropped. It would be wrong for a ring straddling a pole or the
dateline, and at this scale Natural Earth has neither.

## The packed format

gzip over a body of varints. `pkg/shore/codec.go` carries the full layout in a
comment above the constants; the short version:

```
"USHR"          magic
version         uvarint
cell count      uvarint
per cell:
  row, column   uvarint each
  line count    uvarint
  per line:
    point count uvarint
    lat, lon    zigzag varint, 1e-5 degrees, then one pair per further point
                holding the step from the point before it
```

Three things make it small. Coordinates are fixed point at 1e-5 degrees, about
a metre, which is finer than a pixel at any range uScope shows and keeps
floating point out of the file. Points are stored as steps rather than
positions, and two neighbouring points on a Natural Earth coastline are
usually a few hundred steps apart, so one fits in two bytes where a position
needs five. And gzip at its best level takes another 140 KB off the result,
which costs the generator a second and the reader nothing.

Cells are written in ascending order and nothing else is sorted, so the same
two input files always produce the same bytes.

## The cell scheme

The world is cut into 5 degree cells, 36 rows by 72 columns, and every
polyline is filed under the cell it lies in. Any polyline crossing a boundary
is cut there, and the two pieces share the crossing segment so the line does
not show a gap.

A scope covering a few hundred nautical miles touches a handful of cells, so
`Within` walks a few thousand points rather than half a million. Five degrees
is the compromise: smaller cells mean a longer index and more lines cut in
half, larger ones mean more points visited that are nowhere near the scope.

## Regenerating

```
make shore-data
```

That downloads both files to a temporary directory, runs
`internal/tools/shoregen` over them, and writes `pkg/shore/shore.bin.gz`. The
downloads are 15 MB and never land in the repository.

The generator is thin on purpose. Reading the GeoJSON, filtering it, cutting
the polylines and encoding them all live in this package, where they are
tested; `internal/tools/shoregen` is the flags, the files and one line of
output.

## Reading the decoder's limits

`Decode` checks every count in the file against the bytes that are left before
allocating for it, refuses a decompressed body over 64 MB, and caps the set at
a million polylines and twenty million points. Those are not tuning knobs.
They are what stops a corrupt or crafted file from exhausting memory before a
single byte of geometry has been read, and every failure comes back as one of
the sentinel errors at the top of `codec.go` rather than as a panic.
