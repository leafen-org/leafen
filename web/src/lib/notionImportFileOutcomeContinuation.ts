import { api } from "@/api";
import {
  nativeNotionImportBounds,
  type NativeNotionImportFileOutcome,
  type NativeNotionImportFileOutcomePage,
} from "@/notionImportWire.generated";
import type { Session } from "@/types";

type FileOutcomePageReader = (
  session: Session,
  workspaceId: string,
  jobId: string,
  cursor: string,
  options: { signal?: AbortSignal },
) => Promise<NativeNotionImportFileOutcomePage>;

type FileOutcomeWindowOptions = {
  sourceCursor?: string;
  signal?: AbortSignal;
};

function invalidCursor(): never {
  throw new Error("The file outcome read position repeated.");
}

function invalidOrder(): never {
  throw new Error("The file outcome order could not be confirmed.");
}

function validatedWindow(
  page: NativeNotionImportFileOutcomePage,
  sourceCursor: string,
): NativeNotionImportFileOutcomePage {
  if (!Array.isArray(page.items) || page.items.length > nativeNotionImportBounds.fileOutcomeItems) {
    return invalidOrder();
  }

  let previous = sourceCursor;
  const items: NativeNotionImportFileOutcome[] = [];
  for (const item of page.items) {
    if (!item || typeof item.sourceId !== "string" || item.sourceId.length === 0 || item.sourceId <= previous) {
      return invalidOrder();
    }
    previous = item.sourceId;
    items.push(item);
  }

  if (page.hasMore) {
    if (items.length === 0 || page.nextCursor !== previous || page.nextCursor <= sourceCursor) {
      return invalidCursor();
    }
  } else if (page.nextCursor !== "") {
    return invalidCursor();
  }

  return { items, nextCursor: page.nextCursor, hasMore: page.hasMore };
}

export function createNotionImportFileOutcomeContinuation(readPage: FileOutcomePageReader) {
  async function readWindow(
    session: Session,
    workspaceId: string,
    jobId: string,
    options: FileOutcomeWindowOptions = {},
  ) {
    const sourceCursor = options.sourceCursor ?? "";
    const page = await readPage(
      session,
      workspaceId,
      jobId,
      sourceCursor,
      { ...(options.signal ? { signal: options.signal } : {}) },
    );
    return validatedWindow(page, sourceCursor);
  }

  return { readWindow };
}

const notionImportFileOutcomeContinuation = createNotionImportFileOutcomeContinuation(
  api.notionImportFileOutcomePage,
);

export const readNotionImportFileOutcomeWindow = notionImportFileOutcomeContinuation.readWindow;
