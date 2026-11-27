# Sharing and permissions

<!-- guide-sources:
web/src/components/page/TopBarShare.tsx
web/src/components/page/TopBarShareGeneralAccess.tsx
web/src/components/page/TopBarShareInviteField.tsx
web/src/components/page/TopBarShareWebRole.tsx
web/src/components/page/TopBarShareAdvanced.tsx
web/src/components/page/TopBarShareInherited.tsx
web/src/components/page/TopBarShareAccessPanel.tsx
web/src/components/page/TopBarEditRequest.tsx
web/src/components/page/topBarShareModel.ts
web/src/components/product/LocalAddressHint.tsx
web/src/components/auth/AuthGateScreen.tsx
web/src/components/PublicSiteSharePanel.tsx
web/src/locales/en/topBar.json
web/src/locales/en/siteBuilder.json
internal/app/page_access_policy.go
internal/app/page_share_inheritance.go
-->

This chapter covers who can open a page and how to change it: the share menu,
access levels, guests, link sharing and publishing a page as a site. The
words workspace, teamspace, member and guest are explained in
[Concepts](concepts.md); adding people to the workspace itself is in
[Workspaces and members](workspaces-and-members.md).

## The share menu

**Share** in a page's top bar (Cmd+Shift+O, Ctrl+Shift+O) opens the share
menu. Its **Share** tab holds:

- a field, "Email or group, separated by commas", and **Share**;
- the people and groups the page is shared with, each with their level. The
  first row is the person who holds full access to the page — you on your
  own private page, the workspace's owner on a teamspace page;
- **General access**: who else may open the page;
- **Advanced** and **Copy link**.

The button's icon and tip say how the page is shared: "Only you can access",
"Shared with specific people", "Everyone in the workspace can access",
"Anyone with the link can access" or "Published to the web". The top bar's
access chip (**Private** with a lock on a page only you can open) opens the
same menu.

## Access levels

| Level | Lets them |
| --- | --- |
| **Full access** | Edit, suggest, comment, and share |
| **Can edit** | Edit, suggest, and comment |
| **Can comment** | Suggest and comment |
| **Can view** | Read the page |

A row's level menu also has **Remove**, which takes that person's access
away (they may still reach the page another way, which the menu says).
A member of the workspace who can only view or comment on a page has
**Request edit access** in its top bar (in the page menu where the bar is
narrow), and their share menu says "You can
only view this page" (or "You can only comment on this page") beside it. A
guest or a restricted member has no such request, and a guest who cannot
edit the page has no **Share** button at all.

## Share with someone

1. In the field, type an email, a member's name or a group, and choose it.
   Several can be added at once.
2. The level beside them starts at **Can view**; choose another if you like.
   **Write a note to go with the invite…** writes a note. When everyone chosen is
   a member of the workspace (not an email, a guest or a restricted member),
   **Notify me when page is accessed** also asks to tell you when they open
   it.
3. Choose **Share**.

A member of the workspace gets the page at once; it shows in their sidebar's
**Shared** section. A private page of yours moves from **Private** to
**Shared** in your own sidebar once it is shared. Someone outside the
workspace becomes a **guest**: an invitation is made for their email and sent
by mail where the server sends mail. Their row says **Guest**; where no mail
went ("No email sent"), **Copy invite link** on the row gives the link to pass
on. Whoever opens it sees **You're invited**, types a name over the one filled
in and chooses **Join**, then the language Leafen uses; the page opens, and
their sidebar lists under **Shared** only the pages shared with them. Someone
who already has an account on this server chooses **I already have an account
here** and logs in instead.

The link is made with the server's own address. While the server has only a
local address, the share menu's foot says "Only this computer can open this
address" (or "Only devices on the same network can open this address."), and
a guest elsewhere cannot open the link; a server administrator sees **Turn on
outside access** beside it ([Outside address](outside-address.md)).

