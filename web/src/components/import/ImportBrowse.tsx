import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { CloudUploadIcon, CodeIcon, FileText, PdfFileIcon, TableIcon, WordDocumentIcon } from "@/icons/leafen";
import { useFileDrop } from "@/lib/fileDropTarget";
import { CONTENT_BOX_ACCEPT, contentBoxFileKind } from "@/lib/contentBoxFileKind";
import { FILE_IMPORT_ZIP_MAX_BYTES, type FileImportKind } from "@/lib/fileImportSources";
import { holdsLeafenArchive, isHanjiExportFile } from "@/lib/leafenImportFile";
import { confirmAction } from "../ConfirmActionDialog";
import type { Session } from "../../types";
import type { CsvImportPlace } from "./CsvImportPlacePicker";
import type { ImportFormat } from "./ImportDialog";
import { ImportKindWindow, type ImportFileKind } from "./ImportKindWindow";
import styles from "./ImportBrowse.module.css";

// The file kinds "파일 기반 가져오기" offers: CSV opens the CSV window, as
// Notion's two-pane "CSV를 Notion으로 가져오기" (seen 2026-10-11), the others
// their own window choosing only their own files.
const FILE_KINDS: Array<{ key: "csv" | ImportFileKind["key"]; label: string; icon: ReactNode; accept: string }> = [
  { key: "csv", label: "CSV", icon: <TableIcon size="regular" aria-hidden="true" />, accept: ".csv,text/csv" },
  { key: "pdf", label: "PDF", icon: <PdfFileIcon size="regular" aria-hidden="true" />, accept: ".pdf,application/pdf" },
  { key: "text", label: "Text & Markdown", icon: <FileText size="regular" aria-hidden="true" />,
    accept: ".md,.markdown,.txt,text/markdown,text/plain" },
  { key: "html", label: "HTML", icon: <CodeIcon size="regular" aria-hidden="true" />, accept: ".html,.htm,text/html" },
  { key: "word", label: "Word", icon: <WordDocumentIcon size="regular" aria-hidden="true" />,
    accept: ".docx,application/vnd.openxmlformats-officedocument.wordprocessingml.document" },
];

// Notion's general importer, "Zip 가져오기": one file, chosen from ZIPs.
const ZIP_KIND: ImportFileKind = { key: "zip", icon: <FileText size="regular" aria-hidden="true" />, accept: ".zip,application/zip" };

// The window the file chosen in "콘텐츠 가져오기" opens, as Notion's (seen
// 2026-11-19): its card's window, and a ZIP or an EPUB (no card of its own)
// the general importer's, the file chosen there.
function importFileKindOf(found: FileImportKind | "zip"): ImportFileKind {
  const same = FILE_KINDS.find(({ key }) => key === (found === "markdown" ? "text" : found === "docx" ? "word" : found));
  return same && same.key !== "csv" ? { key: same.key, icon: same.icon, accept: same.accept } : ZIP_KIND;
}

function importFormatKind(format: ImportFormat): ImportFileKind | null {
  if (format === "zip") return ZIP_KIND;
  const key = format === "markdown" ? "text" : format;
  const same = FILE_KINDS.find((candidate) => candidate.key === key);
  return same && same.key !== "csv" ? { key: same.key, icon: same.icon, accept: same.accept } : null;
}

/**
 * The import screen's "둘러보기", as Notion's (seen 2026-10-08): "콘텐츠
 * 가져오기" — a place to drop files or choose them, ZIP included — then
 * "파일 기반 가져오기" with a card per kind of file, the other places
 * Leafen imports from (a Notion workspace, a Leafen archive), and where
 * Notion lists third-party apps, the tools installed apps move content
 * from.
 */
