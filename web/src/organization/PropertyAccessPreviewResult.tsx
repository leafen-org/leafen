import { Trans, useTranslation } from "react-i18next";
import type { PropertyAccessPreview } from "./propertyAccessApi";
import { LevelPicture } from "./PropertyAccessLevelPicture";
import { levelText } from "./PropertyAccessParts";
import styles from "./PropertyAccessDialog.module.css";

/**
 * What the previewed person gets and why, as Notion's preview says it
 * (columnPropertyPermissionsModal.viewAs, its words and the code choosing
 * them read 2026-11-18; seen in Chrome at 1440 the same day for its owner and
 * for someone the database isn't shared with): the level's picture and name —
 * full access to the database "전체 허용" (Notion's window says "모두 허용";
 * docs/screen-words.md keeps the share menu's word), "속성을 볼 수 없음" for none —
 * and under it where it comes from: no access to the database at all, an
 * exception for the person or for a group (its name in
 * semibold), the rule's level for everyone, or the database's own access —
 * through the teamspace, a group's share, everyone in the workspace's share,
 * or the database itself (its own page or a share to the person); someone the
 * database lets only comment is named "댓글 허용". Under it, each person
 * property's exception that would give more where it names them or where
 * they made the page, and, where two or more give different levels, that the
 * highest wins (code only: Notion's screen was not seen with these).
 */
export function PreviewResult({ preview }: { preview: PropertyAccessPreview }) {
  const { t } = useTranslation("propertyAccess");
  const label = preview.reason === "fullAccess" ? t("fullAccess") : preview.comment ? t("commentLevel")
    : preview.level === "none" ? t("cannotSeeProperty") : levelText(t, preview.level).label;
  const conditions = preview.conditions ?? [];
  return (
    <>
      <div className={styles.previewResult} data-access-preview-level={preview.level} data-access-preview-reason={preview.reason}>
        <LevelPicture level={preview.level} />
        <span className={styles.levelText}>
          <span>{label}</span>
          <span className={styles.levelDescription}><PreviewReason preview={preview} /></span>
        </span>
      </div>
      {conditions.map((condition, index) => (
        <div key={index} className={styles.previewCondition} data-access-preview-condition={condition.level}>
          <LevelPicture level={condition.level} valuesOnly={condition.level === "view" || condition.level === "none"} />
          <span className={styles.levelText}>
            <span>{levelText(t, condition.level).label}</span>
            <span className={styles.levelDescription}>
              {condition.pageCreator ? t("previewCondition.pageCreator") : t("previewCondition.mentioned", { property: condition.propertyName ?? "" })}
            </span>
          </span>
        </div>
      ))}
      {conditions.length >= 2 && new Set(conditions.map((condition) => condition.level)).size > 1
        ? <p className={styles.previewNote}>{t("previewCondition.highestWins")}</p> : null}
    </>
  );
}

const VIA_LINES = { teamspace: "previewVia.teamspace", workspace: "previewVia.workspace" } as const;

function PreviewReason({ preview }: { preview: PropertyAccessPreview }) {
  const { t } = useTranslation("propertyAccess");
  const bold = { b: <strong className={styles.groupName} /> };
  switch (preview.reason) {
    case "noDatabaseAccess":
    case "personException":
    case "defaultAccess":
      return <>{t(`previewReason.${preview.reason}`)}</>;
    case "groupException":
      return preview.groupName
        ? <Trans t={t} i18nKey="previewReason.groupException" values={{ group: preview.groupName }} components={bold} />
        : <>{t("previewReason.unnamedGroupException")}</>;
  }
  // The database's own access, said by how it came.
  if (preview.via === "group") {
    return preview.viaGroupName
      ? <Trans t={t} i18nKey="previewVia.group" values={{ group: preview.viaGroupName }} components={bold} />
      : <>{t("previewVia.unnamedGroup")}</>;
  }
  if (preview.via === "teamspace" || preview.via === "workspace") return <>{t(VIA_LINES[preview.via])}</>;
  return <>{t("inheritedNote")}</>;
}
