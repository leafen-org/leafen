# syntax=docker/dockerfile:1.7

# Leafen in Docker: the server alone (cmd/leafen), without the desktop app's
# window — Leafen runs no agents of its own, so nothing here needs a window's
# libraries. One static executable on a distroless base: no shell, CA
# certificates and time zones, one user without privileges (uid 65532, the
# data's owner as before) and everything in /data. Agents come in from
# outside, by MCP, with their person's rights.

FROM --platform=$BUILDPLATFORM golang:1.27.4-bookworm AS go-toolchain

FROM --platform=$BUILDPLATFORM node:22.22.0-bookworm-slim AS web-builder
COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"
# One build check is a Go program run from the module root
# (scripts/native-operation-inputs, the standard library only), from the root
# module alone as the server is.
ENV GOWORK=off
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --ignore-scripts
# The whole source tree, as a checkout has it: the web build's checks read
# across it (Go sources, go.mod, contracts, scripts, the screen words, the
# app SDK, the Office app's viewer), and a list of folders here falls behind
# the next check that reads another file. .dockerignore leaves out only what
# no build reads.
COPY . /src
RUN npm run build

# The server is pure Go (SQLite is modernc's), so it is built where the build
# runs, for the image's platform, with no C toolchain and no target libraries.
FROM --platform=$BUILDPLATFORM golang:1.27.4-bookworm AS go-builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
# The server's main package is one large compile: collecting its garbage more
# often keeps it near 3 GB rather than 4.5 GB, so a Docker with 4 GiB builds it.
ENV GOWORK=off GOGC=25
COPY go.mod go.sum ./
COPY third_party ./third_party
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY LICENSE ./LICENSE
COPY NOTICE ./NOTICE
COPY internal ./internal
# The organization features, kept apart from the core.
COPY organization ./organization
COPY sdk/app ./sdk/app
COPY web/embed.go web/notices.go web/notices_noapps.go ./web/
COPY web/notices ./web/notices
COPY --from=web-builder /src/web/dist ./web/dist
COPY cmd/leafen ./cmd/leafen
# A release image is built with LEAFEN_BUILD_TAGS=leafen_release and the
# release's version (as web/package.json's), which trusts only the production
# signing keys (internal/app/release_keys.go). Built from source without them
# it is a development build, its version marked "-dev": a release version
# without the tag would trust the development keys, and refuses to start.
ARG LEAFEN_BUILD_TAGS=""
ARG LEAFEN_VERSION="0.2.0-dev"
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -tags "$LEAFEN_BUILD_TAGS" \
    -ldflags="-buildid= -X leafen.org/leafen/internal/app.leafenReleaseVersion=$LEAFEN_VERSION" -o /out/leafen ./cmd/leafen
RUN cp LICENSE /out/LICENSE && cp NOTICE /out/NOTICE && mkdir /out/data /out/backups
# The third-party notices of the edition built, as the server serves them:
# a build tagged leafen_noapps has its own.
RUN edition=""; for tag in $(echo "$LEAFEN_BUILD_TAGS" | tr ',' ' '); do \
      if [ "$tag" = leafen_noapps ]; then edition="-noapps"; fi; done; \
    cp "web/notices/THIRD-PARTY-NOTICES${edition}.txt" /out/THIRD-PARTY-NOTICES.txt

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"
COPY --from=go-builder /out/leafen /usr/local/bin/leafen
COPY --from=go-builder /out/LICENSE /usr/share/licenses/leafen/LICENSE
COPY --from=go-builder /out/NOTICE /usr/share/licenses/leafen/NOTICE
COPY --from=go-builder /out/THIRD-PARTY-NOTICES.txt /usr/share/licenses/leafen/THIRD-PARTY-NOTICES.txt
# The data folder, the user's to write, and the backups' folder beside it
# (compose.yaml's "leafen-backups" volume): a new volume mounted there takes
# its owner, so backups are written there with nothing to set up.
COPY --from=go-builder --chown=65532:65532 /out/data /data
COPY --from=go-builder --chown=65532:65532 /out/backups /backups
ENV LEAFEN_CONTAINER_RUNTIME=1 \
    LEAFEN_SETUP_OPEN_MODE=print
# Every command run in the container works on that data without --dir
# (`docker compose exec leafen leafen setup status`, backup, restore,
# account, connect): their data folder, "data" in the working directory
# unless --dir names another, is then /data. The base's own,
# /home/nonroot, had them leave a request in an empty folder there and wait
# for a server that never answered.
WORKDIR /
USER 65532:65532
VOLUME ["/data"]
EXPOSE 8090
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/leafen"]
CMD ["serve", "--http=0.0.0.0:8090", "--dir=/data"]
