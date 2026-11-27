import { PersonAvatar } from "@/components/people/PersonAvatar";
import { createContext, useContext, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { personIsBot, personIsInvitee } from "@/lib/peopleDirectory";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { personLabel } from "./people";
import styles from "./databaseSheet";

// A database template's person that is whoever uses the template.
export const templateMe = "@me";

/**
 * How a place shows people as values, as Notion's (seen 2026-11-12, 1440
 * dark): a cell and a row's page a 20px face and the name; a board's or
 * gallery's card an 18px face and its 12px name; a calendar's card a 16px
 * face and the name; a list's row and a timeline's bar the 20px faces alone,
 * each 5px over the last.
 */
export type PersonValueLook = "cell" | "card" | "calendar" | "faces";

const AVATAR_SIZES: Readonly<Record<PersonValueLook, number>> = { cell: 20, card: 18, calendar: 16, faces: 20 };
const PersonValueLookContext = createContext<PersonValueLook>("cell");

/** The people values inside it take that place's look. */
export function PersonValueLookArea({ look, children }: { look: PersonValueLook; children: ReactNode }) {
  return <PersonValueLookContext.Provider value={look}>{children}</PersonValueLookContext.Provider>;
}

/**
 * A person as a value shows them: their avatar and name, the same in a cell
 * one can edit, a read-only cell and a card, as Notion's do, at the size of
 * the place it is in; a connection's bot its icon on a rounded square and
 * its name, without a person's card; someone invited without an account
 * yet the address's first letter and the address, without one either.
 */
export function PersonBadge({ id, currentUserId, look }: {
  id: string;
  currentUserId?: string | null;
  /** Its look, else the look of the place it is in. */
  look?: PersonValueLook;
}) {
  const { t } = useTranslation(["propertyCell", "common"]);
  usePeopleDirectoryRevision();
  const placeLook = useContext(PersonValueLookContext);
  const shown = look ?? placeLook;
  // A value names the person alone, one's own too, as Notion's cell does.
  const label = id === templateMe ? t("propertyCell:templateValue.me") : personLabel(id, currentUserId ?? undefined, false);
  if (!label) return null;
  const faceOnly = shown === "faces";
  return (
    // A member's name shows their card on hover (PersonHoverCard).
    <span className={styles.personChip} data-person-look={shown} data-person-card={id === templateMe || personIsBot(id) || personIsInvitee(id) ? undefined : id}
      role={faceOnly ? "img" : undefined} aria-label={faceOnly ? label : undefined}>
      <PersonAvatar className={styles.personAvatar} userId={id} name={label} size={AVATAR_SIZES[shown]} />
      {faceOnly ? null : <span>{label}</span>}
    </span>
  );
}

/**
 * A row's people as a value: each in its own field, or, where faces stand
 * alone, the faces in one overlapping run.
 */
export function PersonValues({ ids, currentUserId }: { ids: readonly string[]; currentUserId?: string | null }) {
  const look = useContext(PersonValueLookContext);
  if (look === "faces") {
    return (
      <span className={`${styles.cardField} ${styles.personFaces}`}>
        {ids.map((id) => <PersonBadge key={id} id={id} currentUserId={currentUserId} />)}
      </span>
    );
  }
  return <>{ids.map((id) => <span key={id} className={styles.cardField}><PersonBadge id={id} currentUserId={currentUserId} /></span>)}</>;
}
