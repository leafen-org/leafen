"use client";

import { SwitchInput } from "@/components/Switch";
import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { enterpriseDLPErrorCodes } from "@/enterpriseDLPWire.generated";
import { organizationErrorCodes } from "@/organizationWire.generated";
import { workspaceExportErrorCodes } from "@/workspaceExportWire.generated";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { phoneWindowForm } from "@/lib/phoneWindowSheet";
import { phoneScreen } from "@/lib/screenKind";
import {
  clampPrintScale,
  PRINT_PAPER_FORMATS,
  PRINT_SCALE_BOUNDS,
  type PrintPaperFormat,
} from "@/lib/printExportDocument";
import type { Session } from "@/types";
import styles from "./PageExportDialog.module.css";
import { AppSelect } from "@/components/AppSelect";
import { PageExportPhone } from "./PageExportPhone";

export type PageExportFormat = "pdf" | "html" | "markdown_csv" | "leafen_archive";

export type PageExportChoice = {
  format: PageExportFormat;
  includeFiles: boolean;
  includeSubpages: boolean;
  createFolders: boolean;
  includeComments: boolean;
  /** Databases show their first view instead of the one their block shows. */
  defaultDatabaseViews: boolean;
  /** A whole workspace's private pages go too (Notion's "개인 페이지 포함", off). */
  includePrivatePages: boolean;
  paper: PrintPaperFormat;
  scale: number;
};

const FORMATS: PageExportFormat[] = ["pdf", "html", "markdown_csv", "leafen_archive"];
// The format last chosen opens the next export, as Notion's does (seen 2026-10-08), in this browser.
const FORMAT_KEY = "leafen:page-export-format";

function rememberedFormat(formats: readonly PageExportFormat[]): PageExportFormat {
  let stored: string | null = null;
  try { stored = window.localStorage.getItem(FORMAT_KEY); } catch { /* no storage: the first format */ }
  return formats.find((format) => format === stored) ?? (formats.includes("pdf") ? "pdf" : formats[0] ?? "pdf");
}

function rememberFormat(format: PageExportFormat) {
  try { window.localStorage.setItem(FORMAT_KEY, format); } catch { /* the choice just isn't kept */ }
}

/** A new export's choices, on the format last chosen. */
export function initialExportChoice(formats: readonly PageExportFormat[]): PageExportChoice {
  return {
    format: rememberedFormat(formats), includeFiles: true, includeSubpages: false, createFolders: true,
    includeComments: false, defaultDatabaseViews: false, includePrivatePages: false, paper: "A4", scale: PRINT_SCALE_BOUNDS.default,
  };
}

/**
 * The rows of Notion's export window, the same for a page, a database and a
 * whole workspace (seen 2026-11-04): format, database view, page content, a
 * PDF's paper and scale, subpages (not for a workspace, which is every page),
 * a workspace's private pages (seen 2026-11-13, off at first), their folders,
 * an HTML's comments.
 */
