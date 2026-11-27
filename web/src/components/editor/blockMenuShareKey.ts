import { useContext, useEffect, useRef } from "react";
import type { OverlayKind } from "@/lib/overlayKinds";
import { NativeTiptapReactContext } from "./nativeTiptapReactContext";

const shareKeyOpened = "leafen:share-key-opened";

/**
 * ⌘⇧O with a block's menu open, as Notion's (2026-11-26, Chrome 1440, judged
 * by the screen and one letter after the keys with nothing run in the page
 * before them, in two tabs at two times): the menu its handle's press opened
 * closes, its blocks unchosen, as the share opens over the page; the one ⌘/
 * opened keeps the key, nothing opening and the menu staying. Off for a
 * rehearsal or a phone's sheet.
 */
export function useBlockMenuShareKey(on: boolean, close: () => void) {
  const editor = useContext(NativeTiptapReactContext);
  const latest = useRef({ editor, close });
  latest.current = { editor, close };
  useEffect(() => {
    if (!on) return undefined;
    const onOpened = () => {
      latest.current.editor?.setSelection(new Set());
      latest.current.close();
    };
    window.addEventListener(shareKeyOpened, onOpened);
    return () => window.removeEventListener(shareKeyOpened, onOpened);
  }, [on]);
}

/**
 * Whether a block's menu the keys opened keeps ⌘⇧O, the share opening not
 * (TopBarShareFace): its handle reads `data-menu-open="command"` (BlockHandle).
 */
export function blockMenuKeepsShareKey() {
  return document.querySelector('[data-menu-open="command"]') !== null;
}

/**
 * Told by the share key as it opens the share, with the kind of the window or menu on top as it was put down
 * (topLayerKind): a block's menu its handle's press opened closes, and the text selection's toolbar lets its words go
 * (useShareKeyOpened).
 */
export function shareKeyOpensShare(over?: OverlayKind) {
  window.dispatchEvent(new CustomEvent(shareKeyOpened, { detail: { over } }));
}

/** Hears the share key open the share, with the kind of the window or menu on top as it was put down. */
export function useShareKeyOpened(on: boolean, opened: (over: OverlayKind | undefined) => void) {
  const latest = useRef(opened);
  latest.current = opened;
  useEffect(() => {
    if (!on) return undefined;
    const onOpened = (event: Event) => latest.current((event as CustomEvent<{ over?: OverlayKind }>).detail?.over);
    window.addEventListener(shareKeyOpened, onOpened);
    return () => window.removeEventListener(shareKeyOpened, onOpened);
  }, [on]);
}
