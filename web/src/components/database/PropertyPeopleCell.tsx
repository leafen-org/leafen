import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useContext, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { i18next } from "@/i18n";
import { scheduleOrganizationPeopleTypeahead } from "@/lib/typeaheadSearch";
import type { DbProperty, OrganizationProfile, Page } from "@/lib/types";
import { normalizePersonIds, personLabel } from "./people";
import { PersonBadge, templateMe } from "./PersonBadge";
import { CheckIcon, Plus, XMarkIcon } from "../icons";
import { CellMenuPortal, openEditorFromCellBackground } from "./PropertyCellMenu";
import { capturePropertyCellRequest, type PropertyCellEnvironment, type PropertyCellPresentation } from "./propertyCellTypes";
import { FieldPagesContext } from "@/components/editor/mentionSnapshotContext";
import { findInvitedPeople, PeoplePickerEmpty, pickerSession } from "./PeoplePickerEmpty";
import { inviteMentionDoneText } from "../page/InviteAndMentionPanel";
import styles from "./databaseSheet";
import { organizationRoleLabel, organizationStatusLabel, personDistinction } from "@/lib/peopleDirectory";
import { invitedPeopleValues } from "@/lib/workspacePeopleDirectory";
import { valueAsRead } from "./valueAsRead";
import { restoreFocusUnlessChosen } from "./tableCellKeys";
import { readOnlyStop, useCellReadOnly } from "./cellReadOnly";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet, PhoneSheetGroup } from "../shell/PhoneSheet";
import { PickerOpened } from "./PropertySelectPicker";
import sheet from "./PropertyPeopleSheet.module.css";
import { SelfMark } from "@/components/people/SelfMark";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

export function PeopleReadonly({ value, userId, snapshot }: {
  value: unknown;
  userId?: string | null;
  snapshot: PropertyCellEnvironment["snapshot"];
}) {
  const ids = Array.from(new Set(normalizePersonIds(value, {
    maxNodes: snapshot.formulaBounds.rollup.maxPersonNodes,
    maxDepth: snapshot.formulaBounds.rollup.maxPersonDepth,
    maxEntries: snapshot.formulaBounds.rollup.maxPeopleEntries,
  })));
  return (
    <span className={styles.peopleReadonly}>
      {ids.map((id) => <PersonBadge key={id} id={id} currentUserId={userId} />)}
      {ids.length === 0 && <span className={styles.cellEmpty}>&nbsp;</span>}
    </span>
  );
}

export function organizationProfilePersonLabel(profile: OrganizationProfile) {
  return profile.displayName?.trim()
    || profile.email?.trim()
    || profile.userId?.trim()
    || i18next.t("propertyCell:person");
}

function organizationProfilePersonDescription(profile: OrganizationProfile) {
  const parts = [
    profile.email?.trim(),
    profile.organizationRole
      ? i18next.t("propertyCell:roleInOrganization", { role: organizationRoleLabel(profile.organizationRole) })
      : null,
    profile.status && profile.status !== "active" ? organizationStatusLabel(profile.status) : null,
  ].filter(Boolean);
  return parts.join(" - ") || i18next.t("propertyCell:orgPersonFallback");
}

export function personOptionMatches(label: string, description: string, userId: string, query: string) {
  if (!query) return true;
  const haystack = `${label} ${description} ${userId}`.toLowerCase();
  return query.split(/\s+/).filter(Boolean).every((token) => haystack.includes(token));
}

