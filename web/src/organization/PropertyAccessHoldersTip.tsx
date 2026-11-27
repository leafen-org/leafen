import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { HoverTip } from "@/components/IconTipButton";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { SharePeopleIcon } from "@/icons/leafen";
import type { PropertyAccessRule } from "./propertyAccessApi";
import styles from "./PropertyAccessHoldersTip.module.css";

/**
 * The fixed first exception's icon and name, naming in a dark tip over them
 * who has full access to the database, as Notion's (read 2026-11-19 in Chrome
 * at 1440, dpr 2, a private database and a teamspace's, two states each): the
 * rule's sentence, 6 under it a row each — a person's 18px face, else an
 * 18px group of people in grey — 6 before the name, 12/16.8, rows 4 apart; a
 * person's name in the tip's own white, the others' in its grey. It is 300
 * wide and starts at the icon, 6 over it, while the pointer rests on the icon
 * or the name and not on the rest of the row. Who they are comes with the
 * rule read (database_property_access_holders.go); past those it names, the
 * last row counts the rest — Leafen's own (Notion's long list not seen).
 */
export function PropertyAccessHoldersTip({ fullAccess, className, children }: {
  fullAccess: PropertyAccessRule["fullAccess"]; className?: string; children: ReactNode;
}) {
  const { t } = useTranslation("propertyAccess");
  const holders = fullAccess?.holders ?? [];
  const label = (
    <>
      <span className={styles.sentence}>{t("fullAccessTip")}</span>
      {holders.map((holder, index) => {
        const person = holder.kind === "person" || holder.kind === "app";
        const name = holder.kind === "workspace" ? t("holderWorkspace") : holder.kind === "teamspace_members" ? t("holderTeamspaceMembers")
          : holder.kind === "teamspace_owners" ? t("holderTeamspaceOwners") : holder.name || t("unknownPerson");
        return (
          <span key={`${holder.kind}:${holder.id ?? ""}:${index}`} className={person ? styles.holder : `${styles.holder} ${styles.group}`}
            data-access-holder={holder.kind}>
            {person ? <PersonAvatar userId={holder.id || null} name={name} size={18} bot={holder.kind === "app"} className={styles.face} />
              : <SharePeopleIcon size="page" weight="fill" aria-hidden="true" className={styles.glyph} />}
            <span className={styles.name}>{name}</span>
          </span>
        );
      })}
      {fullAccess?.more ? <span className={`${styles.holder} ${styles.group}`} data-access-holder="more">
        <span className={styles.name}>{t("holdersMore", { count: fullAccess.more })}</span>
      </span> : null}
    </>
  );
  return <HoverTip label={label} placement="top-start" wide className={className}>{children}</HoverTip>;
}
