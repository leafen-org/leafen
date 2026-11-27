export const LEAFEN_EXPORT_FORMAT = "leafen.export";
export const LEAFEN_EXPORT_FORMAT_VERSION = 1;

export interface NativeExportWarning {
  code: string;
  entityId?: string;
  detail?: string;
}

export interface LeafenExportDocument {
  format: typeof LEAFEN_EXPORT_FORMAT;
  formatVersion: typeof LEAFEN_EXPORT_FORMAT_VERSION;
  generatedAt?: string;
  app?: { name: string; version?: string };
  scope?: { kind: "workspace" | "subtree"; rootIds: string[] };
  source?: { workspaceId: string; workspaceName?: string; workspaceIcon?: string };
  counts?: Record<string, number>;
  files?: { included: false; strippedReferences: number };
  entities: {
    pages: unknown[];
    blocks?: unknown[];
    dbProperties?: unknown[];
    dbViews?: unknown[];
    dbTemplates?: unknown[];
    comments?: unknown[];
  };
  relationPairs?: unknown[];
  warnings?: NativeExportWarning[];
}

export interface ImportNativeResult {
  rootPageIds: string[];
  counts: Record<string, number>;
  warnings: NativeExportWarning[];
}

export function isLeafenExportDocument(value: unknown): value is LeafenExportDocument {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  if (
    record.format !== LEAFEN_EXPORT_FORMAT
    || record.formatVersion !== LEAFEN_EXPORT_FORMAT_VERSION
  ) {
    return false;
  }
  const entities = record.entities;
  return Boolean(
    entities
    && typeof entities === "object"
    && !Array.isArray(entities)
    && Array.isArray((entities as Record<string, unknown>).pages),
  );
}
