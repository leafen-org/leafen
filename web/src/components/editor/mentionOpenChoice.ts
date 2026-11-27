import { placeInScrollBox } from "@/lib/scrollBoxPlace";
import { blockItemLabels } from "./blockItemLabels";
import type { MentionItem } from "./blockPickerModel";
import { mentionGroupLabel, type MorePagesRow } from "./mentionMenuItems";
import styles from "./editorStyles.generated.module.css";

// The row an "@" menu opens on, and how it is shown (useCaretPicker's
// `opening`), apart from the menu.

/**
 * An "@" just typed in a page's line or title, a database's text cell or a
 * comment opens on the first row past its dates, as Notion's (seen 2026-11-24
 * in Chrome, other tabs and loads apart): its people's first, "초대" where it
 * is the owner's and no one else is listed, the first page for a guest, who
 * is offered no one — in a page's lines, a database's text cell and a
 * comment's alike. Notion draws its dates a moment after the rest and leaves
 * the row it drew first lit, so the owner's first "@" after a page loads
 * often had "초대" lit under the person who came later (five times of six),
 * and a member's the first page under the owner; this menu lists the people
 * it knows as it opens (the workspace's, those this tab found —
 * lib/mentionPeopleSeen.ts), the row past the dates following them while
 * nothing moved it. With nothing past the dates it opens on its first row.
 * A letter typed lights the first row again, the dates too, and so does
 * taking it back to the "@" alone ("@tmr" lit 내일, back to "@" 오늘). A
 * to-do's "@", whose dates set its due date, and an automation's keep their
 * first row (not seen in Notion).
 */
export function mentionOpenKey(items: readonly (MentionItem | MorePagesRow)[]) {
  const dates = blockItemLabels().groupDate;
  return items.find((item) => mentionGroupLabel(item) !== dates)?.id;
}

/**
 * Shows the row the menu opened on past its dates as Notion's list does (seen
 * 2026-11-24 in a guest's "@", windows 772 and 724 high): scrolled down as
 * little as it takes for the row's whole group to show and, under it, the next
 * group's name, its dates going up out of view — a comment's, with no group
 * after its pages, to its end; never so far that the group's own head leaves
 * the top. A group that already shows so stays where it is (the people's).
 * Measured in the list's own pixels as the menu grows into place
 * (placeInScrollBox).
 */
export function showMentionOpenRow(row: HTMLElement) {
  const group = row.parentElement, list = group?.parentElement;
  if (!group || !list) return;
  const next = group.nextElementSibling;
  const nextName = next?.classList.contains(styles.mentionSection) ? next.firstElementChild : null;
  const top = list.clientTop, foot = top + list.clientHeight, shown = placeInScrollBox(group, list);
  // With nothing after its group, the list's own end.
  const end = nextName ? placeInScrollBox(nextName, list).bottom
    : next ? shown.bottom : foot + list.scrollHeight - list.clientHeight - list.scrollTop;
  const by = Math.min(end - foot, shown.top - top);
  if (by > 0) list.scrollTop += by;
}
