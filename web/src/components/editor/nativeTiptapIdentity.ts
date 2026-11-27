import type { Node } from "@tiptap/pm/model";
import { newId } from "@/lib/ids";

/** A copy's occurrence ID is stable; its text ID still names the original. */
export function nativeTiptapInstance(node: Node | null) {
  const { id, sourceId } = node?.attrs ?? {};
  return typeof id === "string" && typeof sourceId === "string" && id.endsWith(`/${sourceId}`)
    ? id.slice(0, -sourceId.length) : "";
}

export function newNativeTiptapAttrs(kind: string, props: Record<string, unknown> = {}, instance = "") {
  const id = newId();
  return { id: instance + id, sourceId: instance ? id : null, kind, props };
}
