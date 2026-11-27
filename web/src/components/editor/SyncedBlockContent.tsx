import { useContext, useEffect, useState, useSyncExternalStore, type ReactNode } from "react";
import { api } from "@/api";
import type { GraphBlock } from "@/contentGraph";
import { Copy, DotsHorizontal } from "@/icons/leafen";
import { userErrorText } from "@/lib/settingsErrors";
import { blockItemText } from "./blockItemLabels";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { SyncedOriginalTrail } from "./syncedBlockContext";
import { copySyncedBlock } from "./syncedBlockClipboard";
import { openBlockToolMenu } from "./editorCommands";
import { letSyncedCopiesGo } from "./syncedBlockCopies";
import { SYNCED_SOURCELESS, syncedBlockSource, type SyncedBlockSource } from "./syncedBlockModel";
import { SyncedBlockPlaces } from "./SyncedBlockPlaces";
import { SyncedOriginalEditor } from "./SyncedOriginalEditor";
import styles from "./editorStyles.generated.module.css";
import { confirmAction } from "../ConfirmActionDialog";
import { ReadFailed } from "@/components/ReadFailed";

/** What the block menu and the synced block's own buttons do. */
export function syncedBlockActions(adapter: NativeEditorGraphAdapter) {
  return {
    /**
     * The original goes to the clipboard, and pasting it into a page makes
     * a copy there, as Notion's "Copy and sync" does.
     */
    async copy(block: GraphBlock) {
      const source = await placedSyncedSource(adapter, block);
      const copied = source ? await copySyncedBlock(source, blockItemText("synced.block")) : false;
      adapter.moveContext?.onStatus?.(blockItemText(copied ? "synced.copied" : "synced.copyFailed"), copied ? "success" : "error");
      return copied;
    },
    /** The copy becomes ordinary blocks holding the original's content. */
    async unsync(block: GraphBlock) {
      const session = adapter.session;
      if (!session || adapter.readOnly) return false;
      try {
        await adapter.graphSync?.flush();
        await api.unsyncSyncedBlock(session, adapter.pageId, block.id);
        await adapter.graphSync?.reread();
        return true;
      } catch (error) {
        adapter.moveContext?.onStatus?.(userErrorText(error, blockItemText("synced.unavailable")), "error");
        return false;
      }
    },
    /**
     * "Unsync all", as in Notion: each copy becomes ordinary blocks holding
     * the original's content, or, when there are more than ten, every copy is
     * removed once the reader agrees. Copies on pages the reader can't change
     * stay as they are and are counted as kept.
     */
    async unsyncAll(block: GraphBlock) {
      const session = adapter.session;
      if (!session || adapter.readOnly) return;
      let result;
      try {
        await adapter.graphSync?.flush();
        result = await letSyncedCopiesGo(session, adapter.pageId, block.id,
          () => confirmAction({ message: blockItemText("synced.removeAllConfirm"),
            confirmLabel: blockItemText("synced.removeAllConfirmAction"), danger: true }));
        if (!result) return;
        await adapter.graphSync?.reread();
      } catch (error) {
        adapter.moveContext?.onStatus?.(userErrorText(error, blockItemText("synced.unavailable")), "error");
        return;
      }
      adapter.moveContext?.onStatus?.(result.kept > 0
        ? blockItemText("synced.unsyncAllKept", { count: result.kept })
        : blockItemText(result.removed ? "synced.removedAll" : "synced.unsyncedAll", { count: result.done }),
      result.kept > 0 ? "error" : "success");
    },
  };
}

