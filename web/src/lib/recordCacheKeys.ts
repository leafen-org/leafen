/**
 * Canonical record-cache key schema.
 *
 * Entity tables and query membership metadata deliberately have separate
 * builders. Callers must not assemble cache keys themselves: a mutation can
 * then update the canonical entity value without pretending that it knows
 * whether a filtered query should gain or lose membership.
 */

export const recordCacheTables = {
  blocks: (pageId: string) => `blocks:${pageId}`,
  comments: (pageId: string) => `comments:${pageId}`,
  databaseProperties: (databaseId: string) => `props:${databaseId}`,
  databaseTemplates: (databaseId: string) => `templates:${databaseId}`,
  databaseViews: (databaseId: string) => `views:${databaseId}`,
  databaseRows: (databaseId: string, suffix: string) =>
    `rowsdata:${databaseId}:${suffix}`,
  databaseRelatedPages: (databaseId: string, suffix: string) =>
    `rowsrelated:${databaseId}:${suffix}`,
} as const;

export const recordCacheMeta = {
  blocksStamp: (pageId: string) => `blocksStamp:${pageId}`,
  bootstrap: (bootstrapKey: string) => `bootstrap:${bootstrapKey}`,
  commentsCachedAt: (pageId: string) => `commentsCachedAt:${pageId}`,
  databaseMetadataStamp: (databaseId: string) => `dbMetaStamp:${databaseId}`,
  databaseRowQuery: (databaseId: string, suffix: string) =>
    `rows:${databaseId}:${suffix}`,
  databaseRowQueryRegistry: (databaseId: string) => `rowsKeys:${databaseId}`,
  /** A page's own reads — a database page's row, properties and access, any page's reach — kept beside its body's blocks. */
  rowPage: (pageId: string) => `rowPage:${pageId}`,
  /** The database template changes written here that the server may not have yet (templateDraftSend.ts). */
  templateDrafts: () => "templateDrafts",
} as const;

export interface DatabaseRowCacheKeys {
  dataTable: string;
  meta: string;
  relatedPagesTable: string;
  suffix: string;
}

export function databaseRowCacheKeysFromSuffix(
  databaseId: string,
  suffix: string
): DatabaseRowCacheKeys {
  return {
    dataTable: recordCacheTables.databaseRows(databaseId, suffix),
    meta: recordCacheMeta.databaseRowQuery(databaseId, suffix),
    relatedPagesTable: recordCacheTables.databaseRelatedPages(databaseId, suffix),
    suffix,
  };
}
