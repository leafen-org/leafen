import { useCallback, useEffect, useRef } from "react";
import type * as Y from "yjs";
import { i18next } from "@/i18n";
import { compareGraphOrder, type ContentGraph, type GraphBlock, type GraphOperation } from "@/contentGraph";
import { BLOCK_TEXT_ROOT_PREFIX } from "@/lib/blockTextEditing";
import { copyText } from "@/lib/copyText";
import type { NativeGraphMutationOutbox } from "@/lib/nativePageMutationBrowser";
import { productLockRefusalReason } from "@/lib/productErrorPresentation";
import { useProductToast } from "@/lib/productToast";
import type { RefusedEditsHeard } from "@/lib/refusedTextLetGo";

/** The page's text as it was here, block by block in the page's order. */
function pageTextHere(graph: ContentGraph, document: Y.Doc): string {
  const children = new Map<string, GraphBlock[]>();
  for (const block of graph.blocks) {
    const siblings = children.get(block.parentId ?? "") ?? [];
    siblings.push(block);
    children.set(block.parentId ?? "", siblings);
  }
  const lines: string[] = [];
  const walk = (parentId: string) => {
    for (const block of (children.get(parentId) ?? []).sort(compareGraphOrder)) {
      const text = document.getText(BLOCK_TEXT_ROOT_PREFIX + block.id).toString();
      if (text) lines.push(text);
      walk(block.id);
    }
  };
  walk("");
  return lines.join("\n");
}

/**
 * Says that what was written here and refused was let go, and offers the page
 * as it was here to copy, so the person can keep it elsewhere
 * (2026-10-16: text written while away, after access turned read-only, was
 * let go with no way to keep it). Let go because the page is locked, it says
 * so as the lock's refusal of changes kept here does, in the same notice.
 */
export function useRefusedEditsNotice(readGraph: () => ContentGraph, message = "syncStatusBadge:refusedEdits"): RefusedEditsHeard {
  const { notify } = useProductToast();
  const graphNow = useRef(readGraph);
  graphNow.current = readGraph;
  return useCallback((document: Y.Doc, lockedPageId?: string) => {
    const text = pageTextHere(graphNow.current(), document);
    if (lockedPageId) sayLockRefused(notify, lockedPageId, productLockRefusalReason({ code: "LEAFEN_PAGE_LOCKED", status: 423 }), [["page-text", text]]);
    else notify(i18next.t(message), "error", text ? copyAction(text, notify) : undefined);
  }, [message, notify]);
}

type Notify = ReturnType<typeof useProductToast>["notify"];

/** The action that copies what was let go, and says whether it could. */
function copyAction(text: string, notify: Notify) {
  return {
    label: i18next.t("syncStatusBadge:copyRefusedEdits"),
    autoDismissMs: null,
    onClick: () => {
      void copyText(text).then((copied) => notify(
        i18next.t(copied ? "syncStatusBadge:refusedEditsCopied" : "syncStatusBadge:refusedEditsNotCopied"),
        copied ? "success" : "error",
      ));
    },
  };
}

/**
 * What a change kept on this device wrote, by what it is, for a person to
 * keep: a title, the text of a block it made or changed (as the page's copy
 * here holds it), a property's or a view's name, a page's text.
 */
function keptChangeWords(written: unknown, document: Y.Doc | null, words: Map<string, () => string>) {
  const value = written && typeof written === "object" ? written as Record<string, unknown> : {};
  const title = ((value.kind === "page_create" ? value.page : value.change) as { title?: unknown } | undefined)?.title;
  if ((value.kind === "page_metadata" || value.kind === "page_create") && typeof title === "string") words.set("title", () => title);
  const content = (value.patch as { content?: unknown } | undefined)?.content;
  if (value.kind === "page_update" && typeof content === "string") words.set("text", () => content);
  if (value.kind !== "content_graph_mutation" || !Array.isArray(value.operations)) return;
  for (const operation of value.operations as GraphOperation[]) {
    if (operation?.kind === "upsertBlock" && document) {
      words.set(`block:${operation.block.id}`, () => document.getText(BLOCK_TEXT_ROOT_PREFIX + operation.block.id).toString());
    } else if (operation?.kind === "upsertProperty") words.set(`property:${operation.property.id}`, () => operation.property.name);
    else if (operation?.kind === "upsertView") words.set(`view:${operation.view.id}`, () => operation.view.name);
  }
}

// Refusals of one turn of the outbox come a round trip apart: they are said once.
const LOCK_REFUSALS_SETTLE_MS = 300;
// What a lock refused of a page's text comes some seconds after the outbox's
// refusals as the page shows locked (lib/refusedTextLetGo.ts): one notice
// says both, the words of each to copy.
const LOCK_REFUSALS_JOIN_MS = 15_000;
const lockRefusalsSaid = new Map<string, { reason: string; words: Map<string, string>; at: number }>();

/** Says a lock's refusal of what was written here on a page, with the words said of it a moment before. */
function sayLockRefused(notify: Notify, pageId: string, reason: string, words: Iterable<readonly [string, string]>) {
  const before = lockRefusalsSaid.get(pageId);
  const joined = before && before.reason === reason && Date.now() - before.at < LOCK_REFUSALS_JOIN_MS ? before.words : new Map<string, string>();
  for (const [key, word] of words) joined.set(key, word);
  lockRefusalsSaid.set(pageId, { reason, words: joined, at: Date.now() });
  // The page's title first, as the page shows it, then the rest in the order written.
  const text = [...joined].sort(([a], [b]) => Number(b === "title") - Number(a === "title")).map(([, word]) => word).filter(Boolean).join("\n");
  notify(text ? `${reason} ${i18next.t("syncStatusBadge:lockRefusedKept")}` : reason, "error", text ? copyAction(text, notify) : undefined);
}

/**
 * Says that a page's or a database's lock refused changes kept on this
 * device for the page shown — kept while away, say, as someone locked it —
 * which were let go and not kept waiting for an unlock, and offers what they
 * wrote to copy, as text let go when the page turned read-only is
 * (useRefusedEditsNotice): the page then shows what the server holds.
 */
export function useLockRefusedKeptNotice(outbox: NativeGraphMutationOutbox | null | undefined, pageId: string | null | undefined, document: Y.Doc | null) {
  const { notify } = useProductToast();
  const documentNow = useRef(document);
  documentNow.current = document;
  useEffect(() => {
    if (!outbox || !pageId) return undefined;
    let refused: { reason: string; words: Map<string, () => string>; timer: number } | null = null;
    const say = () => {
      if (!refused) return;
      sayLockRefused(notify, pageId, refused.reason, [...refused.words].map(([key, word]) => [key, word()] as const));
      refused = null;
    };
    const stop = outbox.subscribeGraphLane((event) => {
      if (event.kind !== "rejected" || event.pageId !== pageId) return;
      const reason = productLockRefusalReason(event.error);
      if (!reason) return;
      if (refused) window.clearTimeout(refused.timer);
      refused ??= { reason, words: new Map(), timer: 0 };
      keptChangeWords(event.written, documentNow.current, refused.words);
      refused.timer = window.setTimeout(say, LOCK_REFUSALS_SETTLE_MS);
    });
    return () => {
      stop();
      if (refused) window.clearTimeout(refused.timer);
    };
  }, [notify, outbox, pageId]);
}
