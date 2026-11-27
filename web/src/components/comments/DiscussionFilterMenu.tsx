import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { FilterIcon, SharePeopleIcon } from "@/icons/leafen";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { EVERY_DISCUSSION, filterChanged, type DiscussionFilter, type DiscussionPerson } from "./discussionListModel";
import styles from "./PageDiscussions.module.css";
import menuStyles from "./DiscussionFilterMenu.module.css";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { SelfMark } from "@/components/people/SelfMark";

type Flag = Exclude<keyof DiscussionFilter, "people">;

/**
 * "All discussions"' filter, as Notion's (seen 2026-09-30): 유형 (comments,
 * suggestions), 상태 (open — unread and read — and resolved) and 사람, the
 * people who wrote in the discussions; none picked shows everyone's.
 */
export function DiscussionFilterMenu({ filter, people, onChange }: {
  filter: DiscussionFilter;
  /** The people who wrote in the discussions; `self` the reader, whose name says so (discussionFilterPeople). */
  people: readonly DiscussionPerson[];
  onChange: (filter: DiscussionFilter) => void;
}) {
  const { t } = useTranslation("commentsPanel");
  const [open, setOpen] = useState(false);
  const host = useRef<HTMLSpanElement>(null);
  // The filter shows as on (blue) while resolved discussions show or it was
  // changed, as Notion's "모든 토론" (seen 2026-11-09: blue with every box
  // ticked, and with "해결" unticked).
  const filtered = filter.resolved || filterChanged(filter, EVERY_DISCUSSION);

  const menuRef = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const { zIndex, onKeyDown } = useOverlayLayer(open, { kind: "menu", ref: menuRef, onEscape: () => setOpen(false),
    onOutsidePress: () => setOpen(false), inside: () => [host.current] });
  // Under the filter button, its top on the button's foot (DiscussionFilterMenu.module.css).
  const placement = usePopoverPlacement(menuRef, open && button.current
    ? (size, viewport) => placeAround(popoverRectOf(button.current)!, size, viewport, { gap: 0 }) : null, [open], undefined, false);

  const flag = (key: Flag, label: string, sub = false) => (
    <label key={key} className={menuStyles.item} data-sub={sub || undefined}>
      <input type="checkbox" checked={filter[key]} data-discussion-filter={key}
        onChange={(event) => onChange({ ...filter, [key]: event.currentTarget.checked })} />
      <span>{label}</span>
    </label>
  );
  const openBoth = filter.unread && filter.read;

  return (
    <span ref={host} className={styles.menuHost}>
      <button ref={button} type="button" className={styles.iconButton} aria-haspopup="dialog" aria-expanded={open}
        aria-label={t("discussions.filter")} title={t("discussions.filter")} data-filtered={filtered || undefined}
        data-discussion-filter-button onClick={() => setOpen((current) => !current)}>
        <FilterIcon size="regular" aria-hidden="true" />
      </button>
      {open ? (
        <div ref={menuRef} style={{ ...popoverPlacementStyle(placement), zIndex }} onKeyDown={onKeyDown} className={menuStyles.menu}
          role="dialog" aria-label={t("discussions.filter")}>
          <div className={menuStyles.group} role="group" aria-label={t("discussions.type")}>
            <div className={menuStyles.label} aria-hidden="true">{t("discussions.type")}</div>
            {flag("comments", t("discussions.comments"))}
            {flag("suggestions", t("discussions.suggestions"))}
          </div>
          <div className={menuStyles.group} role="group" aria-label={t("discussions.status")}>
            <div className={menuStyles.label} aria-hidden="true">{t("discussions.status")}</div>
            <label className={menuStyles.item}>
              <input type="checkbox" checked={openBoth} data-discussion-filter="open"
                ref={(input) => { if (input) input.indeterminate = filter.unread !== filter.read; }}
                onChange={() => onChange({ ...filter, unread: !openBoth, read: !openBoth })} />
              <span>{t("discussions.open")}</span>
            </label>
            {flag("unread", t("discussions.unread"), true)}
            {flag("read", t("discussions.read"), true)}
            {flag("resolved", t("discussions.resolved"))}
          </div>
          {people.length > 0 ? (
            <div className={menuStyles.group} role="group" aria-label={t("discussions.people")}>
              <div className={menuStyles.label} aria-hidden="true">{t("discussions.people")}</div>
              {people.map((person) => (
                <label key={person.id} className={menuStyles.item}>
                  <input type="checkbox" checked={filter.people.includes(person.id)} data-discussion-filter-person={person.id}
                    onChange={(event) => onChange({
                      ...filter,
                      people: event.currentTarget.checked
                        ? [...filter.people, person.id]
                        : filter.people.filter((id) => id !== person.id),
                    })} />
                  <PersonAvatar className={`${styles.avatar} ${menuStyles.face}`} userId={person.id} name={person.label} size={20} />
                  <span>{person.label}{person.self ? <SelfMark className={menuStyles.self} text={t("discussions.youMark")} /> : null}</span>
                </label>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
    </span>
  );
}

/**
 * A phone's filter for "모든 토론", as Notion's "필터" sheet (seen
 * 2026-10-27): the same three groups as rows that tick, "미해결" over its
 * "읽지 않음" and "읽음", and "모든 사용자" over the people, which it clears,
 * the reader's own name marked as the desktop menu's (seen 2026-11-25 in its
 * Android app twice).
 */
export function DiscussionFilterSheet({ filter, people, onChange, onClose }: {
  filter: DiscussionFilter;
  people: readonly DiscussionPerson[];
  onChange: (filter: DiscussionFilter) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("commentsPanel");
  const flag = (key: Flag, label: string, sub = false) => (
    <PhoneSheetRow key={key} small more={false} sub={sub} checked={filter[key]} label={label}
      data={{ "data-discussion-filter": key }} onPress={() => onChange({ ...filter, [key]: !filter[key] })} />
  );
  const openBoth = filter.unread && filter.read;
  return (
    <PhoneSheet title={t("discussions.filter")} closeButton="small" onClose={onClose} data={{ "data-discussion-filter-sheet": "" }}>
      <PhoneSheetGroup label={t("discussions.type")}>
        {flag("comments", t("discussions.comments"))}
        {flag("suggestions", t("discussions.suggestions"))}
      </PhoneSheetGroup>
      <PhoneSheetGroup label={t("discussions.status")}>
        <PhoneSheetRow small more={false} checked={openBoth || (filter.unread || filter.read ? "mixed" : false)} label={t("discussions.open")}
          data={{ "data-discussion-filter": "open" }} onPress={() => onChange({ ...filter, unread: !openBoth, read: !openBoth })} />
        {flag("unread", t("discussions.unread"), true)}
        {flag("read", t("discussions.read"), true)}
        {flag("resolved", t("discussions.resolved"))}
      </PhoneSheetGroup>
      <PhoneSheetGroup label={t("discussions.people")}>
        <PhoneSheetRow small more={false} checked={filter.people.length === 0} icon={<SharePeopleIcon size="regular" />}
          label={t("discussions.everyone")} data={{ "data-discussion-filter-everyone": "" }} onPress={() => onChange({ ...filter, people: [] })} />
        {people.map((person) => {
          const picked = filter.people.includes(person.id);
          return (
            <PhoneSheetRow key={person.id} small more={false} checked={picked}
              label={<>{person.label}{person.self ? <SelfMark className={menuStyles.self} text={t("discussions.youMark")} /> : null}</>}
              icon={<PersonAvatar className={styles.avatar} userId={person.id} name={person.label} size={22} />}
              data={{ "data-discussion-filter-person": person.id }}
              onPress={() => onChange({ ...filter, people: picked ? filter.people.filter((id) => id !== person.id) : [...filter.people, person.id] })} />
          );
        })}
      </PhoneSheetGroup>
    </PhoneSheet>
  );
}
