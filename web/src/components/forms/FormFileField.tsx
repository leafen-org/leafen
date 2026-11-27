import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { FileUploadIcon, X } from "@/icons/leafen";
import { newId } from "@/lib/ids";
import styles from "./FormSurface.module.css";

export type FormFileAnswer = { fileId: string; name: string; size: number };

/** At most this many files in one answer, as Notion's ("파일 제한: 20."). */
export const FORM_FILE_LIMIT = 20;
/** What one file may weigh, as Notion's ("크기 제한: 100MB."); over a request's size it goes up in parts. */
const FORM_FILE_BYTES = 100 * 1024 * 1024;

function formFileAnswers(value: unknown): FormFileAnswer[] {
  return Array.isArray(value) ? value.filter((item): item is FormFileAnswer =>
    Boolean(item) && typeof item === "object" && typeof (item as FormFileAnswer).fileId === "string") : [];
}

/**
 * A file question as Notion's form asks it (seen 2026-11-02): a grey bar,
 * "[📄 업로드]" and what it may take, its size limit and at most 20 files; each
 * file chosen goes up at once and is listed by name with × to take it out.
 * Without a way to send (the builder's preview), the button does nothing.
 */
export function FormFileField({ id, label, value, disabled, upload, onChange }: {
  id: string;
  label: string;
  value: unknown;
  disabled: boolean;
  upload?: (file: File, requestId: string) => Promise<FormFileAnswer>;
  onChange: (value: FormFileAnswer[]) => void;
}) {
  const { t } = useTranslation("formView");
  const input = useRef<HTMLInputElement>(null);
  const [sending, setSending] = useState(0);
  const [failed, setFailed] = useState("");
  const files = formFileAnswers(value);
  const latest = useRef(files); latest.current = files;
  const choose = async (chosen: FileList | null) => {
    if (!chosen || !upload) return;
    setFailed("");
    const room = FORM_FILE_LIMIT - latest.current.length;
    for (const file of Array.from(chosen).slice(0, Math.max(0, room))) {
      if (file.size > FORM_FILE_BYTES) { setFailed(t("fileTooLarge", { name: file.name })); continue; }
      setSending((count) => count + 1);
      try {
        const sent = await upload(file, newId());
        onChange([...latest.current, { fileId: sent.fileId, name: sent.name, size: sent.size }]);
      } catch {
        setFailed(t("fileUploadFailed", { name: file.name }));
      } finally {
        setSending((count) => count - 1);
      }
    }
  };
  return (
    <span className={styles.fileAnswer} data-form-answer={id}>
      <span className={styles.fileBar}>
        <button id={`form-answer-${id}`} type="button" className={styles.fileUpload} aria-label={t("uploadFilesFor", { label })}
          disabled={disabled || !upload || sending > 0 || files.length >= FORM_FILE_LIMIT} data-form-file-upload
          onClick={() => input.current?.click()}>
          <FileUploadMark />{sending ? t("uploadingFiles") : t("uploadFiles")}
        </button>
        <span className={styles.fileLimits}>{t("fileLimits", { size: 100, count: FORM_FILE_LIMIT })}</span>
        <input ref={input} type="file" multiple hidden tabIndex={-1} onChange={(event) => {
          void choose(event.currentTarget.files); event.currentTarget.value = "";
        }} />
      </span>
      {files.length ? <span className={styles.fileList}>
        {files.map((file) => <span key={file.fileId} className={styles.fileChip} data-form-file={file.fileId}>
          <span>{file.name}</span>
          <button type="button" aria-label={t("removeFile", { name: file.name })} disabled={disabled}
            onClick={() => onChange(latest.current.filter((candidate) => candidate.fileId !== file.fileId))}>
            <X size="tiny" aria-hidden="true" />
          </button>
        </span>)}
      </span> : null}
      {failed ? <span className={styles.fileFailed} role="alert">{failed}</span> : null}
    </span>
  );
}

/**
 * A file question's "업로드" mark, in the builder and for its respondent: a page with an arrow up 20 tall in its own
 * 11.75 width, as Notion's (seen 2026-11-24 in two tabs).
 */
export function FileUploadMark() {
  return <FileUploadIcon size="regular" trim glyphWidth={9.4} aria-hidden="true" />;
}
