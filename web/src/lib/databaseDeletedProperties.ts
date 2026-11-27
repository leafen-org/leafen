import { request } from "@/apiTransport";
import {
  databaseDataSourceOperations,
  type DatabaseDeletedProperty,
  type DatabaseDeletedPropertyList,
  type DatabaseViewShownResponse,
} from "@/databaseDataSourceWire.generated";
import { newId } from "@/lib/ids";
import type { Session } from "@/types";

/** Brings back a deleted property, with its values and its places in the views. */
export function restoreDatabaseProperty(session: Session, pageId: string, propertyId: string) {
  return request<unknown>(
    path(databaseDataSourceOperations.restoreProperty.path, { pageId, propertyId }),
    { method: databaseDataSourceOperations.restoreProperty.method, body: JSON.stringify({ requestId: newId() }) },
    session,
  );
}

/**
 * Says a view was shown: when the property it groups or dates by is in
 * Deleted properties, the server writes its fallback for good, as Notion's.
 */
export function markDatabaseViewShown(session: Session, pageId: string, viewId: string) {
  return request<DatabaseViewShownResponse>(
    path(databaseDataSourceOperations.viewShown.path, { pageId, viewId }),
    { method: databaseDataSourceOperations.viewShown.method, body: JSON.stringify({ requestId: newId() }) },
    session,
  );
}

/**
 * A database's Deleted properties, newest first a window at a time (with
 * the cursor of the window before): restore one or delete it for good.
 */
export type DatabaseDeletedPropertiesSurface = {
  load(signal?: AbortSignal, cursor?: string): Promise<{ items: DatabaseDeletedProperty[]; nextCursor?: string }>;
  restore(propertyId: string): Promise<void>;
  deletePermanently(propertyId: string): Promise<void>;
};

function path(template: string, values: Record<string, string>) {
  return Object.entries(values).reduce(
    (current, [key, value]) => current.replace(`{${key}}`, encodeURIComponent(value)),
    template,
  );
}

export function createDatabaseDeletedPropertiesSurface(
  session: Session,
  pageId: string,
  dataSourceId: string,
  onChanged: () => Promise<unknown> | void,
): DatabaseDeletedPropertiesSurface {
  const change = (operation: { method: string; path: string }, propertyId: string) => request<unknown>(
    path(operation.path, { pageId, propertyId }),
    { method: operation.method, body: JSON.stringify({ requestId: newId() }) },
    session,
  );
  return {
    async load(signal, cursor) {
      const address = path(databaseDataSourceOperations.deletedProperties.path, { pageId, dataSourceId });
      const response = await request<DatabaseDeletedPropertyList>(
        cursor ? `${address}?cursor=${encodeURIComponent(cursor)}` : address,
        { method: databaseDataSourceOperations.deletedProperties.method, signal },
        session,
      );
      return { items: response.items, nextCursor: response.nextCursor || undefined };
    },
    async restore(propertyId) {
      await restoreDatabaseProperty(session, pageId, propertyId);
      await onChanged();
    },
    // The views still naming it let go of it in the same change.
    async deletePermanently(propertyId) {
      await change(databaseDataSourceOperations.deletePropertyPermanently, propertyId);
      await onChanged();
    },
  };
}
