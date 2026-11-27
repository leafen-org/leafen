import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { i18next } from "@/i18n";
import { useOptionalProductToast } from "@/lib/productToast";
import type { Session } from "@/types";
import { workspaceMembershipErrorCodes, type SidebarLayout } from "@/workspaceMembershipWire.generated";
import { mergeSidebarLayouts, sameSidebarLayout } from "./sidebarLayoutMerge";
import { DEFAULT_SIDEBAR_LAYOUT } from "./sidebarLayoutModel";

// One layout per reader and workspace, shared by everything that shows or
// changes it — the sidebar and the library's teamspaces order alike — so a
// change made in one is the layout the other saves next.
const layouts = new Map<string, SidebarLayout>();
const listeners = new Set<() => void>();

/**
 * What this screen knows of the server's layout: the one it last read or
 * kept (`kept`, with its revision), the save going out (one at a time, each
 * sending the layout as it then is) and the revision a read is wanted for
 * (Infinity when not known, 0 for none), read once no save or read goes, if
 * this screen hasn't kept that revision meanwhile.
 */
type LayoutFollow = {
  session: Session;
  workspaceId: string;
  kept?: SidebarLayout;
  saving: Promise<void> | null;
  wanted: number;
  reading: boolean;
};
const follows = new Map<string, LayoutFollow>();
// A save that keeps meeting a newer layout stops after this many tries; the
// next change or read sends it again.
const SAVE_TRIES = 5;

