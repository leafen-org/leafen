import { createContext, type ReactNode, useContext, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { FocusScope } from "react-aria";
import { parentChain } from "./parentChain";

/** Runs a focus now, or once every popover layer holding it has joined Aria's focus scopes (PopoverLayer). */
type WhenLayerJoins = (focus: () => void) => void;
const LayerJoins = createContext<WhenLayerJoins>((focus) => focus());

/**
 * How a control drawn in a popover layer takes the focus as it opens, in the
 * same pass, before React's passive effects: at once outside a layer, and
 * inside one once the layer's focus scope has joined Aria's tree of them —
 * a scope joins after what it holds (Aria adds each as its layout effects
 * run, inner ones first), and the outermost one joining in a pass is put
 * under the scope the focus is in then: under a menu that had already taken
 * the focus inside it, the layer became that menu's parent and its child at
 * once, and the next scope to open walked that circle without end
 * (2026-11-18: a page's "⋯" › "테마와 레이아웃" › "인라인 댓글" pointed at
 * froze the tab). A layer inside a layer waits for the outer one too.
 */
export function useFocusWhenLayerJoins(): WhenLayerJoins {
  return useContext(LayerJoins);
}

// Where each popover drawn by PopoverLayer was opened from: an empty mark
// left in its place in the page, which owns the popover as before.
const homes = new WeakMap<Element, Element>();

/** The place in the page a node drawn by a PopoverLayer belongs to; null for any other node. */
export function popoverHome(node: Node): Element | null {
  const element = node instanceof Element ? node : node.parentElement;
  const layer = element?.closest("[data-popover-layer]");
  return layer ? homes.get(layer) ?? null : null;
}

/** Whether `node` is inside `container`, or in a popover opened from inside it, however deep (lib/parentChain.ts). */
export function holdsPlaced(container: Element | null | undefined, node: Node | null | undefined): boolean {
  if (!container) return false;
  const climb = parentChain<Node>();
  for (let at = node ?? null; at && climb(at); at = popoverHome(at)) {
    if (container.contains(at)) return true;
  }
  return false;
}

/** The first element matching `selector` in `root` or in a popover opened from inside it. */
export function queryPlaced<T extends Element = HTMLElement>(root: Element, selector: string): T | null {
  const own = root.querySelector<T>(selector);
  if (own) return own;
  for (const layer of document.querySelectorAll("[data-popover-layer]")) {
    const home = homes.get(layer), found = home && holdsPlaced(root, home) ? layer.querySelector<T>(selector) : null;
    if (found) return found;
  }
  return null;
}

/**
 * Draws a popover on the page's body, over the sidebar, a side peek and
 * every card, block or scroller of the page, as Notion draws its menus in one
 * overlay over the page: drawn in its place, it was held in the layer of what
 * held it (a block's row is one, under the sidebar's; 2026-11-08: a form's
 * question options went under the sidebar in the desktop app, half unread).
 * It still belongs where it was opened: React's events pass up from it as
 * before, a window or menu it was opened in holds it (overlayLayer.ts), and
 * what looks for it in its block finds it there (queryPlaced). Its text is
 * the body's, as every menu drawn over the page. A panel docked beside the
 * page is the page's own layer: drawn in a bar or block, a menu went under it
 * (2026-11-11: the page's "…" behind its docked updates). It is a focus scope
 * of its own inside the one it was opened in, so a picker that keeps the
 * focus inside it (Aria's FocusScope contain: the icon and cover pickers)
 * lets it take the focus: drawn out of the picker, the skin tone and icon
 * colour lists had their focus pulled back to their buttons as they opened.
 */
export function PopoverLayer({ children }: { children: ReactNode }) {
  const home = useRef<HTMLSpanElement>(null), layer = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const at = layer.current, from = home.current;
    if (!at || !from) return undefined;
    homes.set(at, from);
    return () => { homes.delete(at); };
  }, []);
  // Its scope joins in its own layout effects, which run before this one and
  // after those of what it holds; a focus they asked for meanwhile waits for
  // this (useFocusWhenLayerJoins). Put away (React's Activity) and shown
  // again, it joins again.
  const outer = useContext(LayerJoins), outerRef = useRef(outer);
  outerRef.current = outer;
  const joined = useRef(false), waiting = useRef<(() => void)[]>([]);
  const [whenJoined] = useState<WhenLayerJoins>(() => (focus: () => void) => {
    if (joined.current) outerRef.current(focus); else waiting.current.push(focus);
  });
  useLayoutEffect(() => {
    joined.current = true;
    for (const focus of waiting.current.splice(0)) outer(focus);
    return () => { joined.current = false; };
  }, [outer]);
  if (typeof document === "undefined") return <>{children}</>;
  return <>
    <span ref={home} hidden data-popover-home="" />
    {createPortal(<div ref={layer} data-popover-layer="" style={{ display: "contents" }}>
      <FocusScope><LayerJoins.Provider value={whenJoined}>{children}</LayerJoins.Provider></FocusScope>
    </div>, document.body)}
  </>;
}
