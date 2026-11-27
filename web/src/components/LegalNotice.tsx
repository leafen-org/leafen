import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { DEFAULT_LEGAL_LINKS, THIRD_PARTY_NOTICES_PATH } from "@/lib/legalLinks";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import styles from "./LegalNotice.module.css";

/**
 * The source, the license and the open source notices in one small line,
 * where everyone sees it — the end of the sign-in screen's panel (its foot
 * is the language's, as Notion's) and the foot of settings' 서버 계정,
 * members and guests included: Leafen is AGPL-3.0, whose
 * section 13 has a server running a changed Leafen offer its source to the
 * people using it (2026-11-22; the sign-in screen's line until 2026-10-18,
 * back). The source is the runtime config's address, the license its
 * LICENSE; the server console's version line keeps its own links.
 */
export function LegalNotice({ place }: { place: "panel" | "settings" }) {
  const { t } = useTranslation("serverConsole");
  const [sourceUrl, setSourceUrl] = useState(DEFAULT_LEGAL_LINKS.sourceUrl);
  useEffect(() => {
    let mounted = true;
    void fetchRuntimeConfig().then(({ config }) => {
      if (mounted) setSourceUrl(config.legal.sourceUrl);
    }).catch(() => undefined);
    return () => {
      mounted = false;
    };
  }, []);
  return (
    <p className={`${styles.inline} ${styles[place]}`} data-testid="legal-notice" data-legal-notice={place}>
      <a href={sourceUrl} target="_blank" rel="noreferrer noopener">{t("updates.sourceCode")}</a>
      <span aria-hidden="true" className={styles.separator}>·</span>
      <a href={`${sourceUrl}/blob/main/LICENSE`} target="_blank" rel="noreferrer noopener license">{t("updates.license")}</a>
      <span aria-hidden="true" className={styles.separator}>·</span>
      <a href={THIRD_PARTY_NOTICES_PATH} target="_blank" rel="noreferrer noopener">{t("updates.openSourceNotices")}</a>
    </p>
  );
}
