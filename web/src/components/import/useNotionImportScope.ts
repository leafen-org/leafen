import { api } from "@/api";
import { notionRequestFailureText, notionTokenRejected } from "@/lib/notionImportFailure";
import { useReadGeneration } from "@/lib/useReadGeneration";
import type { NativeNotionImportConnection, NativeNotionImportRoot } from "@/notionImportWire.generated";
import type { Session } from "@/types";
import type { TFunction } from "i18next";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";

type NotionImportScope = Awaited<ReturnType<typeof api.notionImportScope>>;

/**
 * The last read of the list that failed: the list itself (`list` — the top
 * ones, or what `query` finds) or "더 찾기"'s next part (`more`), and what
 * its line says.
 */
export type NotionImportScopeFailure = { at: "list" | "more"; query: string; text: string };

/** A failed read's line: the words searched for, then why Notion's request failed. */
function scopeFailureText(t: TFunction, error: unknown, query: string) {
  const why = notionRequestFailureText(t, error, query ? "" : t("importDialog:cantScanRoots"), true);
  return query ? [t("importDialog:rootSearchFailed", { query }), why].filter(Boolean).join(" ") : why;
}

function mergeNotionImportRoots(
  current: NativeNotionImportRoot[],
  next: NativeNotionImportRoot[],
) {
  const roots = new Map(current.map((root) => [root.id, root]));
  for (const root of next) roots.set(root.id, root);
  return [...roots.values()];
}

/**
 * The roots' step of Notion's window: the list it shows — what the chosen
 * connection reaches at the top, or what words typed find at any depth —
 * and the roots chosen from it.
 *
 * The top ones are read once per connection, and that read goes on by
 * itself: words typed while it still reads are searched at once, and the
 * top ones, when they come, are kept for the words cleared and never put
 * over the search's answer (2026-11-13: with Notion slow, the words typed
 * stayed in the field and the list came as the top ones). The connection
 * is the window's as soon as Notion answers for it, the search's answer or
 * the top ones', whichever comes first (`confirm`); a token Notion refuses
 * goes back to ask for a new one (`refuse`).
 *
 * A read of the list that fails is the list's state until the list is read
 * again (`failure`): a search that failed shows no results — not the list
 * shown before it under "검색 결과" — and says the words with a way to try
 * again (`retryScope`); "더 찾기"'s failure keeps the list (2026-11-13).
 */
