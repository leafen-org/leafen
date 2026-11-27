import { useTranslation } from "react-i18next";
import { EyeSlashIcon, LockIcon } from "@/icons/leafen";
import styles from "./PropertyAccessMarks.module.css";

/** The lock a restricted property's name shows, to everyone, as Notion's (seen 2026-10-12). */
export function PropertyAccessLock() {
  const { t } = useTranslation("tableView");
  return (
    <span className={styles.lock} role="img" aria-label={t("propertyAccessRestricted")} data-property-access-lock>
      <LockIcon size="tiny" aria-hidden="true" />
    </span>
  );
}

/** A value the reader may not see: "사용 권한 없음" beside a struck eye, as Notion's. */
export function PropertyNoAccessValue() {
  const { t } = useTranslation("tableView");
  return (
    <span className={styles.noAccess} data-property-no-access>
      <EyeSlashIcon size="small" aria-hidden="true" />{t("propertyNoAccess")}
    </span>
  );
}
