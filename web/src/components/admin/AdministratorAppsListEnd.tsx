import { useTranslation } from "react-i18next";
import { EmptyLineReadFailed } from "@/components/settings/SettingsRowNotice";
import { administratorUserStyles as rowStyles } from "./administratorUserManagementStyles";

/**
 * After the installed apps: a failed read of them with a way to read again
 * (never "no apps"), while none are shown that they load or that there are
 * none, and what an action that failed said.
 */
export function AdministratorAppsListEnd({ readFailed, onReadAgain, loading, empty, error }: {
  readFailed: string; onReadAgain: () => void; loading: boolean; empty: boolean; error: string;
}) {
  const { t } = useTranslation("serverConsole");
  return <>
    {readFailed ? <EmptyLineReadFailed onRetry={onReadAgain} data-apps-read-failed>{readFailed}</EmptyLineReadFailed>
      : empty ? <div className={rowStyles.emptyState} role={loading ? "status" : undefined}>{t(loading ? "apps.loading" : "apps.empty")}</div> : null}
    {error ? <div className={rowStyles.live} data-error="true" role="alert">{error}</div> : null}
  </>;
}
