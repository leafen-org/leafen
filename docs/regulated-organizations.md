# Leafen in a regulated organization

English · [한국어](regulated-organizations.ko.md)

> This page is general information about Leafen's settings, not legal advice.
> What a law firm, a clinic or a public body owes its clients, patients or
> citizens differs by country and profession; check with your own counsel and
> regulator before you rely on any of it.

Some organizations can't let their material leave: a law firm with its
clients' files, a clinic with patient records, a public office with citizens'
data. This page goes through how such an organization can bring in Leafen and
the agents its people use — where the material stays, which model reads it,
what an agent may reach, how clients come in as guests, and what is recorded —
with each setting by its name and where it is.

## 1. Keep the server your own

Leafen runs on a server you run ([self-hosting](self-hosting.md)): pages,
databases, files and records stay on that server and its backups. No
Leafen service sits between your people and it. Put it where your rules allow
— your own office, your own cloud account, your country — and keep its
backups there too.

## 2. Choose where the model reads

Leafen runs no model of its own. Agents come in from outside — Claude,
ChatGPT, Codex, Claude Code or a model you run — over MCP, and an agent sends
what it works on — the page, the client's file, the question — to its model.
Where that model runs decides where the material goes: nowhere, for a model on
your own computers or server; to the provider and kept nowhere, for a cloud
model under zero data retention; to the provider under its usual terms,
otherwise.

Settings → Identity → **Enterprise-managed connections** holds which agents
may connect: **Approved MCP clients only**, with the clients you approve,
blocks every other client and its tokens.

When a cloud model reads client material, it is processed by that provider,
often in another country. Then, as most rules ask:

- **Tell your clients** — in the engagement letter, the consent form or the
  privacy notice — that an AI service processes their material, which one, and
  in which country.
- **Sign a processing agreement** with the provider (a data processing
  agreement, an entrustment contract), with zero data retention written in
  where you rely on it.
- **Check the rules on sending data abroad** that apply to you, and on what may
  never leave (some records, some classified material).

## 3. What an agent may reach

An agent's connection works with its person's rights and no more: what they
may not open, it may not either. A connection reaches only the workspaces its
person gave it, with the scope they chose — read, content, structure or admin
— and the person sees and ends it as Notion's connections are. What also holds
it in:

- Settings → Enterprise controls → **Data loss prevention**: sensitive terms
  checked as pages, rows, comments and files are written, and **Block public
  web sharing**, **Block external sharing**, **Block file downloads**,
  **Block exports**.
- Settings → Security → Members & guests: **Who can invite guests to pages**,
  **Who can add other members to the workspace** and **Who can request
  workspace membership** (members may ask and anyone may request until you
  narrow them); Settings → Security → General → View links: **Allow view
  links without a secret** off, so a view link also needs a secret sent apart.

A client sees only what is shared with them (below). Leafen doesn't hold a
change back for approval before a guest sees it, so keep the step that puts
something in front of a client a person's: agents draft in your own pages,
and a person moves or shares the finished piece into the client's.

## 4. Clients as guests

Invite a client as a guest: a guest sees only the pages shared with them,
never the rest of the workspace. Who sees what is set with the same tools as
in Notion:

- **Teamspaces.** Keep each practice group's or matter's pages in a
  teamspace of its own (Settings → Teamspaces → New teamspace): **Closed**
  lets others in the workspace see it but not join, and **Private** hides
  that it exists from everyone who isn't its member — the wall between
  people who must not see each other's matters. Add its people one by one or
  as a **group** (Settings → People → Groups).
- **Sharing.** A page's Share menu gives the client a level on that page —
  view, comment or edit — and the pages under it come with it, as Notion's
  sub-pages take their parent's sharing. Sharing a page with the client
  makes them the organization's guest.
- **Page-level access.** In a database's Share menu, Page-level access → "Add
  new rule" gives everyone selected in a person property access to their own
  pages — say, each client to the matters they are named in.
- **Property access.** A property's menu → Property access hides a property
  (fees, internal notes) from those it doesn't list, in every view, export and
  agent read.

## 5. What is recorded

- **Edits.** Every edit stays in a page's version history and the workspace's
  updates; an agent's edits carry its person's name and its connection's.
- **The audit log** (Settings → Organization, for the owner and compliance
  administrators): permissions given and taken, sharing, members and guests,
  page-level rules, logins, keys and connections —
  filtered by person, period, kind and target, and exported as CSV.
  A member's **Activity history** opens from the members table
  ("History") or their profile; each person sees their own **Login history**
  under Settings → Account.
- **The read log.** Every page, item and file the server gives out — in the
  app, through the API, to an agent, by a share link or in an export — is
  recorded with who, when and how. A page's ⋯ menu → **Read log** shows the
  reads of it and all under it to those with full access there. A compliance
  administrator can set **Who reads this log** to "Compliance administrators
  only" for a page and all under it. Only the
  server's administrators can turn the read log off, for the whole server
  (Settings → Server → Audit log → Read log); keep it on.
- **Who viewed, in page analytics** (Settings → Organization, under the audit
  log): "Each person chooses", as Notion's "Show my view history"; "Show
  everyone", so no one hides their views — a law firm's choice; or "Show no
  one", only view counts — where seniors' visits shouldn't show to their
  staff. The read log keeps every read whichever it is.
- **Retention.** Settings → Enterprise controls → **Audit log** → **Audit
  record retention** keeps the audit log and the read log (unless changed),
  or deletes what is older than the days you choose, one or more. **Legal
  holds** there keep what a hold covers from being deleted.

## A short list

1. Run the server where your rules allow, with its backups.
2. Only approved MCP clients; agents on models of your own or that keep no
   data, and for any cloud model tell clients and sign the processing
   agreement.
3. Connections held to the workspaces and scope the work needs.
4. Data loss prevention on, with your sensitive terms; view links only with a
   secret.
5. Practice groups in closed or private teamspaces, clients as guests on the
   pages shared with them, with page-level rules and property access for what
   is internal.
6. The audit log with a compliance administrator, the read log on, who reads
   it and who viewed chosen, retention and legal holds set.
