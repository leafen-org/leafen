# Development

Leafen's production artifact is one Go program with an embedded React bundle:
the desktop app on macOS and Windows, and its server alone (`cmd/leafen`) in
Docker and on Linux, which has no desktop app.

## Prerequisites

- Go 1.27
- Node.js 22.12 or later
- npm
- the window's C toolchain: Xcode command line tools on macOS; none on
  Windows, nor for the server alone
- Docker with BuildKit when testing the container

## Build

Run from the repository root:

```sh
npm --prefix web ci --ignore-scripts
npm --prefix web run build
go build -trimpath -buildvcs=false -o dist/leafen ./desktop
```

On Windows, in Command Prompt or PowerShell:

```bat
npm.cmd --prefix web ci --ignore-scripts
npm.cmd --prefix web run build
go build -trimpath -buildvcs=false -o dist\leafen.exe .\desktop
```

`npm.cmd`, because PowerShell's default execution policy refuses npm's
`npm.ps1`; `leafen.exe`, because Go adds `.exe` only to a name it picks itself.

The Web build checks the generated Go/TypeScript contracts and emits
`web/dist`. Go embeds that directory through `web/embed.go`. Leafen is one
program whose entry is `desktop/`, which the Go workspace (`go.work`) ties to
the rest of the source: `dist/leafen` (`dist\leafen.exe` on Windows) opens
the desktop app, and its server's commands (`serve` and the rest) run without
a window. The desktop app is macOS's and Windows'; on Linux, build the server
alone, as the Docker image does:
`go build -trimpath -buildvcs=false -o dist/leafen ./cmd/leafen`, with no C
toolchain. From another system, check the desktop app as Windows
builds it: `GOOS=windows CGO_ENABLED=0 go vet ./desktop`.

These commands build the release, which includes the organization module in
`organization/` and `web/src/organization/` (kept apart from the core, under
the same AGPL-3.0-only license). To build the core alone, build the Web with
`LEAFEN_CORE=1` and Go with `-tags leafen_core`:

```sh
LEAFEN_CORE=1 npm --prefix web run build
go build -trimpath -buildvcs=false -tags leafen_core -o dist/leafen-core ./desktop
```

The core never imports the module; `node scripts/check-organization-boundary.mjs`
checks that.

## Run the frontend development loop

Start the native API with a disposable data directory:

```sh
go run ./desktop --setup-open=print --dev \
  --dir=./data-dev serve --http=127.0.0.1:8787
```

On Linux, run `./cmd/leafen` in place of `./desktop`. On Windows, write it on
one line, without the `\`.

In a second terminal (`npm.cmd` in PowerShell on Windows):

```sh
npm --prefix web run dev
```

Open `http://127.0.0.1:3000/`. Vite proxies product requests to the native
server. Run `npm --prefix web run build` to refresh the embedded production-style
bundle.

## Static checks

```sh
npm --prefix web run typecheck
npm --prefix web run build
go vet ./...
go vet -tags leafen_noapps ./...
go test ./...
```

`go test ./...` runs the smoke tests (`internal/smoke`): one walk along the
core path with the server program built from the checkout, which needs the
web build first — a server on an empty folder, its first run by commands, a
page and a database through the API, an agent's MCP connection, a backup
restored into a new folder. `LEAFEN_SMOKE_BINARY` names a program built
already; `go test -short` skips them. The public CI
(`.github/workflows/ci.yml`) runs these checks, the third-party notices'
check and the tests of `third_party/ygo`.

The repository contains product source and build-time contract generators.
Only files inside this repository participate in the product build.

## Wire contracts

Files under `contracts/` are the shared Go/TypeScript wire authority. Scripts
under `scripts/generate-*-wire.mjs` generate both declarations. Do not hand-edit
a generated file or change only one language's view of a payload.

The Web build runs each generator with `--check`. To change a contract, update
the neutral contract, run its generator without `--check`, and then repeat the
complete build.

## Schema migrations

Schema changes are ordered migrations. The first is the schema baseline
(`internal/app/schema_baseline.go`): the schema the migrations up to version
831 left, kept in `internal/app/schema_baseline.json` and applied by a new data
root in one step, which records it as version 831. The migrations after it, from
832 on, are in `internal/app/schema_migration_definitions_0832.go`; a new one goes
at the end with the next version, and its checksum is a constant beside it. An
existing data root applies the ones it lacks; a new one applies the baseline and
then each later migration.