export function ExportChoiceRows({ choice, update, formats, archiveNote, everyPage = false }: {
  choice: PageExportChoice; update: (change: Partial<PageExportChoice>) => void; formats: readonly PageExportFormat[];
  archiveNote: string; everyPage?: boolean;
}) {
  const { t } = useTranslation("pageExport");
  const readable = choice.format === "html" || choice.format === "markdown_csv";
  // Each choice's list, as Notion's export window's (seen 2026-11-07): 180px
  // wide at its button's end, 4px under it, its rows unmarked and none lit.
  const list = { look: "value", plain: "spaced", menuAlign: "end", menuWidth: 180 } as const;
  const toggle = (key: "includeSubpages" | "includePrivatePages" | "createFolders" | "includeComments", label: string, disabled = false) => (
    <label className={styles.toggleRow} data-disabled={disabled ? "true" : undefined}>
      <span>{label}</span>
      <SwitchInput checked={choice[key]} disabled={disabled} data-page-export-toggle={key}
        onChange={(event) => update({ [key]: event.target.checked })} />
    </label>
  );
  return <>
    <label className={styles.row}>
      <span>{t("format")}</span>
      <AppSelect aria-label={t("format")} {...list} value={choice.format} data-page-export-format
        onChange={(event) => {
          const format = event.target.value as PageExportFormat;
          rememberFormat(format);
          update({ format });
        }}>
        {formats.map((format) => <option key={format} value={format}>{t(`formats.${format}`)}</option>)}
      </AppSelect>
    </label>
    {choice.format === "leafen_archive" ? <p className={styles.note}>{archiveNote}</p> : <>
      <label className={styles.row}>
        <span>{t("includeDatabases")}</span>
        <AppSelect aria-label={t("includeDatabases")} {...list} value={choice.defaultDatabaseViews ? "default" : "current"}
          data-page-export-database-view onChange={(event) => update({ defaultDatabaseViews: event.target.value === "default" })}>
          <option value="current">{t("databaseView.current")}</option>
          <option value="default">{t("databaseView.default")}</option>
        </AppSelect>
      </label>
      <label className={styles.row}>
        <span>{t("includeContent")}</span>
        <AppSelect aria-label={t("includeContent")} {...list} value={choice.includeFiles ? "everything" : "noFiles"}
          data-page-export-content onChange={(event) => update({ includeFiles: event.target.value === "everything" })}>
          <option value="everything">{t("content.everything")}</option>
          <option value="noFiles">{t("content.noFiles")}</option>
        </AppSelect>
      </label>
      {choice.format === "pdf" ? <>
        <label className={styles.row}>
          <span>{t("pageFormat")}</span>
          <AppSelect aria-label={t("pageFormat")} {...list} value={choice.paper} data-page-export-paper
            onChange={(event) => update({ paper: event.target.value as PrintPaperFormat })}>
            {PRINT_PAPER_FORMATS.map((paper) => <option key={paper} value={paper}>{t(`paper.${paper}`)}</option>)}
          </AppSelect>
        </label>
        <label className={styles.row}>
          <span>{t("scale")}</span>
          <input type="number" className={styles.scale} data-arrows="none" min={PRINT_SCALE_BOUNDS.min} max={PRINT_SCALE_BOUNDS.max} value={choice.scale}
            onChange={(event) => update({ scale: Number(event.target.value) })} />
        </label>
      </> : null}
      {everyPage ? null : toggle("includeSubpages", t("includeSubpages"))}
      {everyPage && readable ? toggle("includePrivatePages", t("includePrivatePages")) : null}
      {readable ? toggle("createFolders", t("createFolders"), !everyPage && !choice.includeSubpages) : null}
      {choice.format === "html" ? toggle("includeComments", t("includeComments")) : null}
      {choice.format === "pdf" ? <p className={styles.note}>{t("pdfNote")}</p> : null}
    </>}
  </>;
}

/**
 * Notion's page export: a format and the choices that format takes, in
 * Notion's order and words (seen 2026-10-08) — format, database view, page
 * content, a PDF's paper and scale, subpages, their folders (only with
 * subpages), an HTML's comments — under no heading. The Leafen archive always
 * carries files and subpages so it can be restored. A database page offers
 * Notion's formats alone, as its database restores it. A PDF opens the
 * browser's print window, so it has no folders. On a phone it is Notion's
 * phone screen of fewer rows (PageExportPhone).
 */
