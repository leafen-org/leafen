import { api, LeafenApiError, throwInvalidBootstrapContinuation } from "@/api";
import { loadLanguageWords } from "@/i18n";
import type { Page, Session } from "@/types";
import { collectBootstrapPages } from "./bootstrapCollection";
import { defaultWorkspaceName } from "./welcomePage";
import { listJoinableWorkspaces } from "./workspaceAllowedEmailDomains";

// Only the account's first language gate calls this. Existing accounts that
// remove their workspaces keep their empty state on later sign-ins.
export async function prepareInitialWorkspace({ session, language, requestId, signal, isCurrent }: {
  session: Session;
  language: string;
  requestId: string;
  signal: AbortSignal;
  isCurrent(): boolean;
}): Promise<{ page: Page | null; restricted: boolean; joinable: boolean }> {
  const empty = { page: null, restricted: false, joinable: false };
  const current = () => !signal.aborted && isCurrent();
  let hasWorkspace = false;
  await collectBootstrapPages({ session, signal, readPage: api.bootstrap,
    shouldContinue: () => current() && !hasWorkspace,
    onSnapshot: (snapshot) => { hasWorkspace = snapshot.workspaces.length > 0; },
    onInvalidContinuation: throwInvalidBootstrapContinuation,
  });
  if (!current() || hasWorkspace) return empty;
  // Someone whose email's domain a workspace here allows is asked to join it
  // first, as Notion asks at sign-up: no workspace of their own is made, and
  // "워크스페이스 생성 또는 참여" opens with it on top (Sidebar's offer).
  const joinable = await listJoinableWorkspaces(session, signal).then((found) => found.length > 0, () => false);
  if (!current()) return empty;
  if (joinable) return { ...empty, joinable: true };
  await loadLanguageWords(language);
  if (!current()) return empty;
  try {
    // The language goes too: the organization made with this workspace and
    // its welcome page are in it, before the person's choice is saved (2026-10-27).
    const result = await api.createWorkspace(session, {
      requestId, name: defaultWorkspaceName(session, language),
      skipDefaultPages: false, ensureInitialWorkspace: true, language,
    }, signal);
    return current() ? { page: result.page ?? null, restricted: false, joinable: false } : empty;
  } catch (error) {
    if (!current()) return empty;
    // Workspace creation policy must not prevent language completion or
    // access to an invitation. The native refusal is shown on the empty shell.
    if (current() && error instanceof LeafenApiError && error.code === "LEAFEN_WORKSPACE_CREATION_RESTRICTED") {
      return { page: null, restricted: true, joinable: false };
    }
    throw error;
  }
}
