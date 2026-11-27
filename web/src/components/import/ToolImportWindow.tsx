import { ServerAppsIcon, X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useCallback, useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import { appText } from "../admin/administratorAppsApi";
import { AppPlaceFrame } from "../apps/AppPlaceFrame";
import type { UsableApp, UsableAppImportSource } from "../../appPlatformWire.generated";
import type { Session } from "../../types";
import { ToolMigrationView } from "./ToolMigrationView";
import { UploadProgressBar } from "./UploadProgressBar";
import { useToolImport } from "./useToolImport";
import importStyles from "./ImportDialog.module.css";
import windowStyles from "./NotionMigrationWindow.module.css";
import styles from "./ToolImportWindow.module.css";
import { appBridgeRefusal } from "@/components/apps/appFrameBridge";
import { ReadFailed } from "@/components/ReadFailed";

// "〈도구〉에서 옮기기": as Notion's Confluence window, a centred window over the import
// screen — the tool's name and line, Leafen's "앱 · 〈앱〉" mark so no app
// passes as another, then the app's own screen for the tool (an account to
// connect, an export to choose). When the app hands Leafen its bundle,
// Leafen takes it and the migration's draft, run and report follow here.

export function ToolImportWindow({ usable, source, onClose, session, workspaceId }: {
  usable: UsableApp;
  source: UsableAppImportSource;
  onClose(): void;
  session: Session;
  workspaceId: string;
}) {
  const { t, i18n } = useTranslation(["importDialog", "common"]);
  const titleId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const port = useRef<MessagePort | null>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: onClose,
  });
  useEffect(() => {
    const frame = window.requestAnimationFrame(() => closeRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, []);
  const name = appText(source.name, i18n.language);
  const tool = useToolImport({ session, workspaceId, label: name, tool: `${usable.appId}:${source.key}` });
  const { phase, earlier, ready, earlierFailed, readEarlierAgain, handOff, finish } = tool;
  const extra = useCallback(async (method: string, params: Record<string, unknown>) => {
    if (method !== "importBundle") throw appBridgeRefusal("unknownCall");
    const bundle = params.bundle;
    if (!(bundle instanceof Blob) || bundle.size === 0) throw new Error("invalid_input: Hand Leafen the bundle as a Blob.");
    await handOff(bundle);
    return null;
  }, [handOff]);
  const onDone = useCallback(() => { void finish(); }, [finish]);

  const window_ = (
    <div className={importStyles.overlay} style={{ zIndex }} data-tool-import-window={`${usable.appId}:${source.key}`}>
      <button type="button" className={importStyles.backdrop} onClick={onClose} tabIndex={-1} aria-label={t("importDialog:close")} />
      <section ref={dialogRef} className={`${importStyles.dialog} ${windowStyles.window}`} role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <header className={windowStyles.header}>
          <div>
            <h2 id={titleId}>{t("importDialog:tools.windowTitle", { name })}</h2>
            <p>{appText(source.description, i18n.language)}</p>
            <span className={styles.mark} data-tool-import-mark>
              <ServerAppsIcon size="small" aria-hidden="true" />
              {t("common:appPlace.markNamed", { app: appText(usable.name, i18n.language) })}
            </span>
          </div>
          <button ref={closeRef} type="button" className={importStyles.close} onClick={onClose} aria-label={t("importDialog:close")}>
            <X size="regular" aria-hidden="true" />
          </button>
        </header>
        <div className={styles.body}>
          {phase.kind === "app" ? (
            earlierFailed ? <ReadFailed message={t("importDialog:tools.earlierFailed")} onRetry={readEarlierAgain} /> : ready ? (
              <>
                {earlier.length ? <p className={styles.note} data-tool-import-earlier>{t("importDialog:tools.earlier")}</p> : null}
                <AppPlaceFrame session={session} usable={usable} place="importSource" title={name}
                  opens={{ surface: "importSource", importSource: source.key }}
                  context={() => ({ importSource: { key: source.key, earlier } })}
                  extra={extra} port={port} style={{ height: 380 }} />
              </>
            ) : <p className={styles.note} role="status">{t("importDialog:migration.reading")}</p>
          ) : phase.kind === "staging" ? (
            <div className={importStyles.jobPanel} role="status" data-tool-import-staging={phase.step}>
              <strong>{t(`importDialog:tools.staging.${phase.step}`)}</strong>
              <UploadProgressBar className={importStyles.progressTrack} label={t(`importDialog:tools.staging.${phase.step}`)}
                progress={{ phase: phase.step === "place" ? "preparing" : "uploading", percent: phase.percent }} />
              <p>{t("importDialog:tools.stagingLead")}</p>
            </div>
          ) : phase.kind === "migrate" ? (
            <ToolMigrationView source={phase.source} earlier={phase.earlier} session={session} workspaceId={workspaceId} onDone={onDone} />
          ) : (
            <div className={importStyles.serverStatus} role="alert" data-error data-tool-import-failed>{phase.message}</div>
          )}
        </div>
      </section>
    </div>
  );
  return typeof document === "undefined" ? window_ : createPortal(window_, document.body);
}
