import type { Dispatch, SetStateAction } from "react";
import type * as Y from "yjs";
import type {
  ContentGraph,
  GraphBlock,
  GraphBlockType,
  GraphDataSource,
  GraphOperation,
  GraphProperty,
  GraphValueType,
  GraphView,
  GraphViewType,
} from "@/contentGraph";
import type { DatabaseContainerSource, DatabaseContainerPickerResponse } from "@/lib/databaseContainerPicker";
import type { NativeFormService } from "@/lib/nativeFormService";
import type { EditorDatabaseChoice } from "./editorGraphModel";
import type { NativePageMetadataChange } from "@/lib/nativePageMetadata";
import type { NativeBoardProductAdapter } from "@/lib/nativeBoardProductAdapter";
import type { NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import type { NativeTimelineProductAdapter } from "@/lib/nativeTimelineProductAdapter";
import type { DatabasePageLock } from "@/components/database/InlineDatabase";
import type { Page, Session } from "@/types";
import type { BlockPickerSnapshot } from "./blockPickerModel";

export type EditorSelection = ReadonlySet<string>;

export type EditorGraphSync = {
  flush(): Promise<boolean>;
  reread(): Promise<boolean>;
  /** Moves a block the server made here, before the page has it, after the page's earlier changes. */
  place?(blockId: string, spot: EditorBlockSpot): Promise<boolean>;
  /** Runs a change the server makes here, the page's reads waiting for it as for the page's own writes. */
  holding?<T>(task: () => Promise<T>): Promise<T>;
};

/** Where a block goes: after `afterId` among `parentId`'s blocks, or first there. */
export type EditorBlockSpot = { parentId: string | null; afterId: string | null };

export type EditorMoveContext = {
  pages: Page[];
  session: Session;
  onPageChange: (page: Page) => void;
  /** Reads the page list again once the body's page blocks changed pages. */
  onPagesChanged?: () => Promise<unknown>;
  onStatus?: (message: string, tone?: "default" | "success" | "error") => void;
  /** What a page block's own menu does to its page, as the page's menu does in the sidebar. */
  pageMenu?: EditorPageMenu;
  /**
   * The synced block copy, on another page, an editor of its original's
   * content is shown in, and that page as its editor has it: blocks moved
   * out of the original by keys go beside the copy there (syncedCopyMove).
   */
  syncedCopy?: { blockId: string; page(): NativeEditorGraphAdapter };
};

export type EditorPageMenu = {
  favoritePageIds: readonly string[];
  onFavoriteChange: (pageId: string, favorite: boolean) => Promise<void>;
  onPageMetadataChange: (
    page: Page,
    change: NativePageMetadataChange,
    signal: AbortSignal,
  ) => Promise<boolean | Page | void>;
  /** A page's copy, as its menu makes it; `withoutContent` leaves a database's pages out. */
  onDuplicatePage: (page: Page, signal: AbortSignal, options?: { withoutContent?: boolean }) => Promise<Page>;
};

export type EditorDatabaseAdapters = {
  boardAdapter?: NativeBoardProductAdapter;
  databaseViewAdapter?: NativeDatabaseViewProductAdapter;
  tableAdapter?: NativeTableProductAdapter;
  timelineAdapter?: NativeTimelineProductAdapter;
  formService?: NativeFormService;
  pendingToolbarDataSourceIds: ReadonlySet<string>;
  /** The page is a locked database: its rows stay editable, its views and properties don't. */
  structureLocked?: boolean;
  /** The page is read only because it is being suggested: its databases still take edits (InlineDatabaseBlock). */
  writableWhileSuggesting?: boolean;
  /** The page is read only by its lock alone for someone who may edit it: its databases' own "잠금" still unlocks them (InlineDatabase). */
  lockableUnderPageLock?: boolean;
  /** The reader may comment on the page, read only or not: its databases' cells take comments then. */
  canComment?: boolean;
  /** A database page's own lock, which its view settings lock and unlock. */
  pageLock?: DatabasePageLock;
  /** Opens the database chooser, for the line a menu chose when one did. */
  onChooseDatabase?: (choice?: EditorDatabaseChoice) => void;
  /** What a new inline database's start panel does besides closing. */
  startPanel?: EditorDatabaseStartActions;
  onAddProperty?: (
    source: GraphDataSource,
    type: GraphValueType,
    name: string,
    config?: Record<string, unknown>,
  ) => Promise<GraphProperty | null>;
  onConfiguredView?: (view: GraphView) => void;
};

export type EditorCommit = (
  next: ContentGraph,
  operations: readonly GraphOperation[],
  /**
   * `reshapes`: the change makes, removes or moves blocks, whose text the
   * others have at once: it goes to the server after the next frame, not
   * once typing pauses.
   */
  change?: { reshapes?: boolean },
) => void;

export type NativeEditorGraphAdapter = {
  readonly pageId: string;
  readonly workspaceId: string;
  readonly session: Session | null;
  readonly generation: number;
  readonly signal: AbortSignal;
  readonly graph: ContentGraph;
  readonly readOnly: boolean;
  /** A full-page database's body is its database, which stays where it is with nothing around it. */
  readonly fixedBody?: boolean;
  readonly collaborationDocument: Y.Doc | null;
  /** Pages and people the @ menu can mention; null where mentions cannot be made. */
  readonly mentionSnapshot: BlockPickerSnapshot | null;
  readonly database: EditorDatabaseAdapters;
  readonly moveContext?: EditorMoveContext;
  readonly pagePlaceholderBlockId: string | null;
  /** Saves waiting changes and reads the page again after the server changed it. */
  readonly graphSync?: EditorGraphSync;
  isCurrent(): boolean;
  observeLocalGraphChanges(listener: (before: ContentGraph, after: ContentGraph, operations: readonly GraphOperation[]) => void): () => void;
  /** A change made beside the adapter (a new database) joins the page's history as one made here. */
  localGraphChanged(before: ContentGraph, after: ContentGraph, operations: readonly GraphOperation[]): void;
  text(block: GraphBlock): string;
  changeText(block: GraphBlock, text: string): void;
  updateBlock(block: GraphBlock, content: Record<string, unknown>): void;
  /**
   * Writes what the app read for a block on its own (a bookmark's preview, a
   * video's size) as no step of the page's undo: ⌘Z after making the block
   * takes the block back, not the read (Notion's, 2026-11-11).
   */
  fillBlock?(block: GraphBlock, content: Record<string, unknown>): void;
  /** Hears each fill (`fillBlock`): the block, its kind and what was written. */
  observeFilledBlocks?(listener: (blockId: string, type: GraphBlockType, content: Record<string, unknown>) => void): () => void;
  /** Opens or closes a toggle for reading without granting content writes. */
  setToggleCollapsed(block: GraphBlock, collapsed: boolean): void;
  replaceBlocks(blocks: readonly GraphBlock[]): void;
  /**
   * With openPage, a block turned into a page opens that page once it is
   * made; with database, the page is made a full-page database first, shown
   * as the chosen view.
   */
  changeType(
    blockIds: readonly string[],
    type: GraphBlockType,
    options?: { openPage?: boolean; database?: { view?: GraphViewType } },
  ): readonly string[];
  insertAfter(afterId: string | null, type?: GraphBlockType, content?: Record<string, unknown>): GraphBlock | null;
  /** A new block last in a container (null: the page's own lines), or right after `after` there (null: first). */
  insertChild(parentId: string | null, type?: GraphBlockType, content?: Record<string, unknown>, after?: string | null): GraphBlock | null;
  insertTabItem(parentId: string, label: string): { item: GraphBlock } | null;
  // A database block may be copied without its rows.
  duplicate(blockIds: readonly string[], options?: { withoutContent?: boolean }): GraphBlock[];
  /**
   * Copies blocks with what they hold and puts the copies where `place` says,
   * as one change. Pages and databases, which are copied on the server, are
   * not copied this way. Returns the copies of the blocks, or none.
   */
  copyBlocks(
    blockIds: readonly string[],
    place: (graph: ContentGraph, copyIds: readonly string[]) => { graph: ContentGraph; operations: GraphOperation[] } | null,
  ): GraphBlock[];
  /**
   * Copies blocks on the server, as pages and databases are copied, and
   * resolves the copies' block IDs, in the blocks' order, once the page has
   * them; none when they can't be copied.
   */
  /** `oneStep`: the copies are the step of the page's history the copying began, one ⌘Z taking them away even before they come. */
  copyOnServer(blockIds: readonly string[], options?: { withoutContent?: boolean; oneStep?: boolean }): Promise<string[]>;
  /**
   * Copies a database block, of this page or another, on the server, for a
   * paste: resolves the copy's block ID once the page has it, where `spot`
   * says when it says, asked before the copy is made (null) and once it is
   * (its ID): the server puts it there before the page reads it.
   */
  copyDatabaseBlock(source: { blockId: string; pageId: string }, spot?: (blockId: string | null) => EditorBlockSpot | null): Promise<string>;
  /** Brings a database block cut from a page back from the trash into this page, as a copy goes; null when it can't. */
  moveCutDatabaseBlock(source: { blockId: string; pageId: string; workspaceId: string }, spot?: (blockId: string | null) => EditorBlockSpot | null): Promise<string | null>;
  /** Brings a sub-page cut from a page back from the trash under this page, as a copy goes; its block's ID, or null when it can't. */
  moveCutPageBlock(pageId: string, spot?: (blockId: string | null) => EditorBlockSpot | null): Promise<string | null>;
  /** Copies a sub-page, of this page or another, under this page for a paste, as a copy goes; its block's ID, or null when it can't. */
  copyPageBlock(pageId: string, spot?: (blockId: string | null) => EditorBlockSpot | null): Promise<string | null>;
  remove(blockIds: readonly string[]): void;
  move(blockId: string, parentId: string | null, afterId: string | null): void;
  restoreGraph(graph: ContentGraph, operations: readonly GraphOperation[]): void;
  /** Turns an inline database into a full-page database in its place, as Notion's block menu does. */
  /** With the open page's graph store, the page shows at once in the database's place. */
  turnDatabaseIntoPage(blockId: string, store?: import("@/lib/pageGraphStore").PageGraphStore | null): void;
  /**
   * Turns a full-page database, by its page's block, inline in its place, as
   * Notion's block menu does; with the open page's graph store, at once where
   * its page's graph is held in this tab.
   */
  turnDatabaseInline(pageBlockId: string, store?: import("@/lib/pageGraphStore").PageGraphStore | null): void;
  /** Applies a composite layout change, such as a column change, as one graph change. */
  changeLayout(change: (graph: ContentGraph) => { graph: ContentGraph; operations: GraphOperation[] } | null): boolean;
};

/** What a new inline database's start panel can turn it into, in its place. */
export type EditorDatabaseStartActions = {
  previewAuthor?: import("@/lib/databaseStarterCatalog").DatabaseStarterAuthor;
  /**
   * Puts a suggested template's database where the new, still empty one
   * stands or, with a data source, only that new empty source of it.
   */
  applyTemplate(blockId: string, selection: import("@/lib/databaseStarterCatalog").DatabaseStarterSelection, signal: AbortSignal, dataSourceId?: string): Promise<boolean>;
  /**
   * Makes the new, still empty database a linked view of an existing source,
   * which then chooses its view, or, with a data source, links the source in
   * place of that new empty one; with a view waiting for its source
   * (`viewId`), that view alone (linkViewInPlaceOperations).
   */
  linkSource(blockId: string, source: DatabaseContainerSource, dataSourceId?: string, viewId?: string): Promise<boolean>;
  /** A page of the sources to link; `limit` how many, where not the first few or a search's page; `first` those to list first. */
  loadDatabaseSources(query: string, cursor: string | undefined, signal: AbortSignal, limit?: number, first?: readonly string[]): Promise<DatabaseContainerPickerResponse>;
};

/** What an empty page's starter does besides writing. */
export type EditorPageStarterActions = {
  previewAuthor?: import("@/lib/databaseStarterCatalog").DatabaseStarterAuthor;
  /** `name`, the template's as shown, is the database's name at the press (the server names it so). */
  onStartTemplate(selection: import("@/lib/databaseStarterCatalog").DatabaseStarterSelection, signal: AbortSignal, name?: string): Promise<boolean>;
  /** A view to start the page as, or "blank" for "빈 데이터베이스" (fullPageDatabase.ts). */
  onStartDatabase(view: GraphViewType | "blank", signal?: AbortSignal): Promise<boolean>;
  onStartLinkedDatabase(source: DatabaseContainerSource, signal: AbortSignal): Promise<boolean>;
  loadDatabaseSources(query: string, cursor: string | undefined, signal: AbortSignal): Promise<DatabaseContainerPickerResponse>;
};

export type EditorProps = {
  adapter: NativeEditorGraphAdapter;
  loading: boolean;
  /** Whether the page couldn't be read: a note shows instead of its body, which takes no input. */
  unavailable?: boolean;
  /** An editor inside another editor shows this block's content alone. */
  rootBlockId?: string | null;
  /** The body is this one top block alone: an inline database opened as a page. */
  onlyBlockId?: string | null;
  showPageStarter: boolean;
  /** The empty page's one empty line, which the starter's choices replace. */
  pagePlaceholderBlockId?: string | null;
  staticReadOnlySemantics?: boolean;
  onAddParagraph: () => void;
  onAddDatabase: () => void;
  /** Without these the starter offers writing and an inline database. */
  starter?: EditorPageStarterActions;
  /** The starter is this line alone, which starts writing, as a database template's empty body. */
  starterHint?: string;
};

export type BlockItemProps = {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  depth: number;
  selected: boolean;
  selection: EditorSelection;
  setSelection: Dispatch<SetStateAction<EditorSelection>>;
  staticReadOnlySemantics?: boolean;
};
