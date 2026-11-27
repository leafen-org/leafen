/**
 * Settings ask the product to open a workspace they made or left
 * (AuthenticatedProduct.tsx): by its ID, or the first of an organization —
 * a test organization's, and the organization it was tried for.
 */
export const OPEN_WORKSPACE_EVENT = "leafen:open-workspace";

export type OpenWorkspaceRequest = { workspaceId?: string; organizationId?: string };

export function requestOpenWorkspace(request: OpenWorkspaceRequest) {
  window.dispatchEvent(new CustomEvent<OpenWorkspaceRequest>(OPEN_WORKSPACE_EVENT, { detail: request }));
}

const WORKSPACE_CREATE_EVENT = "leafen:request-workspace-create";

/** What the new workspace starts from, as the window's choices name it. */
export type WorkspaceCreateStart = "leafen";

// The Leafen file a workspace is asked for with: held from the press, bound
// to the workspace the window then makes from it, and taken by that
// workspace's import, which brings it in at once; let go as the window
// closes without making one, or makes one from something else.
let heldFile: { file: File; workspaceId: string | null } | null = null;

/** Opens the window that starts a workspace, for someone who has none to add a page to. */
export function requestWorkspaceCreate() {
  heldFile = null;
  window.dispatchEvent(new Event(WORKSPACE_CREATE_EVENT));
}

/**
 * Opens it with what it starts from already chosen: a Leafen file, from
 * the import screen of a workspace that has pages (a Leafen file goes only
 * into an empty one), with the file chosen there if one was.
 */
export function requestWorkspaceCreateFrom(start: WorkspaceCreateStart, file: File | null = null) {
  heldFile = file ? { file, workspaceId: null } : null;
  window.dispatchEvent(new CustomEvent<WorkspaceCreateStart>(WORKSPACE_CREATE_EVENT, { detail: start }));
}

/** The workspace the window made takes the file held, made from a Leafen file; null, from anything else. */
export function bindHeldWorkspaceFile(workspaceId: string | null) {
  heldFile = heldFile && workspaceId ? { file: heldFile.file, workspaceId } : null;
}

/** The window closed without making a workspace. */
export function dropHeldWorkspaceFile() {
  if (heldFile?.workspaceId === null) heldFile = null;
}

/** The file held for this workspace, once. */
export function takeHeldWorkspaceFile(workspaceId: string): File | null {
  if (!heldFile || heldFile.workspaceId !== workspaceId) return null;
  const { file } = heldFile;
  heldFile = null;
  return file;
}

export function onWorkspaceCreateRequest(open: (start?: WorkspaceCreateStart) => void) {
  const heard = (event: Event) => open(event instanceof CustomEvent && event.detail === "leafen" ? "leafen" : undefined);
  window.addEventListener(WORKSPACE_CREATE_EVENT, heard);
  return () => window.removeEventListener(WORKSPACE_CREATE_EVENT, heard);
}
