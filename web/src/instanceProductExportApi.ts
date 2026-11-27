import type { Session } from "./types";
import type {
  InstanceProductExportCurrentResult,
  InstanceProductExportInput,
  InstanceProductExportJobResult,
} from "./instanceProductExportWire.generated";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

type ResponseRequest = (path: string, session: Session) => Promise<Response>;

const instanceProductExportPath = "/admin/product-exports";

const instanceProductExportJobPath = (jobId: string) =>
  `${instanceProductExportPath}/${encodeURIComponent(jobId)}`;

export function createInstanceProductExportApi(request: JsonRequest, requestBinaryResponse: ResponseRequest) {
  return {
    startInstanceProductExport: (session: Session, input: InstanceProductExportInput) =>
      request<InstanceProductExportJobResult>(
        instanceProductExportPath,
        { method: "POST", body: JSON.stringify(input) },
        session,
      ),
    currentInstanceProductExport: (session: Session) =>
      request<InstanceProductExportCurrentResult>(
        `${instanceProductExportPath}/current`,
        {},
        session,
      ),
    instanceProductExport: (session: Session, jobId: string) =>
      request<InstanceProductExportJobResult>(
        instanceProductExportJobPath(jobId),
        {},
        session,
      ),
    downloadInstanceProductExport: (session: Session, jobId: string) =>
      requestBinaryResponse(`${instanceProductExportJobPath(jobId)}/download`, session),
  };
}