// Whether a key was pressed since the pointer last moved: a key puts a
// chosen synced block's toolbar away till the pointer moves, as Notion's
// (seen 2026-11-14: Escape, Shift+↑ or ⌘D left the frame alone until then).
// Heard from the first synced block drawn on, the key that chooses one too.
let keyed = false;
let watching = false;
const keyedHeard = new Set<() => void>();
let lastPointer: { x: number; y: number } | null = null;
function setKeyed(next: boolean) {
  if (keyed === next) return;
  keyed = next;
  for (const heard of keyedHeard) heard();
}
function watchKeyed() {
  if (watching) return;
  watching = true;
  // A key put to the toolbar itself — its places list, a button of it —
  // leaves it there, the focus staying in it: Escape closing the list leaves
  // the list's button focused, ringed, a letter after it too (Notion's, seen
  // 2026-11-16 in two tabs).
  window.addEventListener("keydown", (event) => {
    if (!(event.target instanceof Element && event.target.closest(`.${styles.syncedToolbar}`))) setKeyed(true);
  }, true);
  const pointed = (event: PointerEvent) => {
    const moved = event.type === "pointerdown" || !lastPointer || lastPointer.x !== event.clientX || lastPointer.y !== event.clientY;
    lastPointer = { x: event.clientX, y: event.clientY };
    if (moved) setKeyed(false);
  };
  window.addEventListener("pointermove", pointed, true);
  window.addEventListener("pointerdown", pointed, true);
}
function hearKeyed(heard: () => void) {
  keyedHeard.add(heard);
  return () => { keyedHeard.delete(heard); };
}
const noHearing = () => () => {};

/**
 * A synced block shows its content where its blocks would be; a copy shows
 * the original's content and edits it where the copy is. As Notion's (seen
 * 2026-10-31), nothing marks it until it is worked in: then a frame goes
 * round it and a toolbar over its top right holds the pages it is synced to,
 * copy and the block's menu. It is worked in while the caret is in it, while
 * what is chosen was chosen in it, as Escape from its text chooses its line
 * — its lines then show their own handles — and while it is chosen itself
 * (`chosen`, syncedFrameChoice; Notion's, seen 2026-11-14), not with other
 * blocks; chosen, its toolbar waits for the pointer to move after a key.
 */
export function SyncedBlockContent({ adapter, block, engineContent, caretInside = false, chosen = null }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  /** A local native projection already contains the original's children. */
  engineContent?: ReactNode;
  /** Whether the page editor's caret is in the block's own projection. */
  caretInside?: boolean;
  /** What is chosen of it, where it is drawn: all of it inside it, or it itself (syncedFrameChoice). */
  chosen?: "inside" | "self" | null;
}) {
  const source = syncedBlockSource(block, adapter.pageId, adapter.graph.blocks);
  const actions = syncedBlockActions(adapter);
  // Where else the block is reads each time the caret or focus comes into it.
  const [focused, setFocused] = useState(false);
  useEffect(watchKeyed, []);
  const keyedNow = useSyncExternalStore(chosen ? hearKeyed : noHearing, () => keyed);
  return (
    <section className={styles.syncedBlock} data-copy={source.copy ? "true" : undefined} data-caret-inside={caretInside ? "true" : undefined}
      data-chosen={chosen ?? undefined} data-chosen-toolbar={chosen && !keyedNow ? "true" : undefined}
      aria-label={blockItemText(source.copy ? "synced.copy" : "synced.block")} onFocus={() => setFocused(true)}
      onBlur={(event) => { if (!(event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget))) setFocused(false); }}>
      {/* A press on it keeps the focus where it is: the editor losing it took
          the caret out of the block, which put the toolbar away under the
          press, and the press fell on the line under it (2026-11-15). */}
      <div className={styles.syncedToolbar} contentEditable={false} role="toolbar" aria-label={blockItemText(source.copy ? "synced.copy" : "synced.block")}
        onMouseDown={(event) => event.preventDefault()}>
        <SyncedBlockPlaces adapter={adapter} block={block} active={focused || caretInside || chosen !== null} />
        <span className={styles.syncedToolbarDivider} aria-hidden="true" />
        <button type="button" data-synced-copy aria-label={blockItemText("synced.copyBlock")} data-icon-tip={blockItemText("synced.copyBlock")}
          onClick={() => void actions.copy(block)}><Copy size="small" aria-hidden="true" /></button>
        {adapter.readOnly ? null : (
          <button type="button" aria-label={blockItemText("code.blockMenu")} data-icon-tip={blockItemText("code.blockMenu")}
            onClick={(event) => openBlockToolMenu(block.id, event)}><DotsHorizontal size="small" aria-hidden="true" /></button>
        )}
      </div>
      <div className={styles.syncedContent} contentEditable={source.copy && !engineContent ? false : undefined}>
        {engineContent ?? <SyncedCopyContent adapter={adapter} block={block} source={source} />}
      </div>
    </section>
  );
}

