# The Leafen guide

This guide explains Leafen one feature at a time. A person reads the chapter
they need. An agent connected to Leafen reads the same chapter and gets the
exact names, limits, defaults and steps, so it can act without guessing.

Every chapter is written from Leafen's source and checked against it. The
names on screen are the names in the chapters. Backticked names (an MCP tool,
an API path, a `leafen` command, a setting) are names that exist. Pictures use
made-up data only, and each one is retaken when the screen it shows changes.

Installing, running a server, the API and apps already have their own
documents. This guide links to them and does not repeat them.

## Start here

| If you are | Read |
| --- | --- |
| new to Leafen | [Installing Leafen](../install.md), then [Getting started](getting-started.md) and [Concepts](concepts.md) |
| writing pages | [Pages](pages.md), then [Editor blocks](editor-blocks.md) |
| building a database | [Databases](databases.md), then [Properties](properties.md) and [Views](views.md) |
| moving from Notion | [Moving from Notion](moving-from-notion.md), then [Differences from Notion](differences-from-notion.md) |
| connecting an AI agent | [Agents](agents.md), then [Leafen MCP](../../MCP.md) |
| building on the API, or making an app | [Building on Leafen](../developers.md), [Building Leafen apps](../apps.md) |
| running Leafen for other people | [Self-hosting](../self-hosting.md), then [Outside address](outside-address.md) and [Backups](backups.md) |
| stuck | [Troubleshooting](troubleshooting.md) |

Chapters without a link are still being written. Each one becomes a link when
it is published.

## Everything

### Using Leafen

- [Getting started](getting-started.md): the first page after Leafen is installed
- [Concepts](concepts.md): workspaces, teamspaces, pages, databases, people and permissions, and how they nest
- [The interface](interface.md): the sidebar, search, settings and the inbox
- [Pages](pages.md): title, icon, cover, sub-pages, the page menu
- [Editor blocks](editor-blocks.md): every block type, the slash menu, Markdown shortcuts
- [Mentions and comments](mentions-and-comments.md): mentions, comments and suggestions
- [Page history](page-history.md): earlier versions and bringing one back
- [Trash](trash.md): what moving to the trash does, and restoring
- [Sharing and permissions](sharing-and-permissions.md): page access, teamspaces, guests, link sharing, publishing a site
- [Workspaces and members](workspaces-and-members.md): members, invitations, roles, teamspaces
- [Language and time](language-and-time.md): language, time zone, date and time formats
- [Templates](templates.md): page templates
- [Favorites, recents and notifications](favorites-recents-and-notifications.md)
- [Keyboard shortcuts](keyboard-shortcuts.md)

### Databases

- [Databases](databases.md): creating one, inline databases, data sources, locking
- [Properties](properties.md): every property type, with its limits and defaults
- [Views](views.md): every view type, with filters, sorts and groups
- [Formulas](formulas.md): every function, with examples
- [Relations and rollups](relations-and-rollups.md)
- [Forms](forms.md)
- [Automations and buttons](automations-and-buttons.md)
- [Database templates](database-templates.md): row templates and repeating rows
- [Sprints](sprints.md)

### Leafen and connections

- [Moving from Notion](moving-from-notion.md): importing, matching people, importing again
- [Exporting](exporting.md): every way out of Leafen
- [Differences from Notion](differences-from-notion.md): what Leafen does differently on purpose
- [Outside address](outside-address.md): reaching Leafen from outside, through Tailscale, your own domain or a temporary link
- [Desktop and phone apps](desktop-and-phone-apps.md): logging in, device codes, notifications
- [Accounts and devices](accounts-and-devices.md): login, devices, sessions
- [Organizations and audit](organization-and-audit.md): organizations, administrators, the audit log
- [Agents](agents.md): connecting an agent over MCP, keys, working as a person, instruction files
- [File apps](file-apps.md): opening Leafen's files from other apps, such as over WebDAV
- [Backups](backups.md): backups and restores from the app; server backups are in [Self-hosting](../self-hosting.md)
- [Troubleshooting](troubleshooting.md): common failures and how to find the cause

### Installing, running and building

- [Installing Leafen](../install.md): desktop, Docker and source installs, and the first run
- [Self-hosting](../self-hosting.md): running a server, settings, proxies, backups, restores, upgrades
- [Leafen in a regulated organization](../regulated-organizations.md)
- [Leafen MCP](../../MCP.md): connecting, keys, and every MCP tool
- [Building on Leafen](../developers.md): keys, registered apps, the API, webhooks, view links
- [Building Leafen apps](../apps.md)
- [Leafen workspace bundles](../bundle.md): the file format Leafen imports and exports
- [Architecture](../architecture.md)
- [Development](../development.md): building Leafen from source and its checks

## For agents reading this

Each chapter stands alone: its first paragraph says what it covers and links
to what it does not. There is no reading order.

Where the screen and the tools use different words for one thing, the chapter
says so where it matters. An agent works as the person who connected it and
sees what that person may see, as the Agents chapter explains.

## Writing a chapter

The chapter template and the writing rules are in [Writing the guide](writing.md).
