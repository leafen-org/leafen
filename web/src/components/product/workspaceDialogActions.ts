import { api } from "@/api";
import { i18next } from "@/i18n";
import type { WorkspaceCreateChoice } from "@/components/WorkspaceCreateDialog";
import { bindHeldWorkspaceFile } from "@/lib/openWorkspace";
import { joinWorkspaceByEmailDomain } from "@/lib/workspaceAllowedEmailDomains";
import { pushRoute } from "@/lib/router";
import { defaultWorkspaceName } from "@/lib/welcomePage";
import type { Page, Session, Workspace } from "@/types";

/**
 * What the sidebar's workspace dialogs do: make a workspace, opening its
 * first page and what its choice leads to (an import, the move), or join one
 * the email's domain may join, opening its Home.
 */
export function workspaceDialogActions({ session, refresh, selectWorkspaceId, leavePage, openPage, openImport, openMove }: {
  session: Session;
  refresh: (session: Session) => Promise<unknown>;
  selectWorkspaceId: (workspaceId: string) => void;
  /** No page open, as on the workspace's Home. */
  leavePage: () => void;
  openPage: (page: Page) => void;
  openImport: (source: Exclude<WorkspaceCreateChoice, "blank" | "move">) => void;
  openMove: () => void;
}) {
  async function createWorkspaceFromDialog({ workspaceId, name, choice }: { workspaceId: string; name: string; choice: WorkspaceCreateChoice }): Promise<Workspace> {
    const result = await api.createWorkspace(session, {
      requestId: workspaceId,
      name: name || defaultWorkspaceName(session),
      skipDefaultPages: choice !== "blank",
      language: i18next.resolvedLanguage,
    });
    await refresh(session);
    selectWorkspaceId(result.id);
    if (result.page) {
      openPage(result.page);
    } else {
      leavePage();
      pushRoute("/");
    }
    // A Leafen file chosen where it could not go goes into the workspace made for it.
    bindHeldWorkspaceFile(choice === "leafen" ? result.id : null);
    if (choice === "move") openMove();
    else if (choice !== "blank") openImport(choice);
    return { id: result.id, organizationId: result.organizationId, name: result.name, role: result.role };
  }

  async function joinWorkspace(workspaceId: string) {
    await joinWorkspaceByEmailDomain(session, workspaceId);
    await refresh(session);
    selectWorkspaceId(workspaceId);
    leavePage();
    pushRoute("/");
  }

  return { createWorkspaceFromDialog, joinWorkspace };
}
