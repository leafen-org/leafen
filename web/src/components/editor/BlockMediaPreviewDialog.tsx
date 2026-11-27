import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { phoneScreen } from "@/lib/screenKind";
import type { GraphBlock } from "@/contentGraph";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { MediaBlockContent } from "./MediaBlockContent";
import styles from "./BlockMediaPreviewDialog.module.css";

// Room Notion leaves around a full screen embed: 32px from each side of the
// window, and none on a phone's screen (seen 2026-11-17 in iPhone Safari at
// 402: a 366 × 334.6 embed shown 402 × 367.5 in the screen's middle).
const EDGE = 32;

/** The embed's box on the page, as wide and high as it is drawn there; 16:9 when it can't be found. */
function drawnBox(blockId: string) {
  const frame = document.querySelector(`[data-block-id="${CSS.escape(blockId)}"] [data-embed-frame]`);
  const rect = frame?.getBoundingClientRect();
  return rect && rect.width > 0 && rect.height > 0 ? { width: rect.width, height: rect.height } : { width: 16, height: 9 };
}

/** The drawn box grown or shrunk to the window less its edges (Notion's, 2026-11-06: 704×331 → 1216×572, 704×900 → 633×809). */
function fittedBox(box: { width: number; height: number }) {
  const edge = phoneScreen() ? 0 : EDGE;
  const scale = Math.min((window.innerWidth - 2 * edge) / box.width, (window.innerHeight - 2 * edge) / box.height);
  return { width: Math.max(1, Math.round(box.width * scale)), height: Math.max(1, Math.round(box.height * scale)) };
}

/**
 * An embed in full screen as Notion's (seen 2026-11-06): the window darkened
 * and the embed alone in its middle, in the shape it has on the page and as
 * large as the window lets it with 32px around, with no head or buttons; a
 * press beside it or Escape closes it; a phone's, the screen's width with
 * nothing about it, no window (phoneWindowSheet.ts names it a viewer). It
 * reuses the actual media renderer, including HTML's ticketed opaque
 * sandbox, so full screen never opens active HTML under the product origin.
 */
export function BlockMediaPreviewDialog({ adapter, block, onClose }: {
  adapter: NativeEditorGraphAdapter; block: GraphBlock; onClose(): void;
}) {
  const { t } = useTranslation(["blockHandle"]);
  const root = useRef<HTMLDivElement>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: root, modalOwned: () => [root.current],
    restoreFocus: true, onEscape: onClose, onOutsidePress: onClose });
  const [box] = useState(() => drawnBox(block.id));
  const [size, setSize] = useState(() => fittedBox(box));
  useEffect(() => {
    root.current?.focus({ preventScroll: true });
    const follow = () => setSize(fittedBox(box));
    window.addEventListener("resize", follow);
    return () => window.removeEventListener("resize", follow);
  }, [box]);
  const name = String(block.content.name ?? block.content.title ?? t("blockHandle:media.fullScreen"));
  return createPortal(<div className={styles.backdrop} style={{ zIndex }}>
    <div ref={root} className={styles.frame} role="dialog" aria-modal="true" aria-label={name} tabIndex={-1}
      style={size} data-block-media-preview>
      <MediaBlockContent adapter={{ ...adapter, readOnly: true }} block={block} />
    </div>
  </div>, document.body);
}
