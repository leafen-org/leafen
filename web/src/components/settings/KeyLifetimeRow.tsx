import { useTranslation } from "react-i18next";
import { AppSelect } from "@/components/AppSelect";
import { activeDateLocale } from "@/lib/i18n";
import {
  personalAccessTokenExpiry,
  PERSONAL_ACCESS_TOKEN_LIFETIMES,
  type PersonalAccessTokenLifetime,
} from "@/lib/personalAccessTokens";
import styles from "./DeveloperSettings.module.css";

/** Each lifetime's word, as Notion's menu names them. */
export const KEY_LIFETIME_KEYS: Record<PersonalAccessTokenLifetime, string> = {
  "7d": "personalAccessTokenLifetime7d",
  "30d": "personalAccessTokenLifetime30d",
  "90d": "personalAccessTokenLifetime90d",
  "180d": "personalAccessTokenLifetime180d",
  "1y": "personalAccessTokenLifetime1y",
};

/** The day a key made now with this lifetime ends, as the person reads dates. */
export function keyLifetimeEnd(lifetime: PersonalAccessTokenLifetime) {
  return new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(personalAccessTokenExpiry(lifetime));
}

/**
 * The expiry row of Notion's "새 개인 액세스 토큰" (seen 2026-10-02 and
 * 2026-11-13): "만료일" over the day the key ends, and the small "1년 ⌄" at
 * the row's end. Agent keys take the same lifetimes (agent key decision of
 * 2026-11-16), so their windows share it.
 */
export function KeyLifetimeRow({ value, onChange }: {
  value: PersonalAccessTokenLifetime;
  onChange(lifetime: PersonalAccessTokenLifetime): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  return (
    <div className={styles.expiryRow} data-key-lifetime={value}>
      <span className={styles.expiryText}>
        <span className={styles.fieldLabel}>{t("personalAccessTokenExpiryLabel")}</span>
        <span className={styles.fieldNote}>{t("personalAccessTokenExpiryNote", { date: keyLifetimeEnd(value) })}</span>
      </span>
      <AppSelect look="value" plain="ticked" className={styles.expiryChoice} menuWidth={180} menuGap={8} menuAlign="end"
        aria-label={t("personalAccessTokenExpiryLabel")} value={value}
        onChange={(event) => onChange(event.target.value as PersonalAccessTokenLifetime)}>
        {PERSONAL_ACCESS_TOKEN_LIFETIMES.map((lifetime) => (
          <option key={lifetime} value={lifetime}>{t(KEY_LIFETIME_KEYS[lifetime])}</option>
        ))}
      </AppSelect>
    </div>
  );
}