The baseline stays as it is: adding a migration changes nothing in it. Only the
instance export policy's schema version follows the last migration: run
`node scripts/generate-schema-migration-impact.mjs --write`, which the Web build
checks. A migration that adds a table the instance export carries
(`internal/app/instance_product_export_policy.go`) puts the export's change
tracking triggers on it with `trackInstanceProductExportSourceRevisions` once it
has created the table. The Web build fails when the baseline file isn't the one
the Go code names (`scripts/check-schema-baseline.mjs`). To check that it
still fits this build's PocketBase and that a fresh database takes it and the
later migrations and passes their checks:

```sh
go run ./cmd/leafen-schema-baseline
```

The baseline replaces PocketBase's own `users` table and collection, and holds
them as PocketBase made them when it was recorded. A PocketBase update that
changes them stops a new data root's start instead of losing the change: update
the baseline's statements for it, then record what they replace again with
`go run ./cmd/leafen-schema-baseline -write`. A migration that takes out or
reshapes something the baseline made names it in `schemaBaselineRetired`
(`internal/app/schema_baseline_standing.go`), whose check otherwise holds every
later migration to the baseline's tables, columns, indexes and triggers.

Data whose schema history runs migration by migration from version 1, made by a
Leafen from before the baseline, is not opened: the start, `leafen doctor` and
every restore refuse it with that reason and leave it as it is
(`internal/app/schema_history_refusal.go`).

## App SDK

`sdk/app/` is the app SDK, licensed under Apache-2.0 (its own `LICENSE`), as
are `contracts/`, `examples/` and `bench/` (each with its `LICENSE`); the rest
of Leafen is AGPL-3.0-only (`LICENSE`, `NOTICE`). The SDK's
built files are generated and committed: after changing `sdk/app/src/index.ts`
run `node scripts/generate-app-sdk.mjs` (the Web build checks it), and after
changing the app manifest's Go structure run `go run ./cmd/leafen-app-schema`
for the manifest's JSON Schema and TypeScript types (`--check` fails when they
are stale). The Go worker helper `sdk/app/go` and the example's worker are
separate Go modules; check them with `go vet ./...` there, also with
`GOOS=wasip1 GOARCH=wasm`.

The built-in apps' package files are in `internal/app/builtinapps/` and their
workers' sources in `builtin-apps/<app>/worker/` (each its own Go module).
After changing a worker or the SDK run `node scripts/build-builtin-apps.mjs`
(`--check` fails when the committed `workers/main.wasm` or `ui/leafen-app-sdk.js`
differs; the wasm matches only from the same Go release). An installed version
is fixed by its fingerprint, so a changed app needs a new `version`.

## UI source ownership

`web/src/` is the only executable React product source. Product surfaces retain
Leafen's established behavior, structure, styles, accessibility, keyboard
handling, and responsive states and reach the server only through typed native
adapters.

A change to an established surface belongs in `web/src/` and must keep its
direct style, asset, locale, focus, and error-state dependencies coherent.

## Third-party notices

