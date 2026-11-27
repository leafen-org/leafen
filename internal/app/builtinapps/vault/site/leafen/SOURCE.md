# The web vault in Leafen's Vault app

Bitwarden's web vault as Vaultwarden builds it, version 2026.7.0, unchanged but
for its source maps, which are left out. Leafen doesn't ship it: the server
fetches the release below, checked by its SHA-256, the first time the web vault
is opened, and keeps it in its data folder.

- Build: https://github.com/dani-garcia/bw_web_builds/releases/download/v2026.7.0/bw_web_v2026.7.0.tar.gz
  (SHA-256 002e972bf0d0487ec0324b06d916de33e29de4c29bffd92ee3b843084c300570)
- Its build's source: https://github.com/dani-garcia/bw_web_builds (tag v2026.7.0)
- The web vault's source: https://github.com/bitwarden/clients (apps/web)

The web vault is free software under the GNU General Public License,
version 3 (COPYING.txt here, once the web vault is fetched). Bitwarden is a
trademark of Bitwarden Inc.; Leafen's Vault app is compatible with Bitwarden's
apps and is not made by or affiliated with Bitwarden Inc. or Vaultwarden.

A server without the internet takes the release's archive put in its data
folder as `app-page-packs/002e972bf0d0487ec0324b06d916de33e29de4c29bffd92ee3b843084c300570.tar.gz`.
