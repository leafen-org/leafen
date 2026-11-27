import { useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { CellMenuPortal } from "@/components/database/PropertyCellMenu";
import styles from "@/components/database/databaseSheet";
import { XMarkIcon } from "@/icons/leafen";
import { personDistinction, personLabel } from "@/lib/peopleDirectory";
import { scheduleOrganizationPeopleTypeahead } from "@/lib/typeaheadSearch";
import { INVITED_PERSON_STATUS } from "@/lib/organizationPeopleSearch";
import type { OrganizationProfile } from "@/lib/types";
import type { Session } from "@/types";
import type { BlockPickerSnapshot } from "./blockPickerModel";
import { pageUnreachablePeople } from "./mentionInvite";
import { findInvitedPeople, PeoplePickerEmpty } from "@/components/database/PeoplePickerEmpty";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { SelfMark } from "@/components/people/SelfMark";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "./openedFieldCaret";

/**
 * A to-do's "담당자 추가" list, as Notion's (seen 2026-11-09): the people
 * chosen stand in the field before what's typed, each with its ×, then "원하는
 * 만큼 선택" over the people not yet chosen — the reader first as "〈이름〉(나)"
 * — and a pick adds them while the list stays open. It assigns, it writes no
 * mention in the line. Only people the page opens to are offered: someone it
 * doesn't is no assignee, for they couldn't read the to-do. With no one left
 * to offer it reads as a property's picker does (PeoplePickerEmpty), its
 * invite sharing the to-do's page and assigning whoever has an account. An
 * address no account of the workspace's people holds is refused before
 * anything is sent, as a block that allows them no access, as Notion's
 * to-do invites no guest without an account (seen 2026-11-12, by hand); so
 * neither are the addresses a person picker offers, invited before, assignees.
 * Its keys and pointer light the people as a property's picker does
 * (PropertyPeopleCell, Notion's seen 2026-11-14): none lit until ↓ or what is
 * typed, Enter with none lit doing nothing.
 */
export function TodoAssigneePicker({ triggerRef, assignees, snapshot, session, pageId, onChange, onClose }: {
  triggerRef: RefObject<HTMLElement | null>;
  assignees: readonly string[];
  snapshot: BlockPickerSnapshot;
  session: Session | null;
  pageId: string;
  onChange: (next: string[]) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation(["propertyCell", "blockItem"]);
  const [query, setQuery] = useState("");
  const [searched, setSearched] = useState<{ key: string; people: OrganizationProfile[] }>({ key: "", people: [] });
  const [unreachable, setUnreachable] = useState<{ key: string; ids: ReadonlySet<string> | null }>({ key: "", ids: null });
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const me = snapshot.currentUserId || "local-user";
  const key = query.trim().toLowerCase();

  useEffect(() => {
    return focusOpenedField(() => searchRef.current);
  }, []);
  useEffect(() => {
    if (!snapshot.organizationId) return;
    return scheduleOrganizationPeopleTypeahead({ organizationId: snapshot.organizationId, query: key, limit: 12 }, {
      search: snapshot.searchOrganizationPeople,
      onResult: (result) => setSearched({ key, people: result.people ?? [] }),
      onError: () => setSearched({ key, people: [] }),
    });
  }, [key, snapshot.organizationId, snapshot.searchOrganizationPeople]);

  const candidates = useMemo(() => {
    const seen = new Set<string>();
    const list: Array<{ id: string; label: string }> = [];
    const add = (id: string | null | undefined, label: string) => {
      const trimmed = id?.trim();
      if (!trimmed || seen.has(trimmed)) return;
      seen.add(trimmed);
      if (key && !`${label} ${trimmed}`.toLowerCase().includes(key)) return;
      list.push({ id: trimmed, label });
    };
    add(me, personLabel(me, snapshot.currentUserId || undefined, false));
    for (const profile of [...searched.people, ...snapshot.organizationProfiles]) {
      if (profile.status === INVITED_PERSON_STATUS) continue;
      add(profile.userId, profile.displayName?.trim() || profile.email?.trim() || personLabel(profile.userId ?? ""));
    }
    return list.slice(0, 12);
  }, [key, me, searched.people, snapshot.currentUserId, snapshot.organizationProfiles]);

  // Whom the page opens to, asked of the people listed; until it answers the
  // others wait, so no one it doesn't open to can be picked meanwhile.
  const others = candidates.filter((person) => person.id !== me).map((person) => person.id);
  const othersKey = others.join(",");
  useEffect(() => {
    if (!session || !othersKey) return;
    let current = true;
    void pageUnreachablePeople(session, pageId, othersKey.split(",")).then((ids) => {
      if (current) setUnreachable({ key: othersKey, ids });
    });
    return () => { current = false; };
  }, [othersKey, pageId, session]);
  const known = unreachable.key === othersKey ? unreachable.ids : null;
  // No one left to offer shows once whom the page opens to is known.
  const settled = othersKey === "" || known !== null || !session;
  const offered = useMemo(() => candidates.filter((person) => !assignees.includes(person.id)
    && (person.id === me || (known !== null && !known.has(person.id)))), [assignees, candidates, known, me]);

  // Only addresses whose accounts are among the workspace's people are invited from here.
  const search = (input: Parameters<typeof snapshot.searchOrganizationPeople>[0]) => snapshot.searchOrganizationPeople(input, {});
  const admit = async (emails: string[]) =>
    (await findInvitedPeople(search, snapshot.organizationId, emails)).length >= new Set(emails.map((email) => email.toLowerCase())).size;

  const toggle = (id: string) => onChange(assignees.includes(id) ? assignees.filter((value) => value !== id) : [...assignees, id]);
  const picker = useNativeAutocomplete({ items: offered, query, onQuery: setQuery, inputRef: searchRef, listRef,
    keyOf: (person) => person.id, textOf: (person) => person.label, onPick: (person) => { setQuery(""); toggle(person.id); },
    onClose, highlightFirst: query.trim().length > 0, commitTab: false, label: t("propertyCell:searchOrganizationPeople") });
  const lit = picker.state.selectionManager.isFocused ? picker.activeKey : null;

  return (
    <CellMenuPortal triggerRef={triggerRef} onClose={onClose} ariaLabel={t("blockItem:todo.addAssignee")}
      className={styles.peopleMenu} backdropClassName={styles.menuBackdrop} width={240}
      onKeyDown={(event) => { if (event.key === "Escape") { event.preventDefault(); onClose(); } }}>
      {/* eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions */}
      <div className={styles.selectField} onClick={() => searchRef.current?.focus()}>
        {assignees.map((id) => {
          const name = personLabel(id, snapshot.currentUserId, false);
          return (
            <span key={id} className={styles.pickedPerson} data-selected-person={id}>
              <PersonAvatar userId={id} name={name} size={20} />{name}
              <button type="button" className={styles.chipRemove} aria-label={t("propertyCell:removeOption", { name })}
                onClick={(event) => { event.stopPropagation(); toggle(id); }}><XMarkIcon size="micro" aria-hidden="true" /></button>
            </span>
          );
        })}
        <input {...picker.inputProps} ref={searchRef} className={styles.selectFieldInput} type="search"
          placeholder={assignees.length ? undefined : t("propertyCell:searchPeopleOrGroups")}
          onKeyDown={(event) => {
            // Backspace in an empty field takes the last one chosen back;
            // Enter picks the lit one, with none lit nothing.
            if (event.key === "Backspace" && !query && assignees.length > 0) {
              event.preventDefault();
              toggle(assignees[assignees.length - 1]);
              return;
            }
            if (fieldEnter(event) && lit == null) {
              event.preventDefault();
              return;
            }
            picker.inputProps.onKeyDown?.(event);
          }} />
      </div>
      {offered.length ? <><div className={styles.propMenuLabel}>{t("propertyCell:pickAnyPeople")}</div>
        <div {...picker.listBoxProps} ref={listRef} className={styles.peopleList} aria-multiselectable>
        {offered.map((person) => (
          <NativePickerOption key={person.id} itemKey={person.id} state={picker.state} tabIndex={-1} className={styles.peopleOption}
            aria-selected={false}>
            {({ labelProps }) => <>
            <PersonAvatar className={styles.personAvatar} userId={person.id} name={person.label} size={20} />
            <span className={styles.peopleOptionText}>
              <span {...labelProps}>{person.label}{person.id === me ? <SelfMark className={styles.peopleOptionMe} text={t("propertyCell:youMark")} /> : null}</span>
              {person.id !== me && personDistinction(person.id) ? <span>{personDistinction(person.id)}</span> : null}
            </span>
            </>}
          </NativePickerOption>
        ))}
      </div></> : settled ? (
        <PeoplePickerEmpty query={query} label={<div className={styles.propMenuLabel}>{t("propertyCell:pickAnyPeople")}</div>}
          invitePageId={pageId} session={session} admit={admit} onInvited={(emails) => {
            void findInvitedPeople(search, snapshot.organizationId, emails).then((found) => {
              const fresh = found.filter((id) => !assignees.includes(id));
              if (fresh.length) { setQuery(""); onChange([...assignees, ...fresh]); }
            });
          }} />
      ) : null}
    </CellMenuPortal>
  );
}