`web/notices/` holds the license notices of everything Leafen is distributed
with: the packages bundled into the web client, the Go modules linked into the
executable and into the desktop and phone apps, the libraries in the built-in
apps' screens the executable carries, and the Android app's libraries — one
file per edition, each naming only what that edition holds.
`THIRD-PARTY-NOTICES.txt` is Leafen's with the app platform;
`THIRD-PARTY-NOTICES-noapps.txt` is a build tagged `leafen_noapps` (the phone
apps, the Mac App Store's), which carries no app runtime and, of the built-in
apps, only the PDF reader its file preview uses. The executable embeds its
edition's file and serves it at `/THIRD-PARTY-NOTICES.txt` (the login
screen's foot and settings' server accounts link it beside the source and the
license for everyone — AGPL-3.0's section 13 — and the server console's version
line beside the source; `web/embed.go`, `web/src/components/LegalNotice.tsx`), the
container image copies it to `/usr/share/licenses/leafen/` and the packaged
executables carry it beside `LICENSE` and `NOTICE`. After changing a
dependency, run `node scripts/generate-third-party-notices.mjs` (it needs the
web build, `web/node_modules`, the Go module cache and a JDK for Gradle —
`JAVA_HOME`, else Homebrew's `openjdk@21`); `--check` fails when a committed
file is stale, and the script stops on a license it can't name, for review.

## Documentation site

`docs/` and `MCP.md` are published at https://leafen.org/docs, one set of
pages per minor version, each read from that version's newest release tag
(`vX.Y.Z`) when the site is built; a release brings its documents along, and
the files here are the only copy. Write them for GitHub: relative links
between documents and pictures under `docs/` work the same on both, and a
link to any other file of this repository opens it on GitHub at that
version's tag. The site's frame (its Docusaurus setup, sidebar and styles)
is kept with the leafen.org pages, outside this repository.

## Release builds

A release is built with the production signing keys in
`internal/app/release_keys_production.go` (`leafen_release`), which also names
the address the [release lists](release-lists.md) are published under and the
[official app catalog](apps.md)'s address and list key; a release build
without the keys refuses to start, and a development build names none of
them. Publishing, pushing and uploading happen only when the maintainers
decide to release. The app store editions (the Microsoft Store's, the Mac App
Store's and the phone apps') are Leafen's own, released by Leafen as the
copyright holder under each store's terms; contributions come under the
[contributor license agreement](../CLA.md) for this.

### Desktop packages

After the web build, `node scripts/package-desktop.mjs --target <os>-<arch>`
writes the system's files to `dist/desktop` and prints each one's kind,
SHA-256 and size for the [desktop update list](release-lists.md#the-desktop-update-list).
macOS gets two: the zipped app (`app-zip`), which the app updates itself from,
and the disk image people download (`dmg`). Windows gets the program folder
zipped (`zip`), which people download; it is not in the update list, as
Windows updates only through the Microsoft Store. The package is a release build
(`leafen_release`) of web/package.json's version; `--development` packages a
development build (version `0.2.0-dev`) instead, which starts without the
production keys, to try the steps.

On macOS the app is signed with the hardened runtime and
`desktop/build/darwin/entitlements.plist`, whose one entitlement lets the app
platform's WebAssembly runtime run the code it compiles (without it the app
worker is killed when an app runs). A release is signed with a Developer ID
and notarized by Apple. Once, on the Mac that builds, with the Apple Developer
Program account:

1. In Xcode › Settings › Accounts, add the account, then under **Manage
   Certificates** add a **Developer ID Application** certificate. It goes into
   the login keychain, and `security find-identity -v -p codesigning` lists it
   as `Developer ID Application: <name> (<team ID>)`.
2. Make an app-specific password for the Apple Account (account.apple.com ›
   Sign-In and Security), and keep the notarization login in the keychain
   under a profile name; the command asks for the password:

   ```sh
   xcrun notarytool store-credentials leafen-notary --apple-id <Apple Account email> --team-id <team ID>
   ```

Then for each release, for `darwin-arm64` and `darwin-amd64`:

```sh
LEAFEN_MAC_SIGN_IDENTITY="Developer ID Application: <name> (<team ID>)" \
LEAFEN_MAC_NOTARY_PROFILE=leafen-notary \
  node scripts/package-desktop.mjs --target darwin-arm64
```

Apple notarizes the app, whose ticket is stapled before it is zipped, and
then the signed disk image, stapled too; each takes a few minutes. The script
prints each verdict with its submission id (`xcrun notarytool log <id>
--keychain-profile leafen-notary` shows Apple's log of it), and a refusal
prints Apple's log at once. To check a disk image as a downloaded one is
checked:

```sh
xcrun stapler validate dist/desktop/leafen-desktop-<version>-darwin-arm64.dmg
spctl --assess --type open --context context:primary-signature -v dist/desktop/leafen-desktop-<version>-darwin-arm64.dmg
```

`spctl` says `accepted` and `source=Notarized Developer ID`. Without
`LEAFEN_MAC_SIGN_IDENTITY` the app is signed ad hoc, and without
`LEAFEN_MAC_NOTARY_PROFILE` it isn't notarized: the script says so, and macOS
warns when a downloaded copy opens.

### Microsoft Store package

On Windows targets, `--msix` also writes the Microsoft Store's package
(`msix`): the same program with full trust, with
`desktop/build/windows/AppxManifest.xml` and the icons beside it. The store
signs it, so it carries no signature of ours. The packer is `makemsix` from
Microsoft's open-source [msix-packaging](https://github.com/microsoft/msix-packaging),
built once with pack support (CMake and Ninja; a few minutes):

```sh
git clone --depth 1 https://github.com/microsoft/msix-packaging.git
cd msix-packaging
cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=MinSizeRel -DMSIX_PACK=on \
  -DUSE_VALIDATION_PARSER=on -DXML_PARSER=xerces -DUSE_MSIX_SDK_ZLIB=on \
  -DMSIX_SAMPLES=off -DMSIX_TESTS=off -DMACOS=on -DCMAKE_OSX_ARCHITECTURES=arm64 \
  -DCMAKE_TOOLCHAIN_FILE="$PWD/cmake/macos.cmake"
cmake --build build
```

`build/bin/makemsix` stays where it was built (it finds its library there);
put it on `PATH` or name it in `LEAFEN_MAKEMSIX`. Then, for `windows-amd64`
and `windows-arm64`, with the identity from Partner Center (the app's
Product management › Product identity):

```sh
LEAFEN_MAKEMSIX=<msix-packaging>/build/bin/makemsix \
LEAFEN_MSIX_IDENTITY_NAME=<Package/Identity/Name> \
LEAFEN_MSIX_PUBLISHER="<Package/Identity/Publisher>" \
LEAFEN_MSIX_PUBLISHER_DISPLAY_NAME="<Package/Properties/PublisherDisplayName>" \
  node scripts/package-desktop.mjs --target windows-amd64 --msix
```

Without them the package has a development identity (`CN=Leafen
Development`), which installs only on a test machine that trusts a test
certificate of that name — signed there with the Windows SDK's `signtool`.
The package's version is the release's with a fourth number, 0, as the store
asks; `LEAFEN_MSIX_VERSION` (four numbers, the last 0) sets it on its own when
the store needs a package above one it already has, say `0.1.1.0` after
`0.1.0.0`, while the program keeps the release's version. Inside the package
the app:

- keeps its data where the program does unpackaged, `%APPDATA%\Leafen`
  (the server's data in `local`). Windows would otherwise put new files there
  in the package's own storage and remove them with the app; the manifest
  leaves that folder outside it (`unvirtualizedResources`, a restricted
  capability Partner Center asks the reason for: the person's workspace must
  stay when the app is removed, where the same program keeps it). Removing the
  app leaves the data.
- opens at login by the manifest's startup task, behind its window, in place
  of the Run key; the "Open at login" switch turns it on and off, and
  Windows' Settings › Apps › Startup lists it.
- doesn't update itself: the store does, and the app's update switch isn't
  shown.
- opens `leafen:` links.

### Docker image

`scripts/publish-image.sh` builds the image for `linux/amd64` and
`linux/arm64` with `leafen_release` and the release's version (web/package.json's),
and pushes it as `ghcr.io/leafen-org/leafen:<version>` and `:latest`. Docker's
builder must build for several platforms at once: Docker Desktop's does, and
Docker Engine's with the containerd image store (the default since Engine 29);
otherwise make one with `docker buildx create --use --driver docker-container
--driver-opt network=host` (the host's network, to reach a registry on the
same machine). The two platforms' Go builds run at once and need more than
8 GiB of the builder's memory together.

Once, sign Docker in to the GitHub Container Registry with a token that may
write packages, for example the GitHub CLI's:

```sh
gh auth refresh --scopes write:packages
gh auth token | docker login ghcr.io --username <GitHub user> --password-stdin
```

Then for each release, from the release's commit:

```sh
scripts/publish-image.sh
```

The image's source label links the package to the repository. After the
first push, make the package public in its settings on GitHub (the
organization's **Packages**), so `docker compose up -d` pulls it without
logging in.

To try the procedure without publishing, push a development build to a
registry of your own and run it from there:

```sh
docker run -d --name registry -p 127.0.0.1:5000:5000 registry:2
scripts/publish-image.sh --development --image 127.0.0.1:5000/leafen
LEAFEN_IMAGE=127.0.0.1:5000/leafen:0.2.0-dev docker compose up -d
```
