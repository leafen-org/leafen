/**
 * The place a page's writes are made in (useContentGraphWrites): the page,
 * the person and their session, numbered anew whenever one of them changes,
 * so a write's answer is shown only while its place is current.
 */
export type ContentGraphWriteScope = {
  generation: number;
  pageId: string | null;
  principalId: string;
  sessionIdentity: string;
};

export function advanceContentGraphWriteScope(
  current: ContentGraphWriteScope,
  next: Omit<ContentGraphWriteScope, "generation">,
): ContentGraphWriteScope {
  if (
    current.pageId === next.pageId &&
    current.principalId === next.principalId &&
    current.sessionIdentity === next.sessionIdentity
  ) return current;
  return { ...next, generation: current.generation + 1 };
}

export function matchesContentGraphWriteScope(
  current: ContentGraphWriteScope,
  generation: number,
  pageId: string,
): boolean {
  return current.generation === generation && current.pageId === pageId;
}
