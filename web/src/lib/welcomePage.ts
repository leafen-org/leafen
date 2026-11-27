import { i18next } from "@/i18n";
import { workspaceMembershipBounds } from "@/workspaceMembershipWire.generated";
import type { Session } from "@/types";

/**
 * A new workspace is named for the person who makes it, as Notion's are
 * ("〈이름〉님의 워크스페이스", seen 2026-10-06), so two people's first
 * workspaces never read the same in a workspace menu.
 */
export function defaultWorkspaceName(session: Session, language?: string) {
  const translate = language ? i18next.getFixedT(language) : i18next.t.bind(i18next);
  const name = session.user.name?.trim() || session.user.email?.split("@")[0]?.trim() || "";
  // A long name leaves a name the workspace can still take.
  return Array.from(String(translate("common:generated.workspaceName", { name })))
    .slice(0, workspaceMembershipBounds.creationNameRunes).join("");
}
