import { useState } from "react";
import { useTranslation } from "react-i18next";
import { SwitchInput } from "@/components/Switch";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { PRINT_PAPER_FORMATS, type PrintPaperFormat } from "@/lib/printExportDocument";
import type { PageExportChoice, PageExportFormat } from "./PageExportDialog";
import desktop from "./PageExportDialog.module.css";
import styles from "./PageExportPhone.module.css";

/**
 * A page's export on a phone, as Notion's (seen 2026-11-17: Galaxy A90
 * Chrome 411 light, iPhone Safari 402 dark): the window covers the screen
 * (phoneWindowSheet.ts), "내보내기" in its head and a blue "닫기" at the
 * head's end; under it the rows Notion's phone keeps — the format, a PDF's
 * paper, "하위 페이지 포함" — and then a blue "내보내기" row. The format and
 * the paper are chosen in a sheet of their names, unticked. The desktop's
 * other choices keep their first values.
 */
export function PageExportPhone({
  titleId, choice, formats, busy, turnedOff, error, onFormat, update, onClose, onExport,
}: {
  titleId: string;
  choice: PageExportChoice;
  formats: readonly PageExportFormat[];
  busy: boolean;
  turnedOff: boolean;
  error: string;
  onFormat(format: PageExportFormat): void;
  update(change: Partial<PageExportChoice>): void;
  onClose(): void;
  onExport(): void;
}) {
  const { t } = useTranslation(["pageExport", "common"]);
  const [picking, setPicking] = useState<"format" | "paper" | null>(null);
  const archive = choice.format === "leafen_archive";
  const note = archive ? t("pageExport:archiveNote") : choice.format === "pdf" ? t("pageExport:pdfNote") : "";
  return <>
    <header className={styles.head}>
      <h2 id={titleId} className={styles.title}>{t("pageExport:title")}</h2>
      <button type="button" className={styles.close} disabled={busy} onClick={onClose}>{t("common:actions.close")}</button>
    </header>
    <fieldset className={styles.body} disabled={busy || turnedOff}>
      <div className={styles.section}>
        <div className={styles.rows}>
          <button type="button" className={styles.row} data-page-export-format onClick={() => setPicking("format")}>
            <span className={styles.label}>{t("pageExport:format")}</span>
            <span className={styles.value}>{t(`pageExport:formats.${choice.format}`)}</span>
          </button>
          {choice.format === "pdf" ? (
            <button type="button" className={styles.row} data-page-export-paper onClick={() => setPicking("paper")}>
              <span className={styles.label}>{t("pageExport:pageFormat")}</span>
              <span className={styles.value}>{t(`pageExport:paper.${choice.paper}`)}</span>
            </button>
          ) : null}
          {archive ? null : (
            <label className={styles.row}>
              <span className={styles.label}>{t("pageExport:includeSubpages")}</span>
              <SwitchInput checked={choice.includeSubpages} data-page-export-toggle="includeSubpages"
                onChange={(event) => update({ includeSubpages: event.target.checked })} />
            </label>
          )}
        </div>
        {note ? <p className={styles.note}>{note}</p> : null}
        {turnedOff || error ? <p className={styles.error} role="alert">{turnedOff ? t("pageExport:turnedOff") : error}</p> : null}
      </div>
      <div className={styles.section}>
        <div className={styles.rows}>
          <button type="button" className={`${styles.row} ${styles.action}`} data-off={turnedOff ? "true" : undefined} onClick={onExport}>
            {busy ? <span className={styles.progress} role="status" data-page-export-progress>
              {t("pageExport:exporting")}<span className={desktop.spinner} aria-hidden="true" />
            </span> : t("pageExport:export")}
          </button>
        </div>
      </div>
    </fieldset>
    {picking ? (
      <PhoneSheet title="" label={t(picking === "format" ? "pageExport:format" : "pageExport:pageFormat")} onClose={() => setPicking(null)}>
        <PhoneSheetGroup>
          {picking === "format" ? formats.map((format) => (
            <PhoneSheetRow key={format} label={t(`pageExport:formats.${format}`)} more={false} data={{ "data-page-export-choice": format }}
              onPress={() => { onFormat(format); setPicking(null); }} />
          )) : PRINT_PAPER_FORMATS.map((paper: PrintPaperFormat) => (
            <PhoneSheetRow key={paper} label={t(`pageExport:paper.${paper}`)} more={false} data={{ "data-page-export-choice": paper }}
              onPress={() => { update({ paper }); setPicking(null); }} />
          ))}
        </PhoneSheetGroup>
      </PhoneSheet>
    ) : null}
  </>;
}
