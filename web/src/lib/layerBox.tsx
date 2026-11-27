import type { CSSProperties, HTMLAttributes, ReactNode, RefObject } from "react";
import { type OverlayLayerOptions, useOverlayLayer } from "./overlayLayer";

/**
 * The box a layer is drawn in, taking the layer itself (useOverlayLayer) when
 * given one: its taking a place among the layers, and its showing, draw this
 * box alone, not what it holds, which its holder drew. Held by the holder,
 * that state drew the holder again with all it holds (2026-11-14: a
 * database's view settings docked at its page's end drew their toolbar and
 * their 157 parts once more as they opened, NAS condition, CPU ×2), as a
 * page's "…" menu keeps its own box (RowMenuPlaced). `raised`, it stands at
 * its layer's height; else where its style puts it.
 */
export function LayerBox({ boxRef, layer, raised = false, style, children, ...attributes }: {
  boxRef: RefObject<HTMLDivElement | null>;
  layer?: Omit<OverlayLayerOptions, "ref"> | null;
  raised?: boolean;
  style?: CSSProperties;
  children: ReactNode;
} & Omit<HTMLAttributes<HTMLDivElement>, "style" | "children">) {
  const { zIndex } = useOverlayLayer(Boolean(layer), { ...layer, ref: boxRef });
  return (
    <div ref={boxRef} style={raised && zIndex !== undefined ? { ...style, zIndex } : style} {...attributes}>
      {children}
    </div>
  );
}
