# Embedded shorelines and land

The world's coastlines, lake shores and land, compiled into the uScope binary
so the scope can show where the land is on a handheld with no network.

There are two packed files, because the scope draws two different things:

| File | Size | Holds | Read with |
|---|---|---|---|
| `shore.bin.gz` | 2.1 MB | 5,453 open polylines, 564,386 points | `Set.Within` |
| `land.bin.gz` | 1.9 MB | 8,158 closed rings, 509,822 points | `Set.LandWithin` |

`shore.Load()` decodes both once and hands back one `*Set`.

The outlines are what the coastline is drawn as. The rings are what says which
side of it is sea: a coastline is a line, and nothing in a line records which
side the water is on, so the fill has to come from polygons.

## Where it came from

Natural Earth, 1:10m physical vectors, in the GeoJSON builds published at
[nvkelso/natural-earth-vector](https://github.com/nvkelso/natural-earth-vector):

| File | What is taken from it |
|---|---|
| `ne_10m_coastline.geojson` | every `LineString` and `MultiLineString`, whole |
| `ne_10m_lakes.geojson` | the outer ring of each `Polygon` and `MultiPolygon` enclosing at least 5 km² |
| `ne_10m_land.geojson` | every ring of every `Polygon` and `MultiPolygon`, outer boundaries and holes alike |

The lakes are read twice. They are outlines in the first file and rings in the
second, because Natural Earth's land layer does not cut its lakes out: without
them the IJsselmeer would be filled as land. Putting them in the land file as
further rings is what makes one even-odd pass enough. A pixel inside a lake is
enclosed by the coastline around it and by the lake shore, twice over, so it
comes out as the water it was flooded with and nothing has to record which
rings are lakes.

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

Both files use it. gzip over a body of varints. `pkg/shore/codec.go` carries
the full layout in a comment above the constants; the short version:

```
"USHR"          magic
version         uvarint
cell count      uvarint
per cell:
  row, column   uvarint each
  shape count   uvarint      polylines in shore.bin.gz, rings in land.bin.gz
  per shape:
    point count uvarint
    lat, lon    zigzag varint, 1e-5 degrees, then one pair per further point
                holding the step from the point before it
```

One format, two meanings. A shape in `shore.bin.gz` is an open line and is
drawn point to point. A shape in `land.bin.gz` is a closed ring: the edge from
its last point back to its first is walked whether or not the point is
repeated, and it is not, because repeating it would cost bytes and say nothing.
`Decode` reads either, with the same limits and the same sentinel errors, which
is why the two are separate files rather than two sections of one. A format
with an optional half in it is a format with a version problem waiting in it.

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

The world is cut into 5 degree cells, 36 rows by 72 columns, and every shape is
filed under the cell it lies in. The two files are cut into those cells in
opposite ways, because a line and an area are cut differently.

A polyline crossing a boundary is split there, and the two pieces share the
crossing segment so the line does not show a gap.

A ring crossing a boundary is clipped with Sutherland-Hodgman against the
cell's rectangle, so what each cell holds is a closed ring again: where the
original ran off the cell, the piece runs along the boundary instead. Splitting
it the way a line is split would leave two open curves, and an open curve
cannot be filled. Two neighbouring cells compute the same crossing from the
same pair of points with the same arithmetic, so their pieces meet on exactly
the same coordinates and an even-odd fill over both sees one continuous span
rather than a seam. A cell that lies wholly inside a landmass gets that
landmass as its own rectangle, four points, which is the cheapest thing in
either file.

Sutherland-Hodgman leaves zero-width bridges along the boundary where a
concave ring re-enters a cell, which is the textbook complaint about it. An
even-odd fill counts both edges of a bridge and the parity ends where it
started, so they cost nothing here.

A scope covering a few hundred nautical miles touches a handful of cells, so
`Within` walks a few thousand points rather than half a million. Five degrees
is the compromise: smaller cells mean a longer index and more lines cut in
half, larger ones mean more points visited that are nowhere near the scope.

## The size budget

The binary has room for about 2 MB of land, and the rings do not fit in it
unsimplified: at full detail they pack to 2,244,714 bytes. Douglas-Peucker at
100 metres, which `pkg/shore/build.go` applies as `defaultLandToleranceM`,
brings that to 1,988,267 bytes for 509,822 points instead of 599,604.

A hundred metres is a fifth of a pixel at the widest range uScope draws. The
outlines keep every point they had, so at the tightest ranges the fill's edge
can sit about a pixel off the coastline drawn over it; the outline is on top
and covers it.

The land file needs the simplification and the shoreline file does not, because
the land carries the same geography twice over. Every coastline appears once as
the boundary of a landmass and again wherever a ring is clipped at a cell
boundary and walks along it.

## Regenerating

```
make shore-data
```

That downloads the three GeoJSON files to a temporary directory, runs
`internal/tools/shoregen` over them, and writes `pkg/shore/shore.bin.gz` and
`pkg/shore/land.bin.gz`. The downloads are 25 MB and never land in the
repository.

Both outputs are reproducible. Cells are written in ascending key order,
nothing else is sorted, and the geometry follows the order of the input, so the
same three files always produce the same bytes and a run that changes nothing
leaves `git status` clean.

The generator is thin on purpose. Reading the GeoJSON, filtering it, cutting
the polylines, clipping the rings and encoding both all live in this package,
where they are tested; `internal/tools/shoregen` is the flags, the files and
two lines of output.

## Reading the decoder's limits

`Decode` and `DecodeWithLand` check every count in the file against the bytes
that are left before allocating for it, refuse a decompressed body over 64 MB,
and cap each file at a million shapes and twenty million points. Those are not
tuning knobs. They are what stops a corrupt or crafted file from exhausting
memory before a single byte of geometry has been read, and every failure comes
back as one of the sentinel errors at the top of `codec.go` rather than as a
panic.
