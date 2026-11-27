# Keyboard shortcuts

<!-- guide-sources:
web/src/lib/keyboardShortcuts.ts
web/src/components/editor/editorInputModel.ts
web/src/components/shortcuts/KeyboardShortcutsDialog.tsx
web/src/locales/en/keyboardShortcuts.json
-->

This chapter lists every keyboard shortcut Leafen has, on a Mac and on
Windows and Linux. The Markdown you can type to make blocks is in
[Editor blocks](editor-blocks.md#markdown-shortcuts); the keys of a
database's views are in the Databases chapters
([Databases](README.md#databases)).

## The shortcuts window

Cmd+Option+/ (Ctrl+Alt+/ on Windows and Linux) opens **Keyboard shortcuts**:
the shortcuts in three lists, **Popular**, **Create & style** and **Edit &
move**, with **Search shortcuts…** to find one. The window shows the keys of
the computer you are on. Some shortcuts below are not in the window and work
all the same.

In the tables, "Cmd" is the Command key on a Mac and Ctrl on Windows and
Linux; "Option" is Alt on Windows and Linux.

## Anywhere

| Action | Mac | Windows and Linux |
| --- | --- | --- |
| **New page** | Cmd+N | Ctrl+N |
| **Search, or go back to a page you saw recently** | Cmd+K or Cmd+P | Ctrl+K or Ctrl+P |
| **Copy page URL** | Cmd+Option+L in a browser; Cmd+L in the Mac app | Ctrl+Alt+L in a browser; Ctrl+Alt+L or Ctrl+L in the Windows app |
| **Open the share menu** | Cmd+Shift+O | Ctrl+Shift+O |
| **Go back a page** | Cmd+[ | Ctrl+[ |
| **Go forward a page** | Cmd+] | Ctrl+] |
| **Toggle dark mode** | Cmd+Shift+L | Ctrl+Shift+L |
| **Open parent page** | Cmd+Shift+U | Ctrl+Shift+U |
| **Move current page** | Cmd+Shift+P | Ctrl+Shift+P |
| **Toggle sidebar** | Cmd+\\ | Ctrl+\\ |
| **Open inbox** | Cmd+Option+U | Ctrl+Alt+U |
| **Open keyboard shortcuts** | Cmd+Option+/ | Ctrl+Alt+/ |
| Go to the first to ninth workspace in the workspace menu (desktop app) | Control+Shift+1 to 9 | Ctrl+Shift+1 to 9 |

## On a page

| Action | Mac | Windows and Linux |
| --- | --- | --- |
| **Find in current page** | Cmd+F | Ctrl+F |
| **Rename current page** | Cmd+Shift+R | Ctrl+Shift+R |
| **Start presentation** | Cmd+Option+P | Ctrl+Alt+P |
| **Expand/close all toggles** | Cmd+Option+T | Ctrl+Alt+T |
| **Close panel** (the panel beside a page) | Cmd+Shift+\\ | Ctrl+Shift+\\ |
| **Open in new tab** (with a menu open) | Cmd+Shift+Enter | Ctrl+Shift+Enter |
| **Previous page** in a database page's peek | Control+Shift+K | Ctrl+Shift+K |
| **Next page** in a database page's peek | Control+Shift+J | Ctrl+Shift+J |

## Writing

| Action | Mac | Windows and Linux |
| --- | --- | --- |
| **Bold** | Cmd+B | Ctrl+B |
| **Italic** | Cmd+I | Ctrl+I |
| **Underline** | Cmd+U | Ctrl+U |
| **Strikethrough** | Cmd+Shift+X or Cmd+Shift+S | Ctrl+Shift+X or Ctrl+Shift+S |
| **Inline code** | Cmd+E | Ctrl+E |
| **Inline equation** | Cmd+Shift+E | Ctrl+Shift+E |
| **Clear formatting** | Cmd+Option+\\ | Ctrl+Alt+\\ |
| **Add link** | Cmd+K | Ctrl+K |
| **Add comment** | Cmd+Shift+M | Ctrl+Shift+M |
| **Suggest edits** | Cmd+Shift+Option+X | Ctrl+Shift+Alt+X |
| **Apply last used color** | Cmd+Shift+H | Ctrl+Shift+H |

Cmd+K adds a link while words are chosen; with nothing chosen it opens
search.

### Turn a block into another kind

| Keys (Mac: Cmd+Option; Windows and Linux: Ctrl+Alt) | Turns the block into |
| --- | --- |
| 0 | Text |
| 1, 2, 3 | Heading 1, 2, 3 |
| 4 | To-do list |
| 5 | Bulleted list |
| 6 | Numbered list |
| 7 | Toggle list |
| 8 | Code |
| 9 | Page |

## Blocks

These act on the chosen blocks (Esc chooses the block the caret is in).

| Action | Mac | Windows and Linux |
| --- | --- | --- |
| **Open block actions** | Cmd+/ | Ctrl+/ |
| **Move block to different page** | Cmd+Shift+P | Ctrl+Shift+P |
| **Copy link to block** | Cmd+Control+L | — |
| **Duplicate selected blocks** | Cmd+D | Ctrl+D |
| **Duplicate without content** | Cmd+Shift+D | Ctrl+Shift+D |
| **Delete selected blocks** | Delete or Backspace | Delete or Backspace |
| **Move selected block up** | Cmd+Shift+Up | Ctrl+Shift+Up |
| **Move selected block down** | Cmd+Shift+Down | Ctrl+Shift+Down |
| **Caption** (an image or file) | Cmd+Option+M | Ctrl+Alt+M |
| **Image link** | Cmd+K | Ctrl+K |
| **Full screen** (an image) | Space | Space |
| **Unsync all** (a synced block) | Cmd+Shift+G | Ctrl+Shift+G |

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Shortcuts window lists | **Popular**, **Create & style**, **Edit & move** | Opens on **Popular**. |
| Copy link to block | Mac only | No Windows or Linux key. |

## For agents and the API

Shortcuts are the screen's own; an agent does the same work through MCP and
the API, as the chapters of each part describe.

## Differences from Notion

- Option+Delete at the end of a line, and Control+Option+Backspace at its
  start, join the two lines into one, as Delete does; Notion copies the other
  line's words and leaves that line too.
- Cmd+Z keeps working on a page after you visit another page and come back,
  while the app stays open.

## Related chapters

- [Editor blocks](editor-blocks.md)
- [The interface](interface.md)
- [Pages](pages.md)
