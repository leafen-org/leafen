import { useEffect, useMemo, useRef, useState } from "react";
import { Trans, useTranslation } from "react-i18next";

import { api } from "../../api";
import { LeafenApiError } from "../../apiTransport";
import { newId } from "../../lib/ids";
import { usePageNames } from "@/lib/pageNames";
import { outOfTrashIn } from "@/lib/pageIndex";
import { approveNotionOAuthRequest, readNotionOAuthRequest, type NotionOAuthRequest } from "../../notionOAuthApi";
import type { Page, Session, Workspace } from "../../types";
import styles from "./MCPAuthorizationPage.module.css";
import tree from "./NotionAuthorizationPage.module.css";
import { AppSelect } from "@/components/AppSelect";

type Requested = { clientId: string; redirectUri: string; state: string };

function decodeRequest(value: string): Requested | null {
  try {
    const parsed = JSON.parse(atob(value.replace(/-/g, "+").replace(/_/g, "/"))) as Partial<Requested>;
    return typeof parsed.clientId === "string" && typeof parsed.redirectUri === "string"
      ? { clientId: parsed.clientId, redirectUri: parsed.redirectUri, state: typeof parsed.state === "string" ? parsed.state : "" }
      : null;
  } catch {
    return null;
  }
}

function withQuery(address: string, values: Record<string, string>) {
  const target = new URL(address);
  for (const [key, value] of Object.entries(values)) if (value) target.searchParams.set(key, value);
  return target.toString();
}

/**
 * A Notion-compatible integration asking to connect, as Notion's public
 * integrations ask: the person chooses a workspace and the pages it may
 * reach — those and the pages under them, nothing when none — and allows
 * or cancels; the integration gets a code, or `access_denied`.
 */
