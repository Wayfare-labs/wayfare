# Build a static binary, then ship it on distroless.
#
# The image carries no shell and no package manager. This service holds no
# keys and moves no funds, but it does publish figures under Wayfare's name,
# and the smallest possible surface is the cheapest way to keep it that way.

# golang:1.27.2-alpine, pinned by digest so the same commit builds against the
# same bytes. The tag stays in the reference — Dependabot's docker ecosystem
# uses it to know which release to move to (.github/dependabot.yml) — and the
# digest is what actually fixes the image.
FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build

WORKDIR /src

# Dependencies first, so a source-only change does not refetch them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO off for a genuinely static binary; distroless/static has no libc.
# The snapshots under testdata/ are test fixtures and are not needed at
# runtime — .dockerignore keeps them out of the build context.
#
# git is needed in the build stage only to stamp the binary with the code
# commit and the data/ commit; it is not shipped in the distroless final
# image.
RUN apk add --no-cache git

# Stamp the binary with the code commit and the commit of data/ embedded at
# build time, so /healthz reports which provenance is serving (issue #576).
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w \
      -X github.com/Wayfare-labs/wayfare/server.codeCommit=$(git rev-parse HEAD) \
      -X github.com/Wayfare-labs/wayfare/server.dataCommit=$(git log -1 --format=%H -- data/)" \
    -o /out/wayfared ./cmd/wayfared

# gcr.io/distroless/static-debian12:nonroot, pinned by digest for the same
# reason: a tag is a mutable pointer, and the runtime image is the artifact
# the scans in CI describe.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

COPY --from=build /out/wayfared /wayfared

# No default data directory, deliberately.
#
# Pointing WAYFARE_DATA_DIR at /data here would make the image crash on any
# host without a disk attached: the store opens by creating the directory, and
# a read-only filesystem fails that at startup. Left empty, the binary serves
# the history embedded at build time, which is the correct behaviour for an
# ephemeral host and the common case for this image.
#
# A deployment with a persistent disk sets WAYFARE_DATA_DIR itself and gets a
# writable store, with no change here.

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/wayfared"]

# Defaults chosen so the image is correct with no arguments, because the
# platforms that run it are the ones most likely to get arguments wrong.
#
#   -addr 0.0.0.0:8080  bind all interfaces; localhost is unreachable from
#                       outside a container
#   -schedule=0         do not measure here. A host that sleeps produces a
#                       history full of holes, so the measure workflow is the
#                       clock. Drop this on an always-on instance.
#   -history-first      serve the embedded chain; ?live=1 measures on demand
#
# Overridable as usual: `docker run image -verify-store -data /data` replaces
# these entirely, which is what CI does.
CMD ["-addr", "0.0.0.0:8080", "-schedule=0", "-history-first"]
