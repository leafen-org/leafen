import type { Session } from "./types";
import type {
  NativeImportedPeopleApplyInput,
  NativeImportedPeopleApplyResult,
  NativeImportedPeopleScanInput,
  NativeImportedPeopleScanResult,
} from "./notionImportWire.generated";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

function importedPeoplePath(workspaceId: string, suffix: "scan" | "apply") {
  return `/workspaces/${encodeURIComponent(workspaceId)}/imported-people/${suffix}`;
}

export function createImportedPeopleMappingApi(request: JsonRequest) {
  return {
    scanImportedPeople: (
      session: Session,
      workspaceId: string,
      input: NativeImportedPeopleScanInput,
      options: { signal?: AbortSignal } = {},
    ) => request<NativeImportedPeopleScanResult>(
      importedPeoplePath(workspaceId, "scan"),
      { method: "POST", body: JSON.stringify(input), signal: options.signal },
      session,
    ),
    applyImportedPeopleMappings: (
      session: Session,
      workspaceId: string,
      input: NativeImportedPeopleApplyInput,
      options: { signal?: AbortSignal } = {},
    ) => request<NativeImportedPeopleApplyResult>(
      importedPeoplePath(workspaceId, "apply"),
      { method: "POST", body: JSON.stringify(input), signal: options.signal },
      session,
    ),
  };
}
