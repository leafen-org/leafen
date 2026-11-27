import type { CSSProperties } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft, DoubleChevronRight } from "@/icons/leafen";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import editorStyles from "@/components/editor/editorStyles.generated.module.css";
import sheet from "./databaseSheet";
import { storedRowPeekWidth } from "./sidePeekWidth";
import { useMainContentRect } from "./useMainContentRect";

/**
 * A database page the server wasn't there to read, and this device keeps no
 * copy of: its peek or its own page opens all the same, its name as its row
 * shows it, and says what a page says then — "이 페이지를 열지 못했습니다.
 * 연결이 돌아오면 엽니다." (EditorSurface) — until it is read (2026-11-11:
 * offline a board card never opened changed the address and showed nothing).
 */
export function RowPageUnavailable({ mode, title, onClose }: {
  mode: "side" | "center" | "full";
  /** The row's name as the view holding it shows it, where one does. */
  title?: string;
  onClose: () => void;
}) {
  const { t } = useTranslation(["databaseView", "editor"]);
  const fullRect = useMainContentRect(mode === "full");
  const name = title || untitledPageDisplayTitle("page");
  const style = mode === "side"
    ? ({ "--row-peek-width": `${storedRowPeekWidth()}px` } as CSSProperties)
    : mode === "full" && fullRect
      ? ({ left: fullRect.left, top: fullRect.top, width: fullRect.width, height: fullRect.height } as CSSProperties)
      : undefined;
  const page = (
    <>
      {mode === "center" ? (
        <button type="button" className={sheet.rowPeekBackdrop} data-mode={mode} data-motion-state="open" tabIndex={-1}
          aria-label={t("databaseView:closeRowPreview")} onClick={onClose} />
      ) : null}
      <aside className={sheet.rowPeek} data-mode={mode} data-motion-state="open" data-row-peek-panel data-row-page-unavailable
        style={style} role={mode === "full" ? "region" : "dialog"} aria-label={mode === "full" ? name : t("databaseView:rowPreviewFor", { title: name })}
        tabIndex={-1} onKeyDown={(event) => { if (event.key === "Escape" && mode !== "full") { event.stopPropagation(); onClose(); } }}>
        <div className={sheet.rowPeekTop} data-row-peek-top={mode === "full" ? undefined : ""}>
          <div className={sheet.rowPeekChromeSide} data-row-peek-chrome-side>
            {mode === "full" ? (
              <button type="button" className={sheet.rowPeekBack} data-row-page-back aria-label={t("databaseView:backToDatabase", { title: t("databaseView:databaseFallbackTitle") })}
                onClick={onClose}>
                <ArrowLeft size="regular" />
              </button>
            ) : mode === "side" ? (
              <button type="button" className={sheet.rowPeekIconAction} data-row-peek-close aria-label={t("databaseView:closeRowPreview")} onClick={onClose}>
                <DoubleChevronRight size="regular" />
              </button>
            ) : null}
          </div>
        </div>
        <div className={`${sheet.rowPeekScroll} nscroll`}>
          <div className={sheet.rowPeekDoc} data-row-page="true" data-row-page-mode={mode}>
            <h1 className={sheet.rowPeekTitle}>{name}</h1>
            <p className={editorStyles.editorContentUnavailable} data-page-body-unavailable>{t("editor:content.pageBodyUnavailable")}</p>
          </div>
        </div>
      </aside>
    </>
  );
  return typeof document === "undefined" ? page : createPortal(page, document.body);
}
