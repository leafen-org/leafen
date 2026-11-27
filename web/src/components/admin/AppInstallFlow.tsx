import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AppPackageReview, AppPermission, AppTenancy } from "@/appPlatformWire.generated";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { appText, fetchCatalogApp, installAppPackage, listInstalledApps, setInstalledAppGrants } from "./administratorAppsApi";
import { AppPackageReviewDialog } from "./AppPackageReviewDialog";
import { AppSecretsDialog } from "./AppSecretsDialog";
import { useAppPackageRead } from "./useAppPackageRead";

// What a just-installed app may need from its administrator before it works.
const SETUP_KINDS = ["secret", "outsideSignIn", "webhook", "settingSocket", "personCredential"];

/**
 * Installing one catalog app from where it is listed: its review over the
 * page, who may use it, then what it needs set up, without leaving the page.
 */
export function AppInstallFlow({ session, catalogId, appId, name, version, currentTenancy, onInstalled, onClose }: {
  session: Session;
  /** The organization the person installs from ("org:<id>"): its people may use the app at first. */
  currentTenancy?: string;
  catalogId: string;
  appId: string;
  /** The app's name and version as listed, said while it is read. */
  name?: Record<string, string>;
  version?: string;
  onInstalled: (name: string) => void;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation(["serverConsole", "common"]);
  const [review, setReview] = useState<AppPackageReview | null>(null);
  const [tenancies, setTenancies] = useState<AppTenancy[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [settingUp, setSettingUp] = useState<{ installId: string; name: string } | null>(null);
  // The review's window says it reads the app from the press on, and why it
  // couldn't, with another try (2026-11-12).
  const packages = useAppPackageRead(setReview);

  useEffect(() => {
    packages.read({ source: "catalog", name: name ? appText(name, i18n.language) : undefined, detail: version ? t("apps.version", { version }) : undefined },
      (signal) => Promise.all([fetchCatalogApp(session, catalogId, appId, signal), listInstalledApps(session, signal)]).then(([package_, listed]) => {
        setTenancies(listed.tenancies ?? []);
        return package_;
      }), (failure) => settingsErrorMessage(failure, t("apps.browse.fetchFailed")));
    // One flow reads its app once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function install(acceptDataRemovals: boolean, everyone: readonly string[], personCredentials?: boolean) {
    if (!review) return;
    setBusy(true);
    try {
      const installed = await installAppPackage(session, review, acceptDataRemovals, personCredentials);
      if (everyone.length) {
        await setInstalledAppGrants(session, installed.installId, everyone.map((tenancyKey) => ({ tenancyKey, everyone: true, userIds: [] })));
      }
      const name = appText(installed.name, i18n.language);
      if (installed.permissions.some((permission: AppPermission) => SETUP_KINDS.includes(permission.kind))) {
        setSettingUp({ installId: installed.installId, name });
      } else {
        onInstalled(name);
      }
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      setBusy(false);
    }
  }

  if (settingUp) {
    return (
      <AppSecretsDialog session={session} installId={settingUp.installId} name={settingUp.name} tenancies={tenancies}
        onClose={() => onInstalled(settingUp.name)} />
    );
  }
  return review || packages.reading ? (
    <AppPackageReviewDialog review={review} reading={packages.reading} onReadAgain={packages.again} tenancies={tenancies} currentTenancy={currentTenancy}
      busy={busy} inTrash={false} error={error}
      onInstall={(acceptDataRemovals, everyone, personCredentials) => void install(acceptDataRemovals, everyone, personCredentials)} onClose={onClose} />
  ) : null;
}
