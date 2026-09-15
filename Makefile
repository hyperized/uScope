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

.PHONY: all build build-aarch64 build-macos run run-demo run-airline run-beast \
        run-blocks run-pattern run-specimen test test-coverage lint fmt ship \
        pattern specimen radar test-device clean

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

# Draw a real feed from a remote demodulator. Nothing here needs a radio: the
# frames are already decoded on the other end.
run-beast:
	@test -n "$(BEAST)" || { echo "BEAST not set. Try: make run-beast BEAST=host:30005"; exit 1; }
	go run . --beast $(BEAST)

# Force the half-block renderer, which is what to compare against when the
# kitty output looks wrong.
run-blocks:
	go run . --backend blocks --demo

# The orientation pattern, live. v steps on to the specimen and round to the
# radar; q quits.
run-pattern:
	go run . --scene pattern

# The type specimen, live. In Ghostty this lands on the kitty backend and the
# fonts render at their real pixel sizes, which is the only way to judge them
# without a uConsole on the desk. v switches back to the pattern, q quits.
run-specimen:
	go run . --scene specimen

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

# Paint the type specimen on the panel and leave it there. Same one-frame,
# no-state-changed deal as pattern, so it is safe over ssh. This is the check
# that matters for slice 3: whether Terminus at 12, 16 and 32 pixels is
# readable at arm's length on the real screen.
specimen: ship
	@ssh $(DEVICE) './uScope --scene specimen --test-pattern'

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
