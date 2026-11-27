import { type RefObject, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { ownDigits } from "@/lib/i18n";
import { personDeactivated, personIsBot, personIsInvitee, personName } from "@/lib/peopleDirectory";
import { personProfileHref, usePeopleProfileSettings } from "@/lib/peopleProfiles";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { isInertMenuKey } from "@/lib/menuKeyboard";
import { goRoundMenuStops } from "@/lib/menuTabStops";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { menuFootEditedTime } from "@/lib/menuFootWords";
import type { EditedFootActivity, EditedFootPerson, PageLastEditVia } from "@/lib/pageEditedActivity";
import { PopoverLayer } from "@/lib/popoverLayer";
import { placeBeside, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { relativeAgoLabel } from "@/lib/relativeTime";
import styles from "./RowMenu.module.css";
import activityStyles from "./RowMenuEditedFoot.module.css";

/**
 * Who edited last in a menu's foot: their face and name, and for an edit
 * through a connection its 12px mark tucked 4px behind the face and "〈사람〉
 * 〈연결〉와(과)" — Notion's page, sidebar row and block menus (seen 2026-11-09).
 * Unless `bot` says so, the face is a connection's when the people directory
 * names the editor as one (an app's own account among them).
 */
export function EditedByLine({ className, userId, name, bot, via, data }: {
  className: string; userId: string | null; name: string; bot?: boolean; via: string; data?: Record<string, string>;
}) {
  const { t } = useTranslation("blockHandle");
  return (
    <span className={className} {...data}>
      <span className={styles.menuFootFaces}>
        <PersonAvatar userId={userId} name={name} size={16} bot={bot} />
        {via ? <span className={styles.menuFootViaMark}><PersonAvatar name={via} size={12} bot /></span> : null}
      </span>
      <span><EditorName userId={bot ? null : userId} name={name} />{via ? <> <span data-menu-foot-via>{t("footer.via", { name: via })}</span></> : null}</span>
    </span>
  );
}

/**
 * A person's name in a menu's foot, as a link to their profile — Notion's
 * page "⋯" and block "⋮⋮" feet (seen 2026-11-26 in Chrome 1440, two tabs,
 * no script before the keys): grey as the foot, the text's own color under
 * the pointer and in the keys' ring; pressed, their profile opens in a new
 * tab and the menu stays; Enter on it opens nothing. It is a stop of the
 * menu's Tab round (menuTabStops.ts): Tab from the search reaches it and
 * the next Tab goes back. A connection, an app's account, someone invited
 * or deactivated, or a workspace without profiles (openPersonProfile) has
 * no profile to open: their name stays words, as Notion's connection's.
 * The activity panel beside the foot names its people so too
 * (FootActivityPanel), out of the menu's Tab round (`menuStop` false): its
 * Tab goes from the field to the foot's name and back, the panel's passed
 * over (seen 2026-11-26).
 */
function EditorName({ userId, name, menuStop = true }: { userId: string | null; name: string; menuStop?: boolean }) {
  const profiles = usePeopleProfileSettings();
  // Drawn again as the directory learns the person (a block menu's foot reads its editor first).
  usePeopleDirectoryRevision(Boolean(userId));
  const linked = Boolean(userId && profiles?.settings.directory && personName(userId)
    && !personIsBot(userId) && !personIsInvitee(userId) && !personDeactivated(userId));
  if (!linked || !userId) return <>{name}</>;
  const mark = menuStop ? { "data-menu-foot-person": userId } : { "data-menu-foot-activity-person": userId };
  return (
    <a className={activityStyles.personLink} href={personProfileHref(userId)} target="_blank" rel="noopener noreferrer" {...mark}
      onKeyDown={(event) => {
        if (goRoundMenuStops(event)) return;
        // Alone in its menu (a sidebar row's, with no search) it keeps the Tab, as the menu does.
        if (event.key === "Tab" && isInertMenuKey(event)) { event.preventDefault(); event.stopPropagation(); }
        if (fieldEnter(event)) event.preventDefault();
      }}>{name}</a>
  );
}

/**
 * The foot of a sidebar row's "…" menu, as Notion's (seen 2026-10-03 and
 * 2026-10-04): under a line, who edited the page last and when ("오늘 오전
 * 12:07"). A page's own "…" menu puts its word count above ("0개의 단어", seen
 * 2026-10-07); the library's puts its words and characters below.
 */
export function RowMenuEditedFoot({ name, editorId, via, at, marker, words, counts, activity }: {
  name: string;
  /**
   * Who edited last, for a page's own "…" menu and a sidebar row's: Notion's
   * foot there names them on a line of their own under "Last edited", with
   * their face (seen 2026-10-19 and 2026-10-20); the label stays in Korean on
   * Korean screens.
   */
  editorId?: string | null;
  /**
   * The connection the last edit came through, beside the editor as Notion's
   * (usePageLastEditVia); an integration's names the edit alone, as Notion's
   * foot and activity name an integration's (seen 2026-11-05).
   */
  via?: PageLastEditVia;
  at?: string | null;
  /** The page's word count line, last, for the page's own menu (seen 2026-10-17). */
  words?: string;
  /** The page's words and characters, last, for the library's menu. */
  counts?: string;
  /** Names the foot for the menu it closes, as data-{marker}. */
  marker: string;
  /**
   * A page's editor and maker, shown beside the menu while the pointer
   * rests on the foot or on them (FootActivityPanel).
   */
  activity?: EditedFootActivity;
}) {
  const { t } = useTranslation(["blockHandle", "updatesPanel"]);
  // Redrawn as the directory learns who the editor is (an app's own account).
  usePeopleDirectoryRevision(Boolean(editorId));
  const footRef = useRef<HTMLDivElement>(null);
  const { open, pointed } = useFootActivityHover(Boolean(activity));
  const time = at ? Date.parse(at) : Number.NaN;
  // Its time, words and characters in the language's own digits, as Notion's
  // foot writes them (Arabic "اليوم الساعة ٩:٤٥ ص", "٢٣ من الكلمات", seen
  // 2026-11-19); the activity beside it keeps its "… ago" times.
  const when = Number.isNaN(time) ? "" : menuFootEditedTime(time);
  if (!name && !when && !words && !counts) return null;
  const integration = via?.integration ? via.name : "";
  const shownName = integration || name;
  const shownId = integration ? null : editorId;
  // An app's own account is no person: named alone on a connection's face,
  // with no connection beside it (the app is the one that edited).
  const app = !integration && Boolean(editorId) && personIsBot(editorId ?? "");
  // A connection's face where one is known, else the directory's (PersonAvatar).
  const rows: FootActivityRow[] = activity ? [
    { key: "editor", label: t("blockHandle:footer.activityEditor"),
      person: integration ? { ...activity.editor, id: null, name: integration } : activity.editor, bot: integration ? true : undefined },
    ...(activity.creator ? [{ key: "creator", label: t("blockHandle:footer.activityCreator"),
      ...(via?.maker && via.maker.actorId === activity.creator.id
        ? { person: { ...activity.creator, id: null, name: via.maker.name }, bot: true } : { person: activity.creator }) }] : []),
  ] : [];
  return (
    <>
      <div className={styles.divider} role="separator" />
      <div ref={footRef} className={styles.menuFoot} {...{ [`data-${marker}`]: "" }}
        onMouseEnter={activity ? () => pointed(true) : undefined} onMouseLeave={activity ? () => pointed(false) : undefined}>
        {shownName && editorId !== undefined ? (
          <>
            <span className={styles.menuFootLabel}>{t("blockHandle:footer.lastEdited")}</span>
            <EditedByLine className={styles.menuFootPerson} userId={shownId ?? null} name={shownName} bot={integration ? true : undefined}
              via={via?.name && !integration && !app ? via.name : ""} />
          </>
        ) : shownName ? <span>{t("blockHandle:footer.editedBy", { name: shownName })}</span> : null}
        {when ? <span>{when}</span> : null}
        {words ? <span>{ownDigits(words)}</span> : null}
        {counts ? <span>{ownDigits(counts)}</span> : null}
      </div>
      {open ? <FootActivityPanel foot={footRef} head={t("blockHandle:footer.activity")} rows={rows} pointed={pointed} /> : null}
    </>
  );
}

// Notion's panel comes a quarter of a second after the pointer comes to the
// foot and goes half a second after the pointer has left the foot and the
// panel both (seen 2026-11-26 in Chrome 1440, dpr 1, two tabs: in 259 ms
// twice, out 535–544 ms four times, drawn and gone at once with nothing faded).
const ACTIVITY_SHOW_MS = 250;
const ACTIVITY_HIDE_MS = 500;

/** One line of the activity panel: "편집자: 〈name〉" or "생성자 〈name〉", a connection's face where `bot`. */
export type FootActivityRow = { key: string; label: string; person: EditedFootPerson; bot?: boolean };

/**
 * Whether the pointer rests on a menu's foot or on its activity panel: each
 * passes `pointed` the pointer coming and going, so the way from the foot
 * into the panel keeps it, as Notion's (seen 2026-11-26).
 */
export function useFootActivityHover(enabled: boolean) {
  const [shown, setShown] = useState(false);
  const timer = useRef(0);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  useEffect(() => {
    if (enabled) return;
    window.clearTimeout(timer.current);
    setShown(false);
  }, [enabled]);
  const pointed = useCallback((on: boolean) => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setShown(on), on ? ACTIVITY_SHOW_MS : ACTIVITY_HIDE_MS);
  }, []);
  return { open: enabled && shown, pointed };
}

