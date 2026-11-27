import { request } from "../apiTransport";
import type { Session } from "../types";
import {
  publicMutationPaths,
  type PublicMutationShareLinkJoin,
  type PublicMutationShareLinkJoinInput,
} from "../publicMutationWire.generated";

// A web link opens its page in the app for the organization's people, with
// the link's role (page_link_access.go): joining records them with the
// link. Someone signed in joins as the link opens (SharedPageView); someone
// not signed in may sign in first from a link that lets them comment or
// edit, and the join waits for them in this tab.

const PENDING_KEY = "leafen.shareLinkJoin";

export function joinShareLink(session: Session, input: PublicMutationShareLinkJoinInput) {
  return request<PublicMutationShareLinkJoin>(
    publicMutationPaths.joinShareLink.path,
    { method: publicMutationPaths.joinShareLink.method, body: JSON.stringify(input) },
    session,
  );
}

/** Keeps a join for after signing in. */
export function rememberShareLinkJoin(input: PublicMutationShareLinkJoinInput) {
  try {
    window.sessionStorage.setItem(PENDING_KEY, JSON.stringify(input));
  } catch {
    // Without storage the person opens the link again after signing in.
  }
}

/** The join kept for after signing in, taken once. */
export function takeShareLinkJoin(): PublicMutationShareLinkJoinInput | null {
  try {
    const raw = window.sessionStorage.getItem(PENDING_KEY);
    window.sessionStorage.removeItem(PENDING_KEY);
    const parsed = raw ? JSON.parse(raw) as Partial<PublicMutationShareLinkJoinInput> : null;
    return parsed && typeof parsed.token === "string" && parsed.token
      ? { token: parsed.token, ...(typeof parsed.pageId === "string" && parsed.pageId ? { pageId: parsed.pageId } : {}) }
      : null;
  } catch {
    return null;
  }
}
