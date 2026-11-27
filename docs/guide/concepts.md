# Concepts

<!-- guide-sources:
internal/app/page_access_policy.go
internal/app/page_share_inheritance.go
internal/app/teamspace_page_roles.go
web/src/components/sidebar/SidebarPageSections.tsx
web/src/components/page/TopBarShareGeneralAccess.tsx
-->

This chapter explains the things Leafen is made of — servers, accounts,
workspaces, teamspaces, pages, blocks, databases, people and access — and how
they hold one another. It names no buttons; the chapters that do are linked
from each part. [Getting started](getting-started.md) is the place to start
using Leafen.

## How things nest

```text
Leafen server
├── accounts (people who log in) and their devices
└── workspaces
    ├── members, guests and groups
    ├── teamspaces → their pages
    └── private pages
        └── a page → blocks, sub-pages and databases
                         └── a database → data sources → rows (each a page)
```

## Server and accounts

A **Leafen server** is one running Leafen: the desktop app on a computer, or
a server run with Docker. It keeps everything in one data folder. Its
**administrators** run it (settings, backups, login rules); the person who
set it up is the first one. A server's administrators can open every
workspace on it read only, and what they open is recorded for the workspace's
owner to read ([Self-hosting](../self-hosting.md#what-administrators-see)).

An **account** is one person on one server. It logs in from **devices**:
the first is the one that set it up or accepted the invitation, and others
join with a one-time code from a logged-in device. An email, a password and a
passkey are optional backups. One app or browser can hold accounts on several
servers and switch between them.

## Workspaces

A **workspace** is a shared place of pages and the people in it. A person can
be in several workspaces, on one server or many, and switches between them
from the sidebar. Each workspace has one **owner**, who changes its settings
and decides who is in it; [Workspaces and members](workspaces-and-members.md)
covers members, roles and invitations.

An **organization** gathers a company's workspaces on a server, with its own
administrators for people, security and audit
([Organizations and audit](organization-and-audit.md)).

## Teamspaces

A **teamspace** is a group of a workspace's members with pages of its own. Its
pages show in the sidebar's **Teamspaces** section for its members. A
teamspace is **Default** (everyone is in it), **Open** (anyone may see and
join it), **Closed** (anyone may see it; members invite others) or **Private**
(only its members know it exists). Its owners set what access its members and
the rest of the workspace have to its pages.

## Pages and blocks

A **page** is a document: a title, an optional icon and cover, and a body of
**blocks**. Every paragraph, heading, list item, image, table or embedded
database is one block, and blocks can hold other blocks (a toggle's contents,
a column's). A page can hold **sub-pages**, which show under it in the
sidebar, so pages make a tree.

Every page lives in one place:

- **Private** — at the top of your own pages. A private page is yours alone
  until you share it.
- **A teamspace** — at the top of a teamspace, for its members.
- **Under another page** — a sub-page, in the place its top page is in.
- **In a database** — a database's row is a page too.

The sidebar's **Shared** section lists pages other people shared with you
directly. [Pages](pages.md) and [Editor blocks](editor-blocks.md) describe
pages and blocks.

## Databases

A **database** is a collection of pages with properties — a status, a date,
a person, a number — shown in **views** such as a table, a board or a
calendar. A database's rows come from one or more **data sources**, each with
its own properties; every row is a page that can hold blocks of its own. A
database is **full page** (a page of its own) or **inline** (a block inside a
page). Databases have their own chapters
([Databases](README.md#databases)).

## People and access

| Who | What they are |
| --- | --- |
| **Workspace owner** | The one person who runs a workspace. |
| **Member** | In the workspace: sees its teamspaces' pages as their access allows. |
| **Restricted member** | In the workspace, but sees only what they made or what is shared with them. |
| **Guest** | Outside the workspace: opens only the pages shared with them. |
| **Group** | Members gathered under one name, to share with at once. |
| **Agent** | An AI agent connected by a person. It works as that person and sees what they may see. |

Access to a page is one of four levels, from least to most:

| Level | Lets you |
| --- | --- |
| **Can view** | Read the page. |
| **Can comment** | Read, comment and suggest changes. |
| **Can edit** | Read, comment, suggest and edit. |
| **Full access** | All of that, and share the page with others. |

A page's access comes from where it is and from what is shared:

- A **private page** gives its owner full access and nobody else anything.
- A **teamspace page** gives its teamspace's owners, its members and the rest
  of the workspace the levels the teamspace sets.
- A **share** gives one person, group or the whole workspace a level on one
  page. Its **General access** says who else gets in: only the people
  invited, everyone in the workspace, or anyone on the web with the link.
- A **sub-page** has its parent's access, until its own sharing is changed;
  it is then unlinked from the parent's.

Someone with access from more than one of these has the highest.
[Sharing and permissions](sharing-and-permissions.md) shows how to share.

## Trash and history

Nothing you delete is gone at once. A deleted page goes to the **Trash**,
where it can be restored for 30 days ([Trash](trash.md)), and every page keeps
earlier versions you can look at and bring back
([Page history](page-history.md)).

## For agents and the API

- An agent connected over MCP acts as the person who connected it, inside the
  workspaces that person chose for it, with that person's access. It never
  sees more than the person.
- `notion-get-self` names the connection's person and workspaces;
  `leafen_list_workspaces` lists the workspaces it reaches.
- Leafen's MCP and API use Notion's words for the same things: a page, a
  database, a `collection://` data source, a `view://` view. See the
  [MCP reference](../../MCP.md).

## Differences from Notion

- A Leafen server is yours: it runs on your computer or your server, and its
  administrators can open what is on it.
- Devices join an account with a one-time code from a logged-in device; there
  is no Notion account to log in to.
- The sidebar is one list, without Notion's tabs for its AI chat and
  meetings: Leafen has no built-in AI.

## Related chapters

- [Getting started](getting-started.md)
- [Workspaces and members](workspaces-and-members.md)
- [Sharing and permissions](sharing-and-permissions.md)
- [Pages](pages.md)
- [The interface](interface.md)
