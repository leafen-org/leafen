# Favorites, recents and notifications

<!-- guide-sources:
web/src/components/sidebar/SidebarPageSections.tsx
web/src/components/home/HomeRecents.tsx
web/src/components/UpdatesPanel.tsx
web/src/components/account/AccountNotificationSettings.tsx
web/src/locales/en/updatesPanel.json
web/src/locales/en/sidebar.json
internal/app/notification_digest.go
-->

This chapter covers three ways back to what matters to you: favorite pages,
the pages you visited lately, and notifications in the inbox, by mail and
from the browser. The sidebar and Home themselves are in
[The interface](interface.md); comments and mentions, which send most
notifications, are in [Mentions and comments](mentions-and-comments.md).

## Favorites

☆ in a page's top bar (**Add to Favorites**), or **Add to Favorites** in the
page's sidebar menu, stars the page. Starred pages are listed in the
sidebar's **Favorites** section, which appears at the top once there is one,
and in the Library's **Favorites** tab. The page stays where it is; a
favorite is only yours, and starring changes nobody's access. Choose ★
(**Remove from your favorites**) to take it off.

## Recents

Leafen keeps the pages you open and edit:

- **Search** with nothing typed lists your recently viewed pages, by
  **Today**, **Yesterday**, **Past 7 days**, **Past 30 days** and **Older**;
- **Home** shows **Recently visited** and **Recently edited**;
- the Library's **Recents** tab lists them all;
- **Customize sidebar** › **Add section** › **Recents** puts them in the
  sidebar ("Your recently opened pages show up here.").

Only you see your recents. Whether others see that you viewed a page is
**Show my view history** in Settings › **Preferences** (people with edit or
full access see who viewed a page, unless the organization decides).

## The inbox

**Inbox** at the top of the sidebar (Cmd+Option+U, Ctrl+Alt+U) opens your
notifications beside the sidebar; the button names how many are unread.

A notification says what happened, such as "Alex Example mentioned you in
Trip plan", "… commented in …", "… invited you to …", "… requested access
to …", "Reminder in …" or "… edited …", grouped by **Today**, **This week**,
**Last week** and **Earlier**. Choose one to open the place it names. Hover
it to **Mark this notification as read** (or unread) or **Archive this
notification**; an access request has **Approve** and **Decline** in place.

The inbox's header holds:

- **Pin** — keeps the inbox open beside pages;
- **Filter notifications** — **Status**: **Unread & read** (how it starts),
  **Unread**, **Archived** or **All workspace updates** (every change in the
  workspace, not only yours); **Type**: **Permissions**, **Discussions**,
  **Mentions**, **Automations & runs**, **Updates** and **Workspace**;
- ⋯ — **Mark all as read**, **Archive all** and **Notification settings**.

### What you hear about

You hear about mentions of you, replies and comments in discussions you are
in, invitations and access given to you, access requests to pages you hold,
reminders you set, and the pages you follow. Each page's menu sets what it
tells you: **Notify me** › **All comments** (how a page starts) or **Replies
and @mentions** ([Pages](pages.md#the-page-menu)). A discussion's ⋯ › **Mute
replies** stops one discussion.

## Notification settings

Settings › **Notifications** (in the **Account** group):

| Setting | What it does | Starts |
| --- | --- | --- |
| **Desktop app push notifications** | Notify this browser or app about mentions, replies and pages you follow if you have not seen them after 10 seconds. | Off |
| **Slack notifications** | Sends notifications to Slack, where the server's administrator set up a Slack app. | Off |
| **Activity in my workspace** | Emails about comments, mentions, page invites, reminders, access requests and database property changes. | On |
| **Always send email notifications** | Emails even while you are active in Leafen. | Off |
| **Page updates** | A daily email digest of the pages you follow or made that others changed. | On |
| **Workspace digest** | A weekly email digest of what is most read in your workspaces. | On |

Email needs the server to send mail; without it, the email settings are off
and say so, and an administrator sets up mail in Settings › **Email
delivery**. Each digest has a link to stop that digest alone.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Page notifications | **All comments** | Per page, under **Notify me**. |
| Inbox filter | **Unread & read** | |
| Push delay | 10 seconds unseen | Then the push goes out. |
| Page updates digest | Once a day, up to 20 items | |
| Workspace digest | Once a week, the 5 most read pages | |

## For agents and the API

- `notion-list-favorite-pages` lists your favorites, the latest first, and
  `notion-update-page-favorites` stars or unstars 1 to 100 pages.
- `notion-list-recent-pages` lists the pages you visited lately, as Recents
  does.
- `leafen_inbox` reads your inbox when you ask an agent about your notices
  or its task concerns one; an agent doesn't bring them up on its own.
  `leafen_inbox_item`, `leafen_inbox_read` and `leafen_inbox_remind` open one,
  mark them read and leave them out of that agent's list until later. See
  the [MCP reference](../../MCP.md#the-inbox-on-the-native-endpoint).
- The native API: `GET /api/leafen/v1/workspaces/{workspaceId}/notifications`.

## Differences from Notion

- The notification settings have no line for Notion's announcements and
  update emails.
- The phone apps get no real-time push from Leafen; web push reaches Leafen
  added to a phone's home screen.

## Related chapters

- [The interface](interface.md)
- [Mentions and comments](mentions-and-comments.md)
- [Pages](pages.md)
- [Language and time](language-and-time.md)
