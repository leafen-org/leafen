import type { DbProperty, Page } from "@/lib/types";
import type { FormulaBounds } from "./formula";

export interface PropertyTypeConfigOperationContext {
  contextIdentity: string;
  snapshotIdentity: string;
}

export interface PropertyTypeConfigSnapshot {
  databases: readonly Page[];
  formulaBounds: FormulaBounds;
  propertiesByDatabase: Readonly<Record<string, readonly DbProperty[]>>;
  snapshotIdentity: string;
  /** The pages loaded with the database, which the formula editor previews on. */
  pagesById?: Record<string, Page>;
}

export interface PropertyTypeConfigOps {
  loadDatabase: (
    operation: PropertyTypeConfigOperationContext,
    databaseId: string,
    options: { rows: false },
  ) => Promise<void> | void;
  setRelationDatabase: (
    operation: PropertyTypeConfigOperationContext,
    propertyId: string,
    targetDatabaseId: string,
  ) => Promise<void> | void;
  setRelationTwoWay: (
    operation: PropertyTypeConfigOperationContext,
    propertyId: string,
    enabled: boolean,
    reciprocalName?: string,
  ) => Promise<void> | void;
  updateProperty: (
    operation: PropertyTypeConfigOperationContext,
    propertyId: string,
    patch: Partial<DbProperty>,
  ) => Promise<void> | void;
}

export interface PropertyTypeConfigContext {
  contextIdentity: string;
  ops: PropertyTypeConfigOps;
}

export function propertyTypeConfigOperationContext(
  context: PropertyTypeConfigContext,
  snapshot: PropertyTypeConfigSnapshot,
): PropertyTypeConfigOperationContext {
  return {
    contextIdentity: context.contextIdentity,
    snapshotIdentity: snapshot.snapshotIdentity,
  };
}
