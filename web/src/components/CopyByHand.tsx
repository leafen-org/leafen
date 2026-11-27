"use client";

import { useEffect, useRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { i18next } from "@/i18n";
import { useOverlayLayer } from "@/lib/overlayLayer";
import styles from "./CopyByHand.module.css";

let host: HTMLDivElement | null = null;
let root: Root | null = null;
let opened = 0;

/**
 * Text the clipboard didn't take (copyText.ts), as Notion shows it where its
 * clipboard is refused (seen 2026-11-01 at 1280×900 and 1000×700, copying a
 * page's link and a code block alike): a 300px card across the window's
 * middle, its foot at the window's half height, with the text selected in a
 * three-line field and "마우스 오른쪽 버튼을 클릭하여 위의 링크 복사" under it.
 * Escape or a press outside closes it, and focus goes back to the opener.
 */
export function showTextToCopy(text: string, opener: HTMLElement | null = null) {
  if (typeof document === "undefined") return;
  if (!host || !host.isConnected) {
    host = document.createElement("div");
    host.dataset.copyByHandHost = "";
    // Drawn over everything, as a confirmation is: nothing that hides the
    // page outside a window keeps it from being pressed.
    host.dataset.reactAriaTopLayer = "";
    document.body.append(host);
    root = createRoot(host);
  }
  opened += 1;
  root?.render(<CopyByHand key={opened} text={text} opener={opener} onClose={() => root?.render(null)} />);
}

function CopyByHand({ text, opener, onClose }: { text: string; opener: HTMLElement | null; onClose(): void }) {
  const cardRef = useRef<HTMLDivElement>(null);
  const openerRef = useRef(opener);
  const fieldRef = useRef<HTMLTextAreaElement>(null);
  const hint = i18next.t("common:copyByHand.hint");
  const { zIndex } = useOverlayLayer(true, {
    kind: "picker", ref: cardRef, returnFocusRef: openerRef, initialFocus: fieldRef, onEscape: onClose, onOutsidePress: onClose,
  });
  // The text comes selected: copying is one key away.
  useEffect(() => { fieldRef.current?.select(); }, [text]);
  return (
    <div ref={cardRef} role="dialog" aria-label={hint} className={styles.card} style={{ zIndex }} data-copy-by-hand-card>
      <div className={styles.field}>
        <textarea ref={fieldRef} readOnly value={text} rows={3} aria-label={hint}
          onFocus={(event) => event.currentTarget.select()} data-copy-by-hand />
      </div>
      <div className={styles.hint}>{hint}</div>
    </div>
  );
}