export function useNotionImportScope({ confirm, open, refuse, session, workspaceId }: {
  confirm: (connection: NativeNotionImportConnection) => void;
  open: boolean;
  refuse: (connection: NativeNotionImportConnection) => void;
  session: Session;
  workspaceId: string;
}) {
  const { t } = useTranslation(["importDialog", "common"]);
  const [roots, setRoots] = useState<NativeNotionImportRoot[]>([]);
  // The chosen roots themselves, kept while a search shows other pages.
  const [selectedRoots, setSelectedRoots] = useState<NativeNotionImportRoot[]>([]);
  // The words asked for, and the words whose answer the list shows: the list
  // shown before stays until the new words' answer comes.
  const [scopeQuery, setScopeQuery] = useState("");
  const [listQuery, setListQuery] = useState("");
  const [scopeCursor, setScopeCursor] = useState("");
  const [scopeHasMore, setScopeHasMore] = useState(false);
  // The list shown is being read.
  const [reading, setReading] = useState(false);
  const [failure, setFailure] = useState<NotionImportScopeFailure | null>(null);
  // The connection the step reads, from the press on, before Notion answers for it.
  const target = useRef<NativeNotionImportConnection | null>(null);
  // The connection Notion has answered for.
  const confirmed = useRef("");
  // What it reaches at the top, and the read of it while that goes on.
  const top = useRef<{ connectionId: string; scope: NotionImportScope | null; read: Promise<NotionImportScope> | null } | null>(null);
  const consumedScopeCursors = useRef(new Set<string>());
  const route = `${open ? "open" : "closed"}:${workspaceId}`;
  // What the list shows: a newer read of it retires the one before.
  const beginListRead = useReadGeneration({ principalId: session.user.id, sessionToken: session.token, route: `notion-scope:${route}` });
  // The top ones' read, which a search doesn't stop.
  const beginTopRead = useReadGeneration({ principalId: session.user.id, sessionToken: session.token, route: `notion-top:${route}` });

  function showScope(scope: NotionImportScope, cursor = "") {
    if (scope.hasMore && (
      !scope.nextCursor
      || scope.nextCursor === cursor
      || consumedScopeCursors.current.has(scope.nextCursor)
    )) throw new Error("Notion import scope cursor stalled.");
    setRoots((current) => mergeNotionImportRoots(cursor ? current : [], scope.items));
    setScopeCursor(scope.nextCursor);
    setScopeHasMore(scope.hasMore);
  }

  /** The top ones of the connection: kept, the read of them going on, or a read begun (from the saved connection's check while that still reads). */
  function readTop(chosen: NativeNotionImportConnection, checking: Promise<NotionImportScope> | null = null) {
    const kept = top.current?.connectionId === chosen.id ? top.current : null;
    if (kept?.scope) return Promise.resolve(kept.scope);
    if (kept?.read) return kept.read;
    const generation = beginTopRead();
    const fresh = () => api.notionImportScope(session, workspaceId, chosen.id, "", { signal: generation.signal });
    // A check cut off before Notion answered (the window put away) is read
    // again; Notion's answer to it, a failure too, is the read's: a read that
    // waited out Notion's request limit isn't waited out twice.
    const read = checking
      ? checking.catch((error: unknown) => {
        if ((error as { name?: unknown } | null)?.name !== "AbortError") throw error;
        return fresh();
      })
      : fresh();
    const entry = { connectionId: chosen.id, scope: null as NotionImportScope | null, read: read as Promise<NotionImportScope> | null };
    top.current = entry;
    // A read that failed is read again when it is asked for next.
    read.then((scope) => { entry.scope = scope; entry.read = null; }, () => { if (top.current === entry) top.current = null; });
    return read;
  }

  /** How a read for the chosen connection ended: Notion answered for it, or refuses its token. */
  function answered(chosen: NativeNotionImportConnection, error?: unknown) {
    if (target.current?.id !== chosen.id) return;
    if (error === undefined && confirmed.current !== chosen.id) {
      confirmed.current = chosen.id;
      confirm(chosen);
    } else if (error !== undefined && notionTokenRejected(error)) {
      confirmed.current = "";
      refuse(chosen);
    }
  }

  /** "옮길 항목 선택": the roots' step reads what the connection reaches at the top, once per connection. */
  async function chooseScope(chosen: NativeNotionImportConnection, checking: Promise<NotionImportScope> | null) {
    // Back and on again keeps the list, and a read of it that goes on.
    if (target.current?.id === chosen.id && top.current?.connectionId === chosen.id) return;
    target.current = chosen;
    const listRead = beginListRead();
    consumedScopeCursors.current.clear();
    setScopeQuery("");
    setListQuery("");
    setRoots([]);
    setScopeCursor("");
    setScopeHasMore(false);
    setFailure(null);
    setReading(true);
    try {
      const scope = await readTop(chosen, checking);
      answered(chosen);
      if (listRead.isCurrent()) showScope(scope);
    } catch (error) {
      answered(chosen, error);
      if (listRead.isCurrent() && !notionTokenRejected(error)) {
        setFailure({ at: "list", query: "", text: scopeFailureText(t, error, "") });
      }
    } finally {
      if (listRead.isCurrent()) setReading(false);
    }
  }

  /** Finds roots by title at any depth, or lists the top ones again when the words are cleared. */
  async function searchScope(query: string) {
    const chosen = target.current;
    if (!chosen) return;
    const listRead = beginListRead();
    consumedScopeCursors.current.clear();
    setScopeQuery(query);
    setFailure(null);
    const kept = !query && top.current?.connectionId === chosen.id ? top.current.scope : null;
    if (kept) {
      showScope(kept);
      setListQuery("");
      setReading(false);
      return;
    }
    if (!query) {
      // The top ones still read: the list waits for them.
      setListQuery("");
      setRoots([]);
      setScopeCursor("");
      setScopeHasMore(false);
    }
    setReading(true);
    try {
      const scope = query
        ? await api.notionImportScope(session, workspaceId, chosen.id, "", { signal: listRead.signal, query })
        : await readTop(chosen);
      answered(chosen);
      if (listRead.isCurrent()) {
        showScope(scope);
        setListQuery(query);
      }
    } catch (error) {
      if (listRead.signal.aborted) return;
      answered(chosen, error);
      if (listRead.isCurrent() && !notionTokenRejected(error)) {
        // What showed before isn't what the words find.
        setRoots([]);
        setScopeCursor("");
        setScopeHasMore(false);
        setListQuery(query);
        setFailure({ at: "list", query, text: scopeFailureText(t, error, query) });
      }
    } finally {
      if (listRead.isCurrent()) setReading(false);
    }
  }

  async function loadMoreScope() {
    const chosen = target.current;
    if (!chosen || !scopeHasMore || reading) return;
    if (consumedScopeCursors.current.has(scopeCursor)) {
      setScopeHasMore(false);
      setFailure({ at: "more", query: scopeQuery, text: scopeQuery ? t("importDialog:rootSearchFailed", { query: scopeQuery }) : t("importDialog:cantScanRoots") });
      return;
    }
    const cursor = scopeCursor;
    const query = scopeQuery;
    consumedScopeCursors.current.add(cursor);
    const listRead = beginListRead();
    setReading(true);
    setFailure(null);
    try {
      const scope = await api.notionImportScope(session, workspaceId, chosen.id, cursor, { signal: listRead.signal, query });
      if (!listRead.isCurrent()) return;
      showScope(scope, cursor);
      const kept = top.current?.connectionId === chosen.id ? top.current.scope : null;
      if (!query && kept && top.current) top.current.scope = { ...scope, items: mergeNotionImportRoots(kept.items, scope.items) };
    } catch (error) {
      consumedScopeCursors.current.delete(cursor);
      if (listRead.isCurrent()) setFailure({ at: "more", query, text: scopeFailureText(t, error, query) });
    } finally {
      if (listRead.isCurrent()) setReading(false);
    }
  }

  /** "다시 시도" under a list that failed: the same read again — the words, or the top ones. */
  function retryScope() {
    if (failure?.at === "list") void searchScope(failure.query);
  }

  function selectRoot(rootId: string) {
    setSelectedRoots((chosen) => {
      if (chosen.some((root) => root.id === rootId)) return chosen.filter((root) => root.id !== rootId);
      const root = roots.find((candidate) => candidate.id === rootId);
      return root ? [...chosen, root] : chosen;
    });
  }

  /** Forgets the list, the chosen roots and the top ones read: the window opened again, or another connection. */
  function resetScope() {
    beginListRead();
    beginTopRead();
    target.current = null;
    confirmed.current = "";
    top.current = null;
    consumedScopeCursors.current.clear();
    setRoots([]);
    setSelectedRoots([]);
    setScopeQuery("");
    setListQuery("");
    setScopeCursor("");
    setScopeHasMore(false);
    setReading(false);
    setFailure(null);
  }

  return {
    chooseScope,
    listQuery,
    loadMoreScope,
    readingScope: reading,
    resetScope,
    retryScope,
    roots,
    scopeFailure: failure,
    scopeHasMore,
    scopeQuery,
    searchScope,
    selectRoot,
    selectedRoots,
  };
}
