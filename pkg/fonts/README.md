# Embedded fonts

Four console fonts, compiled into the uScope binary so the program stays one
static file with nothing to install beside it.

| File | Size | Accessor |
|---|---|---|
| `Uni3-Terminus12x6.psf.gz` | 6x12 | `fonts.Small()` |
| `Uni3-Terminus16.psf.gz` | 8x16 | `fonts.Body()` |
| `Uni3-TerminusBold16.psf.gz` | 8x16 bold | `fonts.BodyBold()` |
| `Uni3-TerminusBold32x16.psf.gz` | 16x32 bold | `fonts.Large()` |

## Where they came from

These are Debian's `console-setup` package, byte for byte: the Uni3 builds of
Terminus Font that ship in `/usr/share/consolefonts`. Uni3 is the widest of
the character-set variants console-setup builds, which is why it is the one
here. The radar will want box drawing and a degree sign, and going back for a
bigger build later is more work than carrying a few extra kilobytes now.

The two 16-pixel faces are PSF1 and the other two are PSF2. That is not a
choice, it is what console-setup ships, and it is the reason `pkg/psf` reads
both formats.

Terminus is a bitmap font designed for exactly this: fixed pitch, one pixel
grid, no hinting, no anti-aliasing, meant to be read for hours on a screen.
On a 5 inch panel at 1280x720 that matters more than it would on a laptop.

## Licence

Terminus Font is licensed under the SIL Open Font License, Version 1.1.

    Copyright (c) 2010 Dimitar Toshkov Zhekov,
    with Reserved Font Name "Terminus Font".

The full text is in `OFL-Terminus.txt` in this directory, and `fonts.Licence()`
returns it at run time so a program can show it.

## Why the file names stay as they are

The licence reserves the name "Terminus Font". Renaming these files, or
shipping a modified version of them under a name that still says Terminus,
is the thing the Reserved Font Name clause exists to stop. So the files keep
console-setup's names, unmodified, and the Go accessors carry uScope's own
names instead.

If a face ever needs changing, the modified version goes in under a different
font name, not under this one.
