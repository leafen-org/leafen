import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphBlock } from "@/contentGraph";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { ImageAltTextWindow } from "./ImageAltText";
import css from "./ImageCaptionAlt.module.css";

/** Whether a picture's caption line offers "ALT": a picture with no alt text, to its writer. */
export function captionOffersAlt(block: GraphBlock | null | undefined, adapter: { readOnly?: boolean }, suggesting: boolean) {
  return block?.type === "image" && !adapter.readOnly && !suggesting && !String(block.content.altText ?? block.content.alt ?? "");
}

/**
 * "ALT" at the end of a picture's caption line while the picture has no alt
 * text, as Notion's "대체 텍스트 추가" (seen 2026-11-14 on a picture that did
 * not load, 2026-11-15 on an uploaded one, two tabs): a 12px, weight 500 word
 * 6px in, 26 high, on the tertiary tint, round 6, 3.5px under the line's top
 * at the picture's end; there while writing an empty caption, and with one
 * written, whatever the pointer does. A press opens the alt text window; once
 * the picture has alt text, the picture's own "ALT" stands at its foot instead.
 * Pressed under an empty caption, the window opens at "ALT" (Notion's, its
 * caption gone, opened at the window's corner) and the caption closes after it.
 */
export function ImageCaptionAlt({ block, adapter, onOpenChange }: {
  block: GraphBlock; adapter: NativeEditorGraphAdapter; onOpenChange?: (open: boolean) => void;
}) {
  const { t } = useTranslation("blockHandle");
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpenState] = useState(false);
  const setOpen = (next: boolean) => { setOpenState(next); onOpenChange?.(next); };
  return <span className={css.place} contentEditable={false} data-caption-alt>
    <button ref={button} type="button" className={css.button} aria-label={t("altText.add")}
      onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }}
      onClick={(event) => { event.stopPropagation(); setOpen(true); }}>{t("altText.addShort")}</button>
    {open && button.current ? <ImageAltTextWindow anchor={{ badge: button.current }} alt=""
      onClose={() => setOpen(false)} onSave={(text) => {
        const live = adapter.graph.blocks.find((candidate) => candidate.id === block.id);
        if (live && text.trim()) adapter.updateBlock(live, { altText: text.trim() });
      }} /> : null}
  </span>;
}
