import { useContext, useRef, useState, useSyncExternalStore } from "react";
import { NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import type { GraphBlock } from "@/contentGraph";
import { pageIdFromPageHref } from "@/lib/pageLinks";
import { CalloutIcon } from "./CalloutIcon";
import styles from "./editorStyles.generated.module.css";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { MediaBlockContent } from "./MediaBlockContent";
import { PageBlockContent, pageLinkShownPage } from "./PageBlockContent";
import { SpecialBlockContent } from "./SpecialBlockContent";
import { InlineDatabaseBlock } from "./InlineDatabaseBlock";
import { SyncedBlockContent } from "./SyncedBlockContent";
import { syncedBlockSource } from "./syncedBlockModel";
import { syncedFrameChoice } from "./blockOccurrences";
import { adapterDrawnFields, adapterDrawnFieldsBesideGraph, liveAdapter } from "./nativeTiptapLiveState";
import { NativeTiptapAdapterContext, useNativeTiptapNode, useNativeTiptapUi } from "./nativeTiptapReactContext";

const media = new Set(["image", "video", "audio", "bookmark", "link_preview", "embed", "pdf", "file"]);
const pages = new Set(["child_page", "child_database", "link_to_page", "breadcrumb", "table_of_contents"]);

/**
 * What a widget draws from the page's adapter beyond its own block, which
 * it reads from the newest graph (useNativeTiptapNode), by its kind; null
 * for one drawing from all of it, a database (its rows, views and the
 * page's database services). Every one reads what every node view does
 * beside the graph (adapterDrawnFieldsBesideGraph); a table of contents
 * draws the page's headings and a synced copy where its original is, so
 * they read its blocks too. Of the page list and the mentions' pages it
 * reads the pages it names (namedPages); a synced block (its places), a
 * button (its setup, with the databases and their properties) and a page
 * link naming none yet (its picker) read all of them. Its blocks drawn for
 * any change of the graph, each picture, page link and callout icon of a
 * long page drew again in the press that made a line (2026-11-14), and
 * again as the page's edited time came into the page list.
 */
function widgetReads(block: GraphBlock, adapter: NativeEditorGraphAdapter): readonly unknown[] | null {
  const { type } = block;
  if (type === "inline_database") return null;
  const beside = OTHER_BLOCKS.has(type) ? adapterDrawnFields(adapter) : adapterDrawnFieldsBesideGraph(adapter);
  const named = namedPages(block, adapter);
  const pages = named ? [...beside, ...named] : [...beside, adapter.mentionSnapshot?.pagesById, adapter.moveContext?.pages];
  return type === "button" ? [...pages, adapter.graph.dataSources, adapter.graph.properties] : pages;
}

type ListedPage = NonNullable<NativeEditorGraphAdapter["moveContext"]>["pages"][number];
const listedById = new WeakMap<readonly ListedPage[], Map<string, ListedPage>>();

/**
 * The entries of the pages a widget names, in the mentions' pages and the
 * page list, with those of the pages above each, whose trash its link
 * shows and a breadcrumb draws: a page link's page and the page it opens
 * (pageLinkShownPage), a breadcrumb's page, a picture's linked page; none
 * for the rest. Null where it reads all of them.
 */
function namedPages(block: GraphBlock, adapter: NativeEditorGraphAdapter): unknown[] | null {
  const ids: unknown[] = [];
  if (pages.has(block.type) && block.type !== "breadcrumb" && block.type !== "table_of_contents") {
    const shown = pageLinkShownPage(block.id);
    ids.push(block.content.childPageId, block.content.pageId, shown);
    if (block.type === "link_to_page" && !ids.some(id => typeof id === "string" && id.trim())) return null;
  } else if (block.type === "breadcrumb") ids.push(adapter.pageId);
  else if (block.type === "image") ids.push(pageIdFromPageHref(block.content.imageLink));
  else if (OTHER_BLOCKS.has(block.type) || block.type === "button") return null;
  const mentioned = adapter.mentionSnapshot?.pagesById, listed = adapter.moveContext?.pages;
  let index = listed ? listedById.get(listed) : undefined;
  if (listed && !index) listedById.set(listed, index = new Map(listed.map(page => [page.id, page])));
  const entries: unknown[] = [];
  const seen = new Set<string>();
  for (const named of ids) {
    let id = typeof named === "string" ? named.trim() : "";
    while (id && !seen.has(id)) {
      seen.add(id);
      const mention = mentioned?.[id], page = index?.get(id);
      entries.push(mention, page);
      id = page?.parentId ?? mention?.parentId ?? "";
    }
  }
  return entries;
}

const OTHER_BLOCKS = new Set(["table_of_contents", "synced_block"]);

const noSubscribe = () => () => {};
const sameFields = (left: readonly unknown[], right: readonly unknown[]) =>
  left.length === right.length && left.every((field, index) => Object.is(field, right[index]));

/**
 * The page's adapter for a widget of this kind, drawn again only when what
 * it reads of it changes: the page makes a new adapter with each of its
 * redraws, and every widget of a long page — pictures, page links, callout
 * icons — drew again with a database's each row change. What it hands the
 * widget reads and calls the current adapter whenever it is used, never the
 * one it was drawn with, and is a new one as what the widget reads changes,
 * so what keeps it (a bookmark's read, a breadcrumb's chain) goes again then.
 */
function useWidgetAdapter(block: GraphBlock | null | undefined, fallback: NativeEditorGraphAdapter): NativeEditorGraphAdapter {
  const store = useContext(NativeTiptapAdapterContext);
  const kept = useRef<{ fields: readonly unknown[]; adapter: NativeEditorGraphAdapter } | null>(null);
  const [latest] = useState(() => store ? { get current() { return store.get(); } } : null);
  return useSyncExternalStore(store?.subscribe ?? noSubscribe, () => {
    const now = store?.get() ?? fallback;
    const fields = latest && block ? widgetReads(block, now) : null;
    if (!latest || !fields) return now;
    if (kept.current && sameFields(kept.current.fields, fields)) return kept.current.adapter;
    kept.current = { fields, adapter: liveAdapter(latest) };
    return kept.current.adapter;
  });
}

/** Native widgets keep their existing product services and independent controls. */
export function NativeTiptapWidgetView(props: NodeViewProps) {
  const context = useNativeTiptapNode(props);
  const { block, depth, selection, setSelection, staticReadOnlySemantics } = context;
  const adapter = useWidgetAdapter(block, context.adapter);
  const selected = useNativeTiptapUi(state => block ? state.selection.has(block.id) : false, false);
  // A synced copy of another page's original, chosen, is framed with its toolbar (syncedFrameChoice).
  const synced = block?.type === "synced_block" && context.occurrenceId ? context.occurrenceId : null;
  const chosen = useNativeTiptapUi(state => synced ? syncedFrameChoice(state.selection, synced, context.graphIndex.byId) : null, null);
  if (!block) return <NodeViewWrapper contentEditable={false} hidden />;
  const source = syncedBlockSource(block, adapter.pageId, adapter.graph.blocks);
  const structural = ["column", "column_list", "table", "tab"].includes(block.type)
    || block.type === "synced_block" && (!source.copy || source.pageId === adapter.pageId && !source.page);
  const content = block.type === "callout" ? <div className={styles.callout} data-icon-only="true"
    data-no-icon={block.content.iconType === "none" ? "true" : undefined}><CalloutIcon adapter={adapter} block={block} /></div>
    // A widget's depth counts the block it draws too: 1 straight under the page.
    : media.has(block.type) ? <MediaBlockContent adapter={adapter} block={block} nested={depth > 1} />
    : pages.has(block.type) ? <PageBlockContent adapter={adapter} block={block} pagesById={adapter.mentionSnapshot?.pagesById} />
    : block.type === "inline_database" ? <InlineDatabaseBlock block={block} props={{
      adapter, block, depth, selection, setSelection, selected, staticReadOnlySemantics,
    }} /> : block.type === "synced_block" && !structural ? <SyncedBlockContent adapter={adapter} block={block} chosen={chosen} />
    : structural ? null : <SpecialBlockContent adapter={adapter} block={block} />;
  return <NodeViewWrapper contentEditable={false} data-native-widget hidden={structural}>{content}</NodeViewWrapper>;
}
