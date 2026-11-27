import { createContext, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import type { Session } from "@/types";
import { createPageActionAccess, type PageActionAccessReader } from "./pageActionAccess";

const PageActionAccessContext = createContext<PageActionAccessReader | null>(null);

const noSubscribe = () => () => {};

/**
 * Whether the person has the page's full access, read while active; null
 * where no account's access owner is (a screen outside the product).
 */
export function usePageFullAccess(pageId: string, active: boolean): boolean | null {
  const access = useContext(PageActionAccessContext);
  const manages = useSyncExternalStore(access?.subscribe ?? noSubscribe, () => access?.canManage(pageId) ?? false, () => false);
  const snapshot = useSyncExternalStore(access?.subscribe ?? noSubscribe, () => access?.snapshot() ?? 0, () => 0);
  useEffect(() => {
    if (active && access) void access.read(pageId).catch(() => undefined);
  }, [access, active, pageId, snapshot]);
  return access ? manages : null;
}

/**
 * Whether the person may edit the page, read while active; null where no
 * account's access owner is (a screen outside the product).
 */
export function usePageEditAccess(pageId: string, active: boolean): boolean | null {
  const access = useContext(PageActionAccessContext);
  const edits = useSyncExternalStore(access?.subscribe ?? noSubscribe, () => Boolean(pageId) && (access?.canEdit(pageId) ?? false), () => false);
  const snapshot = useSyncExternalStore(access?.subscribe ?? noSubscribe, () => access?.snapshot() ?? 0, () => 0);
  useEffect(() => {
    if (active && access && pageId) void access.read(pageId).catch(() => undefined);
  }, [access, active, pageId, snapshot]);
  return access ? edits : null;
}

/**
 * Whether a guest's page menu offers what changes the page above, as
 * Notion's (seen 2026-11-16, a guest editing the page): one who edits the
 * page above has "복제", which copies the page beside it, and
 * "휴지통으로 이동"; where they only read the page above, or the page tops
 * what is shared with them, "복제" is gone and "휴지통으로 이동" dimmed (the
 * server's page_metadata_guest.go and page_duplication_guest.go refuse the
 * same). True for a member, whose page alone decides.
 */
export function useGuestEditsPageAbove(parentId: string | null | undefined, guest: boolean): boolean {
  const above = guest ? parentId ?? "" : "";
  const editable = usePageEditAccess(above, above !== "");
  return !guest || (above !== "" && editable === true);
}

export function pageActionHintKey(canEdit: boolean, favoritesList = false) {
  if (!canEdit) return "openActions";
  return favoritesList ? "favoriteActionsTitle" : "actionsTitle";
}

export function PageActionAccessProvider({ session, revision, children }: {
  session: Session;
  revision: number;
  children: ReactNode;
}) {
  const access = useMemo(() => createPageActionAccess(session), [session]);
  useLayoutEffect(() => {
    access.activate();
    return () => access.cancel();
  }, [access]);
  const readAt = useRef(revision);
  useLayoutEffect(() => {
    if (readAt.current === revision) return;
    readAt.current = revision;
    access.invalidate();
  }, [access, revision]);
  return <PageActionAccessContext.Provider value={access}>{children}</PageActionAccessContext.Provider>;
}

/** What holds the page's changes back for now, as its access read named it; empty if nothing. */
export function usePageHeldBy(pageId: string): string {
  const access = useContext(PageActionAccessContext);
  return useSyncExternalStore(access?.subscribe ?? noSubscribe, () => access?.busy(pageId) ?? "");
}

export function usePageActionAccessReader() {
  const access = useContext(PageActionAccessContext);
  if (!access) throw new Error("Page actions require their current account's access owner.");
  return access;
}

export function usePageActionAccess(pageId: string, active = false) {
  const access = usePageActionAccessReader();
  const [approached, setApproached] = useState(false);
  const canEdit = useSyncExternalStore(access.subscribe, () => access.canEdit(pageId), () => false);
  const canComment = useSyncExternalStore(access.subscribe, () => access.canComment(pageId), () => false);
  const canManage = useSyncExternalStore(access.subscribe, () => access.canManage(pageId), () => false);
  const known = useSyncExternalStore(access.subscribe, () => access.knows(pageId), () => false);
  // Only a reader that reads the page's access (active, or approached) reads
  // it again for a new snapshot, so only such a reader draws again for one:
  // a sidebar's other rows stay as they are.
  const reads = active || approached;
  const snapshot = useSyncExternalStore(access.subscribe, () => (reads ? access.snapshot() : -1), () => 0);
  useEffect(() => {
    if (active || approached) void access.read(pageId).catch(() => undefined);
  }, [active, approached, access, pageId, snapshot]);
  return {
    canEdit,
    canComment,
    canManage,
    known,
    approach() {
      setApproached(true);
      // Failed reads may be tried again when the person returns to the row.
      void access.read(pageId).catch(() => undefined);
    },
  };
}

/** The longest a menu waits unseen for the access its rows follow, from its
 * opening: it still answers its press within the press's 150 ms, a frame left
 * to draw it (user decision 2026-11-12: a press answers on the next frame).
 * Over a 100 ms round trip an access read takes some 240 ms, so there it shows
 * what it has by then and grows as the answer comes (2026-11-21, NAS
 * condition, CPU ×2: a sidebar row right-pressed as the pointer came to it
 * showed its four rows at 136 ms, its ten at 245, where it showed them at 98
 * and 237 without the wait); a server near at hand answers within it. */
export const ACCESS_HOLD_MS = 120;

/**
 * Whether a menu whose rows follow the person's access, opened before that
 * access is read, waits unseen for it, so it is first drawn with the rows it
 * keeps, whole and in its place, as Notion's menus open (seen 2026-11-21: a
 * sidebar row right-pressed the moment the pointer came to it — when Leafen
 * starts reading the row's access — drew favorite, copy link and the two
 * opens over the point, then grew to its ten rows a frame or two later, its
 * top jumping from 443 to 182; a view section's row "⋯" drew its four rows
 * for the frame its row's page took to be read, then its eight). `opening`
 * names the opening, null while closed; it waits while `waiting`, until
 * what it waits for comes, `read` settles, or ACCESS_HOLD_MS pass from the
 * opening's first drawing. What it shows then grows as the rest comes,
 * placed again in that task (popoverPlaceAgain.ts).
 */
export function useAccessHold(opening: string | null, waiting: boolean, read?: () => Promise<unknown>): boolean {
  const [released, setReleased] = useState<string | null>(null);
  const readRef = useRef(read);
  readRef.current = read;
  // When this opening was first drawn, which its wait counts from.
  const opened = useRef<{ opening: string | null; at: number }>({ opening: null, at: 0 });
  if (opened.current.opening !== opening) opened.current = { opening, at: performance.now() };
  const holds = opening !== null && waiting && released !== opening;
  useEffect(() => {
    if (!holds || opening === null) return undefined;
    let live = true;
    const release = () => { if (live) setReleased(opening); };
    const timer = window.setTimeout(release, Math.max(0, ACCESS_HOLD_MS - (performance.now() - opened.current.at)));
    void readRef.current?.().then(release, release);
    return () => { live = false; window.clearTimeout(timer); };
  }, [holds, opening]);
  useEffect(() => { if (opening === null) setReleased(null); }, [opening]);
  return holds;
}

/** How a held menu stands: placed as it is, unseen and out of the pointer's way (useAccessHold). */
export const accessHeldStyle = { opacity: 0, pointerEvents: "none" } as const;

/** useAccessHold for a menu whose rows follow the page's access, while it is open (`pageId` given). */
export function usePageAccessHold(pageId: string | undefined): boolean {
  const access = useContext(PageActionAccessContext);
  const unknown = useSyncExternalStore(access?.subscribe ?? noSubscribe, () => Boolean(access && pageId && !access.knows(pageId)), () => false);
  return useAccessHold(pageId ?? null, unknown, () => (access && pageId ? access.read(pageId) : Promise.resolve()));
}

/**
 * Whether the person surely can't edit a page: its access was read and says
 * so. The pages asked about are read, once each for every reader of them;
 * a page not read yet counts as one they may edit until its answer comes.
 * Outside the product (no access owner) nothing is refused.
 */
export function usePageEditRefused(readPageIds: readonly string[]): (pageId: string) => boolean {
  const access = useContext(PageActionAccessContext);
  const version = useSyncExternalStore(access?.subscribe ?? noSubscribe, () => access?.snapshot() ?? 0, () => 0);
  // Each answer that comes in gives a new judge, which those who use it draw again with.
  const [answers, redraw] = useState(0);
  useEffect(() => access?.subscribe(() => redraw((count) => count + 1)), [access]);
  const key = readPageIds.join(" ");
  useEffect(() => {
    if (!access) return;
    for (const pageId of key.split(" ")) if (pageId) void access.read(pageId).catch(() => undefined);
  }, [access, key, version]);
  // eslint-disable-next-line react-hooks/exhaustive-deps -- a new answer is a new judge
  return useMemo(() => (pageId: string) => Boolean(access?.knows(pageId) && !access.canEdit(pageId)), [access, answers, version]);
}