/**
 * A copy shows its original's content. On the original's page the same
 * blocks show again and are edited as they are there; on another page they
 * are edited with that page's editor. An original whose content already
 * shows around the copy, as for a copy inside its own original, does not
 * show again.
 */
function SyncedCopyContent({ adapter, block, source }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  source: SyncedBlockSource;
}) {
  const trail = useContext(SyncedOriginalTrail);
  // A copy that doesn't name its original's page reads where it is: null
  // until read, "" when it can't be.
  const [readPage, setReadPage] = useState<string | null>(null);
  // Where the original is could not be read: said with a way to read again,
  // not as an original that is gone.
  const [readFailed, setReadFailed] = useState(false);
  const session = adapter.session;
  // A copy syncing from no block shows nothing and reads nowhere (SYNCED_SOURCELESS).
  const sourceless = source.blockId === SYNCED_SOURCELESS;
  useEffect(() => {
    if (source.pageId || !session || readPage !== null || readFailed || sourceless) return;
    let cancelled = false;
    api.syncedBlockLocations(session, adapter.pageId, block.id)
      .then((read) => { if (!cancelled) setReadPage(read.original.pageId || ""); })
      .catch(() => { if (!cancelled) setReadFailed(true); });
    return () => { cancelled = true; };
  }, [adapter.pageId, block.id, readFailed, readPage, session, source.pageId, sourceless]);
  if (sourceless) return null;
  if (readFailed && !source.pageId) {
    return <div className={styles.syncedMissing} contentEditable={false}><ReadFailed message={blockItemText("synced.readFailed")} onRetry={() => setReadFailed(false)} /></div>;
  }
  const pageId = source.pageId || readPage;
  const unavailable = <div className={styles.syncedMissing}>{blockItemText("synced.unavailable")}</div>;
  if (trail.has(source.blockId) || insideOriginal(adapter.graph.blocks, block, source.blockId)) return unavailable;
  if (pageId === null) return null;
  if (pageId !== adapter.pageId) {
    return session && pageId
      ? <SyncedOriginalEditor session={session} pageId={pageId} blockId={source.blockId}
        workspaceId={adapter.workspaceId} readOnly={adapter.readOnly} copy={{ blockId: block.id, page: () => adapter }} />
      : unavailable;
  }
  // Local originals/copies are projected into this view's contentDOM by
  // nativeTiptapSyncedLayout. Only another page needs its native editor.
  return unavailable;
}

/**
 * Where a synced block's original is. A copy that doesn't name the
 * original's page (an API or import wrote it before 2026-10-31) asks the
 * server, which reads it from the original block; null when it can't be read.
 */
async function placedSyncedSource(adapter: NativeEditorGraphAdapter, block: GraphBlock): Promise<SyncedBlockSource | null> {
  const source = syncedBlockSource(block, adapter.pageId, adapter.graph.blocks);
  if (source.pageId) return source;
  if (!adapter.session) return null;
  try {
    const read = await api.syncedBlockLocations(adapter.session, adapter.pageId, block.id);
    return read.original.pageId ? { ...source, pageId: read.original.pageId } : null;
  } catch {
    return null;
  }
}

/** Whether a block sits inside the original it would show. */
function insideOriginal(blocks: readonly GraphBlock[], block: GraphBlock, originalId: string) {
  const parents = new Map(blocks.map((candidate) => [candidate.id, candidate.parentId]));
  const seen = new Set<string>();
  for (let parentId = block.parentId; parentId && !seen.has(parentId); parentId = parents.get(parentId) ?? null) {
    if (parentId === originalId) return true;
    seen.add(parentId);
  }
  return false;
}