export function NotionAuthorizationPage({ authorizationRequest, session }: { authorizationRequest: string; session: Session }) {
  const pageNames = usePageNames();
  const { t } = useTranslation("authGate");
  const headingRef = useRef<HTMLHeadingElement>(null);
  const requested = useMemo(() => decodeRequest(authorizationRequest), [authorizationRequest]);
  const [request, setRequest] = useState<NotionOAuthRequest | null>(null);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [pages, setPages] = useState<Page[]>([]);
  const [workspaceId, setWorkspaceId] = useState("");
  const [chosen, setChosen] = useState<string[]>([]);
  const [open, setOpen] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!requested) {
      setError(t("notionConsent.loadFailed"));
      return;
    }
    const controller = new AbortController();
    void (async () => {
      try {
        const asked = await readNotionOAuthRequest(session, requested.clientId, requested.redirectUri, controller.signal);
        // The workspaces and pages the person sees, read to the end.
        const seenWorkspaces: Workspace[] = [], seenPages: Page[] = [];
        let cursor: string | null = null;
        do {
          const read = await api.bootstrap(session, cursor, controller.signal);
          for (const workspace of read.workspaces) if (!seenWorkspaces.some(({ id }) => id === workspace.id)) seenWorkspaces.push(workspace);
          seenPages.push(...read.pages);
          cursor = read.hasMore ? read.nextCursor : null;
        } while (cursor);
        setRequest(asked);
        setWorkspaces(seenWorkspaces);
        setPages(seenPages.filter(outOfTrashIn(seenPages)));
        setWorkspaceId(seenWorkspaces[0]?.id ?? "");
        window.requestAnimationFrame(() => headingRef.current?.focus());
      } catch {
        if (!controller.signal.aborted) setError(t("notionConsent.loadFailed"));
      }
    })();
    return () => controller.abort();
  }, [requested, session, t]);

  const inWorkspace = useMemo(() => pages.filter((page) => page.workspaceId === workspaceId), [pages, workspaceId]);
  const children = useMemo(() => {
    const ids = new Set(inWorkspace.map(({ id }) => id));
    const byParent = new Map<string, Page[]>();
    for (const page of inWorkspace) {
      const parent = page.parentId && ids.has(page.parentId) ? page.parentId : "";
      byParent.set(parent, [...byParent.get(parent) ?? [], page]);
    }
    for (const list of byParent.values()) list.sort((left, right) => left.position - right.position);
    return byParent;
  }, [inWorkspace]);
  // A page under a chosen one is reached already.
  const parentOf = useMemo(() => new Map(inWorkspace.map((page) => [page.id, page.parentId ?? ""])), [inWorkspace]);
  const underChosen = (id: string) => {
    for (let parent = parentOf.get(id); parent; parent = parentOf.get(parent)) if (chosen.includes(parent)) return true;
    return false;
  };
  const query = search.trim().toLowerCase();
  const found = query ? inWorkspace.filter((page) => pageNames.title(page).toLowerCase().includes(query)) : null;

  function choose(id: string, checked: boolean) {
    setChosen((current) => checked ? [...current.filter((other) => other !== id), id] : current.filter((other) => other !== id));
  }

  function row(page: Page, depth: number) {
    const below = children.get(page.id) ?? [];
    const expanded = open.includes(page.id);
    const covered = underChosen(page.id);
    return (
      <li key={page.id}>
        <div className={tree.row} style={{ paddingInlineStart: depth * 18 }}>
          {below.length && !found ? (
            <button type="button" className={tree.toggle} aria-expanded={expanded}
              aria-label={t(expanded ? "notionConsent.collapse" : "notionConsent.expand", { page: pageNames.title(page) })}
              onClick={() => setOpen((current) => expanded ? current.filter((id) => id !== page.id) : [...current, page.id])}>
              {expanded ? "▾" : "▸"}
            </button>
          ) : <span className={tree.toggle} aria-hidden="true" />}
          <label className={tree.label}>
            <input type="checkbox" checked={covered || chosen.includes(page.id)} disabled={covered}
              onChange={(event) => choose(page.id, event.target.checked)} />
            <span>{pageNames.title(page)}</span>
          </label>
        </div>
        {expanded && !found && below.length ? <ul className={tree.list}>{below.map((child) => row(child, depth + 1))}</ul> : null}
      </li>
    );
  }

  async function allow() {
    if (!request || !requested || !workspaceId || busy) return;
    setBusy(true);
    setError("");
    try {
      const approved = await approveNotionOAuthRequest(session, {
        requestId: newId(), clientId: requested.clientId, redirectUri: request.redirectUri,
        workspaceId, state: requested.state, resourceIds: chosen.filter((id) => !underChosen(id)),
      });
      window.location.assign(approved.redirectUrl);
    } catch (cause) {
      setError(t(cause instanceof LeafenApiError && cause.status === 404 ? "notionConsent.notAllowed" : "notionConsent.approveFailed"));
      setBusy(false);
    }
  }

  function cancel() {
    if (!request || !requested) return;
    window.location.assign(withQuery(request.redirectUri, { error: "access_denied", state: requested.state }));
  }

  return (
    <main className={styles.layout}>
      <section className={styles.card} aria-labelledby="notion-authorization-title" data-notion-consent>
        <img className={styles.mark} src="/mark-128.png" alt="" width="48" height="48" />
        <h1 id="notion-authorization-title" ref={headingRef} tabIndex={-1}>
          {request ? t("notionConsent.title", { client: request.clientName }) : t("notionConsent.titlePending")}
        </h1>
        {request ? (
          <>
            <p className={styles.intro}>
              <Trans t={t} i18nKey="notionConsent.intro" values={{ client: request.clientName }}
                components={{ client: <span className={styles.clientName} /> }} />
            </p>
            {workspaces.length > 1 ? (
              <label className={tree.field}>
                <span>{t("notionConsent.workspace")}</span>
                <AppSelect aria-label={t("notionConsent.workspace")} value={workspaceId} disabled={busy} onChange={(event) => { setWorkspaceId(event.target.value); setChosen([]); setOpen([]); }}>
                  {workspaces.map((workspace) => <option key={workspace.id} value={workspace.id}>{workspace.name}</option>)}
                </AppSelect>
              </label>
            ) : null}
            <fieldset className={styles.workspaceSet} disabled={busy || !workspaceId}>
              <legend>{t("notionConsent.pages")}</legend>
              <input className={tree.search} type="search" value={search} placeholder={t("notionConsent.search")}
                aria-label={t("notionConsent.search")} onChange={(event) => setSearch(event.target.value)} />
              <ul className={tree.list} data-notion-consent-pages>
                {(found ?? children.get("") ?? []).map((page) => row(page, 0))}
              </ul>
              {(found ?? children.get("") ?? []).length === 0 ? <p className={styles.permissionNote}>{t("notionConsent.noPages")}</p> : null}
            </fieldset>
            <p className={styles.permissionNote} data-notion-consent-chosen={chosen.length}>
              {chosen.length ? t("notionConsent.chosen", { count: chosen.length }) : t("notionConsent.noneChosen")}
            </p>
            <div className={styles.actions}>
              <button className={styles.deny} type="button" onClick={cancel} disabled={busy}>{t("notionConsent.cancel")}</button>
              <button className={styles.allow} type="button" onClick={() => void allow()} disabled={busy || !workspaceId}>
                {busy ? t("notionConsent.processing") : t("notionConsent.allow")}
              </button>
            </div>
          </>
        ) : !error ? <p className={styles.status} role="status">{t("notionConsent.loading")}</p> : null}
        {error && <p className={styles.error} role="alert">{error}</p>}
      </section>
    </main>
  );
}