export function PropertyPeopleCell({ row, prop, snapshot, operations, context }: {
  row: Page;
  prop: DbProperty;
  presentation?: PropertyCellPresentation;
} & PropertyCellEnvironment) {
  const { t } = useTranslation(["propertyCell", "common", "topBar"]);
  const [open, setOpen] = useState(false);
  const readOnly = useCellReadOnly();
  const [query, setQuery] = useState("");
  const [searchedPeople, setSearchedPeople] = useState<{ key: string; people: OrganizationProfile[] }>({ key: "", people: [] });
  const triggerRef = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const { touch } = useSidebarLayout(true);
  const currentId = snapshot.userId || "local-user";
  const ids = Array.from(new Set(normalizePersonIds(valueAsRead(row, prop), {
    maxNodes: snapshot.formulaBounds.rollup.maxPersonNodes,
    maxDepth: snapshot.formulaBounds.rollup.maxPersonDepth,
    maxEntries: snapshot.formulaBounds.rollup.maxPeopleEntries,
  })));
  const selected = new Set(ids);
  const searchKey = query.trim().toLowerCase();
  // Its invite shares the row's page alone, from a view's cell too, as
  // Notion's (seen 2026-11-12: a guest on the row page, none on the database).
  const fieldPages = useContext(FieldPagesContext);
  const session = pickerSession(fieldPages?.moveContext.session, snapshot.userId);
  const invitePageId = row.id;

  // The picker's search takes the keys. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: a
  // person cell pressed, "z" typed went into its search (Leafen the same). A touch screen's sheet leaves it
  // unfocused, no keyboard rising until the field is pressed, as Notion's phone (seen 2026-11-15 at 402pt in iOS
  // Safari: the page's body focused, no keyboard on the screen).
  useEffect(() => {
    if (!open || touch) return;
    return focusOpenedField(() => searchRef.current);
  }, [open, touch]);

  useEffect(() => {
    if (!snapshot.organization?.id) {
      setSearchedPeople({ key: "", people: [] });
      return;
    }
    if (!open) return;
    const request = capturePropertyCellRequest(context);
    const cancel = scheduleOrganizationPeopleTypeahead({
      organizationId: snapshot.organization.id,
      query: searchKey,
      limit: 12,
    }, {
      search: (input, options) => operations.searchPeople(input, options, context),
      onResult: (result) => {
        if (request.isCurrent()) setSearchedPeople({ key: searchKey, people: result.people ?? [] });
      },
      onError: () => {
        if (request.isCurrent()) setSearchedPeople({ key: searchKey, people: [] });
      },
    });
    return () => { request.abort(); cancel(); };
  }, [context, open, operations, searchKey, snapshot.organization?.id]);

  const chosenKey = ids.join("\u0000");
  const peopleOptions = useMemo(() => {
    const chosen = new Set(chosenKey ? chosenKey.split("\u0000") : []);
    const options = new Map<string, { id: string; label: string; description: string }>();
    // The people chosen stand only as chips in the field, out of the list, as
    // Notion's (seen 2026-11-07: "〈이름〉 ×" above, gone below).
    const addOption = (id: string | undefined | null, label: string, description: string) => {
      const normalizedId = id?.trim();
      if (!normalizedId || options.has(normalizedId) || chosen.has(normalizedId)) return;
      if (!personOptionMatches(label, description, normalizedId, searchKey)) return;
      options.set(normalizedId, { id: normalizedId, label, description });
    };
    if (snapshot.templateValues) {
      addOption(templateMe, t("propertyCell:templateValue.me"), t("propertyCell:templateValue.whenDuplicated"));
    }
    addOption(currentId, personLabel(currentId, snapshot.userId ?? undefined, false), t("propertyCell:you"));
    // The people the last search found stay, narrowed to what is typed, while
    // the next search is on its way and when the list opens again.
    for (const profile of [...searchedPeople.people, ...snapshot.organizationProfiles]) {
      addOption(profile.userId, organizationProfilePersonLabel(profile), organizationProfilePersonDescription(profile));
    }
    return Array.from(options.values()).slice(0, 12);
  }, [chosenKey, currentId, searchKey, searchedPeople, snapshot.organizationProfiles, snapshot.templateValues, snapshot.userId, t]);

  function commit(next: string[]) {
    void operations.setValue(row.id, prop, next.length ? next : null, context);
  }

  // The creator a property defaults to stands while the row names no one:
  // taken off, it stays, and a notice says to add someone in its place, as
  // Notion's "×" does (seen 2026-11-11); Backspace leaves it as it is.
  const stored = row.properties?.[prop.id];
  const namesNoOne = Array.isArray(stored) ? stored.length === 0 : stored == null || stored === "";
  const standsByDefault = (personId: string) => prop.config?.defaultCreator === true && namesNoOne && personId === row.createdBy;

  // A property limited to one person takes the one picked in place of its
  // own, as a relation limited to one page does.
  function togglePerson(personId: string) {
    commit(selected.has(personId) ? ids.filter((id) => id !== personId) : prop.config?.limit === 1 ? [personId] : [...ids, personId]);
  }

  // Whoever was invited goes in at once, as Notion's invite puts them in:
  // someone with an account as themselves, someone without one named by the
  // address until they join, with "〈주소〉님을 초대했습니다." (seen 2026-11-12).
  async function chooseInvited(emails: string[]) {
    operations.notify(inviteMentionDoneText(emails), "success");
    const found = await findInvitedPeople((input) => operations.searchPeople(input, {}, context), snapshot.organization?.id, emails);
    // Whom the invite couldn't put in is said, not left out quietly.
    const named = session ? await invitedPeopleValues(session, context.workspaceId, emails).catch(() => {
      operations.notify(t("topBar:inviteMention.notPlaced"), "error");
      return [];
    }) : [];
    const fresh = [...new Set([...found, ...named])].filter((personId) => !selected.has(personId));
    if (!fresh.length) return;
    setQuery("");
    commit(prop.config?.limit === 1 ? [fresh[0]] : [...ids, ...fresh]);
  }

  // The keys and the pointer light the people as Notion's list (seen
  // 2026-11-14): opened, or its search emptied, none is lit and ↓ lights the
  // first, ↑ the last, going round; what is typed lights the first found;
  // Enter picks the lit one, with none lit nothing; the pointer lights the
  // one it moves onto, after the arrows only once it moves into another
  // (`pointerHover`). A pick empties the search, as Notion's.
  const picker = useNativeAutocomplete({ items: peopleOptions, query, onQuery: setQuery, open: open && !readOnly,
    keyOf: (person) => person.id, textOf: (person) => person.label, inputRef: searchRef, listRef,
    onPick: (person) => { setQuery(""); togglePerson(person.id); }, onClose: () => closeMenu(true),
    highlightFirst: query.trim().length > 0, commitTab: false, label: t("propertyCell:searchOrganizationPeople") });
  const lit = picker.state.selectionManager.isFocused ? picker.activeKey : null;

  function removePerson(personId: string, { quietly = false } = {}) {
    if (!standsByDefault(personId)) togglePerson(personId);
    else if (!quietly) operations.notify(t("propertyCell:defaultPersonStays"), "info");
  }


  function closeMenu(restoreFocus = false) {
    setOpen(false);
    setQuery("");
    if (restoreFocus) restoreFocusUnlessChosen(triggerRef.current);
  }

  // A face and its name as the field holds it: 24 and 16px on a touch screen's sheet (Notion's phone, seen
  // 2026-11-15 at 402pt), 20 in the window.
  const chipFace = touch ? 24 : 20, rowFace = touch ? 28 : 20;
  // The people chosen stand in the field before what's typed, as Notion's (seen 2026-11-03).
  const field = (
    // eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions
    <div className={styles.selectField} data-people-field="" onClick={() => searchRef.current?.focus()}>
      {ids.map((id) => {
        // A chip names the person alone, the reader too, as Notion's (no "(나)").
        const name = personLabel(id, snapshot.userId ?? undefined, false);
        return (
          <span key={id} className={styles.pickedPerson} data-selected-person={id}>
            <PersonAvatar userId={id} name={name} size={chipFace} />{name}
            <button type="button" className={styles.chipRemove} aria-label={t("propertyCell:removeOption", { name })}
              onClick={(event) => { event.stopPropagation(); removePerson(id); }}><XMarkIcon size={touch ? "small" : "micro"} aria-hidden="true" /></button>
          </span>
        );
      })}
      <input
        {...picker.inputProps}
        ref={searchRef}
        className={styles.selectFieldInput}
        type="search"
        placeholder={ids.length ? undefined : t("propertyCell:searchPeopleOrGroups")}
        onKeyDown={(event) => {
          // Backspace in an empty field takes the last chosen person back.
          if (event.key === "Backspace" && !query && ids.length > 0) {
            event.preventDefault();
            removePerson(ids[ids.length - 1], { quietly: true });
            return;
          }
          // Enter with no one lit picks no one and leaves the list open.
          if (fieldEnter(event) && lit == null) {
            event.preventDefault();
            return;
          }
          picker.inputProps.onKeyDown?.(event);
        }}
      />
    </div>
  );
  // "원하는 만큼 선택" over the people, or "1개 선택" where one may be chosen, as Notion's (seen 2026-11-12).
  const pickText = t(prop.config?.limit === 1 ? "propertyCell:pickOnePerson" : "propertyCell:pickAnyPeople");
  const pickLabel = <div className={styles.propMenuLabel}>{pickText}</div>;
  const empty = peopleOptions.length === 0 ? (
    <PeoplePickerEmpty query={query} label={touch ? undefined : pickLabel} invitePageId={invitePageId} session={session}
      onInvited={(emails) => void chooseInvited(emails)} />
  ) : null;
  const listBox = (
    <div {...picker.listBoxProps} ref={listRef} className={styles.peopleList} data-people-list=""
      aria-multiselectable={prop.config?.limit !== 1}>
      {peopleOptions.map((person) => (
        <NativePickerOption
          key={person.id}
          itemKey={person.id}
          state={picker.state}
          tabIndex={-1}
          className={styles.peopleOption}
          aria-selected={selected.has(person.id)}
          data-selected={selected.has(person.id) ? "true" : undefined}
        >
          {({ labelProps }) => <>
          <PersonAvatar className={styles.personAvatar} userId={person.id} name={person.label} size={rowFace} />
          {/* The reader's own row reads "〈이름〉(나)", a name alone otherwise (Notion's, seen 2026-11-03). */}
          <span className={styles.peopleOptionText}>
            <span {...labelProps}>{person.label}{person.id === currentId ? <SelfMark className={styles.peopleOptionMe} text={t("propertyCell:youMark")} /> : null}</span>
            {person.id === templateMe ? <span>{person.description}</span> : null}
            {person.id !== templateMe && person.id !== currentId && personDistinction(person.id)
              ? <span>{personDistinction(person.id)}</span> : null}
          </span>
          {selected.has(person.id) && <span className={styles.check}><CheckIcon size="small" aria-hidden="true" /></span>}
          </>}
        </NativePickerOption>
      ))}
    </div>
  );

  return (
    <div className={styles.peopleCell}>
      <div
        className={styles.personChips}
        role="group"
        aria-label={prop.name}
        data-empty={ids.length === 0 ? "true" : undefined}
        onClick={(event) => openEditorFromCellBackground(event, () => setOpen(!readOnly))}
      >
        {ids.length > 0 && <span className={styles.valueMark}>{ids.map((id) => (
          <PersonBadge key={id} id={id} currentUserId={snapshot.userId} />
        ))}</span>}
        {ids.length === 0 && <span className={styles.cellEmpty}>&nbsp;</span>}
        <button
          ref={triggerRef}
          type="button"
          className={styles.propertyCellEditTrigger}
          aria-label={t("propertyCell:editPeople", { name: prop.name })}
          aria-haspopup="dialog"
          aria-expanded={open}
          {...readOnlyStop(readOnly)}
          onClick={() => setOpen(!readOnly)}
        >
          <Plus size="tiny" aria-hidden="true" />
        </button>
      </div>
      {/* A touch screen's is Notion's phone sheet "〈속성〉" (seen 2026-11-15 at 402pt in iOS Safari): half the
          screen, the field in a group of its own (17px, not focused), then "원하는 만큼 선택" over the people,
          each a 45px row of a 28px face and a 16px name; a pick leaves it open, the chosen one in the field. */}
      {open && !readOnly && touch ? (
        <TouchSheetLayout>
          <PhoneSheet size="half" title={prop.name} label={t("propertyCell:editPersonProperty")} tone="settings"
            onClose={() => closeMenu(true)} data={{ "data-people-sheet": "" }}>
            <PhoneSheetGroup className={sheet.field}>{field}</PhoneSheetGroup>
            {empty ? <PhoneSheetGroup label={pickText}>{empty}</PhoneSheetGroup>
              : <PhoneSheetGroup label={pickText} className={sheet.people}>{listBox}</PhoneSheetGroup>}
          </PhoneSheet>
          <PickerOpened />
        </TouchSheetLayout>
      ) : open && !readOnly ? (
        <CellMenuPortal
          triggerRef={triggerRef}
          onClose={() => closeMenu(true)}
          ariaLabel={t("propertyCell:editPersonProperty")}
          className={styles.peopleMenu}
          backdropClassName={styles.menuBackdrop}
          width={240}
          cover
          tabLeavesCell
          onKeyDown={(event) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            closeMenu(true);
          }}
        >
          {field}
          {empty ?? <>{pickLabel}{listBox}</>}
        </CellMenuPortal>
      ) : null}
    </div>
  );
}
