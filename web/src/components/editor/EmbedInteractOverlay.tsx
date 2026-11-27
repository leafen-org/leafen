import { useState } from "react";
import { HandTapIcon } from "@/icons/leafen";
import { blockItemText } from "./blockItemLabels";
import styles from "./EmbedInteractOverlay.module.css";

/**
 * A document embed's "상호 작용", as Notion's (seen 2026-11-12: a CSV, an
 * Excel, a Word and a PowerPoint file's embed; a link's or a service's embed
 * has none). Until it is used the document takes no press or wheel, so the
 * page scrolls over it; pointed at, the document dims and a dark "상호 작용"
 * pill shows in its middle. The first press, or Enter on it, only lets the
 * document be used — nothing in it is chosen — and it stays so while the page
 * is open: Escape, a press beside it, scrolling or using another one doesn't
 * put it back. Reached by Tab, it shows, and Enter or Space presses it.
 *
 * The frame gets `frameClassName`, the document `contentClassName`, and
 * `overlay` goes in the frame beside the document.
 */
export function useEmbedInteract(enabled: boolean) {
  const [interacting, setInteracting] = useState(false);
  const gated = enabled && !interacting;
  return {
    frameClassName: gated ? styles.gate : "",
    contentClassName: gated ? styles.idle : "",
    overlay: gated ? <button type="button" className={styles.overlay} data-embed-interact
      onClick={() => setInteracting(true)}>
      <span className={styles.pill}>
        <HandTapIcon size="regular" aria-hidden="true" />
        <span>{blockItemText("embed.interact")}</span>
      </span>
    </button> : null,
  };
}
