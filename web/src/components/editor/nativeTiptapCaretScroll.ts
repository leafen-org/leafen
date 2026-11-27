import { Extension } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";

type ScrollingView = EditorView & { scrollToSelection(): void };

/**
 * A change that scrolls the caret into view (a key, a paste, a command) has
 * it scrolled in the next frame, before that frame is drawn, rather than in
 * the change itself: measuring the caret there laid the page out while the
 * change's new lines still waited for their frames, which React draws a
 * moment later, and the frame laid it out again (2026-11-14: about 10 ms of
 * an Enter's 41 in a 600-block page at CPU ×2). Measured first in the frame,
 * the caret's place costs the layout the frame does anyway, once, however
 * many changes came before it. The frame shows the caret scrolled as before:
 * the editor's distances from its scroller's edges (scrollThreshold,
 * scrollMargin) are the engine's own, kept as they are.
 */
export const NativeTiptapCaretScroll = Extension.create({
  name: "nativeTiptapCaretScroll",
  addProseMirrorPlugins() {
    let frame = 0, scrolling = false;
    return [new Plugin({
      view: () => ({ destroy: () => { if (frame) cancelAnimationFrame(frame); frame = 0; } }),
      props: {
        handleScrollToSelection: (view) => {
          if (scrolling) return false;
          if (!frame) frame = requestAnimationFrame(() => {
            frame = 0;
            if (view.isDestroyed) return;
            scrolling = true;
            try { (view as ScrollingView).scrollToSelection(); } finally { scrolling = false; }
          });
          return true;
        },
      },
    })];
  },
});
