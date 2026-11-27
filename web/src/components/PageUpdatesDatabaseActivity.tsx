import type { MouseEvent } from "react";
import { Trans, useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import type { GraphValueType } from "@/contentGraphWire.generated";
import { pageHref } from "@/lib/navigation";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import type { PageHistoryActorProfile } from "@/pageHistoryWire.generated";
import type { WorkspaceActivityRecord } from "@/workspaceActivityWire.generated";
import type { Session } from "@/types";
import { PropertyTypeIcon } from "./database/PropertyTypeIcon";
import { ActivityViewIcon } from "./updates/ActivityViewIcon";
import { untitledActivityName } from "./updates/activityNames";

import styles from "./PageAnalyticsPanel.module.css";

// What each change is called: a renamed row, view or property is edited, as
// Notion's updates say of a row's new name.
const verbOf: Record<string, string> = {
  create: "create", edit: "edit", rename: "edit", delete: "delete", restore: "restore", move: "move",
};
const entities = new Set(["page", "view", "property", "dataSource"]);

/**
 * One of a database's own updates, as Notion's updates tab for an inline
 * database opened alone says it (seen 2026-11-11): a row made (with a stop),
 * a view made in the database by someone (no stop; the reader's own named
 * "본인" before "님", as its language file has one line for all, seen on a
 * wiki's 2026-11-17), a property made (with a stop) and the database itself
 * made, as a "컬렉션" (with a stop), after who did it, as a page's
 * updates name them; a row's edit shows its title under it. A row's update
 * sits under the database, a view's under its page, and a property's or the
 * database's own has no place (`place`).
 */
export function databaseActivityHead({ item, database, databaseHref, titleLabel, profiles, session, onFollow, t }: {
  item: WorkspaceActivityRecord;
  database: string;
  databaseHref: string;
  titleLabel?: string;
  profiles: ReadonlyMap<string, PageHistoryActorProfile>;
  session: Session;
  onFollow(event: MouseEvent<HTMLAnchorElement>): void;
  t: ReturnType<typeof useTranslation<"pageAnalytics">>["t"];
}) {
  const self = item.actorId === session.user.id;
  // An integration's change is named by it alone, as a page's are, and an
  // app's own account's by its name alone as a connection is (no "님").
  const app = !self && profiles.get(item.actorId)?.app === true;
  const integration = item.integrationsOnly ? (item.integrations ?? []).join(", ") : "";
  const name = integration || (self ? t("updates.database.self") : profiles.get(item.actorId)?.displayName || t("updates.someone"));
  const alone = Boolean(integration) || app;
  const entity = entities.has(item.entityType) ? item.entityType : "page";
  // An unnamed view is called its layout, a property its type (activityNames.ts).
  const title = item.name.trim() || untitledActivityName(entity, item.kind);
  // A row opens as its page; a view, a property or the database itself, the database.
  const href = entity === "page" ? pageHref(item.entityId) : databaseHref;
  const icon = entity === "view" ? <ActivityViewIcon kind={item.kind} size="small" />
    : entity === "property" && item.kind ? <PropertyTypeIcon type={item.kind as GraphValueType} /> : <span />;
  const edited = entity === "page" && (item.action === "edit" || item.action === "rename");
  return {
    place: entity === "page" ? "database" as const : entity === "view" ? "page" as const : null,
    avatar: (
      <PersonAvatar className={styles.updateAvatar} name={integration || profiles.get(item.actorId)?.displayName || name} size={24} bot={alone}
        userId={integration ? null : item.actorId} />
    ),
    summary: (
      <p className={styles.updateSummary}>
        <Trans t={t} i18nKey={`updates.database.${entity}`}
          values={{ name, honorific: self || alone ? "" : t("updates.database.honorific", { defaultValue: "" }), title,
            database: database.trim() || untitledPageDisplayTitle({ kind: "database" }), verb: t(`updates.database.verbs.${verbOf[item.action] ?? "edit"}`) }}
          components={{ actor: self ? <span /> : <strong className={styles.updateWho} />, icon,
            item: <a href={href} className={styles.updatePageLink} onClick={onFollow} />,
            db: <a href={databaseHref} className={styles.updatePageLink} onClick={onFollow} /> }} />
      </p>
    ),
    // A row's edit shows its title under it, as Notion's ("Aa Name / row two").
    value: edited ? (
      <div className={styles.updateChanges}>
        <div className={styles.updateTitle}>
          <span className={styles.updateTitleLabel}><PropertyTypeIcon type="title" /><span>{titleLabel?.trim() || t("updates.titleLabel")}</span></span>
          <a href={href} onClick={onFollow}><span data-added>{title}</span></a>
        </div>
      </div>
    ) : null,
  };
}
