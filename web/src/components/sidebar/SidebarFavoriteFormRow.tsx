import { useTranslation } from "react-i18next";
import { FormBlockIcon, CheckboxIcon } from "@/icons/leafen";
import type { FormFavorite } from "@/lib/formFavorites";
import { formHref } from "@/lib/formViewModel";
import { pushRoute } from "@/lib/router";
import { styles as treeStyles } from "./sidebarStyles";
import { onTreeRowKeyDown } from "./pageTreeDom";

function openLink(href: string, newTab: boolean) {
  if (newTab) window.open(href, "_blank", "noopener");
  else pushRoute(href);
}

/** A form among favorites, as Notion's sidebar lists it (seen 2026-11-05): its name, or "제목 없음", which opens its page. */
export function SidebarFavoriteFormRow({ favorite }: { favorite: FormFavorite }) {
  const { t } = useTranslation("formView");
  const href = formHref(favorite.token);
  return (
    <a className={treeStyles.treeRow} href={href} role="treeitem" aria-level={1} data-sidebar-favorite-form onClick={(event) => {
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      pushRoute(href);
    }} onKeyDown={(event) => onTreeRowKeyDown(event, { open: (newTab) => openLink(href, newTab) })}>
      <span className={treeStyles.treeLeading} data-tree-leading="true">
        <span className={treeStyles.treeIcon} data-tree-icon="true" aria-hidden="true">{favorite.icon || <FormBlockIcon size="small" />}</span>
      </span>
      <span className={treeStyles.treeTitle} data-tree-title="true">{favorite.title || t("untitledForm")}</span>
    </a>
  );
}

/** "내 작업" among favorites, as Notion's sidebar lists it (seen 2026-11-05). */
export function SidebarFavoriteMyTasksRow() {
  const { t } = useTranslation("myTasks");
  return (
    <a className={treeStyles.treeRow} href="/my-tasks" role="treeitem" aria-level={1} data-sidebar-favorite-my-tasks onClick={(event) => {
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      pushRoute("/my-tasks");
    }} onKeyDown={(event) => onTreeRowKeyDown(event, { open: (newTab) => openLink("/my-tasks", newTab) })}>
      <span className={treeStyles.treeLeading} data-tree-leading="true">
        <span className={treeStyles.treeIcon} data-tree-icon="true" aria-hidden="true"><CheckboxIcon size="small" /></span>
      </span>
      <span className={treeStyles.treeTitle} data-tree-title="true">{t("title")}</span>
    </a>
  );
}