function publish(key: string, layout: SidebarLayout) {
  layouts.set(key, layout);
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function layoutKey(session: Session, workspaceId: string) {
  return `${session.user.id}\u0000${workspaceId}`;
}

function follow(session: Session, workspaceId: string): LayoutFollow {
  const key = layoutKey(session, workspaceId);
  const known = follows.get(key);
  if (known) {
    // A renewed sign-in saves with its new token.
    known.session = session;
    return known;
  }
  const started: LayoutFollow = { session, workspaceId, saving: null, wanted: 0, reading: false };
  follows.set(key, started);
  return started;
}

function revisionOf(layout: SidebarLayout | undefined) {
  return layout?.revision ?? 0;
}

/**
 * The server's layout as read: shown, unless this screen already has that
 * revision or a later one. A change made here and not kept yet (a save that
 * couldn't go) goes on top of it and is sent again; while a save goes, it is
 * read again once that save is done.
 */
function receive(key: string, read: SidebarLayout) {
  const state = follows.get(key);
  if (!state) return;
  if (state.saving) {
    state.wanted = Math.max(state.wanted, revisionOf(read));
    return;
  }
  const before = state.kept;
  if (before && revisionOf(read) <= revisionOf(before)) return;
  state.kept = read;
  const shown = layouts.get(key);
  const base = before ?? DEFAULT_SIDEBAR_LAYOUT;
  if (!shown || sameSidebarLayout(shown, base)) {
    publish(key, read);
    return;
  }
  publish(key, mergeSidebarLayouts(base, shown, read));
  void save(key).catch(() => undefined);
}

/**
 * Reads the layout again, as the socket says it changed to a revision —
 * unless this screen already keeps that revision, as one it saved itself.
 */
async function readAgain(key: string, revision = Infinity) {
  const state = follows.get(key);
  if (!state) return;
  state.wanted = Math.max(state.wanted, revision);
  if (state.saving || state.reading) return;
  const wanted = state.wanted;
  state.wanted = 0;
  if (wanted <= revisionOf(state.kept)) return;
  state.reading = true;
  try {
    receive(key, await api.readSidebarLayout(state.session, state.workspaceId));
  } catch {
    // The layout shown stays; the next change or read tries again.
  } finally {
    state.reading = false;
    readLaterIfAsked(key, state);
  }
}

function readLaterIfAsked(key: string, state: LayoutFollow) {
  if (state.wanted !== 0 && !state.saving && !state.reading) void readAgain(key, 0);
}

/**
 * Keeps the layout shown on the server, on the revision this screen read or
 * kept last. One changed meanwhile elsewhere is refused (409); it is read
 * again, this screen's changes are put on it (sidebarLayoutMerge.ts) and
 * sent again, so neither side's change is lost. Changes made while a save
 * goes are sent together after it.
 */
function save(key: string): Promise<void> {
  const state = follows.get(key);
  if (!state) return Promise.resolve();
  if (state.saving) return state.saving;
  const going = (async () => {
    // Past this task, so `saving` names this save before it can end.
    await Promise.resolve();
    try {
      for (let tries = 0; tries < SAVE_TRIES; tries += 1) {
        const base = state.kept ?? DEFAULT_SIDEBAR_LAYOUT;
        const mine = layouts.get(key);
        if (!mine || sameSidebarLayout(mine, base)) return;
        try {
          const kept = await api.saveSidebarLayout(state.session, state.workspaceId, { ...mine, revision: revisionOf(state.kept) });
          state.kept = kept;
          // The server's completed layout is shown unless a change came meanwhile, sent next.
          if (layouts.get(key) === mine) {
            publish(key, kept);
            return;
          }
        } catch (error) {
          if (!(error instanceof LeafenApiError) || error.code !== workspaceMembershipErrorCodes.sidebarLayoutRevisionConflict) throw error;
          const now = await api.readSidebarLayout(state.session, state.workspaceId);
          state.kept = now;
          publish(key, mergeSidebarLayouts(base, layouts.get(key) ?? mine, now));
        }
      }
    } finally {
      state.saving = null;
      readLaterIfAsked(key, state);
    }
  })();
  state.saving = going;
  return going;
}

/**
 * The reader's layout in a workspace is read again: the server said another
 * screen, device or agent changed it to `revision`, or the screen came back
 * into view (product/useWorkspaceChangeFollow.ts) and doesn't know.
 */
export function readSidebarLayoutAgain(session: Session, workspaceId: string, revision = Infinity) {
  void readAgain(layoutKey(session, workspaceId), revision);
}

/** The layout as already read, for what follows it without reading it itself (the top bar's teamspace menu). */
export function useReadSidebarLayout(userId: string, workspaceId: string): SidebarLayout | undefined {
  const key = userId && workspaceId ? `${userId}\u0000${workspaceId}` : "";
  return useSyncExternalStore(subscribe, () => (key ? layouts.get(key) : undefined), () => undefined);
}

/**
 * Changes the reader's layout from outside the sidebar, as a database's
 * "사이드바에 단축키 추가" does: the layout as already read — read first when
 * it isn't, so the server's is never replaced by the default — shown at once
 * and kept on the server. Answers whether it changed.
 */
export async function changeSidebarLayout(session: Session, workspaceId: string, update: (layout: SidebarLayout) => SidebarLayout) {
  const key = layoutKey(session, workspaceId);
  follow(session, workspaceId);
  if (!layouts.has(key)) receive(key, await api.readSidebarLayout(session, workspaceId));
  const current = layouts.get(key) ?? DEFAULT_SIDEBAR_LAYOUT;
  const next = update(current);
  if (next === current) return false;
  publish(key, next);
  await save(key);
  return true;
}

// Where a page let go among the favorites joins them (placeNextFavorite), for as long as its favorite is on its way.
const favoritePlaces = new Map<string, { order: readonly string[]; at: number }>();
const FAVORITE_PLACE_KEPT_MS = 60_000;

/** The order the favorites take once `key` joins them where a drop put it, not first (SidebarPageSections). */
export function placeNextFavorite(key: string, order: readonly string[]) {
  favoritePlaces.set(key, { order, at: Date.now() });
}

/**
 * A favorite just added comes first among the reader's favorites, as Notion's
 * (seen 2026-11-06): a page's id, or a database's key (favoriteOrderKey);
 * one a drop put among them stays where it was let go (placeNextFavorite).
 * The order keeps the latest ones only, so it stays a small part of the layout.
 */
export function putFavoriteFirst(session: Session, workspaceId: string, key: string) {
  const placed = favoritePlaces.get(key);
  favoritePlaces.delete(key);
  void changeSidebarLayout(session, workspaceId, (layout) => {
    const order = layout.favoriteOrder ?? [];
    if (placed && Date.now() - placed.at < FAVORITE_PLACE_KEPT_MS) return { ...layout, favoriteOrder: placed.order.slice(0, FAVORITE_ORDER_KEPT) };
    if (order[0] === key) return layout;
    return { ...layout, favoriteOrder: [key, ...order.filter((item) => item !== key)].slice(0, FAVORITE_ORDER_KEPT) };
  }).catch(() => undefined);
}
const FAVORITE_ORDER_KEPT = 200;

/**
 * The reader's sidebar layout in a workspace: the default until the server's
 * arrives, then each change shown at once and kept on the server, and a
 * change made on another screen, device or by an agent shown when the server
 * says so. A renewed sign-in reads it again without going back to the
 * default. `loaded` says the server's answered (or couldn't be read, leaving
 * the default).
 */
export function useSidebarLayout(session: Session | null, workspaceId: string | null) {
  const key = session && workspaceId ? layoutKey(session, workspaceId) : "";
  const stored = useSyncExternalStore(subscribe, () => (key ? layouts.get(key) : undefined), () => undefined);
  const [unreadable, setUnreadable] = useState("");
  const toast = useOptionalProductToast();
  const latest = useRef({ session, workspaceId });
  latest.current = { session, workspaceId };
  useEffect(() => {
    if (!session || !workspaceId) return undefined;
    follow(session, workspaceId);
    const controller = new AbortController();
    api.readSidebarLayout(session, workspaceId, controller.signal)
      .then((read) => receive(key, read))
      .catch(() => {
        // The default stays when the layout can't be read.
        if (!controller.signal.aborted) setUnreadable(key);
      });
    return () => controller.abort();
  }, [key, session, workspaceId]);

  function change(next: SidebarLayout) {
    const { session: current, workspaceId: workspace } = latest.current;
    if (!key) return;
    publish(key, next);
    if (!current || !workspace) return;
    follow(current, workspace);
    void save(key).catch((error: unknown) => {
      // The change stays here; the next one or the next read sends it again.
      // A layout too large to keep is said, since sending it again won't do.
      const failure = error as { code?: unknown; status?: unknown } | null;
      if (failure?.code === workspaceMembershipErrorCodes.sidebarLayoutTooLarge || failure?.status === 413) {
        toast?.notify(i18next.t("sidebar:layoutTooLarge"), "error");
      }
    });
  }

  return {
    layout: stored ?? DEFAULT_SIDEBAR_LAYOUT,
    loaded: key !== "" && (stored !== undefined || unreadable === key),
    change,
  };
}
