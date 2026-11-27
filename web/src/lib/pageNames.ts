import { useCallback, useLayoutEffect, useMemo, useReducer, useRef, useSyncExternalStore } from "react";
import type { Page } from "@/types";
import type { Page as ModelPage } from "./modelTypes/page";
import type { TextSpan } from "@/lib/types";
import { databaseDisplayTitle, listedPageDisplayTitle, pageDisplayTitle } from "./pageTitle";
import {
  flushPageNames, followPageName, followPageNames, newerPageName, notePageName, pageNameChangesSince, pageNameVersion, pageNamesVersion,
  sameName, setListedPages, typedPageTitle, typingPageTitle,
  type HeldPageName, type PageNameFields,
} from "./pageNameRecords";

/*
 * How a place names a page: by the owner of pages' names (pageNameRecords.ts),
 * never by what it holds of the page alone — a page held as it was read names
 * it as it was then. A place drawn names its pages through a hook, which draws
 * it again when one is renamed or given a new icon (useNamedPage, usePageName,
 * usePageNames); what is worked out once, at a press or for a copy, through
 * pageNamesNow. scripts/check-page-names.mjs keeps every place to these.
 */

export { dropTypedPageTitle, knownPageName, showTypedPageTitle, typedPageTitle, followPageName, type HeldPageName } from "./pageNameRecords";
// What an untitled page of a kind is called, with no page to name.
export { untitledPageDisplayTitle } from "./pageTitle";

/** A page as its name is worked out: what pageDisplayTitle reads. */
type NamedPage = NonNullable<Parameters<typeof pageDisplayTitle>[0]>;

// The copy named anew given for each object held, while the name it was given stands.
const namedCopies = new WeakMap<object, { name: PageNameFields; copy: object }>();

/**
 * The page as a place holds it, with the newest name and icon known of it:
 * the same object where the place's own stands, else a copy (the same one
 * while that name stands).
 */
function namedNow<T extends HeldPageName>(held: T): T {
  const name = newerPageName(held);
  if (!name) return held;
  const kept = namedCopies.get(held);
  if (kept && (kept.name === name || sameName(kept.name, name))) return kept.copy as T;
  const copy = {
    ...held,
    title: name.title,
    titleSpans: name.titleSpans as TextSpan[] | undefined,
    ...(name.iconType !== undefined ? { icon: name.icon ?? "", iconType: name.iconType } : {}),
  } as T;
  namedCopies.set(held, { name, copy });
  return copy;
}

/** Names pages: each read through it is named as the owner knows it now. */
export type PageNameReader = {
  /** The page with its newest name and icon. */
  named<T extends HeldPageName>(page: T): T;
  /** The page's name as every place shows it (pageDisplayTitle), "" for none. */
  title(page: NamedPage | null | undefined): string;
  /** A page named where the reader's page list may not hold it (listedPageDisplayTitle). */
  listed(...args: Parameters<typeof listedPageDisplayTitle>): string;
  /** A database by its page, a linked one by what it links (databaseDisplayTitle). */
  database(page: Parameters<typeof databaseDisplayTitle>[0]): string;
};

function readerOf(read: (id: string) => void): PageNameReader {
  const named = <T extends HeldPageName>(page: T): T => {
    if (page.id) read(page.id);
    return namedNow(page);
  };
  return {
    named,
    title: (page) => (page ? pageDisplayTitle(named(page)) : ""),
    listed: (page, ...rest) => listedPageDisplayTitle(page ? named(page) : page, ...rest),
    database: (page) => databaseDisplayTitle(page ? named(page as HeldPageName & NamedPage) : page),
  };
}

/**
 * Names as they are now, for what isn't drawn again as a name changes: a
 * press's notice, a copy, a file, an order or a search worked out at once.
 */
export const pageNamesNow: PageNameReader = readerOf(() => undefined);

/** The page with its newest name and icon, drawn again as they change. */
export function useNamedPage<T extends HeldPageName>(held: T): T;
export function useNamedPage<T extends HeldPageName>(held: T | null | undefined): T | null | undefined;
export function useNamedPage<T extends HeldPageName>(held: T | null | undefined): T | null | undefined {
  const id = held?.id;
  const follow = useCallback((listener: () => void) => (id ? followPageName(id, listener) : () => undefined), [id]);
  useSyncExternalStore(follow, () => (id ? pageNameVersion(id) : 0), () => 0);
  return held ? namedNow(held) : held;
}

/**
 * The title being typed for the page over its name `over` — in its title, a
 * row's table cell or head, a sidebar rename — drawn again at each key, a
 * syllable still composing too; undefined while none is.
 */
export function useTypedPageTitle(id: string | null | undefined, over: string): string | undefined {
  const follow = useCallback((listener: () => void) => (id ? followPageName(id, listener) : () => undefined), [id]);
  return useSyncExternalStore(follow, () => (id ? typedPageTitle(id, over) : undefined), () => undefined);
}

