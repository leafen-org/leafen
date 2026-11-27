import { useSyncExternalStore, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { usePathname } from "@/lib/router";
import styles from "./PhoneFoot.module.css";

/**
 * A phone's foot, as the Notion Android app's on its inbox and its pages
 * (measured 2026-11-21 on the A90 at 411, light): two round 48 buttons, search
 * and a new page, 16 from each side and 4 over the window's foot; the app's
 * "AI에게 질문하기" bar between them is left out. The inbox holds it
 * over its list's end (`inbox`); a page has it over the page (`page`).
 */
export function PhoneFoot({ place, children }: { place: "inbox" | "page"; children: ReactNode }) {
  return <div className={styles.foot} data-phone-foot={place} data-inbox-phone-foot={place === "inbox" ? "" : undefined}>{children}</div>;
}

/**
 * Whether a field takes typing now: a page's text, a title, an input; not a
 * table cell whose keyboard its strip put away, its box kept open
 * (data-keyboard-away, CellPhoneStrip), as the app's foot came back then.
 */
function editingNow() {
  const active = typeof document === "undefined" ? null : document.activeElement;
  return active instanceof HTMLElement && (active.isContentEditable || active.matches("input, textarea, select"))
    && !active.hasAttribute("data-keyboard-away");
}
// The keyboard going or coming resizes what shows: a field's keyboard put away is heard so.
function watchFocus(changed: () => void) {
  document.addEventListener("focusin", changed);
  document.addEventListener("focusout", changed);
  window.visualViewport?.addEventListener("resize", changed);
  return () => {
    document.removeEventListener("focusin", changed); document.removeEventListener("focusout", changed);
    window.visualViewport?.removeEventListener("resize", changed);
  };
}

/**
 * A page's foot on a phone, while nothing is being typed: the app's goes as a
 * line is edited, its caret tools standing on the keyboard instead, and
 * comes back as the keyboard goes (keyboardDownEndsEditing.ts), as w2270 saw
 * (2026-11-21, twice). Over the page, not in it: it scrolls with nothing.
 */
export function PhonePageFoot({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const editing = useSyncExternalStore(watchFocus, editingNow, () => false);
  if (!pathname.startsWith("/p/") || editing) return null;
  return createPortal(<PhoneFoot place="page">{children}</PhoneFoot>, document.body);
}
