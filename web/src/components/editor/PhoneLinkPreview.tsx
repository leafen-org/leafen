import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { Editor } from "@tiptap/core";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { Copy, GlobeIcon } from "@/icons/leafen";
import { PageIcon } from "@/components/page/PageIcon";
import { useProductPages } from "@/components/product/productPagesContext";
import { copyText } from "@/lib/copyText";
import { pressedByFinger } from "@/lib/lastPress";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePageNames } from "@/lib/pageNames";
import { linkCopyAddress } from "./linkCopyAddress";
import { linkedPage } from "./linkOpening";
import styles from "./editorStyles.generated.module.css";
import phoneStyles from "./PhoneLinkPreview.module.css";

/** How long after a finger's long press the words it chooses are its own. */
const CHOOSING_MS = 1000;

/**
 * Calls `chosen` each time a finger's long press chooses words in `editor`'s
 * field while it is being written, and `left` as the words chosen change
 * after: Android Chrome hands a long press over as the menu event, its word
 * chosen on either side of it (seen 2026-11-25 on a Galaxy A90: focusin,
 * contextmenu, then the selection). Words chosen any other way (a drag of
 * the handles, a sheet choosing them again as it closes) are not a long
 * press's, nor are words a long press chose with the field not being written,
 * the keyboard down: the app then shows no bar under a link's words, only the
 * words' tools on the keyboard that comes (seen twice; textLongPress.ts).
 */
export function useLongPressChoice(editor: Editor, chosen: () => void, left: () => void) {
  const latest = useRef({ chosen, left });
  latest.current = { chosen, left };
  useEffect(() => {
    const field = editor.view.dom as HTMLElement;
    // The words it shows for while shown, which the same words chosen again keep.
    let until = 0, shown: { from: number; to: number } | null = null;
    // Whether the field was being written as the finger came down.
    let writing = false;
    const touched = (event: PointerEvent) => { if (event.pointerType === "touch") writing = !editor.isDestroyed && editor.view.hasFocus(); };
    const look = () => {
      const { from, to, empty } = editor.state.selection;
      if (editor.isDestroyed || empty) return false;
      shown = { from, to };
      latest.current.chosen();
      return true;
    };
    const leave = () => { if (shown) { shown = null; latest.current.left(); } };
    // The words chosen as the menu event comes, or as they come after it.
    const pressed = () => {
      if (!pressedByFinger() || !writing) return;
      until = performance.now() + CHOOSING_MS;
      look();
    };
    const changed = ({ transaction }: { transaction: { selectionSet: boolean; docChanged: boolean } }) => {
      if (!transaction.selectionSet && !transaction.docChanged) return;
      if (!transaction.docChanged && performance.now() < until && look()) return;
      const { from, to } = editor.state.selection;
      if (transaction.docChanged || from !== shown?.from || to !== shown?.to) leave();
    };
    const blurred = () => { until = 0; leave(); };
    field.addEventListener("pointerdown", touched, true);
    field.addEventListener("contextmenu", pressed, true);
    editor.on("transaction", changed); editor.on("blur", blurred);
    return () => {
      field.removeEventListener("pointerdown", touched, true);
      field.removeEventListener("contextmenu", pressed, true);
      editor.off("transaction", changed); editor.off("blur", blurred);
    };
  }, [editor]);
}

/**
 * The bar a link's words chosen by a finger's long press show under them on
 * a touch screen, as the Notion Android app's (seen 2026-11-25 on a Galaxy
 * A90 at 411, a page's line and a table text cell's box): the desktop pill's
 * look (NativeTiptapLinkPill) — a globe, or a page's icon and name, the
 * address, a copy button and "편집" — 32 high, from the words' start under
 * them and kept 12 inside the window's end, its words 14px. A press on it
 * keeps the field's focus and keyboard; "편집" opens the link's edit
 * (PhoneLinkSheet).
 */
export function PhoneLinkPreview({ href, at, field, readOnly, onEdit }: {
  href: string;
  /** Whose words: a page's text, whose strip gives way to it, or a table cell's, whose strip stays (NativePhoneToolbar.module.css). */
  field: "page" | "cell";
  /** Where the words are drawn. */
  at: { left: number; bottom: number };
  readOnly?: boolean;
  onEdit(): void;
}) {
  const { t } = useTranslation("selectionToolbar");
  const pageNames = usePageNames();
  const page = linkedPage(href, useProductPages()?.pages);
  const ref = useRef<HTMLDivElement>(null);
  const [left, setLeft] = useState(at.left);
  const { zIndex } = useOverlayLayer(true, { kind: "tooltip", ref });
  useLayoutEffect(() => {
    const width = ref.current?.offsetWidth ?? 0;
    setLeft(Math.max(12, Math.min(at.left, document.documentElement.clientWidth - 12 - width)));
  }, [at.left, href]);
  const keep = (event: { preventDefault(): void }) => event.preventDefault();
  return createPortal(
    <div ref={ref} className={`${styles.linkPill} ${phoneStyles.preview}`} contentEditable={false} data-phone-link-preview={field}
      style={{ left, top: at.bottom + 6, zIndex }} onPointerDown={keep} onMouseDown={keep}>
      {page ? <PageIcon page={page} size={15} className={styles.linkPillPageIcon} />
        : <GlobeIcon size="tiny" className={styles.linkPillGlobe} aria-hidden="true" />}
      <span className={styles.linkPillAddress}>{page ? pageNames.title(page) : href}</span>
      <button type="button" className={styles.linkPillButton} aria-label={t("linkEditor.copy")} title={t("linkEditor.copy")}
        onClick={() => void copyText(linkCopyAddress(href))}><Copy size="small" aria-hidden="true" /></button>
      {readOnly ? null : <button type="button" className={styles.linkPillButton} onClick={onEdit}>{t("linkEditor.edit")}</button>}
    </div>, document.body);
}
