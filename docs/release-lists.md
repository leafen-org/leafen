# Release lists

Once a day a Leafen server asks whether two small signed lists at the list
address changed, and fetches them when they did (`LEAFEN_RELEASE_LIST_URL` for
test lists; the official address comes with the official keys):

| File | What it holds | Signed by |
| --- | --- | --- |
| `release-v1.json` | the latest version, whether it is a security release, its notes and upgrade links | the release key |
| `sponsors-v1.json` | Leafen's own line and the sponsors, each with its share (name, logo, slogan, share) | the sponsor list key |
| `desktop-v1.json` | the latest desktop app and the macOS files (address, SHA-256, size) | the release key |

The desktop app also reads `desktop-v1.json`: on macOS to update itself, and
the Windows zip to say a newer version is out
([self-hosting](self-hosting.md#the-desktop-app-updates-itself)).

The server sends nothing but a plain GET for each file, asking for it only if
it changed since the list it holds was issued (`If-Modified-Since`). Without
the internet, or when a file doesn't pass its checks, it keeps the lists
bundled with the release.

## Each file

A file is an envelope around the signed list:

```json
{ "format": "leafen-release-notice/1", "keyId": "…", "payload": "<base64 of the list>", "signature": "<base64 Ed25519 signature of the payload>" }
```

The sponsor list's envelope says `leafen-sponsor-list/1`. The list inside has
`"format": 1` and an `issuedAt` time; a list older than the one the server
already has, or than the one bundled with it, is not taken.

- A server trusts each key for its own file only: the sponsor list key can't
  make an update notice.
- A list with any field the server doesn't know is refused whole, so nothing
  unreviewed reaches the screen.
- An update notice may link only to Leafen's own release pages
  (`https://github.com/leafen-org/leafen/releases…`), so even a stolen key
  can't send administrators to another download.

## The sponsor list

No screen shows the list yet: the sponsor list and the row at the bottom of
Home that will show it come in an early update after the first release
([People backing Leafen](sponsors.md)).
The list's format stays as it is meanwhile.

```json
{ "format": 1, "issuedAt": "…",
  "house": { "url": "https://…", "share": 9000 },
  "sponsors": [{ "id": "example", "name": "Example", "logo": "data:image/png;base64,…", "share": 1000,
    "slogan": { "language": "en", "text": { "en": "…", "ko": "…" } } }] }
```

- Shares are whole numbers out of 10,000 and add up to 10,000. Leafen's own
  line has at least 500 (5%) and no sponsor more than 3,333 (a third). A
  hundredth of a percent is fine enough that a sponsor's share moves by less
  than 0.01% when rounded, and whole numbers add up exactly. Whoever makes the
  list rounds each sponsor's share down and gives the remainder to Leafen's
  line, so neither limit is crossed.
- The list carries no amounts ([People backing Leafen](sponsors.md)) and no
  sponsor address: a credit links to `https://leafen.org/go/<id>`.
- A slogan names the language it was written in and its text in each language
  it has, that one among them. Each text is one line no wider than 60 narrow
  characters, where a wide one (Korean, Chinese, Japanese) counts as two, with
  no control characters, link or address (a scheme, `www.`, a dot before a
  domain ending, an `@`).
- A list's sponsors show for 45 days from its `issuedAt`, the bundled list's
  too; after that only Leafen's own line shows. The list is published again
  at least once a month, unchanged or not.

## The desktop update list

The desktop list's envelope says `leafen-desktop-release/1`. Its list has
`format`, `issuedAt`, `version`, `security`, `notesUrl` (a Leafen release
page) and `assets`. Each asset names:

- its system: `os` (`darwin`) and `arch` (`arm64`, `amd64`);
- its `kind`: `app-zip` (the macOS app bundle, zipped);
- where it comes from, and its `sha256` and `size`.

The list names no Windows file: Windows updates only through the Microsoft
Store, and the Windows zip only tells the person a newer version is out and
opens its `notesUrl`. A list naming another kind of file is refused whole.

A file may come only from Leafen's GitHub releases
(`https://github.com/leafen-org/leafen/releases/download/…`). With
`LEAFEN_RELEASE_LIST_URL` set to a test address, files may also come from
under that address, and macOS's team and notarization checks are left to the
developer.

The release step uploads each system's file to the release, puts each macOS
`app-zip`'s SHA-256 and size into the list, and signs it with the release key.

## Publishing a new format

A server reads only the files of the format it knows. To add a field, publish
the new format under its own name (`release-v2.json`, `sponsors-v2.json`)
**and keep publishing the earlier ones**, filled with what earlier servers
understand. Installed servers stay for years; they keep getting update and
security notices, and the sponsors they can show, from the files of their own
format.

The sponsor list and each release's notice are signed when they're published,
with keys kept encrypted off the repository, and every published list stays
public. A release build trusts only the production keys and reads the
official address, both in `internal/app/release_keys_production.go`. A
development build trusts the development keys in the source, which sign test
lists only, and reads lists only from `LEAFEN_RELEASE_LIST_URL`.
