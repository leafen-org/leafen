import { ADD_TO_PEEK_PARAM } from "@/lib/pagePeek";
import { HOME_PATH } from "@/lib/router";

/**
 * A task opens in a center peek over the home, its database named in the
 * peek's bar to move it to another, as Notion's home widget opens it (seen
 * 2026-11-14: /<home>?p=<task>&pm=c, the home left under it, "옮기기 ✓ 작업
 * 트래커 ⌄" in the bar).
 */
export function taskHref(task: { rowId: string }) {
  return `${HOME_PATH}?${new URLSearchParams({ p: task.rowId, pm: "c", [ADD_TO_PEEK_PARAM]: "true" })}`;
}
