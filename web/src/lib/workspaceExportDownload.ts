import type { Session } from "@/types";
import type { WorkspaceExportFormat } from "@/workspaceExportWire.generated";
import {
  LEAFEN_ARCHIVE_EXT,
  leafenArchiveFileName,
} from "./leafenExportDownload";
import { saveNativeArchiveResponse } from "./nativeArchive";
import { workspaceExportService } from "./workspaceExportService";

const PORTABLE_ARCHIVE_SUFFIX = "-markdown-csv.zip";
const HTML_ARCHIVE_SUFFIX = "-html.zip";

export function workspaceExportArtifactFileName(
  workspaceName: string,
  format: WorkspaceExportFormat,
  now = new Date(),
) {
  const nativeName = leafenArchiveFileName(workspaceName || "workspace", now);
  if (format === "native_archive") return nativeName;
  const suffix = format === "html" ? HTML_ARCHIVE_SUFFIX : PORTABLE_ARCHIVE_SUFFIX;
  return `${nativeName.slice(0, -LEAFEN_ARCHIVE_EXT.length)}${suffix}`;
}

export async function downloadWorkspaceExportToBrowser(input: {
  session: Session;
  workspaceId: string;
  workspaceName?: string;
  jobId: string;
  format: WorkspaceExportFormat;
  now?: Date;
}) {
  const response = await workspaceExportService.download(
    input.session,
    input.workspaceId,
    input.jobId,
  );
  return saveNativeArchiveResponse(
    response,
    workspaceExportArtifactFileName(
      input.workspaceName || "workspace",
      input.format,
      input.now,
    ),
  );
}
