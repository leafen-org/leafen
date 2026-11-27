import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { ReadFailed } from "@/components/ReadFailed";
import { CheckIcon, VerifiedIcon, X } from "@/icons/leafen";
import { activeDateLocale } from "@/lib/i18n";
import { isPageVerified } from "@/lib/pageVerification";
import { usePageBusy } from "@/lib/pageBusy";
import { usePageFilling } from "@/lib/pageCopyProgress";
import { keepRowPageReads, readKeptRowPage } from "@/lib/keptRowPage";
import { isServerUnreachable } from "@/lib/serverReach";
import { useReadAgainWhenAnswered } from "@/lib/unansweredRead";
import { personName } from "@/lib/peopleDirectory";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { scheduleOrganizationPeopleTypeahead } from "@/lib/typeaheadSearch";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { useProductToast } from "@/lib/productToast";
import type { OrganizationProfile } from "@/lib/types";
import type { Session } from "@/types";
import type { PageVerification, PageVerificationInput } from "@/wikiWire.generated";
import { NATIVE_ICON_GLYPHS } from "./nativeIconGlyphs";
import headerStyles from "./PageHeader.module.css";
import styles from "./PageVerification.module.css";
import { useVerifyOffered } from "./verifyOffered";
import { AppDateField } from "@/components/database/ScheduleFieldPickers";

/** A page's verification as a page carries it, or a database's on a page. */
export type PageVerificationFields = {
  id: string;
  /** Its workspace, whose people its owners are picked from. */
  workspaceId?: string;
  /** A database opened as its own page: its block on the page `id` names. */
  databaseBlockId?: string;
  wikiRevision?: number;
  verifiedAt?: string | null;
  verifiedBy?: string | null;
  verificationExpiresAt?: string | null;
};

export type PageVerificationChange = {
  wikiRevision: number;
  verifiedAt: string;
  verifiedBy: string;
  verificationExpiresAt: string;
};

function verifiedNow(page: PageVerificationFields) {
  return isPageVerified({ verifiedAt: page.verifiedAt, verificationExpiresAt: page.verificationExpiresAt } as
    Parameters<typeof isPageVerified>[0]);
}

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(date);
}

/** The end of a chosen day where the reader is, as the instant a verification runs until. */
function endOfLocalDay(day: string) {
  const [year, month, date] = day.split("-").map(Number);
  return new Date(year, month - 1, date, 23, 59, 59).toISOString();
}

