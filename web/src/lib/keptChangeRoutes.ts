import type { NativeDatabaseStart, NativeRowMove, NativeRowTrash, NativeRowValues } from "./nativePageLaneEntries";
import type { Session } from "@/types";

// The routes the outbox sends kept changes by that don't go by the content
// route — a database start the server makes (databaseStartKept.ts), rows
// going to the trash or out of it (rowTrashKept.ts), a row's values written
// by its page's route (rowValuesKept.ts), a row's page moved (rowMoveKept.ts)
// — each named here as its module
// loads, which it does before any such change is made: the outbox's sending
// (AuthenticatedSession.tsx) takes it at once, as a change sent as the page
// goes has no time to import its route (2026-11-27: a row page's title taken
// back with ⌘Z as its tab closed never left, its route's import still
// waiting as the page went).

type Route<Input> = (session: Session, input: Input, signal?: AbortSignal) => Promise<unknown>;

export const keptChangeRoutes: {
  start?: Route<{ mutationId: string; pageId: string; start: NativeDatabaseStart }>;
  rowTrash?: Route<{ mutationId: string; rowTrash: NativeRowTrash }>;
  rowValues?: Route<{ mutationId: string; rowValues: NativeRowValues }>;
  rowMove?: Route<{ mutationId: string; rowMove: NativeRowMove }>;
} = {};