export function PageExportDialog({
  formats = FORMATS,
  policyFor,
  onClose,
  onExport,
}: {
  formats?: readonly PageExportFormat[];
  /** The page whose export the organization may have turned off, asked when the dialog opens. */
  policyFor?: { session: Session; workspaceId: string; pageId: string };
  onClose: () => void;
  onExport: (choice: PageExportChoice, signal: AbortSignal) => Promise<void>;
}) {
  const { t } = useTranslation(["pageExport", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  // A modal layer: the page stays still, Escape closes it while nothing
  // opened over it takes the key, and the focus goes back to its opener —
  // ringed and named in its tip after a press on its shade too, as Notion's
  // "⋯" (seen 2026-11-26 in Chrome at 1440, two tabs at different times).
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: "first",
    ringsAfterPress: true,
  });
  const operationRef = useRef<AbortController | null>(null);
  const [choice, setChoice] = useState<PageExportChoice>(() => initialExportChoice(formats));
  const [busy, setBusy] = useState(false);
  // A failure answers the choice it was made with: changing the format, what
  // goes in or a setting puts it away (useAttemptNotice), as an archive's
  // "links outside" says nothing of a Markdown & CSV export.
  const [error, setError] = useAttemptNotice<string>(choice);

  useEffect(() => () => operationRef.current?.abort(), []);

  // An export the organization's settings keep in says so as the dialog
  // opens, with the export button dimmed, rather than after the choice.
  const [turnedOff, setTurnedOff] = useState(false);
  const policySession = policyFor?.session;
  const policyWorkspaceId = policyFor?.workspaceId;
  const policyPageId = policyFor?.pageId;
  useEffect(() => {
    if (!policySession || !policyWorkspaceId || !policyPageId) return;
    let current = true;
    api.workspaceExportPolicy(policySession, policyWorkspaceId, "page", policyPageId).then((policy) => {
      if (current && !policy.allowed) setTurnedOff(true);
    }, () => undefined);
    return () => { current = false; };
  }, [policySession, policyWorkspaceId, policyPageId]);

  const update = (change: Partial<PageExportChoice>) => setChoice((current) => ({ ...current, ...change }));
  // On a phone whose form for it is the whole screen (phoneWindowSheet.ts), its content is the phone's screen.
  const [phone] = useState(() => phoneScreen() && phoneWindowForm("data-page-export-dialog") === "screen");

  async function submit() {
    if (busy) return;
    const controller = new AbortController();
    operationRef.current = controller;
    setBusy(true);
    setError("");
    try {
      await onExport({ ...choice, scale: clampPrintScale(choice.scale) }, controller.signal);
      if (!controller.signal.aborted) onClose();
    } catch (caught) {
      if (!controller.signal.aborted && !(caught instanceof DOMException && caught.name === "AbortError")) {
        // An export the organization's settings refuse says so, rather than
        // to try again.
        const refused = caught instanceof LeafenApiError && (caught.code === organizationErrorCodes.sharingSecurityBlocked
          || caught.code === enterpriseDLPErrorCodes.policyBlocked);
        // An archive whose linked database or relation reaches outside the
        // page says so and where to go instead, rather than to try again.
        const linkOutside = typeof caught === "object" && caught !== null
          && [Reflect.get(caught, "causeCode"), Reflect.get(caught, "code")].includes(workspaceExportErrorCodes.linkOutside);
        if (refused) setTurnedOff(true);
        else setError(t(linkOutside ? "pageExport:linkOutside" : "pageExport:failed"));
      }
    } finally {
      if (operationRef.current === controller) operationRef.current = null;
      setBusy(false);
    }
  }

  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }}>
      <button
        type="button"
        className={styles.backdrop}
        tabIndex={-1}
        aria-label={t("common:actions.close")}
        onClick={() => { if (!busy) onClose(); }}
      />
      <section
        ref={dialogRef}
        className={`${styles.dialog} ${busy && !phone ? styles.dialogBusy : ""}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-busy={busy || undefined}
        data-page-export-dialog
      >
        {phone ? <PageExportPhone titleId={titleId} choice={choice} formats={formats} busy={busy} turnedOff={turnedOff} error={error ?? ""}
          onFormat={(format) => { rememberFormat(format); update({ format }); }} update={update} onClose={onClose} onExport={() => void submit()} /> : <>
        <h2 id={titleId} className={styles.title}>{t("pageExport:title")}</h2>
        {/* While it runs, the window is a small card saying so, as Notion's (seen 2026-11-07: 180×60, its words and a spinner). */}
        {busy ? <p className={styles.progress} role="status" data-page-export-progress>
          {t("pageExport:exporting")}<span className={styles.spinner} aria-hidden="true" />
        </p> : <>
        <fieldset className={styles.body} disabled={busy || turnedOff}>
          <ExportChoiceRows choice={choice} update={update} formats={formats} archiveNote={t("pageExport:archiveNote")} />
          {turnedOff ? <p className={styles.error} role="alert">{t("pageExport:turnedOff")}</p>
            : error ? <p className={styles.error} role="alert">{error}</p> : null}
        </fieldset>
        <footer className={styles.footer}>
          <button type="button" className={styles.secondary} onClick={onClose} disabled={busy}>
            {t("pageExport:cancel")}
          </button>
          <button type="button" className={styles.primary} onClick={() => void submit()} disabled={busy || turnedOff}>
            {t("pageExport:export")}
          </button>
        </footer>
        </>}
        </>}
      </section>
    </div>,
    document.body,
  );
}
