import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { AppSelect } from "@/components/AppSelect";
import type { Session } from "@/types";
import { listManagedUserWorkspaces, type ManagedUserWorkspace } from "./organizationManagedUsers";

type Choices = { query: string; items: ManagedUserWorkspace[]; nextCursor: string };

/**
 * The managed users' workspace filter: the workspaces the organization's
 * people are in or own, searched by name on the server and read on as the
 * list scrolls to its end, so any of them can be chosen however many there
 * are, as Notion's filters search theirs. The people list's answer gives the
 * first window; the one chosen keeps its name while the list shows others.
 */
export function ManagedUsersWorkspaceFilter({ session, organizationId, first, value, onChange }: {
  session: Session;
  organizationId: string;
  first: { items: ManagedUserWorkspace[]; nextCursor?: string } | null;
  value: string;
  onChange(workspaceId: string): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [choices, setChoices] = useState<Choices | null>(null);
  const [chosen, setChosen] = useState<ManagedUserWorkspace | null>(null);
  const reading = useRef<AbortController | null>(null);
  const timer = useRef(0);
  // The first window comes with the people until the filter searches or reads on.
  const shown: Choices | null = choices ?? (first ? { query: "", items: first.items, nextCursor: first.nextCursor ?? "" } : null);

  useEffect(() => () => {
    reading.current?.abort();
    window.clearTimeout(timer.current);
  }, []);

  // A window from the cursor on, after the ones shown, or the first for new words.
  function read(query: string, cursor = "", before: ManagedUserWorkspace[] = []) {
    reading.current?.abort();
    const controller = new AbortController();
    reading.current = controller;
    listManagedUserWorkspaces(session, organizationId, { query, cursor }, controller.signal).then((window) => {
      if (controller.signal.aborted) return;
      reading.current = null;
      setChoices({ query, items: [...before, ...window.items], nextCursor: window.nextCursor ?? "" });
    }, () => {
      if (reading.current === controller) reading.current = null;
    });
  }

  const items = shown?.items ?? [];
  const listed = chosen && chosen.id === value && !items.some((workspace) => workspace.id === chosen.id) ? [...items, chosen] : items;
  const label = (workspace: ManagedUserWorkspace) => workspace.external ? `${workspace.name} (${t("managedUsersExternal")})` : workspace.name;
  return (
    <AppSelect value={value} aria-label={t("managedUsersFilterWorkspace")} data-managed-users-workspace
      search={{
        ariaLabel: t("searchWorkspacesPlaceholder"), placeholder: t("searchWorkspacesPlaceholder"), noResultsLabel: t("noSearchResults"),
        onQueryChange: (query) => {
          const words = query.trim();
          window.clearTimeout(timer.current);
          if (words === (shown?.query ?? "")) {
            reading.current?.abort();
            reading.current = null;
            return;
          }
          timer.current = window.setTimeout(() => read(words), words ? 250 : 0);
        },
        onEnd: () => {
          if (shown?.nextCursor && !reading.current) read(shown.query, shown.nextCursor, shown.items);
        },
      }}
      onChange={(event) => {
        const id = event.target.value;
        setChosen(listed.find((workspace) => workspace.id === id) ?? null);
        onChange(id);
      }}>
      <option value="">{t("managedUsersFilterWorkspace")}</option>
      {listed.map((workspace) => <option key={workspace.id} value={workspace.id}>{label(workspace)}</option>)}
    </AppSelect>
  );
}