/**
 * The "활동" panel beside a menu while the pointer rests on its foot, as
 * Notion's page "⋯", sidebar row and row page menus show it (seen
 * 2026-10-20) and its block "⋮⋮" menu for the block (BlockMenuFooter, seen
 * 2026-11-26): "활동", then "편집자: 〈name〉" and "생성자 〈name〉" with their
 * faces and how long ago, 6px beside the menu on the side with room, centred
 * on the foot. It is a card the pointer may go into (seen 2026-11-26 in
 * Chrome 1440, two tabs, judged after each action with a letter typed): a
 * person's name in it is the same link to their profile as the foot's
 * (EditorName), a connection's words; a press on it leaves the menu open,
 * the focus going to the page's body; it takes no key — Escape closes the
 * menu with it, the "⋯" given back ringed as with it gone, and Tab goes round
 * the menu's own stops, its name not one of them. A hover card's layer
 * (kind "hovercard"), Escape goes on from it to the menu (escapeLayer); drawn
 * from inside the menu (PopoverLayer), the menu holds it as its own part
 * (holdsPlaced), so a press beside both is the menu's, which takes it away
 * with the foot.
 */
export function FootActivityPanel({ foot, head, rows, pointed }: {
  foot: RefObject<HTMLElement | null>; head: string; rows: FootActivityRow[]; pointed: (on: boolean) => void;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(panelRef, (size, viewport) => {
    const at = foot.current?.getBoundingClientRect();
    const menu = (foot.current?.parentElement ?? foot.current)?.getBoundingClientRect();
    const top = at?.top ?? 0, bottom = at?.bottom ?? 0;
    // At once, with nothing faded or grown, as Notion's (seen 2026-11-26).
    return placeBeside({ top, bottom, left: menu?.left ?? 0, right: menu?.right ?? 0 }, size, viewport,
      { gap: 6, offsetTop: (bottom - top - size.height) / 2, appear: "none" });
  }, [], undefined, { kind: "hovercard" });
  return (
    <PopoverLayer>
      <div ref={panelRef} className={activityStyles.panel} style={popoverPlacementStyle(place)} data-menu-foot-activity=""
        onMouseEnter={() => pointed(true)} onMouseLeave={() => pointed(false)}>
        <div className={activityStyles.head}>{head}</div>
        <div className={activityStyles.line} />
        <div className={activityStyles.rows}>
          {rows.map(({ key, label, person, bot }) => (
            <div key={key} className={activityStyles.row} data-menu-foot-activity-row={key}>
              <PersonAvatar userId={person.id} name={person.name} size={28} bot={bot} />
              <span className={activityStyles.text}>
                <span className={activityStyles.who}>{label} <strong><EditorName userId={bot ? null : person.id} name={person.name} menuStop={false} /></strong></span>
                <span className={activityStyles.when}>{relativeAgoLabel(person.at)}</span>
              </span>
            </div>
          ))}
        </div>
      </div>
    </PopoverLayer>
  );
}
