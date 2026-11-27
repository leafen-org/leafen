import { useEffect, useRef, useState } from "react";
import { api } from "@/api";
import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import type { Session } from "@/types";
import type {
  WorkspaceDocument, WorkspaceDocumentActor, WorkspaceDocumentKind, WorkspaceDocumentSort,
} from "@/workspaceMembershipWire.generated";

const DOCUMENTS_CHOICE_KEY = "leafen:library-documents:v1";
/** Stretches read in a row while each comes back with no document the reader may see. */
const EMPTY_STRETCHES = 20;

/** The kinds the "형식" choice offers; "한글" is both of Hangul's. */
export const LIBRARY_DOCUMENT_KINDS = {
  docx: ["docx"], xlsx: ["xlsx"], pptx: ["pptx"], pdf: ["pdf"], hangul: ["hwp", "hwpx"],
} as const satisfies Record<string, readonly WorkspaceDocumentKind[]>;

export type LibraryDocumentKindChoice = keyof typeof LIBRARY_DOCUMENT_KINDS | "";

export type LibraryDocumentsChoice = { kind: LibraryDocumentKindChoice; sort: WorkspaceDocumentSort };

function readChoice(userId: string, workspaceId: string): LibraryDocumentsChoice {
  const stored = parseStored(personValue(DOCUMENTS_CHOICE_KEY, { userId, part: workspaceId }));
  const record = stored && typeof stored === "object" ? stored as Record<string, unknown> : {};
  const kind = typeof record.kind === "string" && record.kind in LIBRARY_DOCUMENT_KINDS ? record.kind as LibraryDocumentKindChoice : "";
  return { kind, sort: record.sort === "name" ? "name" : "recent" };
}

/** The reader's "형식" and "정렬" for the documents, kept for them in this browser as the library's view settings are. */
export function useLibraryDocumentsChoice(userId: string, workspaceId: string) {
  const key = `${userId}:${workspaceId}`;
  const [state, setState] = useState(() => ({ key, choice: readChoice(userId, workspaceId) }));
  const choice = state.key === key ? state.choice : readChoice(userId, workspaceId);
  function change(next: LibraryDocumentsChoice) {
    setState({ key, choice: next });
    storePersonValue(DOCUMENTS_CHOICE_KEY, JSON.stringify(next), { userId, part: workspaceId });
  }
  return [choice, change] as const;
}

type Listed = {
  key: string;
  documents: WorkspaceDocument[];
  actors: ReadonlyMap<string, WorkspaceDocumentActor>;
  nextCursor?: string;
  loading: boolean;
  failed: boolean;
};

/**
 * The documents on the pages the reader can see, a stretch at a time: the
 * first when what is asked changes or `refresh` does, the next at
 * `loadMore`. A stretch whose blocks the reader can't see brings nothing,
 * so the next one is read on its own.
 */
export function useWorkspaceDocuments(session: Session, workspaceId: string, query: string, choice: LibraryDocumentsChoice, refresh: number) {
  const key = JSON.stringify([workspaceId, query, choice.kind, choice.sort, refresh]);
  const [listed, setListed] = useState<Listed>({ key: "", documents: [], actors: new Map(), loading: true, failed: false });
  const controller = useRef<AbortController | null>(null);

  async function read(from: Listed, cursor: string | undefined, signal: AbortSignal) {
    let documents = from.documents, actors = new Map(from.actors), next = cursor;
    for (let stretch = 0; stretch < EMPTY_STRETCHES; stretch += 1) {
      const result = await api.listWorkspaceDocuments(session, workspaceId, {
        query: query || undefined, kinds: choice.kind ? LIBRARY_DOCUMENT_KINDS[choice.kind] : undefined, sort: choice.sort, cursor: next,
      }, signal);
      documents = [...documents, ...result.documents];
      for (const actor of result.actors) actors.set(actor.userId, actor);
      next = result.nextCursor;
      if (result.documents.length > 0 || !next) break;
    }
    return { key, documents, actors, nextCursor: next, loading: false, failed: false };
  }

  useEffect(() => {
    controller.current?.abort();
    const current = new AbortController();
    controller.current = current;
    setListed({ key, documents: [], actors: new Map(), loading: true, failed: false });
    read({ key, documents: [], actors: new Map(), loading: true, failed: false }, undefined, current.signal)
      .then((next) => { if (!current.signal.aborted) setListed(next); })
      .catch(() => { if (!current.signal.aborted) setListed({ key, documents: [], actors: new Map(), loading: false, failed: true }); });
    return () => current.abort();
    // What is read is what the key names.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, session]);

  const shown = listed.key === key ? listed : { key, documents: [], actors: new Map<string, WorkspaceDocumentActor>(), loading: true, failed: false };
  function loadMore() {
    if (shown.loading || !shown.nextCursor) return;
    const current = controller.current ?? new AbortController();
    setListed({ ...shown, loading: true });
    read(shown, shown.nextCursor, current.signal)
      .then((next) => { if (!current.signal.aborted) setListed(next); })
      .catch(() => { if (!current.signal.aborted) setListed({ ...shown, loading: false, failed: true }); });
  }
  return { ...shown, loadMore };
}
