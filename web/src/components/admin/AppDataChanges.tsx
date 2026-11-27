import { useTranslation } from "react-i18next";
import type { AppDataChange } from "@/appPlatformWire.generated";
import styles from "./AdministratorApps.module.css";

/**
 * What an update changes in the app's data (docs/app-platform.md 6): what it
 * adds, the columns it adds, converts or drops, what goes to the trash, and
 * settings it leaves as people may have changed them. A line only the
 * organizations an earlier update didn't finish in get says so. Each line
 * has the review's one grey dot, with no risk colors (the calm review of
 * 2026-11-05); what goes to the trash comes first, as the server sorts it,
 * and waits for the administrator's check.
 */
export function AppDataChanges({ changes, accepted, busy, onAccept }: {
  changes: AppDataChange[];
  accepted: boolean;
  busy: boolean;
  onAccept: (accepted: boolean) => void;
}) {
  const { t } = useTranslation("serverConsole");
  if (!changes.length) return null;
  const line = (change: AppDataChange) => change.behind ? t("apps.data.behind", { line: what(change) }) : what(change);
  const what = (change: AppDataChange) => {
    switch (change.kind) {
      case "add":
        return t("apps.data.add", { name: change.name });
      case "remove":
        return t("apps.data.remove", { name: change.name });
      case "kept":
        return t("apps.data.kept", { name: change.name });
      case "left":
        return t(change.rows ? "apps.data.leftRows" : "apps.data.left", { name: change.name });
    }
    const parts = [
      change.added.length ? t("apps.data.columnsAdded", { names: change.added.join(", ") }) : "",
      change.converted.length ? t("apps.data.columnsConverted", { names: change.converted.join(", ") }) : "",
      change.removed.length ? t("apps.data.columnsRemoved", { names: change.removed.join(", ") }) : "",
    ].filter(Boolean);
    return `${change.name}: ${parts.join(" · ")}`;
  };
  return (
    <>
      <div className={styles.permissionsTitle}>{t("apps.data.title")}</div>
      <ul className={styles.permissions} data-app-data-changes>
        {changes.map((change, index) => (
          <li key={`${index}:${change.key}`} data-app-data-behind={change.behind ? "" : undefined} data-app-data-change={change.kind}>{line(change)}</li>
        ))}
      </ul>
      {changes.some((change) => change.destructive) ? (
        <label className={styles.acknowledge}>
          <input type="checkbox" checked={accepted} disabled={busy} data-app-data-accept onChange={(event) => onAccept(event.target.checked)} />
          {t("apps.data.accept")}
        </label>
      ) : null}
    </>
  );
}