/** The reader's day so many days from today, as a date input writes it. */
function localDayAfter(days: number) {
  const day = new Date();
  day.setDate(day.getDate() + days);
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${day.getFullYear()}-${pad(day.getMonth() + 1)}-${pad(day.getDate())}`;
}

/** A day as the menu names it: month and day, with the year when it isn't this one. */
function formatMenuDay(day: string) {
  const [year, month, date] = day.split("-").map(Number);
  return new Intl.DateTimeFormat(activeDateLocale(), {
    ...(year === new Date().getFullYear() ? {} : { year: "numeric" }), month: "long", day: "numeric",
  }).format(new Date(year, month - 1, date));
}

// As Notion's "페이지 인증" menu (seen 2026-10-01): ready periods from today, then indefinitely
// and a day picked; pressed open, no period lit until the pointer or an arrow comes (2026-11-16).
const VERIFY_PERIOD_DAYS = [90, 30, 7] as const;

function MenuGlyph({ name }: { name: "alarm" | "infinity" | "calendar" }) {
  const glyph = NATIVE_ICON_GLYPHS[name];
  return (
    <svg className={styles.periodGlyph} viewBox="0 0 20 20" aria-hidden="true">
      <path d={glyph?.stroke} fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

// A page's verification writes go one after another, each on the revision
// the one before it was answered with, and a press doesn't wait for them.
const verificationLanes = new Map<string, { tail: Promise<unknown>; revision: number; turn: number }>();

function verificationOf(page: PageVerificationFields): PageVerificationChange {
  return { wikiRevision: page.wikiRevision ?? 0, verifiedAt: page.verifiedAt ?? "", verifiedBy: page.verifiedBy ?? "",
    verificationExpiresAt: page.verificationExpiresAt ?? "" };
}

/**
 * A verification change shows at once (user decision 2026-11-12: a press
 * answers on the next frame) and goes to the server behind it; the answer
 * takes its place, and a refused one puts back what the page showed before
 * and says why, in its menu while that is open and in a toast.
 */
function useVerificationWrite(
  session: Session,
  page: PageVerificationFields,
  onChange?: (change: PageVerificationChange) => void,
) {
  const { t } = useTranslation("pageHeader");
  const { notify } = useProductToast();
  const [failed, setFailed] = useState(false);
  // The newest owner of the change: an answer comes after the press's screen.
  const latest = useRef(onChange);
  latest.current = onChange;
  const write = (input: Omit<PageVerificationInput, "expectedRevision">, revision: number, shown?: PageVerificationChange) => {
    const key = `${page.id}:${page.databaseBlockId ?? ""}`;
    const before = verificationOf(page);
    const lane = verificationLanes.get(key) ?? { tail: Promise.resolve(), revision: 0, turn: 0 };
    const turn = lane.turn + 1;
    setFailed(false);
    if (shown) onChange?.(shown);
    const sent = lane.tail.then(() => {
      const expectedRevision = Math.max(revision, verificationLanes.get(key)?.revision ?? 0);
      return page.databaseBlockId
        ? api.writeDatabaseVerification(session, page.id, page.databaseBlockId, { ...input, expectedRevision })
        : api.writePageVerification(session, page.id, { ...input, expectedRevision });
    });
    verificationLanes.set(key, { tail: sent.catch(() => undefined), revision: lane.revision, turn });
    return sent.then((result) => {
      const now = verificationLanes.get(key);
      if (now) now.revision = Math.max(now.revision, result.revision);
      latest.current?.({
        wikiRevision: result.revision,
        verifiedAt: result.verifiedAt,
        verifiedBy: result.verifiedBy,
        verificationExpiresAt: result.verificationExpiresAt,
      });
      return result;
    }, () => {
      // A change pressed after it decides what shows.
      if (verificationLanes.get(key)?.turn === turn) {
        if (shown) latest.current?.(before);
        notify(t("verify.failed"), "error");
      }
      setFailed(true);
      return null;
    });
  };
  return { failed, write };
}

/**
 * The "Verify" a page someone can edit offers when its header is hovered, as
 * Notion's does: verified until a day or indefinitely. A wiki's page is
 * verified with its wiki's Verification property instead. A touch screen's
 * head and a workspace guest's leave it out, as Notion's (verifyOffered.ts:
 * "아이콘 추가 · 커버 추가 · 댓글 추가" there; a member's mouse head keeps it at
 * every width, 640 to 1440, with a side peek open or not).
 */
export function PageVerifyControl({
  page,
  session,
  className = headerStyles.ctrlBtn,
  onChange,
}: {
  page: PageVerificationFields;
  session: Session;
  className?: string;
  onChange?: (change: PageVerificationChange) => void;
}) {
  const { t } = useTranslation("pageHeader");
  const [open, setOpen] = useState(false);
  const [choosing, setChoosing] = useState(false);
  const [until, setUntil] = useState("");
  const wrapRef = useRef<HTMLDivElement>(null), menuRef = useRef<HTMLDivElement>(null), periodsRef = useRef<HTMLDivElement>(null);
  const { failed, write } = useVerificationWrite(session, page, onChange);
  const offered = useVerifyOffered(page.workspaceId);
  const verified = verifiedNow(page);
  const close = () => { setOpen(false); setChoosing(false); };
  usePopoverAppear(open && !verified, menuRef, () => wrapRef.current);
  const layer = useOverlayLayer(open && !verified, { kind: "menu", ref: menuRef, inside: () => [wrapRef.current],
    onEscape: close, onOutsidePress: close });
  // The page shows verified as the period is picked, by the one who picked it.
  const verify = (expiresAt?: string) => {
    close();
    void write({ operation: "verify", ...(expiresAt ? { expiresAt } : {}) }, page.wikiRevision ?? 0, {
      wikiRevision: page.wikiRevision ?? 0, verifiedAt: new Date().toISOString(), verifiedBy: session.user.id,
      verificationExpiresAt: expiresAt ?? "",
    });
  };
  const periods = ["90", "30", "7", "indefinite", "choose"];
  const periodText = (key: string) => key === "indefinite" ? t("verify.indefinitely")
    : key === "choose" ? t("verify.pickDate") : t("verify.forDays", { days: Number(key) });
  const picker = useNativeMenu({ items: periods, open: open && !verified, menuRef: periodsRef, quietOnPointer: true,
    label: t("verify.title"), keyOf: key => key, textOf: periodText, onAction: key => {
      if (key === "choose") setChoosing(current => !current);
      else verify(key === "indefinite" ? undefined : endOfLocalDay(localDayAfter(Number(key))));
    } });
  if (verified || !offered) return null;
  return (
    <div ref={wrapRef} className={styles.verifyWrap}>
      <button type="button" className={className} data-page-header-control data-page-verify
        aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen((current) => !current)}>
        <VerifiedIcon size="tiny" weight="fill" aria-hidden="true" />
        <span>{t("addVerification")}</span>
      </button>
      {open ? (
        <div ref={menuRef} className={`${styles.menu} ${styles.verifyMenu}`} role="dialog" aria-label={t("verify.title")}
          style={{ zIndex: layer.zIndex }} onKeyDown={layer.onKeyDown} data-page-verify-menu>
          <div className={styles.menuTitle}>{t("verify.title")}</div>
          <div {...picker.menuProps} ref={periodsRef}>
          {VERIFY_PERIOD_DAYS.map((days) => {
            const day = localDayAfter(days);
            return (
              <NativeMenuItem key={days} state={picker.state} itemKey={String(days)} className={styles.periodItem}
                data-verify-period={days}>{({ labelProps, descriptionProps }) => <>
                <MenuGlyph name="alarm" />
                <span className={styles.periodText}>
                  <span {...labelProps}>{t("verify.forDays", { days })}</span>
                  <span {...descriptionProps} className={styles.periodNote}>{t("verify.verifiedUntil", { date: formatMenuDay(day) })}</span>
                </span>
              </>}</NativeMenuItem>
            );
          })}
          <NativeMenuItem state={picker.state} itemKey="indefinite" className={styles.periodItem}>
            {({ labelProps }) => <>
            <MenuGlyph name="infinity" />
            <span {...labelProps}>{t("verify.indefinitely")}</span>
          </>}</NativeMenuItem>
          <NativeMenuItem state={picker.state} itemKey="choose" className={styles.periodItem} aria-expanded={choosing}>
            {({ labelProps }) => <>
            <MenuGlyph name="calendar" />
            <span {...labelProps}>{t("verify.pickDate")}</span>
          </>}</NativeMenuItem>
          </div>
          {choosing ? (
            <div className={styles.untilRow}>
              {/* From tomorrow on, as Notion's "날짜 선택" (seen 2026-11-11: today and before can't be picked). */}
              <AppDateField label={t("verify.pickDate")} value={until} min={localDayAfter(1)} emptyLabel={t("verify.pickDate")}
                onChange={setUntil} />
              <button type="button" className={styles.primary} disabled={!until}
                onClick={() => verify(endOfLocalDay(until))}>
                {t("verify.untilAction")}
              </button>
            </div>
          ) : null}
          {failed ? <p className={styles.error} role="alert">{t("verify.failed")}</p> : null}
        </div>
      ) : null}
    </div>
  );
}

/**
 * A verified page's "Verified", as Notion shows it at the top of the page:
 * its menu names who verified it and until when, lists its owners, whom an
 * expiring verification reminds, and takes the verification away. On a
 * wiki's page it reads what the wiki's Verification and Owner properties say.
 */
export function PageVerifiedBadge({
  page,
  session,
  organizationId,
  readOnly,
  onChange,
}: {
  page: PageVerificationFields;
  session: Session;
  organizationId?: string;
  readOnly: boolean;
  onChange?: (change: PageVerificationChange) => void;
}) {
  const { t } = useTranslation("pageHeader");
  usePeopleDirectoryRevision();
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<PageVerification | null>(null);
  const [readFailed, setReadFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [query, setQuery] = useState("");
  const [people, setPeople] = useState<OrganizationProfile[]>([]), [peopleFailed, setPeopleFailed] = useState(false), [peopleTurn, setPeopleTurn] = useState(0);
  const wrapRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const { failed, write } = useVerificationWrite(session, page, onChange);
  const verified = verifiedNow(page);

  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    setReadFailed(false);
    void readVerification(session, page, controller.signal)
      .then((next) => { if (!controller.signal.aborted) setState(next); })
      .catch(() => {
        if (!controller.signal.aborted) { setState(null); setReadFailed(true); }
      });
    return () => controller.abort();
  }, [attempt, open, page.databaseBlockId, page.id, page.verifiedAt, session]);

  useEffect(() => {
    if (!open || !organizationId || !query.trim()) {
      setPeople([]);
      return;
    }
    // Its workspace's people, as a person cell's (organization_people_workspace.go).
    const workspaceId = page.workspaceId;
    return scheduleOrganizationPeopleTypeahead({ organizationId, query: query.trim(), limit: 8 }, {
      search: (input, options) => api.searchOrganizationPeople(session, { ...input, ...(workspaceId ? { workspaceId } : {}) }, options),
      onResult: (result) => { setPeople(result.people ?? []); setPeopleFailed(false); },
      onError: () => { setPeople([]); setPeopleFailed(true); },
    });
  }, [open, organizationId, page.workspaceId, peopleTurn, query, session]);

  const layer = useOverlayLayer(open && verified, { kind: "menu", ref: menuRef, inside: () => [wrapRef.current],
    onEscape: () => setOpen(false), onOutsidePress: () => setOpen(false) });
  const editable = !readOnly && state !== null && !state.byProperty;
  const owners = state?.owners ?? [];
  // The owners show as they are added or taken away; a refusal puts them back.
  const setOwners = (next: PageVerification["owners"]) => {
    if (!state) return;
    const before = state.owners;
    setState({ ...state, owners: next });
    void write({ operation: "setOwners", owners: next.map(({ userId }) => userId) }, state.revision).then((result) => {
      if (result) setState(result);
      else setState((current) => current && { ...current, owners: before });
    });
  };
  const candidates = people.filter(person => person.userId && !owners.some(owner => owner.userId === person.userId));
  const picker = useNativeAutocomplete({ items: candidates, query, onQuery: setQuery, inputRef, listRef,
    popoverRef: menuRef, label: t("verify.addOwner"), commitTab: false,
    open: open && verified && editable && Boolean(organizationId) && Boolean(query.trim()),
    keyOf: person => person.userId!, textOf: person => person.displayName || person.email || person.userId!,
    onPick: person => {
      setQuery("");
      setOwners([...owners, { userId: person.userId!, name: person.displayName ?? "", email: person.email ?? "" }]);
    } });
  if (!verified) return null;
  // The one who verified, an owner or not, by the name the people here know
  // (2026-11-10: one who wasn't an owner read "누군가").
  const verifier = owners.find((owner) => owner.userId === page.verifiedBy);
  const verifierName = verifier?.name || verifier?.email || personName(page.verifiedBy ?? "");
  return (
    <div ref={wrapRef} className={styles.badgeWrap}>
      <button type="button" className={styles.badge} data-page-verified aria-haspopup="dialog" aria-expanded={open}
        onClick={() => setOpen((current) => !current)}>
        <CheckIcon size="tiny" aria-hidden="true" />
        <span>{t("verify.verified")}</span>
      </button>
      {open ? (
        <div ref={menuRef} className={styles.menu} role="dialog" aria-label={t("verify.verified")}
          style={{ zIndex: layer.zIndex }} onKeyDown={layer.onKeyDown} data-page-verified-menu>
          <p className={styles.note}>
            {t("verify.verifiedOn", {
              name: verifierName || t("verify.someone"),
              date: formatDate(page.verifiedAt ?? ""),
            })}
          </p>
          <p className={styles.note} data-page-verified-until>
            {page.verificationExpiresAt
              ? t("verify.verifiedUntil", { date: formatDate(page.verificationExpiresAt) })
              : t("verify.verifiedIndefinitely")}
          </p>
          {state?.byProperty ? <p className={styles.note}>{t("verify.wikiHint")}</p> : null}
          <div className={styles.menuTitle}>{t("verify.owners")}</div>
          {readFailed ? <ReadFailed message={t("verify.loadFailed")} onRetry={() => setAttempt((current) => current + 1)} /> : null}
          <ul className={styles.owners} data-page-verification-owners>
            {owners.map((owner) => (
              <li key={owner.userId}>
                <span>{owner.name || owner.email || owner.userId}</span>
                {editable && owners.length > 1 ? (
                  <button type="button" aria-label={t("verify.removeOwner", { name: owner.name || owner.email })}
                    onClick={() => setOwners(owners.filter(({ userId }) => userId !== owner.userId))}>
                    <X size="tiny" aria-hidden="true" />
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
          {editable && organizationId ? (
            <>
              <input {...picker.inputProps} ref={inputRef} className={styles.search} placeholder={t("verify.addOwner")} />
              <div {...picker.listBoxProps} ref={listRef}>
                {candidates.map(person => <NativePickerOption key={person.userId} state={picker.state}
                  itemKey={person.userId!} className={styles.menuItem}>
                  {({ labelProps }) => <span {...labelProps}>{person.displayName || person.email || person.userId}</span>}
                </NativePickerOption>)}
              </div>{peopleFailed ? <ReadFailed message={t("verify.peopleLoadFailed")} onRetry={() => setPeopleTurn((turn) => turn + 1)} /> : null}
            </>
          ) : null}
          {editable ? (
            <button type="button" className={styles.menuItem} data-page-verification-remove
              onClick={() => {
                setOpen(false);
                void write({ operation: "unverify" }, state.revision,
                  { wikiRevision: state.revision, verifiedAt: "", verifiedBy: "", verificationExpiresAt: "" });
              }}>
              {t("verify.remove")}
            </button>
          ) : null}
          {failed ? <p className={styles.error} role="alert">{t("verify.failed")}</p> : null}
        </div>
      ) : null}
    </div>
  );
}

/**
 * A database page's verification, read for the page as its row page opens:
 * the page's "Verified", and its "Verify" for someone who can edit it. A
 * wiki's page is verified with its wiki's Verification property instead.
 */
function readVerification(session: Session, page: Pick<PageVerificationFields, "id" | "databaseBlockId">, signal: AbortSignal) {
  return page.databaseBlockId
    ? api.readDatabaseVerification(session, page.id, page.databaseBlockId, signal)
    : api.readPageVerification(session, page.id, signal);
}

/**
 * A database's verification, read as it opens as its own page (Notion: its
 * "인증 추가" and "인증됨" as a page's), for its header to show.
 */
export function useDatabaseVerification(session: Session | null | undefined, pageId: string, blockId: string | undefined) {
  const [state, setState] = useState<PageVerification | null>(null);
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    setState(null);
    setFailed(false);
    if (!session || !blockId) return undefined;
    const controller = new AbortController();
    void readVerification(session, { id: pageId, databaseBlockId: blockId }, controller.signal)
      .then((next) => { if (!controller.signal.aborted) setState(next); })
      .catch(() => { if (!controller.signal.aborted) setFailed(true); });
    return () => controller.abort();
  }, [attempt, blockId, pageId, session]);
  // An unread verification says so where its badge would be, rather than
  // look unverified.
  if (failed && blockId) return { failed: true as const, retry: () => setAttempt((current) => current + 1) };
  if (!state || !blockId) return null;
  const fields: PageVerificationFields = {
    // The page the database was opened on, whose block names it, even when
    // it shows a database kept on another page.
    id: pageId, databaseBlockId: blockId, wikiRevision: state.revision, verifiedAt: state.verifiedAt,
    verifiedBy: state.verifiedBy, verificationExpiresAt: state.verificationExpiresAt,
  };
  const onChange = (change: PageVerificationChange) => setState((current) => current && {
    ...current, revision: change.wikiRevision, verifiedAt: change.verifiedAt,
    verifiedBy: change.verifiedBy, verificationExpiresAt: change.verificationExpiresAt,
  });
  return { failed: false as const, fields, onChange };
}

const VERIFICATION_REFUSED_WAITS_MS = [500, 1_500, 4_000] as const;

/**
 * A database page's verification, read once: its "인증 추가" goes among the
 * head's options between the cover and the layout, as Notion's (seen
 * 2026-10-17), and its badge over the title.
 */
export function useRowPageVerification(session: Session | null | undefined, rowId: string) {
  const [state, setState] = useState<PageVerification | null>(null);
  const [failed, setFailed] = useState(false);
  const [unanswered, setUnanswered] = useState(false);
  const [attempt, setAttempt] = useState(0);
  // A page refused a moment — one a copy is filling, as a template's
  // database copied into a page just made from it (2026-11-14) — is read
  // again before its read says it failed; one refused while it is known to
  // be held (lib/pageBusy.ts, lib/pageDatabaseCopies.ts) waits for the hold
  // to end, however long, and is read again then.
  const refusals = useRef({ rowId, count: 0 });
  if (refusals.current.rowId !== rowId) refusals.current = { rowId, count: 0 };
  const busy = usePageBusy(rowId);
  const filling = usePageFilling(rowId);
  const held = Boolean(busy) || filling;
  const heldNow = useRef(held);
  heldNow.current = held;
  const refusedHeld = useRef(false);
  useEffect(() => {
    if (held) {
      if (failed) refusedHeld.current = true;
      return;
    }
    if (!refusedHeld.current) return;
    refusedHeld.current = false;
    setAttempt((current) => current + 1);
  }, [failed, held]);
  useEffect(() => {
    if (!session) return;
    const controller = new AbortController();
    let again = 0;
    setState(null);
    setFailed(false);
    setUnanswered(false);
    void api.readPageVerification(session, rowId, controller.signal)
      .then((next) => {
        if (controller.signal.aborted) return;
        refusals.current.count = 0;
        setState(next);
        keepRowPageReads(session, rowId, { verification: next });
      })
      .catch(async (error: unknown) => {
        if (controller.signal.aborted) return;
        const status = Reflect.get(Object(error), "status");
        if ((status === 403 || status === 404) && heldNow.current) {
          refusedHeld.current = true;
          return;
        }
        if ((status === 403 || status === 404) && refusals.current.count < VERIFICATION_REFUSED_WAITS_MS.length) {
          const wait = VERIFICATION_REFUSED_WAITS_MS[refusals.current.count]!;
          refusals.current.count += 1;
          again = window.setTimeout(() => setAttempt((current) => current + 1), wait);
          return;
        }
        // Without the server it shows as the page kept it, and is read again
        // once it answers (keptRowPage.ts).
        if (!isServerUnreachable(error)) return setFailed(true);
        setUnanswered(true);
        const kept = (await readKeptRowPage(session, rowId))?.verification;
        if (kept && !controller.signal.aborted) setState(kept);
      });
    return () => {
      controller.abort();
      window.clearTimeout(again);
    };
  }, [attempt, rowId, session]);
  useReadAgainWhenAnswered(unanswered, () => setAttempt((current) => current + 1));
  if (!session) return null;
  if (failed) return { failed: true as const, retry: () => setAttempt((current) => current + 1) };
  if (!state) return null;
  const page: PageVerificationFields = {
    id: rowId, wikiRevision: state.revision, verifiedAt: state.verifiedAt,
    verifiedBy: state.verifiedBy, verificationExpiresAt: state.verificationExpiresAt,
  };
  const onChange = (change: PageVerificationChange) => setState((current) => current && {
    ...current, revision: change.wikiRevision, verifiedAt: change.verifiedAt,
    verifiedBy: change.verifiedBy, verificationExpiresAt: change.verificationExpiresAt,
  });
  return { failed: false as const, session, page, onChange, byProperty: state.byProperty };
}

type RowPageVerificationState = ReturnType<typeof useRowPageVerification>;

/** The database page's "인증 추가" among its head's options, for one who may verify it. */
export function rowPageVerifyControl(verification: RowPageVerificationState, readOnly: boolean) {
  return verification && !verification.failed && !readOnly && !verification.byProperty ? (
    <PageVerifyControl page={verification.page} session={verification.session} onChange={verification.onChange} />
  ) : null;
}

/** The database page's badge over its title, or why it couldn't be read. */
export function RowPageVerification({ verification, organizationId, readOnly }: {
  verification: RowPageVerificationState;
  organizationId?: string;
  readOnly: boolean;
}) {
  const { t } = useTranslation("pageHeader");
  if (!verification) return null;
  if (verification.failed) {
    return (
      <div className={styles.rowPage} data-row-page-verification>
        <ReadFailed message={t("verify.loadFailed")} onRetry={verification.retry} />
      </div>
    );
  }
  return (
    <div className={styles.rowPage} data-row-page-verification>
      <PageVerifiedBadge page={verification.page} session={verification.session} organizationId={organizationId}
        readOnly={readOnly} onChange={verification.onChange} />
    </div>
  );
}
