import { request } from "@/apiTransport";
import type { TextSpan } from "@/lib/types";
import type { Session } from "@/types";

/** A page the web can open: a published site, a public form or a link anyone can follow. */
export type PublicContentItem = {
  pageId: string;
  title: string;
  /** The title's spans as its reader reads them, while it holds a mention (page_title_spans.go). */
  titleSpans?: TextSpan[];
  /** A site's address on this server, as /site/<slug>. */
  path?: string;
  publishedBy: { id: string; name: string };
  publishedAt: string;
};

export type WorkspacePublicContent = {
  sites: PublicContentItem[];
  forms: PublicContentItem[];
  links: PublicContentItem[];
  /** With a kind's next cursor, more of it follow. */
  sitesNextCursor?: string;
  formsNextCursor?: string;
  linksNextCursor?: string;
};

type PublicContentKind = "sites" | "forms" | "links";

/** A window of each kind, or of one kind from its cursor, or one page's alone. */
export function listWorkspacePublicContent(session: Session, workspaceId: string, signal?: AbortSignal,
  options: { kind?: PublicContentKind; cursor?: string; pageId?: string } = {}) {
  const query = new URLSearchParams();
  if (options.kind) query.set("kind", options.kind);
  if (options.cursor) query.set("cursor", options.cursor);
  if (options.pageId) query.set("pageId", options.pageId);
  const suffix = query.size ? `?${query.toString()}` : "";
  return request<WorkspacePublicContent>(
    `/workspaces/${encodeURIComponent(workspaceId)}/public-content${suffix}`,
    { signal },
    session,
  );
}

/** Every kind to its end, a window at a time, for the screen that counts and sorts them. */
export async function listAllWorkspacePublicContent(session: Session, workspaceId: string, signal?: AbortSignal) {
  const first = await listWorkspacePublicContent(session, workspaceId, signal);
  const content: WorkspacePublicContent = { sites: first.sites, forms: first.forms, links: first.links };
  for (const kind of ["sites", "forms", "links"] as const) {
    let cursor = first[`${kind}NextCursor`];
    while (cursor && !signal?.aborted) {
      const page = await listWorkspacePublicContent(session, workspaceId, signal, { kind, cursor });
      content[kind] = [...content[kind], ...page[kind]];
      cursor = page[`${kind}NextCursor`];
    }
  }
  return content;
}
