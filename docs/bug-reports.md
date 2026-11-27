# Reporting a bug

This guide is for people and for the agents they ask to write a bug report
about Leafen. Both follow the same steps. A report goes in the repository's
issues, through the bug form:
[new bug report](https://github.com/leafen-org/leafen/issues/new?template=bug_report.yml).
Keep it short: it holds only what it takes to make the problem happen again.

Not in an issue:

- Questions and ideas go to
  [Discussions](https://github.com/leafen-org/leafen/discussions).
- Security weaknesses are reported privately, as [SECURITY.md](../SECURITY.md)
  describes.
- Code. Describe the fix you think of in words; we write the fix ourselves,
  and pull requests from outside the team are not accepted yet, translation
  fixes aside ([Translations](../CONTRIBUTING.md#translations)).

## For agents

- When a Leafen tool fails or does other than its description says, or the
  person finds something in Leafen itself hard to use, offer a report once,
  after the task they asked for — once per problem, and never about the
  content of their own data. A security weakness never goes in an issue: it
  is reported privately, as [SECURITY.md](../SECURITY.md) describes.
- Connected to Leafen, draft it with `leafen_bug_report`: it fills in the
  version and the system, gives the body in the form's headings and a link to
  the form with the draft filled in, finds issues with the same words — when
  one is the same problem, add to it instead of opening another — and posts
  nothing.
- Write the report with the person: ask what they did, what they expected and
  what happened, one thing at a time, in their language.
- Use made-up data only. Never copy the person's pages, databases, files,
  names, mail addresses, server address, tokens or codes into the report or
  its attachments. Make up data that shows the same problem, or say how to
  make it.
- Where you can, check that the steps still show the problem starting from a
  new workspace with only that data, and write them as they went.
- Show the person the finished report and post nothing before their yes.
  Then post it with your GitHub login, keeping the form's headings in its
  body, or give them the link to check and submit themselves. If they say
  no, don't offer that report again.

## What a report holds

The form asks for these six, and each is required.

1. **Version.** Leafen's version: Settings › Server › Updates shows it to an
   administrator, and a connected agent reads it with `leafen_server_version`.
   When Leafen's **Report issue** opened the form, or `leafen_bug_report`
   drafted the report, it is filled in already.
2. **Operating system and install.** The operating system and its version, and
   how Leafen runs: desktop app, Docker or from source. In a browser or the
   phone app, which one and its version.
3. **Synthetic data.** A file with made-up content that shows the problem —
   Markdown or CSV that Leafen imports, or a file the problem is about — or
   the steps that make it in a new workspace. When only real data shows the
   problem, describe it without its content: how many pages, rows,
   properties and files; which kinds (block types, property types, views);
   how they nest and relate; how large they are. For example: one database
   of 12,000 rows, three relations to a second database of 400 rows, a
   formula over a rollup.
4. **Steps.** Numbered, from a known state (a new workspace, or the data
   above just imported), one action each.
5. **Expected and actual.** What you expected, and what happened instead:
   the exact message, and the reference (`LF-…`) when one shows; whether it
   happens every time.
6. **The fix you think of.** In words: where you think it goes wrong and what
   should happen instead. No code, patches or diffs.

Screenshots and logs are welcome in the form's last field, with personal data
and secrets taken out. Keep one problem in each report, and search the issues
for the same symptom first.

## After it is posted

We can't answer every report; we fix what reproduces first. A report that
reproduces gets the label **reproduced**, and the work on it goes on in our
own list, not in the thread. The release that fixes it closes the issue: its
public commit names the issue number and the reporter's GitHub name.