export function ImportBrowse({ disabled, onFiles, onKindFiles, onCSV, afterDrop, sources, tools, session, workspaceId, initialPlace, initialFormat }: {
  disabled: boolean;
  onFiles: (files: FileList | File[]) => void;
  /** Files chosen in a kind's window, with where they go. */
  onKindFiles: (files: File[], place: CsvImportPlace) => void;
  /** The CSV card opens the CSV window before any file. */
  onCSV: () => void;
  /** What the dropped files became, shown under the place they were dropped. */
  afterDrop?: ReactNode;
  /** The other places to import from, as cards. */
  sources?: ReactNode;
  /** The tools apps move content from, as cards. */
  tools?: ReactNode;
  session: Session;
  workspaceId: string;
  /** The page a kind's window puts its files under at first. */
  initialPlace?: { pageId: string; title: string } | null;
  /** The kind whose window opens at first, as a slash menu's kind asks (seen 2026-11-05). */
  initialFormat?: ImportFormat;
}) {
  const { t } = useTranslation("importDialog");
  const anyFile = useRef<HTMLInputElement>(null);
  const [kind, setKind] = useState<ImportFileKind | null>(() => initialFormat ? importFormatKind(initialFormat) : null);
  const [picked, setPicked] = useState<File[] | undefined>(undefined);
  const [over, setOver] = useState(false);
  // What the chosen files become shows under the place they were chosen, which
  // the settings window may have scrolled away: it comes into view.
  const afterDropRef = useRef<HTMLDivElement>(null);
  const showsAfterDrop = Boolean(afterDrop);
  useEffect(() => {
    if (showsAfterDrop) afterDropRef.current?.scrollIntoView({ block: "nearest" });
  }, [showsAfterDrop]);
  // The file chosen here opens the window of what it is, with it chosen, as
  // Notion's "콘텐츠 가져오기" (seen 2026-11-04). A CSV or TSV has a window of its
  // own and a Leafen archive a place of its own, by its name or the
  // descriptor it holds. It takes one file at a time, of a kind it takes, as
  // Notion's: its picker chooses one; several dropped, or one of no kind it
  // takes, are refused in a notice over "확인" with nothing opened (seen
  // 2026-11-05 and 2026-11-19: one file at a time, and the kinds it takes
  // named — ZIP, CSV, PDF, TXT, MD or HTML — dropped or chosen alike; its kind
  // windows take several).
  const notice = (message: string) => void confirmAction({ message, confirmLabel: t("browse.noticeOk"), cancelLabel: null });
  const choose = (list: FileList | File[]) => {
    const files = Array.from(list);
    if (files.length > 1) {
      notice(t("browse.oneFileAtATime"));
      return;
    }
    const [only] = files;
    if (!only) return;
    const found = contentBoxFileKind(only);
    // An export of the old name keeps the refusal it has.
    if (isHanjiExportFile(only) || found === "csv") {
      onFiles(files);
      return;
    }
    const openKind = () => {
      if (!found) {
        notice(t("browse.unsupportedFile"));
        return;
      }
      setPicked(files);
      setKind(importFileKindOf(found));
    };
    // A Leafen archive has an import of its own, which the panel opens,
    // whatever its name (a ".leafen" file is no kind Notion's box takes).
    void holdsLeafenArchive(only).then((archive) => archive ? onFiles(files) : openKind(), openKind);
  };
  const dropRef = useFileDrop(choose, { canDrop: (types) => !disabled && types.includes("Files"), onOver: setOver });
  const gigabytes = Math.round(FILE_IMPORT_ZIP_MAX_BYTES / (1024 * 1024 * 1024));
  return (
    <div className={styles.browse} data-import-browse>
      <section className={styles.section}>
        <h3>{t("browse.contentTitle")}</h3>
        <p>{t("browse.contentDescription")}</p>
        <div ref={dropRef} className={styles.drop} data-over={over ? "true" : undefined} data-import-drop>
          <CloudUploadIcon size="large" aria-hidden="true" />
          <strong>{t("browse.dropTitle")}</strong>
          <span>
            {t("browse.dropHint")}{" "}
            <button type="button" className={styles.choose} disabled={disabled} data-import-choose
              onClick={() => anyFile.current?.click()}>{t("browse.choose")}</button>
          </span>
          <small>{t("browse.dropLimit", { gigabytes })}</small>
          <input ref={anyFile} type="file" accept={CONTENT_BOX_ACCEPT} hidden disabled={disabled}
            onChange={(event) => { if (event.currentTarget.files?.length) choose(event.currentTarget.files); event.currentTarget.value = ""; }} />
        </div>
        {afterDrop ? <div ref={afterDropRef} className={styles.afterDrop}>{afterDrop}</div> : null}
      </section>
      <section className={styles.section}>
        <h3>{t("browse.filesTitle")}</h3>
        <p>{t("browse.filesDescription")}</p>
        <div className={styles.cards}>
          {FILE_KINDS.map((kind) => (
            <button key={kind.key} type="button" className={styles.card} disabled={disabled} data-import-kind={kind.key}
              onClick={() => { setPicked(undefined); if (kind.key === "csv") onCSV(); else setKind({ key: kind.key, icon: kind.icon, accept: kind.accept }); }}>
              <span className={styles.cardTitle}>{kind.icon}{kind.label}</span>
              <span className={styles.cardText}>{t(`browse.kinds.${kind.key}`)}</span>
            </button>
          ))}
        </div>
        {kind ? <ImportKindWindow kind={kind} initialFiles={picked} session={session} workspaceId={workspaceId} initialPlace={initialPlace}
          onClose={() => setKind(null)} onContinue={(files, place) => { setKind(null); onKindFiles(files, place); }} /> : null}
      </section>
      {sources ? (
        <section className={styles.section}>
          <h3>{t("browse.otherTitle")}</h3>
          <p>{t("browse.otherDescription")}</p>
          <div className={styles.cards}>{sources}</div>
        </section>
      ) : null}
      {tools ? (
        <section className={styles.section} data-import-tools>
          <h3>{t("browse.toolsTitle")}</h3>
          <p>{t("browse.toolsDescription")}</p>
          <div className={styles.cards}>{tools}</div>
        </section>
      ) : null}
    </div>
  );
}

/** A card for another place to import from; an app's says which app it is. */
export function ImportSourceCard({ icon, title, text, mark, onOpen, data }: {
  icon: ReactNode; title: string; text: string; mark?: string; onOpen: () => void; data?: Record<string, string>;
}) {
  return (
    <button type="button" className={styles.card} onClick={onOpen} {...data}>
      <span className={styles.cardTitle}>{icon}{title}</span>
      <span className={styles.cardText}>{text}</span>
      {mark ? <span className={styles.cardMark}>{mark}</span> : null}
    </button>
  );
}
