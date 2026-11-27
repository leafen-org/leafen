import { useTranslation } from "react-i18next";
import { ArrowRight } from "@/icons/leafen";
import styles from "./InboxPermissionChange.module.css";

/**
 * Under a line telling the reader a share of theirs changed: their name and
 * the role, as Notion's inbox shows it (seen 2026-11-16 in the second
 * account's inbox of the free test workspace, Korean and dark): a share
 * given them on the added band, the role blue ("june lee / 전체 허용", twice);
 * a role changed either way as "〈old〉 → 〈new〉" in the default color
 * ("읽기 허용 → 전체 허용", "전체 허용 → 댓글 허용"); a share taken away, the
 * name and the role both struck through (twice).
 */
export function InboxPermissionChange({ reader, from, to }: { reader: string; from: string; to: string }) {
  const { t } = useTranslation("updatesPanel");
  const role = (value: string) => t(`rows.permissionRoles.${value}`, { defaultValue: value });
  const change = !from ? "added" : !to ? "removed" : "changed";
  return (
    <span className={styles.change} data-change={change} data-inbox-permission-change>
      <span className={styles.person}>{reader}</span>
      {change === "changed" ? (
        <span className={styles.roles}>
          <span className={styles.role}>{role(from)}</span>
          <ArrowRight size="regular" aria-hidden="true" />
          <span className={styles.role}>{role(to)}</span>
        </span>
      ) : <span className={styles.role}>{role(from || to)}</span>}
    </span>
  );
}
