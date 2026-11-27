import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { firstRunStepsSkipped, reopenFirstRun, type FirstRunStep } from "@/lib/firstRunSteps";
import type { Session } from "@/types";
import { styles } from "./accountSettingsStyles";

/**
 * "설치 때 건너뛴 것 마저 하기": the first run's steps left for later — the
 * outside address and outside agents — brought back from
 * Settings, in the app and in a browser alike (the one-program decision of
 * 2026-10-27, 5); Settings closes and the steps show (useFirstRun).
 */
export function FirstRunSkipped({ session }: { session: Session }) {
  const { t } = useTranslation("desktopBackground");
  const [skipped, setSkipped] = useState<readonly FirstRunStep[]>([]);
  useEffect(() => {
    let current = true;
    void firstRunStepsSkipped(session).then((steps) => { if (current) setSkipped(steps); }, () => undefined);
    return () => { current = false; };
  }, [session]);
  if (skipped.length === 0) return null;
  // A section of its own, as the rows above: what was left and the way back
  // to it, not a button hanging under the last row (2026-11-06).
  return (
    <section className={styles.section} role="region" aria-label={t("finishSkippedSection")}>
      <div className={styles.sectionTitle}>{t("finishSkippedSection")}</div>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t("finishSkipped")}</strong>
          <span>{t("finishSkippedSteps", { steps: skipped.map((step) => t(`finishSkippedStep.${step}`)).join(", ") })}</span>
        </div>
        <button type="button" className={styles.secondaryButton} data-first-run-finish-skipped
          onClick={() => { setSkipped([]); void reopenFirstRun(session).catch(() => undefined); }}>
          {t("finishSkippedButton")}
        </button>
      </div>
    </section>
  );
}
