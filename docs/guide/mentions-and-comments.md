# Mentions and comments

<!-- guide-sources:
web/src/components/editor/MentionMenu.tsx
web/src/components/CommentsPanel.tsx
web/src/components/comments/commentAttachmentDraft.ts
web/src/locales/en/blockItem.json
web/src/locales/en/commentsPanel.json
web/src/locales/en/selectionToolbar.json
web/src/commentWire.generated.ts
-->

This chapter covers mentioning people, pages and dates with **@**, commenting
on a page or on words in it, replying, resolving, and suggesting changes
instead of making them. The notifications they send are in
[Favorites, recents and notifications](favorites-recents-and-notifications.md); who may
comment on a page is set in [Sharing and permissions](sharing-and-permissions.md).

## Mentions

Type **@** in a page, a comment or a text property to open the mention menu,
then type to filter it:

- **Date** — **Today** and the dates you type, and **Remind me**, which adds
  a reminder (it offers "Tomorrow 9 AM") that reaches your inbox at that
  time.
- **People** — members and guests of the workspace. Mentioning someone tells
  them in their inbox. Someone who cannot open the page yet is marked, and
  Leafen asks **Invite to page?**: "Without an invite to the page, they get no
  notification." **Invite** shares the page with them (as **Can edit**);
  **Ignore** leaves the mention alone. **Invite "…"…** invites a new person
  by email.
- **Link to page** — pages you can open. The mention shows the page's current
  icon and title and opens it.
- **New page** — **New "…" sub-page** makes a sub-page named by what you
  typed and puts its mention in the line; **New "…" page in…** asks where to
  put it.

**/Mention a person**, **/Mention a page**, **/Date or reminder** and
**/Emoji** in the **/** menu do the same. A date mention's menu changes its
date and time and its reminder. In a template, **@** also offers **Today**,
**Now** and **Me**, filled in when the template is used.

## Comments

There are three places to comment:

- **On the page** — hover above the title and choose **Add comment**. Page
  comments show under the title (**Page comments** in the page menu's
  **Appearance & layout** turns them off or on).
- **On words** — select words and choose **Comment** in the formatting
  toolbar (Cmd+Shift+M, Ctrl+Shift+M). The words are highlighted and the
  discussion shows beside the page.
- **On a block** — the block menu's **Comment**.

Type the comment and press Enter (or **Comment**); Shift+Enter starts a new
line. With Settings › **Preferences** › **Use Enter to add a new line** on,
Enter starts a new line and Cmd+Enter (Ctrl+Enter) sends. In a comment you can **@** people, pages and dates,
use the same text formatting as a page, and attach up to 3 files.

Hover a comment for:

- **Add reaction** — an emoji reaction; choosing a reaction again takes yours
  off;
- **Resolve** (✓) — closes the discussion; **Reopen** brings it back;
- ⋯ — **Mark as unread**, **Edit** (your own), **Copy link**, **Copy link to
  discussion**, **Mute replies** and **Delete** (your own; the first comment
  of a discussion takes its replies with it).

**Reply…** under a discussion answers it.

### All discussions

When a page has comments, a speech-bubble button in its top bar opens
**Discussions**: every discussion on the page, under **New** and
**Earlier**, filtered by **Type** (**Comments**, **Suggestions**), **Status**
(**Open**, **Unread**, **Read**, **Resolved**) and **People**. A resolved
discussion is hidden from the page and kept here.

## Suggesting

Suggesting turns your edits into suggestions that someone accepts or rejects:

1. Turn it on from the page menu's **Suggest edits**, the formatting
   toolbar's **Suggest edits**, or Cmd+Shift+Option+X.
2. Edit as usual. Added words show in color, removed words struck through,
   and each change opens a discussion such as "Add: “…”" or "Delete: “…”".
   Typing over selected words is one change: the new words go in before
   the struck old ones, one discussion reading "Replace: “…”" then
   "with: “…”".
3. Someone who can edit the page chooses **Accept** or **Reject**; whoever
   suggested a change may reject it. A decided suggestion reads **Accepted**,
   **Rejected** or **Withdrawn**.

**Turn off suggesting** in the top bar ends it. Someone with **Can comment**
access can suggest but not edit.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Attachments in a comment | Up to 3 files | |
| Reminder from **Remind me** | Tomorrow 9 AM | Or a time you type. |
| Invite from a mention | **Can edit** | |
| **Page comments** | On | Page menu › **Appearance & layout**. |

## For agents and the API

- `notion-create-comment` comments on a page, a block or selected text, or
  replies to a discussion; `notion-get-comments` reads them (resolved ones
  too, on request).
- `notion-update-comment`, `notion-delete-comment`,
  `notion-set-discussion-resolved` and `notion-set-comment-reaction` change
  them.
- `notion-create-suggested-edit` proposes a change without making it;
  `notion-get-suggested-edit` reads one and `notion-resolve-suggested-edit`
  accepts or rejects it.
- The native API: `GET /api/leafen/v1/pages/{pageId}/comments` and
  `POST /api/leafen/v1/pages/{pageId}/comments`. See the
  [MCP reference](../../MCP.md).

## Differences from Notion

- A database mention in a comment stays a database mention.
- A comment written where the last reaction was taken back is kept.
- A mention picked with **@** while suggesting stays a suggestion, as does a
  multi-line paste.

## Related chapters

- [Editor blocks](editor-blocks.md)
- [Sharing and permissions](sharing-and-permissions.md)
- [Page history](page-history.md)
- [Favorites, recents and notifications](favorites-recents-and-notifications.md)
