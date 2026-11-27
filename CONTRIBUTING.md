# Contributing to Leafen

Leafen is made by one person with Claude, so for now we take part in it this way:

- **Bug reports** go in the repository's issues, through the bug form. We
  strongly encourage writing the report with the agent you use: paste this
  line to it.

  ```text
  Read https://github.com/leafen-org/leafen/blob/main/docs/bug-reports.md and write a bug report.
  ```

  The [bug report guide](docs/bug-reports.md) is the same document a person
  can follow by hand.
- **Questions and ideas** go to the repository's GitHub Discussions.
- **Translation fixes** come as a pull request or an issue, as
  [Translations](#translations) below describes.
- **Pull requests from outside the team are not accepted yet**, translation
  fixes aside. Describe the fix you think of in your report, in words, not as
  code: we write the fix ourselves. We will say so here when pull requests
  open. Every contribution comes under the
  [contributor license agreement](CLA.md), below.
- **Security weaknesses** are reported privately, as [SECURITY.md](SECURITY.md)
  describes — never in an issue or a discussion.

We can't answer every post; we fix what reproduces first. When a release
fixes a reported bug, its public commit names the issue number and the
reporter's GitHub name, and closes the issue.

## Bug reports

The bug form asks for six things, each required: the Leafen version; the
operating system and how Leafen runs (desktop app, Docker, from source);
synthetic data that shows the problem, as a file or the steps that make it —
for a problem only real data shows, its counts, kinds and structure; the
steps to reproduce; what you expected and what happened; and the fix you
think of, in words.

Keep real workspace content, credentials, tokens and personal data out of
every report and attachment: made-up data shows the same problem. Keep one
problem in each report, and search the issues for the same symptom first.

## Questions and ideas

In Discussions, explain the problem before the idea: the affected workflow,
who benefits, the current workaround, the desired outcome, and any important
compatibility or usability trade-offs.

## Translations

The interface is written in English and Korean, and the other languages are
translated from the English. To fix a line that reads wrong in your
language, change it in that language's files under
[`web/src/locales/`](web/src/locales) — keep `{{placeholders}}` and tags as
they are, and leave `_meta.json` alone — and send a pull request; or report
it through the bug form, saying where the line shows, what it says and what
it should say.

A merged fix by a person comes first: machine translation does not overwrite
it. When the English line behind it changes, the line is translated again
with your wording as the guide. In the languages Notion offers, the core
words — page, database, workspace, view, trash and the like — keep Notion's
terms, and a fix that changes one is answered with Notion's term; the rest of
the sentence follows your fix. In a language Notion doesn't offer, your fix
stands as it is.

## Checks

The public CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) covers
the build and the core path (the smoke tests in `internal/smoke`); the full
Notion-compatibility suite is kept privately by the maintainers and run
before every release.

## Contributor license agreement

Every contribution — code, documentation, translations — comes under the
[Leafen Individual Contributor License Agreement](CLA.md), modelled on the
Apache Software Foundation's. You keep the copyright in your work and give
Leafen a license to it, including the right to release it under other terms
than the AGPL: a commercial license Leafen gives a company in a partner
contract, and Leafen's own app store editions under the stores' terms.

You sign it once, with your GitHub account: on your first pull request, the
CLA Assistant check asks you to accept the agreement by posting as a comment
the sentence it gives, and the pull request can be merged once you have.

## License

Leafen — its server, web client, desktop and phone apps and built-in apps — is
licensed under the GNU Affero General Public License, version 3 only
(see [LICENSE](LICENSE)). The app SDK (`sdk/`), the connection contracts
(`contracts/`), the examples (`examples/`), the benchmark tools (`bench/`) and
the documents for building on Leafen are licensed under the Apache License 2.0,
each with that license's text beside it; [NOTICE](NOTICE) lists them.
