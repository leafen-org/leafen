# Security policy

Leafen is a self-hosted workspace: people keep their pages, databases, files
and accounts on a server they run. A weakness in it can expose that data, so
we want to hear about one privately and fix it before it is public.

## Supported versions

Leafen hasn't reached 1.0 yet. Security fixes go into the latest release
only; please check that a weakness is still present there before reporting
it. Once 1.0 is released this section will say which release lines receive
fixes and for how long.

| Version | Supported |
| --- | --- |
| The latest release | Yes |
| Older releases, unreleased branches | No |

## Reporting a vulnerability

Report privately through GitHub's private vulnerability reporting: open the
repository's **Security** tab and choose **Report a vulnerability**. Only the
maintainers can read the report.

Please do not describe a vulnerability in a GitHub issue or discussion, in a
public pull request, or anywhere else in public before it is fixed.

A useful report includes:

- the Leafen version (or source revision) and how it was deployed (desktop
  app, Docker, from source);
- what an attacker can do and what they need first (an account, a role, a
  shared link, network position);
- steps to reproduce from a known state, with the smallest example that shows
  the problem;
- any logs, requests or screenshots, with secrets and personal data removed.

Test only against a server you run yourself, with synthetic accounts and
data. Do not access, change or delete other people's data, and do not run
tests that degrade a server others depend on.

## What happens next

Leafen is maintained by a small team, so these are targets rather than
guarantees:

1. We acknowledge the report within 7 days.
2. We confirm or rule out the problem and tell you our assessment within 30
   days, and keep you updated while a fix is prepared.
3. We release a fix as soon as it is ready and publish an advisory for it,
   normally within 90 days of the report. If a fix needs longer, we agree a
   date with you.
4. With your permission, we credit you in the advisory.

There is no paid bug bounty yet.

## Scope

In scope is the code in this repository, as built from it:

- the Leafen server and its HTTP, WebSocket and MCP endpoints;
- the web app, the desktop apps and the mobile apps;
- login, sessions, sharing, public pages, forms, permissions and
  organization policies;
- file uploads and downloads, imports and exports, backups and restores;
- the app platform: built-in and installed apps and their sandbox, and the
  credentials and connections it keeps for them.

Out of scope:

- weaknesses in third-party services Leafen connects to (Notion, mail, maps,
  identity providers, hosting providers), unless Leafen uses them unsafely;
- a server whose administrator has misconfigured it against the documented
  setup, or an attack that already requires the administrator's account or
  the server machine;
- denial of service by sheer request volume, spam, and social engineering;
- missing hardening that does not lead to a concrete weakness.
