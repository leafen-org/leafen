import { useLayoutEffect, useRef, type HTMLAttributes } from "react";
import styles from "./AdministratorSettingsPage.module.css";

/**
 * The settings' save bar, drawn when there is something to say or save. It
 * sticks to the foot of the settings' scroll, under what brought it up, and
 * the scroll keeps its height as room at the foot: a field the focus goes to
 * stays above it, as does the field being changed as it comes up; one that
 * comes up or grows at the end of what is seen — a refusal's red line and
 * its button under the last field — takes its own place under that field
 * rather than covering it (2026-11-21, iPhone: the backups' "보관할 백업 수"
 * went under the bar as it said a recovery code was needed). Further from
 * its place, as while the person reads up the section, it stays stuck.
 */
export function SettingsSaveBar({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  const bar = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const element = bar.current;
    const scroller = element ? scrollerOf(element) : null;
    if (!element || !scroller) return undefined;
    const keepRoom = () => {
      const box = element.getBoundingClientRect();
      scroller.style.scrollPaddingBottom = `${Math.ceil(box.height)}px`;
      const active = document.activeElement;
      if (active instanceof HTMLElement && !element.contains(active) && scroller.contains(active)) {
        const field = active.getBoundingClientRect();
        if (field.bottom > box.top && field.top < box.bottom) {
          active.scrollIntoView({ block: "nearest" });
          return;
        }
      }
      const before = element.previousElementSibling;
      if (!before) return;
      const place = before.getBoundingClientRect().bottom + (Number.parseFloat(getComputedStyle(element).marginTop) || 0);
      const covered = place - box.top;
      if (covered > 1 && covered <= box.height) scroller.scrollBy({ top: covered });
    };
    keepRoom();
    const observer = new ResizeObserver(keepRoom);
    observer.observe(element);
    return () => {
      observer.disconnect();
      scroller.style.scrollPaddingBottom = "";
    };
  }, []);
  return <div ref={bar} className={className ? `${styles.saveBar} ${className}` : styles.saveBar} {...props} />;
}

/** The nearest box around the bar that scrolls. */
function scrollerOf(element: HTMLElement): HTMLElement | null {
  for (let up = element.parentElement; up; up = up.parentElement) {
    const overflow = getComputedStyle(up).overflowY;
    if (overflow === "auto" || overflow === "scroll" || overflow === "overlay") return up;
  }
  return null;
}
