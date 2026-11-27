import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { softKeyboardShown } from "@/lib/softKeyboard";
import { closeStripPanel, holdStripPanel, keyboardBackFromStripPanel, useStripPanel, type StripPanelKind } from "./stripPanelState";
import editorStyles from "./editorStyles.generated.module.css";
import styles from "./PhoneStripPanel.module.css";

/** A card of the list: its mark, its name and what it does. */
export type StripCard = { key: string; icon: ReactNode; label: ReactNode; run(): void; data?: Record<string, string | undefined> };
/** A group of cards under its name. */
export type StripSection = { label?: string; cards: StripCard[] };

/**
 * A list a phone strip's tool opens in the keyboard's place (stripPanelState.ts),
 * as the Notion Android app's "기본 블록", "전환" and "텍스트 색상" (measured
 * 2026-11-21 on the A90 at 411): its groups' cards two across. The press of a
 * card keeps the page's focus, as the strip's tools do; the card acts and
 * brings the keyboard back. A tap in the page closes it, the keyboard coming
 * back with the caret where the tap put it; a tap anywhere else, or the
 * page's focus going, closes it with the keyboard down. Its owner (`kind`'s
 * menu) is closed by ⓧ or another tool's list through `onClose`.
 */
export function PhoneStripPanel({ kind, label, sections, onClose }: {
  kind: StripPanelKind; label: string; sections: StripSection[]; onClose(): void;
}) {
  const panel = useStripPanel();
  const ref = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => holdStripPanel(kind, () => close.current()), [kind]);
  // While the keyboard is still going down, the list waits under it.
  const [, redraw] = useState(0);
  useEffect(() => {
    const moved = () => redraw(value => value + 1);
    window.visualViewport?.addEventListener("resize", moved);
    window.addEventListener("resize", moved);
    return () => { window.visualViewport?.removeEventListener("resize", moved); window.removeEventListener("resize", moved); };
  }, []);
  const mine = panel?.kind === kind ? panel : null;
  useEffect(() => {
    if (!mine) return;
    const field = mine.field;
    function tapped(event: MouseEvent) {
      const target = event.target instanceof Node ? event.target : null;
      if (!target || ref.current?.contains(target) || target instanceof Element && target.closest("[data-native-phone-toolbar]")) return;
      closeStripPanel(field.contains(target));
    }
    function left() {
      requestAnimationFrame(() => { if (!field.contains(document.activeElement)) closeStripPanel(false); });
    }
    document.addEventListener("click", tapped, true);
    field.addEventListener("focusout", left);
    return () => { document.removeEventListener("click", tapped, true); field.removeEventListener("focusout", left); };
  }, [mine]);
  if (!mine) return null;
  return createPortal(<div ref={ref} className={styles.panel} style={{ height: mine.height, visibility: softKeyboardShown() ? "hidden" : undefined }}
    data-phone-strip-panel={kind} role="menu" aria-label={label}
    onPointerDown={event => event.preventDefault()} onMouseDown={event => event.preventDefault()}>
    {sections.map((section, index) => <div key={section.label ?? index} role="group" aria-label={section.label}>
      {section.label ? <div className={styles.head} aria-hidden="true">{section.label}</div> : null}
      <div className={styles.cards}>
        {section.cards.map(card => <button type="button" key={card.key} tabIndex={-1} role="menuitem" className={styles.card} {...card.data}
          onClick={() => { keyboardBackFromStripPanel(); card.run(); }}>
          <span className={styles.mark} aria-hidden="true">{card.icon}</span>
          <span className={styles.name}>{card.label}</span>
        </button>)}
      </div>
    </div>)}
  </div>, document.body);
}

/** A color card's mark: "A" in a text color, a background's fill (none for the default), in its 22 ringed box. */
export function StripColorMark({ color }: { color: string }) {
  const background = color.endsWith("_background");
  return <span className={`${editorStyles.colorSwatch} ${styles.swatch}`} data-color={color}
    style={background && color !== "default_background" ? { color: `var(--c-${color.replace(/_background$/u, "")})` } : undefined}>
    {background ? null : "A"}</span>;
}
