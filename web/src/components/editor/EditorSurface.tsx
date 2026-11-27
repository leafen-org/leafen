import { useRef, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import styles from "./editorStyles.generated.module.css";
import { usePageBodyReading } from "./usePageBodyReading";

type EditorSurfaceProps = {
  pageId: string;
  loading: boolean;
  /** Whether the page couldn't be read; a note shows in place of its body. */
  unavailable?: boolean;
  populated: boolean;
  /** What an empty page offers to start with, when it shows. */
  starter: ReactNode;
  starterVisible?: boolean;
  /** Whether what it offers is a hint's line alone (PageStarter's hint). */
  starterHint?: boolean;
  children: ReactNode;
};

export function EditorSurface({
  pageId,
  loading,
  unavailable = false,
  populated,
  starter,
  starterVisible = Boolean(starter),
  starterHint = false,
  children,
}: EditorSurfaceProps) {
  const { t } = useTranslation("editor");
  const starterShown = starterVisible && !loading && !unavailable && Boolean(starter);
  // A page's own body, first read, hides its head until both draw in one
  // frame (usePageBodyReading).
  const rootRef = useRef<HTMLDivElement>(null);
  usePageBodyReading(pageId, loading && !unavailable, rootRef);
  return (
    <div
      ref={rootRef}
      className={styles.editor}
      data-editor-page={pageId}
      data-page-body-populated={populated ? "true" : undefined}
      data-page-starter-visible={starterVisible ? "true" : undefined}
      data-page-starter-hint={starterShown && starterHint ? "true" : undefined}
      role="region"
      aria-label={t("content.pageBody")}
      aria-busy={loading}
      data-loading={loading || unavailable ? "true" : undefined}
    >
      {unavailable ? (
        <p className={styles.editorContentUnavailable} data-page-body-unavailable>{t("content.pageBodyUnavailable")}</p>
      ) : loading ? (
        <div className={styles.editorContentLoading} aria-label={t("content.loadingPageBody")} data-page-body-reading-lines>
          <div className={styles.editorContentLoadingTextLine} aria-hidden="true" />
          <div className={styles.editorContentLoadingTextLine} aria-hidden="true" />
          <div className={styles.editorContentLoadingTextLine} aria-hidden="true" />
        </div>
      ) : null}
      {children}
      {loading || unavailable ? null : starter}
      <div className={styles.editorTail} data-editor-tail aria-hidden="true" />
    </div>
  );
}