Whether members may invite guests, and whether owners must approve, is set
in Settings › **Requests** › **Controls**
([Workspaces and members](workspaces-and-members.md#requests)); a teamspace
can turn guests off for its pages (**Disable guests**). Where a member may
only ask, the menu offers **Ask an owner** instead.

## General access

**General access** sets who else may open the page:

- **Only people invited** — only the rows above;
- **Everyone at** the workspace — every member, at the level you choose
  (it starts at **Full access**);
- **Anyone on the web with link** — anyone holding the page's web link, even
  without an account. Its level is **Can view**, **Can comment** or **Can
  edit**: reading is for anyone, while commenting and editing are for logged-in
  people of the organization who open it. **Link expires** ends it **Never**,
  **In an hour**, **In a day**, **In a week** or on a day you pick, and
  **Remove** turns the web link off. "Whoever has the link can read this
  page and see who worked on it." While it is on, the page says "Shared on
  the web: whoever has the link can view this page."

On a teamspace page the choices name the teamspace's members and owners
instead of the workspace.

The web link is an address with a secret token of its own, not the page's own
address. **Copy link** in the share menu copies the right one: the page's
address, or with the web link on, the web link. Turning the web link off, or
letting it expire, ends it: a visitor's open tab says the page is not found
within 30 seconds.

## Sub-pages and inherited access

A sub-page has its parent's sharing. Rows that come from a page above say so
("via user access on …"). Changing a sub-page's own access asks **Change
access and stop following the parent page’s sharing?** and, once done, the
menu says "This page has its own share settings, unlinked from the parent page …",
with **Restore settings** to link them again. A teamspace's pages take the
teamspace's defaults the same way.

## Agents

**Advanced** › **Who can grant agents access** decides who may let an agent
or an app account into the page: **Full access only** (how every page starts)
or **Anyone with access**. It also lists the agents that have access.

## Publish to the web

The share menu's **Publish** tab, **Publish to web**, makes the page a public
website: "Create a website with Leafen". **Publish** takes only someone with
full access, and not a page in the trash. Once it is published:

- the page has an address on the server, which **Change page address**
  edits, and **Copy site link** and **View site**;
- **Customize site styling** sets the **Theme** (**System**, **Light mode**,
  **Dark mode**), the **Header** (**Default** or **Breadcrumbs**),
  **Navigation** (up to 12 pages in the header), the **Favicon**, **Search**,
  **Share** and **Leafen branding**;
- **Search engine indexing** (**Searchable on the web**), the **SEO preview**
  and **Google Analytics**;
- **Duplicate as template** lets visitors copy the page into a workspace of
  their own;
- **Embed this page** gives code that shows the page on another website;
- **Publish changes** sends later edits to the site, and **Unpublish** takes
  it down.

Settings › **Public pages** lists every site, form and link a workspace has
published. An organization can turn publishing off, and a teamspace can
(**Disable publishing sites, forms, and public links**). The server must be
reachable from outside for a site to be seen by others
([Outside address](outside-address.md)).

## Link previews

An address of the server pasted into a messenger or a chat app — its first
screen, its login, a page's address — previews as the server: its name,
"Notion, rebuilt as open source." in the language of the person who set the
server up, and a 1200×630 picture of the server's icon with Leafen's name. A
page's own address previews as the page — its title, its first words and its
cover — only while the page is published to the web, by its web link or as a
site; any other page previews as the server, so nothing of a private page
leaves it. A web link and a site preview as their page, and as the server
once unknown, turned off or expired.

Messengers keep a preview for a while. After renaming the server or
publishing a page, KakaoTalk's copy is cleared in the Kakao share debugger
(<https://developers.kakao.com/tool/debugger/sharing>): enter the address and
press **캐시 초기화**.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| New share's level | **Can view** | Chosen per invite. |
| **Everyone at** the workspace | **Full access** | |
| Web link level | **Can view** | Or **Can comment**, **Can edit**. |
| Web link expiry | **Never** | Or an hour, a day, a week, a day. |
| Who can grant agents access | **Full access only** | |
| Site header pages | Up to 12 | |
| Invitation | 7 days | For a guest's email invitation. |

## For agents and the API

- An agent sees and changes only what the person who connected it may.
- `notion-fetch` and `notion-get-users` show who a page is shared with as the
  person can see it; sharing is changed on the native endpoint through
  `leafen_operations` and `leafen_call`. See the
  [MCP reference](../../MCP.md) and [Building on Leafen](../developers.md).

## Differences from Notion

- An invite starts at **Can view**; the inviter widens it. Everyone in the
  workspace still starts at **Full access**.
- The share menu's top row is the person who holds full access to the page,
  and it keeps full access.
- Guests and restricted members below full access see only their own row in
  the share menu, never other people's emails.
- Only people with full access let agents into a page, unless they change it.
- The web link has a secret token and can be made anew; a visitor's open tab
  closes within 30 seconds when the link or site goes.
- Inviting yourself to a page adds nothing.
- A site has no custom domain of its own; the whole server can be served on
  your own domain instead.

## Related chapters

- [Concepts](concepts.md)
- [Workspaces and members](workspaces-and-members.md)
- [Pages](pages.md)
- [Mentions and comments](mentions-and-comments.md)
