import { newId } from "@/lib/ids";
import { nativeNotionImportBounds } from "@/notionImportWire.generated";
import type {
  NativeImportedPeopleApplyInput,
  NativeImportedPeopleApplyResult,
  NativeImportedPeopleScanInput,
  NativeImportedPeopleScanResult,
} from "@/notionImportWire.generated";
import type { Session } from "@/types";

import type {
  ImportedPeopleApplySummary,
  ImportedPeopleMappingService,
} from "./importedPeopleMappingView";

export interface ImportedPeopleMappingTransport {
  scanImportedPeople(
    session: Session,
    workspaceId: string,
    input: NativeImportedPeopleScanInput,
  ): Promise<NativeImportedPeopleScanResult>;
  applyImportedPeopleMappings(
    session: Session,
    workspaceId: string,
    input: NativeImportedPeopleApplyInput,
  ): Promise<NativeImportedPeopleApplyResult>;
}

function capturedSession(currentSession: () => Session | null) {
  const session = currentSession();
  if (!session) throw new Error("Imported people mapping requires a current session.");
  return session;
}

function assertSessionCurrent(session: Session, currentSession: () => Session | null) {
  const current = currentSession();
  if (current?.user.id !== session.user.id || current.token !== session.token) {
    throw new Error("Imported people mapping session changed.");
  }
}

export function createImportedPeopleMappingService(
  transport: ImportedPeopleMappingTransport,
  currentSession: () => Session | null,
  createRequestId: () => string = () => newId(),
): ImportedPeopleMappingService {
  return {
    async listPeople(workspaceId, cursor) {
      const session = capturedSession(currentSession);
      // The setting lists the members to map to itself; the scan reads people only.
      const page = await transport.scanImportedPeople(session, workspaceId, { cursor: cursor ?? "", peopleOnly: true });
      assertSessionCurrent(session, currentSession);
      if (!Array.isArray(page.people) || !Array.isArray(page.members)
        || page.people.length > nativeNotionImportBounds.personMappingItems
        || page.members.length > nativeNotionImportBounds.personMappingMembers) {
        throw new RangeError("Imported people transport bound exceeded.");
      }
      if (typeof page.nextCursor !== "string" || typeof page.hasMore !== "boolean"
        || page.hasMore !== Boolean(page.nextCursor)) {
        throw new Error("Imported people scan continuation is invalid.");
      }
      if (page.hasMore && page.nextCursor === cursor) {
        throw new Error("Imported people scan continuation stalled.");
      }
      return {
        people: page.people.map((person) => ({ ...person })),
        members: page.members.map((member) => ({ ...member })),
        nextCursor: page.nextCursor,
        hasMore: page.hasMore,
      };
    },
    async applyMappings(workspaceId, mappings): Promise<ImportedPeopleApplySummary> {
      const session = capturedSession(currentSession);
      const requestId = createRequestId();
      let continuation = "";
      let generation = -1;
      for (;;) {
        const result = await transport.applyImportedPeopleMappings(session, workspaceId, {
          requestId,
          mappings,
          continuation,
        });
        assertSessionCurrent(session, currentSession);
        if (result.status === "completed") {
          return {
            mappedPeople: result.mappedPeople,
            changedPages: result.changedPages,
            changedBlocks: result.changedBlocks,
            skippedPages: result.skippedPages,
          };
        }
        if (result.status !== "pending" || !result.continuation) {
          throw new Error("Imported people apply continuation is invalid.");
        }
        if (!Number.isSafeInteger(result.generation) || result.generation <= generation) {
          throw new Error("Imported people apply continuation stalled.");
        }
        generation = result.generation;
        continuation = result.continuation;
      }
    },
  };
}
