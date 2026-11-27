import { FileUploadIcon } from "@/icons/leafen";
import { fileKindIcon, fileRowKindOf } from "./fileKindIcon";
import { documentKindOf, embedDocumentKind } from "@/lib/documentKinds";
import { openDocumentPreview } from "@/components/apps/DocumentPreview";
import type { GraphBlock } from "../../contentGraph";
import type { Session } from "../../types";
import { formatFileSize } from "../database/files";
import { blockItemText } from "./blockItemLabels";
import { useBlockFileActions } from "./useBlockFileActions";
import styles from "./editorStyles.generated.module.css";

type ManagedFileBlockProps = {
  block: GraphBlock; session: Session | null; readOnly?: boolean; onReplace?: () => void;
  /**
   * The document app for the file's format: pressing the row opens the file in
   * it, as Notion's row opens its preview (seen 2026-11-05).
   */
  documentApp?: { name: string; open: () => void };
  /** The page and workspace the block is on, for Leafen's preview to add the file to another page. */
  pageId?: string;
  workspaceId?: string;
};

export function ManagedFileBlock({ block, session, readOnly = false, onReplace, documentApp, pageId, workspaceId }: ManagedFileBlockProps) {
  const fileName = String(block.content.name ?? blockItemText("file.label"));
  const size = Number(block.content.size);
  // A file an import brought shows no size, as Notion's ZIP import's (seen 2026-11-12).
  const fileSize = block.content.sizeHidden === true ? ""
    : formatFileSize(Number.isFinite(size) && size >= 0 ? size : undefined) || blockItemText("file.label");
  const actions = useBlockFileActions(block, session);
  const kind = documentKindOf(fileName, String(block.content.contentType ?? ""));
  const rowKind = fileRowKindOf(fileName, String(block.content.contentType ?? ""));
  // A file of no kind is a page with an arrow up, as Notion's uploaded zip (seen 2026-11-05).
  const KindIcon = fileKindIcon(rowKind, FileUploadIcon);
  // Without a document app for the format, Leafen's own preview shows a Word,
  // Excel, PowerPoint or PDF file, as Notion's row opens its preview (2026-11-05;
  // a PDF opened a new tab, 2026-11-06), and the kin its engines read as an
  // embed draws them (Notion's row of an .xls or .ods file opens its preview of
  // the sheet, seen 2026-11-15 twice); a CSV file's row opens it as before.
  const drawn = embedDocumentKind(fileName, String(block.content.contentType ?? ""));
  const previewKind = !documentApp && session && block.content.fileId
    ? kind === "pdf" ? kind : drawn && drawn !== "csv" ? drawn : null : null;
  const open = documentApp ? documentApp.open : previewKind
    ? () => openDocumentPreview({ name: fileName, kind: previewKind, resolve: actions.resolve, download: actions.download,
      pageId, blockId: block.id, workspaceId })
    : actions.open;
  return <figure className={styles.fileBlock} contentEditable={false} data-read-only={readOnly ? "true" : undefined}>
    <div className={styles.fileCard}>
      <span className={styles.fileIcon} data-kind={rowKind ?? undefined}><KindIcon size="wide" aria-hidden="true" /></span>
      <button type="button" className={styles.fileInfo} disabled={!actions.available} onClick={open}
        aria-label={documentApp ? blockItemText("file.openNamedInApp", { fileName, app: documentApp.name })
          : previewKind ? blockItemText("file.previewNamed", { fileName })
          : blockItemText(actions.downloadsOnOpen ? "file.downloadNamed" : "file.openNamed", { fileName })} data-file-open>
        <span className={styles.fileName}>{fileName}</span>
        <span className={styles.fileMeta}>{actions.error ? blockItemText(actions.blocked ? "file.downloadBlocked" : "file.loadFailed") : fileSize}</span>
      </button>
      <span className={styles.fileActions}>
        {onReplace && !readOnly ? <button type="button" aria-label={blockItemText("file.replaceNamed", { fileName })}
          data-editor-file-action onClick={onReplace}>{blockItemText("common.replace")}</button> : null}
        {actions.available ? <a href="#" download aria-label={blockItemText("file.downloadNamed", { fileName })}
          data-editor-file-action={readOnly ? undefined : "true"}
          onClick={event => { event.preventDefault(); actions.download(); }}>{blockItemText("common.download")}</a>
          : <span data-public-file-unavailable>{blockItemText("file.downloadUnavailable")}</span>}
      </span>
    </div>
  </figure>;
}
