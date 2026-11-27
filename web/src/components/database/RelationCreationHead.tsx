import { useTranslation } from "react-i18next";
import { ArrowLeft, X } from "../icons";
import styles from "./RelationPropertyCreationDialog.module.css";

/**
 * A new relation's head: its screen's name and "×". Listing the databases
 * again from "새 관계형" it has a "←" before the name, back to "새 관계형"
 * with the database chosen, as Notion's (seen 2026-11-16 at 1440: a 24×22
 * button 10px in, the name 8px after it).
 */
export function RelationCreationHead({ title, busy, onBack, onCancel }: {
  title: string;
  busy: boolean;
  onBack?: () => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation(["propertyTypeConfig", "common"]);
  return (
    <div className={styles.relationCreateHeader} data-back={onBack ? "true" : undefined}>
      {onBack ? (
        <button type="button" className={styles.relationCreateBack} aria-label={t("common:actions.back")} disabled={busy} onClick={onBack}>
          <ArrowLeft size="small" aria-hidden="true" />
        </button>
      ) : null}
      <strong>{title}</strong>
      <button type="button" className={styles.relationCreateClose} aria-label={t("propertyTypeConfig:cancelRelationCreation")}
        disabled={busy} onClick={onCancel}>
        <X size="tiny" aria-hidden="true" />
      </button>
    </div>
  );
}
