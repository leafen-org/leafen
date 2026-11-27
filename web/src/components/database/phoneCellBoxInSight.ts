import { phoneTools, watchPhoneTools } from "@/lib/phoneTools";
import { softKeyboardShown } from "@/lib/softKeyboard";

/** How far over the foot of what shows of the window a phone's value box stands at the lowest. */
const FOOT_GAP = 12;
const OPEN = ":focus, [data-field-held]";
const watching = new WeakMap<HTMLElement, () => void>();

/**
 * On a phone the box a table's value is written in stays inside what shows
 * of the window, 12 over its foot, as the Notion Android app's (measured
 * 2026-11-25 on a Galaxy A90 at 411, a 14-row inline table, the text cells
 * of rows 8 and 5 and the title of row 7 pressed in turn with the keyboard
 * down): the page stays where it rested and the box, its row under the
 * keyboard, stands up over the rows above it until its foot is 12 over the
 * keyboard (a one-line box at 474.7 to 525, the keyboard from 536.8),
 * growing up from there as its words wrap (three lines: 421.7 to 524.9); the
 * keyboard put away, the box stands over its cell again, the page still
 * where it was. The app's strip is drawn by the app over its web view and
 * covers the box's foot there; Leafen's strip is part of the page, so the box
 * stands 12 over it, its words in sight (an approved difference). Where the
 * browser brings the box into sight as the keyboard comes (Android Chrome
 * scrolls the page to set it in the middle of the shortened window), the page
 * is put back where it rested. Called whenever the box opens (fitCellEditor);
 * it follows the keyboard, the strip, the page's scroll and the box's size
 * while the box is open, reading where it stands once a frame is laid out,
 * never at the press.
 */
export function keepPhoneCellBoxInSight(editor: HTMLElement) {
  const again = watching.get(editor);
  if (again) { again(); return; }
  if (typeof ResizeObserver === "undefined") return;
  const page = editor.closest<HTMLElement>("[data-page-scroll]");
  let rest: number | null = null, up = softKeyboardShown(), frame = 0;
  const lift = (by: number) => {
    const was = parseFloat(editor.style.getPropertyValue("--cell-editor-lift")) || 0;
    if (Math.abs(by - was) < 0.5) return;
    if (by > 0) editor.style.setProperty("--cell-editor-lift", `${by}px`);
    else editor.style.removeProperty("--cell-editor-lift");
  };
  const fit = () => {
    if (!editor.isConnected || !editor.querySelector(OPEN)) { stop(); return; }
    const shown = softKeyboardShown();
    if (!shown && page) rest = page.scrollTop;
    // The keyboard comes: the page goes back where it rested, the box rising instead.
    else if (shown && !up && page && rest !== null && Math.abs(page.scrollTop - rest) >= 0.5) page.scrollTop = rest;
    up = shown;
    const was = parseFloat(editor.style.getPropertyValue("--cell-editor-lift")) || 0;
    const viewport = window.visualViewport;
    const foot = phoneTools()?.top ?? (viewport ? viewport.offsetTop + viewport.height : window.innerHeight);
    lift(Math.max(0, editor.getBoundingClientRect().bottom + was - (foot - FOOT_GAP)));
  };
  // Its size is reported once the frame is laid out: at the first look, as it grows, and when asked again.
  const sized = new ResizeObserver(fit);
  const look = () => { sized.unobserve(editor); sized.observe(editor); };
  const later = () => { cancelAnimationFrame(frame); frame = requestAnimationFrame(look); };
  const stopTools = watchPhoneTools(fit);
  const viewport = window.visualViewport;
  viewport?.addEventListener("resize", fit);
  viewport?.addEventListener("scroll", fit);
  window.addEventListener("resize", fit);
  page?.addEventListener("scroll", fit, { passive: true });
  editor.addEventListener("focusout", later);
  function stop() {
    cancelAnimationFrame(frame);
    watching.delete(editor);
    sized.disconnect();
    stopTools();
    viewport?.removeEventListener("resize", fit);
    viewport?.removeEventListener("scroll", fit);
    window.removeEventListener("resize", fit);
    page?.removeEventListener("scroll", fit);
    editor.removeEventListener("focusout", later);
    lift(0);
  }
  watching.set(editor, look);
  look();
}
