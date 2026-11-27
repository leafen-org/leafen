import { useEffect, type RefObject } from "react";
import responsiveStyles from "@/components/WorkspaceSettingsResponsive.module.css";

/**
 * Notion's settings row holds its words and its control's side (flex 3 and
 * 1, 12px apart) on one line while both fit and puts the control's side under
 * the words when they don't, where the words take the whole line. Leafen's
 * rows hold the control itself, so the words' three-quarter share is a cap,
 * and it comes off once the control has gone under them: each row whose
 * control sits below its words is marked `data-row-wrapped` (2026-11-07: in a
 * 606px window "이 기기에서 … 선택하세요." broke at 163px of a 230px line,
 * where Notion's is one line).
 */
export function useSettingsRowWrap(panel: RefObject<HTMLElement | null>, section: string) {
  useEffect(() => {
    const content = panel.current?.querySelector<HTMLElement>("[data-settings-content]");
    if (!content) return;
    let frame = 0;
    const mark = () => {
      frame = 0;
      for (const row of content.querySelectorAll<HTMLElement>(`.${responsiveStyles.securityRow}`)) {
        const words = row.firstElementChild;
        const control = row.lastElementChild;
        const wrapped = words !== control && words instanceof HTMLElement && control instanceof HTMLElement
          && control.getBoundingClientRect().top >= words.getBoundingClientRect().bottom - 1;
        if (row.hasAttribute("data-row-wrapped") !== wrapped) row.toggleAttribute("data-row-wrapped", wrapped);
      }
    };
    const later = () => {
      if (!frame) frame = requestAnimationFrame(mark);
    };
    // The section just drawn is read once the frame drawing it has laid it
    // out, before it paints: read as the draw ended, it made the browser work
    // out the section's styles and layout there and then (2026-11-14). The
    // window's width and the rows a section draws later both move them; the
    // watch on the width starts a frame after that, so the marks' own change
    // isn't a size seen again in the frame that made it.
    const first = new ResizeObserver(() => {
      first.disconnect();
      mark();
    });
    first.observe(content);
    const resize = new ResizeObserver(later);
    let watching = requestAnimationFrame(() => { watching = requestAnimationFrame(() => resize.observe(content)); });
    const drawn = new MutationObserver(later);
    drawn.observe(content, { childList: true, subtree: true });
    return () => {
      first.disconnect();
      resize.disconnect();
      drawn.disconnect();
      cancelAnimationFrame(watching);
      if (frame) cancelAnimationFrame(frame);
    };
  }, [panel, section]);
}
