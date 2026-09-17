# Build, test, and deploy targets for uScope.
#
# The deployment host is personal, so it lives in a gitignored .env file
# next to this Makefile:
#
#   DEVICE = user@uconsole-host     # the uConsole with the framebuffer
#
# Targets that scp check for it and explain what to set when it is missing.
# Plain builds and tests need no .env at all.

-include .env

GOARCH_DEV ?= arm64

# The BEAST feed run-beast talks to. Override it on the command line or put it
# in .env next to DEVICE:
#
#   BEAST = 192.168.1.10:30005
BEAST ?=

.PHONY: all build build-aarch64 build-macos run run-demo run-airline run-3d run-beast \
        run-blocks run-pattern shore-data test \
        test-coverage lint fmt ship pattern radar test-device clean

all: build

# Build for the machine you're sitting at.
build:
	go build -o uScope .

build-aarch64:
	env GOOS=linux GOARCH=$(GOARCH_DEV) go build -o uScope-aarch64 .

build-macos:
	env GOOS=darwin GOARCH=arm64 go build -o uScope .

# Run it here, on the machine you are sitting at. With no --backend it works
# out where to draw: Kitty graphics in a terminal that has them, half-blocks
# in one that does not. q quits.
#
# On a machine with no receiver this falls back to the demo fleet and says so
# on stderr. run-demo asks for it outright, which is what to use when you want
# the same twelve aircraft every time and no warning.
run:
	go run .

run-demo:
	go run . --demo

# The demo fleet painted by operator rather than by altitude. The invented
# fleet carries ten real callsign prefixes, one aircraft with no callsign and
# one whose prefix is not in the database, so the legend and the muted
# fallback both have something to show. c switches back while it runs.
run-airline:
	go run . --demo --colour airline

# The demo fleet in the perspective view, with the receiving envelope on. The
# camera orbits on its own; v cycles back to the flat scope, e drops the
# envelope, and --exaggerate changes how far altitude is stretched.
run-3d:
	go run . --demo --view 3d

# Draw a real feed from a remote demodulator. Nothing here needs a radio: the
# frames are already decoded on the other end.
run-beast:
	@test -n "$(BEAST)" || { echo "BEAST not set. Try: make run-beast BEAST=host:30005"; exit 1; }
	go run . --beast $(BEAST)

# Force the half-block renderer, which is what to compare against when the
# kitty output looks wrong.
run-blocks:
	go run . --backend blocks --demo

# The orientation pattern, a still frame's worth of live loop. It is a
# flags-only diagnostic: there is no key back to the radar once it is
# running, so q is the only way out.
run-pattern:
	go run . --scene pattern

# Rebuild the embedded shorelines and land from Natural Earth.
#
# The three GeoJSON files come to 25 MB and go to a temporary directory, never
# into the repository; only the two packed files, 4 MB together, are committed.
# Run this when Natural Earth publishes a new release, not as part of a build:
# the data does not change between one and the next.
#
# Both outputs are reproducible. The same three inputs produce the same bytes,
# so a run that changes nothing leaves `git status` clean.
NE_GEOJSON ?= https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson

shore-data:
	@set -e; \
	tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' EXIT; \
	echo "downloading Natural Earth into $$tmp"; \
	curl -sSfL -o "$$tmp/coastline.geojson" $(NE_GEOJSON)/ne_10m_coastline.geojson; \
	curl -sSfL -o "$$tmp/lakes.geojson"     $(NE_GEOJSON)/ne_10m_lakes.geojson; \
	curl -sSfL -o "$$tmp/land.geojson"      $(NE_GEOJSON)/ne_10m_land.geojson; \
	go run ./internal/tools/shoregen \
	  -coastline "$$tmp/coastline.geojson" \
	  -lakes "$$tmp/lakes.geojson" \
	  -land "$$tmp/land.geojson" \
	  -out pkg/shore/shore.bin.gz \
	  -land-out pkg/shore/land.bin.gz; \
	ls -l pkg/shore/shore.bin.gz pkg/shore/land.bin.gz

test:
	go test -race -cover ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

# Upload beside the target and rename, rather than straight over it. scp onto
# a running binary fails with ETXTBSY; rename(2) only swaps the directory
# entry, so a session that is already open keeps running on the old inode and
# picks the new build up next time it starts.
ship: build-aarch64
	@test -n "$(DEVICE)" || { echo "DEVICE not set. Create .env with: DEVICE = user@host"; exit 1; }
	scp uScope-aarch64 $(DEVICE):~/uScope.new
	ssh $(DEVICE) 'mv -f ~/uScope.new ~/uScope'

# Paint the test pattern and leave it on screen.
#
# Read the result like this: red square top-left and the cyan triangle at the
# top means the rotation is right. If the pattern lands on the wrong edge,
# run it again with --rotate 3. This works over ssh because --test-pattern
# changes no console or terminal state.
pattern: ship
	@ssh $(DEVICE) './uScope --scene pattern --test-pattern'

# Paint one frame of the radar on the panel. With no source flag this opens the
# uConsole's own SDR, which is the whole point of the machine. To watch it live
# rather than as a still frame, ssh in and run ./uScope yourself: make ship puts
# the binary there and every flag works the same on the device as it does here.
radar: ship
	@ssh $(DEVICE) './uScope --test-pattern'

# The framebuffer and console tests need real hardware, so they are built
# here and run there. They are behind the integration tag, so a plain
# `make test` never touches a device.
test-device:
	@test -n "$(DEVICE)" || { echo "DEVICE not set. Create .env with: DEVICE = user@host"; exit 1; }
	mkdir -p dist
	env GOOS=linux GOARCH=$(GOARCH_DEV) go test -c -tags integration -o dist/fbdev.test ./pkg/fbdev
	env GOOS=linux GOARCH=$(GOARCH_DEV) go test -c -tags integration -o dist/vt.test    ./pkg/vt
	env GOOS=linux GOARCH=$(GOARCH_DEV) go test -c -tags integration -o dist/term.test  ./internal/term
	env GOOS=linux GOARCH=$(GOARCH_DEV) go test -c -tags integration -o dist/winsize.test ./pkg/winsize
	scp dist/fbdev.test dist/vt.test dist/term.test dist/winsize.test $(DEVICE):~/
	ssh $(DEVICE) './fbdev.test -test.v && ./vt.test -test.v && ./term.test -test.v && ./winsize.test -test.v'

clean:
	rm -rf dist uScope uScope-aarch64 coverage.out coverage.html
