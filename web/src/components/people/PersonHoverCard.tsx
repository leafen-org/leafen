import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { EnvelopeIcon } from "@/icons/leafen";
import { listenHoverCardKeys } from "@/lib/hoverCard";
import { placeAround, popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { personCardOnce, personLocalTime, usePeopleProfileSettings, type PersonCardData } from "@/lib/peopleProfiles";
import type { Session } from "@/types";
import { PersonAvatar } from "./PersonAvatar";
import styles from "./PersonHoverCard.module.css";

// A card names the person's first three groups or teamspaces, as Notion's
// (no count of the rest); their profile lists the teamspaces all.
const HOVER_NAMES = 3;

// With neither, Notion's card names the person's place in the workspace
// (seen in its code and on its card, 2026-11-13); a deactivated person, none.
const ROLE_LINE: Partial<Record<PersonCardData["membership"], string>> = {
  owner: "memberStatus.owner", member: "memberStatus.member", restricted: "memberStatus.restricted", guest: "memberStatus.guest",
};

const SHOW_AFTER_MS = 500;
const HIDE_AFTER_MS = 150;
/** A person mention, or any other name that shows its person's card. */
const PERSON_SELECTOR = '[data-mention="person"][data-user-id], [data-person-card]';

type Shown = { userId: string; anchor: PopoverRect; note?: string; name: HTMLElement };

function personOf(element: HTMLElement) {
  return element.dataset.personCard || element.dataset.userId || "";
}

/**
 * The card a member's name shows while the pointer rests on it, as Notion's
 * (seen 2026-10-12): after half a second, 6px above the name with its end
 * edge on the name's (2026-11-13), 280px wide — their
 * picture, name ("(나)" for the reader), a yellow "제한됨" for a restricted
 * member, the teamspaces they are in that the reader sees, their local time,
 * and for someone else a button to email them — Notion's "일정" beside it
 * only leads to its separate calendar app (seen 2026-11-13: "Notion 캘린더
 * 소개" and its download), which Leafen leaves out. It stays
 * while the pointer is on the name or the card; a press elsewhere or a scroll
 * ends it, a key does not (listenHoverCardKeys).
 * Where the workspace turned its cards off, or for a guest, names show none.
 */
export function PersonHoverCard({ session }: { session: Session }) {
  const profiles = usePeopleProfileSettings();
  const enabled = Boolean(profiles?.settings.hoverCards);
  const workspaceId = profiles?.workspaceId ?? "";
  const [shown, setShown] = useState<Shown | null>(null);
  const [card, setCard] = useState<PersonCardData | null>(null);
  const shownRef = useRef<Shown | null>(null);
  const cardRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!enabled) return;
    let showTimer = 0;
    let hideTimer = 0;
    const show = (next: Shown | null) => {
      shownRef.current = next;
      setShown(next);
    };
    function onOver(event: MouseEvent) {
      const target = event.target instanceof Element ? event.target : null;
      if (target && cardRef.current?.contains(target)) {
        window.clearTimeout(hideTimer);
        return;
      }
      const name = target?.closest<HTMLElement>(PERSON_SELECTOR);
      const userId = name ? personOf(name) : "";
      if (!name || !userId) {
        window.clearTimeout(showTimer);
        if (shownRef.current) {
          window.clearTimeout(hideTimer);
          hideTimer = window.setTimeout(() => show(null), HIDE_AFTER_MS);
        }
        return;
      }
      window.clearTimeout(hideTimer);
      if (shownRef.current?.userId === userId) return;
      window.clearTimeout(showTimer);
      showTimer = window.setTimeout(() => {
        if (!name.isConnected) return;
        // Where the name is now: the editor may draw it again while the card shows.
        const rect = name.getBoundingClientRect();
        show({ userId, note: name.dataset.personCardNote, name, anchor: { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom } });
      }, SHOW_AFTER_MS);
    }
    function hideNow(event?: Event) {
      if (event?.target instanceof Node && cardRef.current?.contains(event.target)) return;
      window.clearTimeout(showTimer);
      window.clearTimeout(hideTimer);
      if (shownRef.current) show(null);
    }
    const onScroll = () => hideNow();
    document.addEventListener("mouseover", onOver);
    document.addEventListener("mousedown", hideNow, true);
    const keys = listenHoverCardKeys(() => shownRef.current?.name ?? null, () => hideNow());
    window.addEventListener("scroll", onScroll, true);
    return () => {
      window.clearTimeout(showTimer);
      window.clearTimeout(hideTimer);
      document.removeEventListener("mouseover", onOver);
      document.removeEventListener("mousedown", hideNow, true);
      keys();
      window.removeEventListener("scroll", onScroll, true);
      show(null);
    };
  }, [enabled]);

  useEffect(() => {
    setCard(null);
    if (!shown || !workspaceId) return;
    let current = true;
    void personCardOnce(session, workspaceId, shown.userId).then((read) => {
      if (current) setCard(read);
    });
    return () => { current = false; };
  }, [session, shown, workspaceId]);

  if (!enabled || !shown || !card || card.userId !== shown.userId) return null;
  return <PersonCard card={card} anchor={shown.anchor} note={shown.note} cardRef={cardRef} />;
}

function PersonCard({ card, anchor, note, cardRef }: {
  card: PersonCardData;
  anchor: PopoverRect;
  /** What the name that showed the card adds about them, as a line on top. */
  note?: string;
  cardRef: React.RefObject<HTMLDivElement | null>;
}) {
  const { t, i18n } = useTranslation("peopleProfiles");
  const place = usePopoverPlacement(cardRef, (size, viewport) => placeAround(
    anchor, size, viewport, { gap: 6, prefer: "above", align: "end", minHeight: 84 },
  ), [anchor], undefined, { kind: "hovercard" });
  const time = personLocalTime(card.timeZone, i18n.language);
  const names = (card.groups?.length ? card.groups : card.teamspaces).slice(0, HOVER_NAMES).map((entry) => entry.name);
  const role = ROLE_LINE[card.membership];
  const subtitle = names.length ? names.join(", ") : role ? t(role) : "";
  return createPortal(
    <div ref={cardRef} className={styles.card} role="tooltip" data-person-hover-card={card.userId} data-noted={note ? "true" : undefined}
      style={popoverPlacementStyle(place)}>
      {note ? <div className={styles.note} data-person-card-note>{note}</div> : null}
      <div className={styles.top}>
        <PersonAvatar userId={card.userId} avatar={card.avatar} name={card.displayName} size={56} />
        <div className={styles.info}>
          <div className={styles.nameLine}>
            <span className={styles.name}>{card.self ? t("selfName", { name: card.displayName }) : card.displayName}</span>
            {card.membership === "restricted" ? <span className={styles.restricted}>{t("restricted")}</span> : null}
          </div>
          {/* Under the name, the person's first three groups or, with none,
              teamspaces, or else their role — Notion's card (its code and
              view, 2026-11-09 and 2026-11-13). */}
          {subtitle ? <div className={styles.teamspaces} data-person-card-subtitle>{subtitle}</div> : null}
          {time ? <div className={styles.time}>{t("localTime", { time })}</div> : null}
        </div>
      </div>
      {!card.self && card.email ? (
        <div className={styles.actions}>
          <a className={styles.action} href={`mailto:${card.email}`} data-person-card-email>
            <EnvelopeIcon size="small" aria-hidden="true" />{t("email")}
          </a>
        </div>
      ) : null}
    </div>,
    document.body,
  );
}
