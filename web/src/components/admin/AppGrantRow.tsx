import { useId } from "react";
import { useTranslation } from "react-i18next";
import type { AppGrant } from "@/appPlatformWire.generated";
import peopleStyles from "./AppGrantPeopleDialog.module.css";
import styles from "./AppGrantRow.module.css";

/**
 * An organization's line under an app's "사용할 수 있는 사람": its everyone
 * check, then one column holding the organization's name and the link to the
 * people chosen. A name or link that wraps goes on under the name's first
 * letter, not under the check (2026-11-15: at 768 and on a phone "사람 선택"
 * dropped under the check box).
 */
export function AppGrantRow({ tenancyKey, tenancyName, grant, disabled, onEveryone, onChoose }: {
  tenancyKey: string;
  tenancyName: string;
  grant: AppGrant | undefined;
  disabled: boolean;
  onEveryone(value: boolean): void;
  onChoose(): void;
}) {
  const { t } = useTranslation("serverConsole");
  const id = useId();
  return (
    <span className={styles.grant}>
      <span className={styles.check}>
        <input id={id} type="checkbox" disabled={disabled} data-app-grant={tenancyKey}
          checked={Boolean(grant?.everyone)} onChange={(event) => onEveryone(event.target.checked)} />
      </span>
      <span className={styles.text}>
        <label htmlFor={id}>{t("apps.everyoneIn", { tenancy: tenancyName })}</label>
        {grant?.everyone ? null : (
          <button type="button" className={peopleStyles.peopleButton} disabled={disabled} data-app-grant-people-open={tenancyKey} onClick={onChoose}>
            {grant?.groupIds?.length
              ? t("apps.people.openGroups", { groups: grant.groupIds.length, count: grant.userIds.length })
              : grant?.userIds.length ? t("apps.people.open", { count: grant.userIds.length }) : t("apps.people.choose")}
          </button>
        )}
      </span>
    </span>
  );
}
