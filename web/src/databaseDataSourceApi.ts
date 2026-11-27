import type { Session } from "./types";
import {
  databaseDataSourceOperations,
  type DatabaseDataSourceMoveBackRequest,
  type DatabaseDataSourceMoveRequest,
  type DatabaseDataSourceResponse,
} from "./databaseDataSourceWire.generated";

// The Manage data sources menu moves an own data source to another database
// or into trash through the server owners the public API shares.

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

function dataSourcePath(path: string, pageId: string, dataSourceId: string) {
  return path
    .replace("{pageId}", encodeURIComponent(pageId))
    .replace("{dataSourceId}", encodeURIComponent(dataSourceId));
}

export function createDatabaseDataSourceApi(request: JsonRequest) {
  return {
    moveDatabaseDataSource: (
      session: Session, pageId: string, dataSourceId: string, target: DatabaseDataSourceMoveRequest,
    ) => (
      request<DatabaseDataSourceResponse>(
        dataSourcePath(databaseDataSourceOperations.move.path, pageId, dataSourceId),
        { method: databaseDataSourceOperations.move.method, body: JSON.stringify(target) },
        session,
      )
    ),
    // Undoes a move of the source alone, back to a database on this page.
    moveDatabaseDataSourceBack: (
      session: Session, pageId: string, dataSourceId: string, back: DatabaseDataSourceMoveBackRequest,
    ) => (
      request<DatabaseDataSourceResponse>(
        dataSourcePath(databaseDataSourceOperations.moveBack.path, pageId, dataSourceId),
        { method: databaseDataSourceOperations.moveBack.method, body: JSON.stringify(back) },
        session,
      )
    ),
    trashDatabaseDataSource: (session: Session, pageId: string, dataSourceId: string) => (
      request<DatabaseDataSourceResponse>(
        dataSourcePath(databaseDataSourceOperations.trash.path, pageId, dataSourceId),
        { method: databaseDataSourceOperations.trash.method },
        session,
      )
    ),
    restoreDatabaseDataSource: (session: Session, pageId: string, dataSourceId: string) => (
      request<DatabaseDataSourceResponse>(
        dataSourcePath(databaseDataSourceOperations.restore.path, pageId, dataSourceId),
        { method: databaseDataSourceOperations.restore.method },
        session,
      )
    ),
  };
}
