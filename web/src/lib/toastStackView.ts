export interface ToastAction {
  /** An × after the action: by default only on a toast that stays until it
   * is answered, as Notion's passing action toasts have none (seen 2026-10-31). */
  dismissible?: boolean;
  /** How long it stays: an undo or another passing action goes after a while
   * (8s when not given, held while pointed at or focused); null keeps a
   * required decision until it is answered or closed. */
  autoDismissMs?: number | null;
  /** A mark before its label, as Notion's ⧉ 열기 and ↶ 실행 취소 (seen 2026-11-05) —
   * an action in the toast's own words for them has it unasked (ToastStack);
   * "none" keeps an undo bare where Notion's has no arrow, as its notice of a
   * page's notifications changing (seen 2026-11-06). */
  icon?: "open" | "undo" | "none";
  label: string;
  onClick: () => void | Promise<void>;
}

export interface ToastMessage {
  action?: ToastAction;
  /** Actions before it, as Notion's "이동" before "실행 취소" (seen 2026-10-31). */
  leadingActions?: readonly ToastAction[];
  /** A mark before the message, as Notion's lock on a link only you can open,
   *  or a turning ring while something goes on (a page being duplicated);
   *  "none" leaves a corner notice without its check, as Notion's question
   *  whether to invite someone mentioned (seen 2026-11-07). */
  icon?: "lock" | "progress" | "none";
  /** A person a corner notice is about, their face and name under its title (seen 2026-11-07). */
  person?: { userId: string; name: string };
  id: string;
  message: string;
  /** A line under the message, as Notion's "스킬 데이터베이스로 전환됨" explains itself (seen 2026-10-11). */
  detail?: string;
  tone?: "default" | "success" | "error";
}

export interface LiveAnnouncement {
  id: string;
  message: string;
}

export interface ToastStackSyncClearanceInput {
  badgeLeft: number;
  badgeRight: number;
  badgeTop: number;
  stackLeft: number;
  stackRight: number;
  viewportHeight: number;
}

const TOAST_ORDINARY_BOTTOM = 28;
const TOAST_SYNC_GAP = 8;

export function toastStackSyncClearance({
  badgeLeft,
  badgeRight,
  badgeTop,
  stackLeft,
  stackRight,
  viewportHeight,
}: ToastStackSyncClearanceInput): number | null {
  const inlineSeparated = stackRight + TOAST_SYNC_GAP <= badgeLeft
    || badgeRight + TOAST_SYNC_GAP <= stackLeft;
  if (inlineSeparated) return null;
  return Math.max(
    TOAST_ORDINARY_BOTTOM,
    Math.ceil(viewportHeight - badgeTop + TOAST_SYNC_GAP),
  );
}

export async function invokeToastAction(
  action: ToastAction["onClick"],
  reportFailure: (error: unknown) => void,
): Promise<void> {
  try {
    await action();
  } catch (error) {
    reportFailure(error);
  }
}

const TOAST_PLACEMENT_EVENT = "leafen:toast-placement";

/**
 * The sync status badge, which the toasts keep clear of, as it comes and
 * goes (its element's ref): a toast shown is placed again. Its layer is the
 * app's, which the toasts don't watch (ToastStack).
 */
export function toastPlacementTarget(_element: Element | null) {
  if (typeof window !== "undefined") window.dispatchEvent(new Event(TOAST_PLACEMENT_EVENT));
}

/** Told when what the toasts keep clear of comes or goes (toastPlacementTarget); returns the stop. */
export function onToastPlacementChange(listener: () => void) {
  window.addEventListener(TOAST_PLACEMENT_EVENT, listener);
  return () => window.removeEventListener(TOAST_PLACEMENT_EVENT, listener);
}