/** The page's name as every place shows it (pageDisplayTitle), drawn again as it changes; "" for none. */
export function usePageName(held: NamedPage | null | undefined): string {
  const named = useNamedPage(held as (NamedPage & HeldPageName) | null | undefined);
  return named ? pageDisplayTitle(named) : "";
}

/**
 * Names the pages a place lists: each page named through it is followed, and
 * the place drawn again — with a new reader, which what it worked out of the
 * names depends on — when one is renamed or given a new icon.
 */
export function usePageNames(): PageNameReader {
  const [drawn, redraw] = useReducer((count: number) => count + 1, 0);
  const own = useRef<{ read: Set<string>; at: number } | null>(null);
  const state = (own.current ??= { read: new Set(), at: 0 });
  state.at = pageNamesVersion();
  // eslint-disable-next-line react-hooks/exhaustive-deps -- a new reader for each name change heard
  const reader = useMemo(() => readerOf((id) => state.read.add(id)), [drawn, state]);
  // A name it read changed between the drawing and now.
  useLayoutEffect(() => {
    const missed = pageNameChangesSince(state.at);
    if (missed === null ? state.read.size > 0 : missed.some((id) => state.read.has(id))) redraw();
  });
  // One listener for all it named, however many: a search names every page of a workspace.
  useLayoutEffect(() => followPageNames((ids) => {
    // The fewer of the two looked through: a whole list read names every page.
    const [few, many] = ids.size <= state.read.size ? [ids, state.read] : [state.read, ids];
    for (const id of few) if (many.has(id)) return redraw();
  }), [state]);
  return reader;
}

/**
 * The names of the pages a title or text mentions, followed: the place
 * drawing them is drawn again as one is renamed or typed (the mention reads
 * the name, peopleDirectory.ts withCurrentMentionName).
 */
export function useMentionedPageNames(spans: readonly { mention?: string; pageId?: string }[] | null | undefined) {
  const ids = useMemo(() => [...new Set((spans ?? []).flatMap((span) => span.mention === "page" && span.pageId ? [span.pageId] : []))].join(" "), [spans]);
  const follow = useCallback((listener: () => void) => {
    const stops = ids ? ids.split(" ").map((id) => followPageName(id, listener)) : [];
    return () => { for (const stop of stops) stop(); };
  }, [ids]);
  return useSyncExternalStore(follow, () => (ids ? ids.split(" ").reduce((sum, id) => sum + pageNameVersion(id), 0) : 0), () => 0);
}

/**
 * Names the workspace's pages as its list draws them: noted as it draws,
 * before what it draws reads them, and the places naming them told once it
 * is drawn. A list made from the last one by new records of a few pages has
 * only those noted again (listEditsOf), as a 44,000-page workspace's list
 * changes at each title saved.
 */
export function useListedPageNames(pages: readonly Page[]) {
  useMemo(() => setListedPages(pages), [pages]);
  useLayoutEffect(() => flushPageNames(), [pages]);
}

/**
 * Names a page as the place that writes its name holds it — a row's page,
 * its title and icon written in its head — so every other place naming it
 * follows at once: the name it opened with only as a read (older names don't
 * stand over newer), each change after as this screen's own.
 */
export function useOwnPageName(id: string | null | undefined, name: HeldPageName | null) {
  const opened = useRef<string | null>(null);
  const title = name?.title ?? "", spans = name?.titleSpans, icon = name?.icon, iconType = name?.iconType, updatedAt = name?.updatedAt;
  const spansKey = spans?.length ? JSON.stringify(spans) : "";
  useLayoutEffect(() => {
    // A name held only as a title is typed for the page (drawn over its name
    // wherever it is named) is not its own: noted, it would stand over the
    // name typed over, and the typing go on unseen.
    if (!id || !name || title === typingPageTitle(id)) return;
    const first = opened.current !== id;
    opened.current = id;
    // A change after it is this screen's own, newer than any read: its page's last edit as opened says nothing of it.
    notePageName(id, { title, titleSpans: spans, icon, iconType, updatedAt: first ? updatedAt : undefined }, first ? "read" : "local");
    flushPageNames();
  // The spans by their words: a new list at each drawing.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, title, spansKey, icon, iconType, updatedAt, Boolean(name)]);
}

/** A page's icon as its places draw it (PageIconGlyph), with its ID: the icon follows the page's wherever it changes. */
export function pageIconSource(page: Pick<ModelPage, "id" | "icon" | "iconType" | "workspaceId" | "kind" | "notionIcon" | "hasContent">) {
  return {
    id: page.id,
    icon: page.icon ?? "",
    iconType: page.iconType,
    workspaceId: page.workspaceId,
    kind: page.kind,
    notionIcon: page.notionIcon,
    hasContent: page.hasContent,
  };
}
