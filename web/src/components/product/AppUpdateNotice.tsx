import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { X } from "@/icons/leafen";
import { desktopUpdater } from "@/lib/desktopUpdater";
import { isPackagedClient } from "@/lib/packagedClient";
import styles from "../admin/BackupFailureNotice.module.css";

/** A newer version: fetched and ready on a Mac, out for the person to install on the Windows zip. */
type Version = { version: string; security: boolean };
type RolledBack = { version: string };

async function callUpdater<T>(method: string, ...args: unknown[]): Promise<T> {
  const { Call } = await import("@wailsio/runtime");
  return Call.ByName(`main.Updater.${method}`, ...args) as Promise<T>;
}

/**
 * The desktop app's band about its own update (2026-10-17): on a Mac a newer
 * version fetched and checked says a restart switches to it, a security one
 * more plainly, and the band goes when updating is turned off; a version that
 * didn't come up and was rolled back says so once. The Windows zip installs
 * nothing by itself (2026-11-17): a newer version out says so with its
 * download page — the Microsoft Store or the new zip — until the person
 * closes it for this run. Nothing shows in a browser, on a phone or in a
 * store's app.
 */
export function useAppUpdateNotice() {
  const { t } = useTranslation("app");
  const [staged, setStaged] = useState<Version | null>(null);
  const [available, setAvailable] = useState<Version | null>(null);
  const [dismissed, setDismissed] = useState<string | null>(null);
  const [rolledBack, setRolledBack] = useState<RolledBack | null>(null);
  const [restarting, setRestarting] = useState(false);

  // Only the desktop app updates: a browser loads its server's version, a
  // phone app comes from its store.
  useEffect(() => {
    if (!isPackagedClient() || /Android|iPhone|iPad/u.test(navigator.userAgent)) return undefined;
    let live = true;
    const stops: Array<() => void> = [];
    const follow = (event: string, set: (value: Version | null) => void) => {
      void import("@wailsio/runtime").then(({ Events }) => {
        if (!live) return;
        stops.push(Events.On(event, (said: { data: unknown }) => {
          const value = (Array.isArray(said.data) ? said.data[0] : said.data) as Version | null;
          set(value?.version ? value : null);
        }));
      }, () => undefined);
    };
    void desktopUpdater.state().then((state) => {
      if (!live) return;
      if (state.manual) {
        if (!state.enabled) return;
        void callUpdater<Version | null>("Available").then((value) => { if (live && value?.version) setAvailable(value); }, () => undefined);
        follow("leafen:update-available", setAvailable);
        return;
      }
      void callUpdater<Version | null>("Staged").then((value) => { if (live && value?.version) setStaged(value); }, () => undefined);
      void callUpdater<RolledBack | null>("TakeRolledBack").then((value) => { if (live && value?.version) setRolledBack(value); }, () => undefined);
      follow("leafen:update-staged", setStaged);
    }, () => undefined);
    return () => { live = false; stops.forEach((stop) => stop()); };
  }, []);

  if (rolledBack) {
    const text = t("appUpdate.rolledBack", { version: rolledBack.version });
    return (
      <div className={styles.suggestion} role="status" data-app-update-rolled-back>
        <span className={styles.text} title={text}>{text}</span>
        <button type="button" className={styles.dismiss} onClick={() => setRolledBack(null)}
          aria-label={t("appUpdate.dismiss")} title={t("appUpdate.dismiss")}>
          <X size="regular" aria-hidden="true" />
        </button>
      </div>
    );
  }
  if (available && available.version !== dismissed) {
    const text = t(available.security ? "appUpdate.securityAvailable" : "appUpdate.available", { version: available.version });
    return (
      <div className={available.security ? styles.notice : styles.suggestion} role="status"
        data-app-update={available.security ? "security-available" : "available"}>
        <span className={styles.text} title={text}>{text}</span>
        <button type="button" className={available.security ? styles.action : styles.suggestionAction}
          onClick={() => { void callUpdater("OpenDownload").catch(() => undefined); }}>
          {t("appUpdate.openDownload")}
        </button>
        <button type="button" className={styles.dismiss} onClick={() => setDismissed(available.version)}
          aria-label={t("appUpdate.dismiss")} title={t("appUpdate.dismiss")}>
          <X size="regular" aria-hidden="true" />
        </button>
      </div>
    );
  }
  if (!staged) return null;
  const text = t(staged.security ? "appUpdate.securityReady" : "appUpdate.ready", { version: staged.version });
  return (
    <div className={staged.security ? styles.notice : styles.suggestion} role="status" data-app-update={staged.security ? "security" : "ready"}>
      <span className={styles.text} title={text}>{text}</span>
      <button type="button" className={staged.security ? styles.action : styles.suggestionAction} disabled={restarting}
        onClick={() => { setRestarting(true); void callUpdater("RestartNow").catch(() => setRestarting(false)); }}>
        {t("appUpdate.restart")}
      </button>
    </div>
  );
}
