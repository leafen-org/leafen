import { FormEvent, useEffect, useRef, useState } from "react";
import { Search } from "@/icons/leafen";
import { useTranslation } from "react-i18next";
import { userErrorText } from "@/lib/settingsErrors";
import { api } from "../../api";
import type { AdministratorServerWorkspace } from "../../administratorWire.generated";
import type { Session } from "../../types";
import styles from "./AdministratorSettingsPage.module.css";
import { AdministratorWorkspaceContent, administratorWorkspaceSummary } from "./AdministratorWorkspaceContent";
import own from "./AdministratorWorkspaceContent.module.css";
import { administratorUserStyles as userStyles } from "./administratorUserManagementStyles";
import { SettingsSectionTitle } from "@/components/settings/SettingsSectionTitle";
import { EmptyLineReadFailed } from "@/components/settings/SettingsRowNotice";

/**
 * Every workspace on the server by name, with its owner, members, pages and
 * stored files, searchable by workspace or owner; one opens for its content
 * (AdministratorWorkspaceContent).
 */
const SEARCH_WAIT_MS = 250;

export function AdministratorWorkspaceList({ session }: { session: Session }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const { t: tc } = useTranslation("serverConsole");
  const [query, setQuery] = useState("");
  const [applied, setApplied] = useState("");
  const [workspaces, setWorkspaces] = useState<AdministratorServerWorkspace[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  // A failed read, said where the list shows with a way to read it again:
  // the same search from where it failed.
  const [error, setError] = useState("");
  const [failedRead, setFailedRead] = useState<{ search: string; cursor?: string }>({ search: "" });
  const [opened, setOpened] = useState<AdministratorServerWorkspace | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  async function load(search: string, cursor?: string) {
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setLoading(true);
    setError("");
    try {
      const page = await api.administratorWorkspaces(session, { query: search || undefined, cursor }, controller.signal);
      if (controller.signal.aborted) return;
      setWorkspaces((current) => cursor ? [...current, ...page.items] : page.items);
      setNextCursor(page.nextCursor);
      setApplied(search);
    } catch (cause) {
      if (!controller.signal.aborted) {
        setError(userErrorText(cause, t("errLoadWorkspaces")));
        setFailedRead({ search, cursor });
        // A first window that failed leaves nothing of another search's list.
        if (!cursor) { setWorkspaces([]); setNextCursor(null); }
      }
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }

  useEffect(() => {
    void load("");
    return () => abortRef.current?.abort();
    // The list reloads when the session changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session]);

  function submit(event: FormEvent) {
    event.preventDefault();
    void load(query.trim());
  }

  // The list follows what is typed.
  useEffect(() => {
    const wanted = query.trim();
    if (wanted === applied) return undefined;
    const timer = window.setTimeout(() => void load(wanted), SEARCH_WAIT_MS);
    return () => window.clearTimeout(timer);
    // load reads the state it renders with.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, applied]);

  if (opened) {
    return <AdministratorWorkspaceContent session={session} workspace={opened} onBack={() => setOpened(null)}
      onSummary={(next) => {
        setOpened(next);
        setWorkspaces((current) => current.map((workspace) => workspace.id === next.id ? next : workspace));
      }} />;
  }
  return (
    <section className={styles.section} aria-labelledby="administrator-workspaces-title">
      <SettingsSectionTitle id="administrator-workspaces-title" restates>{t("serverWorkspacesTitle")}</SettingsSectionTitle>
      <form className={userStyles.memberDirectoryTools} onSubmit={submit}>
        <label className={userStyles.memberSearch}>
          <Search size="small" aria-hidden="true" />
          <input value={query} aria-label={t("ariaSearchServerWorkspaces")} placeholder={t("serverWorkspacesSearchPlaceholder")} autoComplete="off" onChange={(event) => setQuery(event.target.value)} />
        </label>
      </form>
      <div className={userStyles.memberList} aria-busy={loading}>
        {workspaces.map((workspace) => (
          <button key={workspace.id} type="button" className={`${userStyles.memberRow} ${own.openRow}`}
            data-admin-workspace-row={workspace.id} onClick={() => setOpened(workspace)}>
            <span className={userStyles.memberAvatar} aria-hidden="true">{(workspace.name || "W").slice(0, 1).toUpperCase()}</span>
            <span className={userStyles.memberText}>
              <strong>{workspace.name}</strong>
              <span className={userStyles.memberMetaWrap}>
                {administratorWorkspaceSummary(workspace, tc)}
              </span>
            </span>
          </button>
        ))}
      </div>
      {error ? <EmptyLineReadFailed onRetry={() => void load(failedRead.search, failedRead.cursor)} data-error="true">{error}</EmptyLineReadFailed>
        : !loading && !workspaces.length ? <div className={userStyles.emptyState}>{applied ? t("noSearchResults") : t("noWorkspaces")}</div> : null}
      {nextCursor && !error ? <button type="button" className={styles.secondaryButton} onClick={() => void load(applied, nextCursor)} disabled={loading}>{t("loadMore")}</button> : null}
    </section>
  );
}
