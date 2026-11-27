import { useTranslation } from "react-i18next";
import inherited from "./TopBarShareInherited.module.css";

/**
 * The mark beside a person's name wherever sharing lists them, as Notion's
 * (seen 2026-11-05, 2026-11-07): "게스트" for a guest of the workspace, else
 * "제한됨" for one of its restricted members — their own line too. A phone's
 * "사용자 선택" also marks a workspace owner "워크스페이스 소유자" in grey
 * (seen 2026-11-12 at 402pt), where `owner` is asked for.
 */
export function SharePersonMark({ owner, guest, restricted }: { owner?: boolean; guest?: boolean; restricted?: boolean }) {
  const { t } = useTranslation("topBar");
  if (guest) return <span className={inherited.guest} data-share-guest>{t("guest")}</span>;
  if (restricted) return <span className={inherited.guest} data-share-restricted>{t("restrictedTag")}</span>;
  if (owner) return <span className={inherited.ownerTag} data-share-workspace-owner>{t("workspaceOwnerTag")}</span>;
  return null;
}
