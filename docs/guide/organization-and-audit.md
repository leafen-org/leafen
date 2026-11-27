# Organizations and audit

<!-- guide-sources:
web/src/locales/en/workspaceSettingsDialog.json
web/src/locales/en/serverConsole.json
web/src/components/admin/AdministratorAuditLog.tsx
internal/app/organization_audit.go
internal/app/organization_admin_role_policy.go
organization/enterprise_saml.go
organization/enterprise_scim.go
organization/enterprise_legal_hold.go
organization/enterprise_dlp.go
organization/enterprise_mcp_governance.go
organization/enterprise_audit_retention.go
-->

This chapter is for the people who run a company's Leafen: an organization's
owners and admins, and the server's administrators. It covers the
organization, its admin roles and groups, single sign-on and SCIM, the
enterprise controls, and the records Leafen keeps of who did what. Members,
invitations and teamspaces of one workspace are in
[Workspaces and members](workspaces-and-members.md). For a law firm, clinic or public office, read
[Leafen in a regulated organization](../regulated-organizations.md) as well.

Every feature here is in every Leafen: there are no plans.

## The organization

An organization holds workspaces and the people in them. Setting a server up
makes one for the first workspace, named after its owner (for example "Alex
Example's Organization"), with that person as its **Owner**. An organization
doesn't take over workspaces its people made outside it.

Open the workspace name in the sidebar → **Settings** → **Organization**
(under **Admin**). The page shows:

- **Organization admin**: the organization's name and your role.
- **Members**: everyone in the organization's workspaces, with their role.
- **Groups**: **Add group** makes a group of people across the organization.
  Groups an identity provider sends over SCIM show here too and are changed
  there.
- **Admin roles**: give a member one area of the organization's settings.
  They see only the settings their role covers; their access to pages doesn't
  change.
- **Data retention**: how long the updates feed and page analytics are kept.
- **Audit log**: the organization's record, below.

### Admin roles

| Role | What it may do |
| --- | --- |
| Compliance admin | View the audit log, run content search, and manage legal holds. |
| People admin | View and manage the organization's members, guests and groups. |
| IT admin | Manage identity, security and integrations across the organization. |
| Analytics viewer | View members, content and search analytics in the organization's workspaces. |
| Billing admin | Manage the organization's storage and file usage. |

## Identity: domains, single sign-on and SCIM

Settings → **Identity** (under **Admin**):

1. **Verified domains** → **Add domain**: add your company's email domain and
   put the TXT record the screen gives in its DNS. Leafen checks it; DNS
   changes can take minutes to hours.
2. **User management** then applies to everyone on your verified domains:
   **Managed users dashboard**, whether they may edit their profiles or join
   workspaces your organization doesn't own, **Allowed secondary emails**,
   **Session duration** (180 days by default), **Log out all managed users**
   and **Reset passwords for all managed users**.
3. **SAML single sign-on (SSO)** → **Enable SAML SSO**. Give your identity
   provider the **SAML metadata URL**, or the **Assertion Consumer Service
   (ACS) URL** and the **SAML SSO Entity ID** the page shows. The ACS URL and
   the entity ID hold from before anything is saved, so the provider's side
   can be set up first and its values entered under **Edit SAML SSO
   configuration** after. You choose whether people must use SSO or may
   use any method; organization owners can always log in with a password.
   **Automatic account creation** makes a Leafen account for a new person who
   logs in through SSO, in the workspaces listed under **Linked workspaces**.
4. **SCIM provisioning** → **Use SCIM provisioning**: your identity provider
   creates and deactivates the organization's users and groups at the **Base
   URL** shown, with a token from **SCIM tokens** → **Create token**. A token
   shows only once. Only people on verified domains are provisioned.

**Enterprise-managed connections** → **Approved MCP clients only** lets only
the MCP clients you approve connect to the organization's workspaces;
removing an approval stops its tokens at once (see [Agents](agents.md)).

The addresses a SAML provider uses follow the server's address; see
[Outside address](outside-address.md).

## Enterprise controls

Settings → **Enterprise controls** (under **Admin**):

- **Data loss prevention**: **Use data loss prevention** checks for sensitive
  terms and blocks the ways out you choose.
- **Audit log**: **Audit record retention** keeps records by default; **Open
  audit log** goes to the log.
- **eDiscovery**: **Create evidence export** searches the organization's
  pages, blocks, comments and file details, the trash included, and downloads
  the result as a file.
- **Legal holds**: **Create legal hold** keeps what it holds from being deleted
  for good when someone deletes it.
- **Notion-compatible Admin API**: the legal hold, workspace export and managed
  user session operations of Notion's Admin API, at the address and with the
  tokens this section gives.

Settings → **Security** holds the workspace's switches, such as **Disable
public forms, page links and sites**, **Disable duplicating pages to other
workspaces**, **Disable page exports**, view links for agents and **User
HTML**.

## The records Leafen keeps

| Record | Where | Who reads it |
| --- | --- | --- |
| Organization audit log | Settings → **Organization** → **Audit log** | Owners and compliance admins. Filter by person, kind and dates; **Export CSV**. |
| Server audit log | Settings → Server → **Audit log** | Server administrators: logins and their attempts, device codes, setup and administrator changes. |
| Server administrator log | Settings → **Security** → **Server administrator log** | The workspace's owners and its organization's admins: what server administrators opened or changed in this workspace. |
| Request log | Settings → Server → **Audit log** → **Request log** | Server administrators, to find out what went wrong. |
| Read log | Turned on with **Record reads** in Settings → Server → **Audit log** | People with full access and the organization's compliance admins: who read each page, item and file, when and how. |
| Login history | Settings → your name → **Login history** | The account's own login attempts. |

The request log keeps entries for 5 days by default and one in a hundred
successful requests; failed requests, requests that take a second or more,
administrators' changes, logins and setup are always kept, up to 300,000
entries, the oldest going first. The audit logs are kept apart and aren't
affected. How the read log and the request log are stored is in
[Self-hosting: Request log](../self-hosting.md#request-log) and
[Read log](../self-hosting.md#read-log).

Server administrators can see every workspace on the server, read only; each
workspace's owners see what they opened or changed in the **Server
administrator log**. See
[Self-hosting: What administrators see](../self-hosting.md#what-administrators-see).

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Session duration for managed users | 180 days | Set in **Identity**. |
| Updates feed retention | 1 year | **Data retention** on **Organization**. |
| Page analytics retention | 1 year | Analytics ranges go no further. |
| Audit record retention | Kept | Unless you set a period. |
| Request log | 5 days, one in a hundred successful requests | At most 300,000 entries. |
| SCIM token | Shown once | |

## For agents and the API

An agent connected as an organization's admin can read and change these
settings through the API, as the screens do: `leafen_operations` finds the
operations and `leafen_call` runs them. An identity provider's SCIM
requests go under the **Base URL** on **Identity**, such as
`/api/leafen/v1/scim/v2/Users`, and Notion-compatible Admin API calls go to the
address on **Enterprise controls**. An agent works only within its person's
role.

## Differences from Notion

- Every enterprise feature is in every Leafen, with no plan.
- The settings window also holds the server's settings, for the server's
  administrators, and the enterprise controls and the organization stay among
  the **Admin** items.
- Nobody outside your organization finds a member's name and photo by email.
- An organization doesn't take over workspaces its people made outside it.
- Custom admin roles, a Notion beta, are not in Leafen yet.
- An organization decides which MCP clients may connect, instead of Notion
  AI's settings.

## Related chapters

- [Accounts and devices](accounts-and-devices.md)
- [Exporting](exporting.md)
- [Leafen in a regulated organization](../regulated-organizations.md)
- [Self-hosting: What administrators see](../self-hosting.md#what-administrators-see)
