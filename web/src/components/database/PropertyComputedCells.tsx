import type { PopoverRect } from "@/lib/popoverPlacement";
import { type KeyboardEvent, type MouseEvent, useEffect, useRef, useState } from "react";
import { AlarmIcon, CalendarIcon, CheckIcon, InfinityIcon, VerifiedIcon, X } from "@/icons/leafen";
import { useTranslation } from "react-i18next";
import { activeDateLocale } from "@/lib/i18n";
import { useRouter } from "@/lib/router";
import { buttonResultRequiresConfirmation, type ButtonConfirmationChallenge } from "@/lib/buttonExecutionContract";
import { buttonActionsRefused, buttonRunNoticeText, buttonTargetMissing, dispatchButtonClientOutcomes } from "@/lib/buttonExecution";
import { pageHref } from "@/lib/navigation";
import { useOptionalProductToast } from "@/lib/productToast";
import type { DbProperty, Page } from "@/lib/types";
import { ButtonConfirmationDialog } from "../automation/ButtonConfirmationDialog";
import { evaluateFormula, evaluateRollupCheckboxes, evaluateRollupWithComputedDisplay, formulaDisplay } from "./formula";
import { RichTextCellText } from "./RichTextCellText";
import { rolledUpPageIds, rollupHopDatabaseIds, rollupMayShowCheckboxes, rollupOptionValues, unreadableRelationIds, valueAsIds } from "./rollup";
import { rollupShownFunction } from "./rollupPolicy";
import { chipStyle } from "./colors";
import { rollupShownText } from "./rollupDateDisplay";
import { rollupFormatProperty, rollupGivesNumber, rollupNumberShown, rollupNumberText } from "./rollupNumberDisplay";
import { placePropertySummary, placePropertyValue, verificationPropertySummary, verificationPropertyValue } from "./structuredPropertyValues";
import { CellMenuPortal, propertyValueCell, valueCellFoot } from "./PropertyCellMenu";
import { PlacePicker } from "./PlacePicker";
import { abortWhileRunning, capturePropertyCellRequest, type PropertyCellEnvironment } from "./propertyCellTypes";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { FormulaEditorDialog } from "./FormulaEditorDialog";
import styles from "./databaseSheet";
import { NumberValueDisplay } from "./NumberValueDisplay";
import { getLocalTimeZone, today, type CalendarDate } from "@internationalized/date";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { parseDateDraftIn } from "./dateUtils";
import { personLabel } from "./people";
import editorStyles from "./PropertyDateEditor.module.css";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { restoreFocusUnlessChosen } from "./tableCellKeys";
import { RollupPageLinks } from "./RollupPageLinks";
import { PersonBadge } from "./PersonBadge";
import { readOnlyStop, useCellReadOnly } from "./cellReadOnly";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * A place shows as it does everywhere (placePropertySummary); pressed, its
 * picker opens over the cell, as Notion's place editor does (seen 2026-10-06), with the point
 * picked on a map, or explicitly searched through an operator's provider.
 */
export function PlaceCell({ row, prop, value, onInput, context, operations }: PropertyCellEnvironment & {
 row: Page;
  prop: DbProperty;
  value: unknown;
  onInput: (value: unknown) => void;
}) {
  const { t } = useTranslation(["propertyCell", "common"]);
  const [open, setOpen] = useState(false);
  const readOnly = useCellReadOnly();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const place = placePropertyValue(value);
  const shown = placePropertySummary(value);
  return (
    <div className={styles.placeCell} data-structured-property="place">
      <button ref={triggerRef} type="button" className={styles.placeCellButton} aria-label={t("propertyCell:edit", { name: prop.name })}
        aria-haspopup="dialog" aria-expanded={open} {...readOnlyStop(readOnly)} onClick={() => setOpen((current) => !current && !readOnly)}>{shown}</button>
      {open && !readOnly ? (
        <CellMenuPortal triggerRef={triggerRef} onClose={() => setOpen(false)} ariaLabel={t("propertyCell:edit", { name: prop.name })}
          className={styles.selectMenu} backdropClassName={styles.menuBackdrop} width={240} cover>
          {/* One picker while it is open: a save starts a new request
              generation, and keying it by that drew a new picker over the
              words being typed (2026-11-11: an address cleared after the
              name came back). Its writes check the generation they're in. */}
          <PlacePicker key={`${row.id}/${prop.id}`} value={place}
 isCurrent={() => context.currentGeneration() === context.generation}
 search={operations.searchPlaces ? (query,signal) => operations.searchPlaces!(query,signal,context) : undefined}
 onChange={(next) => { if (context.currentGeneration() === context.generation) onInput(next); }} onRemove={() => {
            onInput(null);
            setOpen(false);
          }} />
        </CellMenuPortal>
      ) : null}
    </div>
  );
}

// Longest first, each with the day it ends, as Notion's "페이지 인증" menu (seen 2026-11-03).
const VERIFICATION_PRESET_DAYS = [90, 30, 7] as const;

export function VerificationCell({ prop, value, currentUserId, onCommit }: {
  prop: DbProperty;
  value: unknown;
  currentUserId?: string | null;
  onCommit: (value: unknown) => void;
}) {
  const { t } = useTranslation(["propertyCell", "tableView", "topBar", "wikiView"]);
  const [open, setOpen] = useState(false);
  const readOnly = useCellReadOnly();
  const [typed, setTyped] = useState("");
  const [focusedDay, setFocusedDay] = useState<CalendarDate | null>(null);
  const weekStart = useWeekStartDay();
  usePeopleDirectoryRevision();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [picking, setPicking] = useState(false);
  const labels = {
    verified: t("tableView:verified"),
    expired: t("topBar:expired"),
    verifiedUntil: (date: string) => t("tableView:verifiedUntil", { date }),
  };
  // The day a verification for so many days ends: its year only when not this one ("12월 3일", "2027년 2월 1일").
  const endsOn = (days: number) => {
    const end = new Date();
    end.setDate(end.getDate() + days);
    return end.toLocaleDateString(activeDateLocale(), { month: "long", day: "numeric",
      ...(end.getFullYear() !== new Date().getFullYear() ? { year: "numeric" } : {}) });
  };
  const current = verificationPropertyValue(value);
  const currentState = current?.state ?? "unverified";
  // Who verified it and on which day, at the foot of the menu, as Notion's (seen 2026-11-05).
  const verifier = typeof current?.verified_by === "string" ? personLabel(current.verified_by, currentUserId ?? undefined, false) : "";
  const verifiedOn = current?.date && typeof current.date === "object" && current.date.start
    ? new Date(current.date.start).toLocaleDateString(activeDateLocale(), { year: "numeric", month: "long", day: "numeric" }) : "";
  // The length it was verified for is ticked: indefinitely, or a preset's days
  // from when it was verified; a day picked is none of them (Notion, seen 2026-11-11).
  const chosen = (() => {
    if (currentState === "unverified" || !current?.date || typeof current.date !== "object") return null;
    if (!current.date.end) return "indefinite";
    const length = (Date.parse(current.date.end) - Date.parse(current.date.start)) / 86_400_000;
    return VERIFICATION_PRESET_DAYS.find((days) => Math.abs(length - days) < 1 / 24) ?? null;
  })();
  const tick = <CheckIcon size="small" className={styles.verificationTick} aria-hidden="true" />;
  const ariaLabel = t("propertyCell:edit", { name: prop.name });

  function close() {
    setOpen(false);
    setTyped("");
    setPicking(false);
    restoreFocusUnlessChosen(triggerRef.current);
  }

  function commit(next: unknown) {
    onCommit(next);
    close();
  }

  // Tomorrow is the first day it can end on; today and before can't be picked (Notion, seen 2026-11-11).
  const firstDay = () => { const day = new Date(); day.setHours(0, 0, 0, 0); day.setDate(day.getDate() + 1); return day; };
  // To the end of the day chosen: it reads "… 6일까지".
  function verifyUntilDay(day: Date) {
    const end = new Date(day);
    end.setHours(23, 59, 59, 0);
    verify(end);
  }

  // A verification runs from now until a time or indefinitely, as a Notion
  // page is verified.
  function verify(end: Date | null) {
    commit({ state: "verified", date: { start: new Date().toISOString(), end: end ? end.toISOString() : null },
      ...(currentUserId ? { verified_by: currentUserId } : {}) });
  }

  function verifyForDays(days: number) {
    const end = new Date();
    end.setDate(end.getDate() + days);
    verify(end);
  }

  return (
    <div className={styles.verificationCell} data-structured-property="verification">
      <button
        ref={triggerRef}
        type="button"
        className={styles.numberDisplay}
        aria-label={ariaLabel}
        aria-haspopup="dialog"
        aria-expanded={open}
        {...readOnlyStop(readOnly)}
        onClick={() => setOpen((current) => !current && !readOnly)}
      >
        {currentState === "verified" ? <VerifiedIcon size="small" weight="fill" className={styles.verifiedBadge} aria-hidden="true" /> : null}
        {verificationPropertySummary(value, labels)}
      </button>
      {open && !readOnly && (
        <CellMenuPortal
          triggerRef={triggerRef}
          onClose={() => setOpen(false)}
          ariaLabel={ariaLabel}
          className={styles.selectMenu}
          // 265px over a narrower column, as Notion's (seen 2026-11-11), so "… 까지 인증 유효" stays on one line.
          width={265}
          backdropClassName={styles.menuBackdrop}
          scroll={false}
          rise
          onKeyDown={(event) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            setOpen(false);
            restoreFocusUnlessChosen(triggerRef.current);
          }}
        >
          {picking ? (
            // "날짜 선택": a day from tomorrow on ends it, as Notion's (seen
            // 2026-11-11); a day before typed closes it unchanged.
            <div className={styles.verificationPick} data-verification-calendar>
              <input className={styles.verificationDayInput} autoFocus value={typed} placeholder={t("wikiView:verifyPickDate")}
                aria-label={t("wikiView:verifyPickDate")} onChange={(event) => setTyped(event.currentTarget.value)}
                onKeyDown={(event) => {
                  if (!fieldEnter(event)) return;
                  const day = parseDateDraftIn(typed);
                  if (!day) return;
                  if (day.getTime() >= firstDay().getTime()) verifyUntilDay(day);
                  else close();
                }} />
              <NativeDateCalendar value={null} focusedValue={focusedDay ?? today(getLocalTimeZone())} onFocusChange={setFocusedDay}
                weekStart={weekStart} label={t("wikiView:verifyPickDate")} previousLabel={t("propertyCell:prevMonth")} nextLabel={t("propertyCell:nextMonth")}
                min={today(getLocalTimeZone()).add({ days: 1 })}
                classes={{ head: editorStyles.calendarHead, weekdays: styles.dateWeekdays, grid: styles.dateGrid }}
                onPick={(picked) => verifyUntilDay(new Date(picked.year, picked.month - 1, picked.day))} />
            </div>
          ) : (
          <div className={styles.selectList} data-verification-list>
            {currentState !== "unverified" ? (
              <div className={styles.verificationNow} data-verification-now>
                {currentState === "verified" ? <VerifiedIcon size="small" weight="fill" className={styles.verifiedBadge} aria-hidden="true" /> : null}
                <span>{verificationPropertySummary(value, labels)}</span>
              </div>
            ) : null}
            <div className={styles.verificationHead}>{t(currentState === "unverified" ? "wikiView:verifyPage" : "wikiView:verifyAgain")}</div>
            {VERIFICATION_PRESET_DAYS.map((days, index) => (
              <button type="button" className={`${styles.summaryMenuItem} ${styles.verificationChoice}`} key={days} data-verification-choice={days}
                autoFocus={index === 0} onClick={() => verifyForDays(days)}>
                <AlarmIcon size="small" aria-hidden="true" />
                <span className={styles.verificationChoiceText}>
                  <span>{t("wikiView:verifyForDays", { count: days })}</span>
                  <small>{t("wikiView:verifyEndsOn", { date: endsOn(days) })}</small>
                </span>
                {chosen === days ? tick : null}
              </button>
            ))}
            <button type="button" className={`${styles.summaryMenuItem} ${styles.verificationPlain}`} data-verification-choice="indefinite"
              onClick={() => verify(null)}><InfinityIcon size="small" aria-hidden="true" /><span>{t("wikiView:verifyForever")}</span>
              {chosen === "indefinite" ? tick : null}</button>
            <button type="button" className={`${styles.summaryMenuItem} ${styles.verificationPlain}`} data-verification-choice="pick"
              onClick={() => setPicking(true)}><CalendarIcon size="small" aria-hidden="true" /><span>{t("wikiView:verifyPickDate")}</span></button>
            {currentState !== "unverified" ? (
              <button type="button" className={`${styles.summaryMenuItem} ${styles.verificationPlain} ${styles.verificationRemove}`} data-verification-choice="remove"
                onClick={() => commit({ state: "unverified" })}><X size="small" aria-hidden="true" /><span>{t("wikiView:removeVerification")}</span></button>
            ) : null}
            {currentState !== "unverified" && verifier ? (
              <div className={styles.verificationFoot} data-verification-foot>
                <span>{t("wikiView:lastVerifiedBy", { name: verifier })}</span>
                {verifiedOn ? <span>{verifiedOn}</span> : null}
              </div>
            ) : null}
          </div>
          )}
        </CellMenuPortal>
      )}
    </div>
  );
}

export function DatabaseButtonCell({ row, prop, operations, context }: {
  row: Page;
  prop: DbProperty;
} & PropertyCellEnvironment) {
  const { t } = useTranslation(["propertyCell", "common", "blockItem"]);
  const router = useRouter();
  const toast = useOptionalProductToast();
  const [running, setRunning] = useState(false);
  const readOnly = useCellReadOnly();
  const [challenge, setChallenge] = useState<ButtonConfirmationChallenge | null>(null);
  const document = prop.config?.button;
  const label = prop.name;
  const configured = document?.version === 1 && Array.isArray(document.actions) && document.actions.length > 0;

  useEffect(() => {
    setRunning(false);
    setChallenge(null);
  }, [context.dataSourceId, context.generation, context.pageId, context.sessionIdentity]);

  async function execute(confirmationToken?: string, cancel = false) {
    if (!configured || running) return;
    const request = capturePropertyCellRequest(context);
    setRunning(true);
    try {
      const result = await operations.runButton(row.id, prop.id, confirmationToken, { signal: request.signal, ...(cancel ? { cancel: true } : {}) }, context);
      // It says how many tasks or pages it made and changed, with the first to
      // open (Notion's, seen 2026-11-02) — the run happened even when the table
      // was read again meanwhile.
      const notice = result && !buttonResultRequiresConfirmation(result) && !result.replayed ? result.notice : undefined;
      const message = notice ? buttonRunNoticeText(notice.createdPageIds.length, notice.editedPageIds.length, notice.tasks) : "";
      const first = notice?.createdPageIds[0] ?? notice?.editedPageIds[0];
      if (message) toast?.notify(message, "default", [
        // Not to the page one is on, as Notion's on the row's own page (seen 2026-11-05).
        ...(first && !window.location.href.includes(first) ? [{ icon: "open" as const, label: t("blockItem:button.open"), onClick: () => router.push(pageHref(first)) }] : []),
        ...(notice?.canUndo && result ? [{ icon: "undo" as const, label: t("blockItem:undo"), onClick: () => operations.undoButton(row.id, prop.id, result.executionId)
          .catch(() => operations.notify(t("propertyCell:buttonUndoFailed"), "error")) }] : []),
      ]);
      if (!request.isCurrent() || !result) return;
      if (!result.replayed) dispatchButtonClientOutcomes(result.clientOutcomes ?? [], { navigate: router.push });
      if (buttonResultRequiresConfirmation(result)) {
        setChallenge(result);
        return;
      }
      setChallenge(null);
    } catch (error) {
      // One its server won't run says where to fix it (2026-11-03: a refused run said nothing more).
      if (request.isCurrent()) operations.notify(t(buttonTargetMissing(error) ? "propertyCell:buttonExecutionTargetMissing"
        : buttonActionsRefused(error) ? "propertyCell:buttonExecutionRefused" : "propertyCell:buttonExecutionFailed"), "error");
    } finally {
      if (request.isCurrent()) setRunning(false);
      request.abort();
    }
  }

  return (
    <>
      {/* Notion's (seen 2026-11-02): its name in a ringed 24px button, the
          same before it has anything to do, when pressing it does nothing. */}
      <button
        type="button"
        className={styles.databaseButtonCell}
        aria-busy={running || undefined}
        disabled={running}
        {...readOnlyStop(readOnly)}
        onClick={() => { if (!readOnly) void execute(); }}
      >
        <span>{label}</span>
      </button>
      {challenge && (
        <ButtonConfirmationDialog
          challenge={challenge}
          busy={running}
          onCancel={() => void execute(challenge.confirmationToken, true)}
          onConfirm={() => void execute(challenge.confirmationToken)}
        />
      )}
    </>
  );
}

export function FormulaCell({ row, prop, snapshot, operations, context }: {
  row: Page;
  prop: DbProperty;
} & PropertyCellEnvironment) {
  const { t } = useTranslation(["common", "propValue"]);
  const structureLocked = useDatabaseStructureLocked();
  const [editorAnchor, setEditorAnchor] = useState<PopoverRect | null>(null);
  const props = snapshot.propertiesByDataSourceId[prop.databaseId] ?? [];
  const display = formulaDisplay(row, prop, snapshot.formulaBounds, untitledPageDisplayTitle("page"), () => evaluateFormula({
    row,
    prop,
    props,
    pagesById: snapshot.pagesById,
    propsByDb: snapshot.propertiesByDataSourceId,
    bounds: snapshot.formulaBounds,
  }));
  // Pressing the cell opens the formula editor, as in Notion, where the
  // database's properties may change: not when its structure is locked, nor
  // on a database page opened alone. A link in the cell still opens.
  const editable = !useCellReadOnly() && !structureLocked && !operations.addOptionsOnly;
  // It hangs from the foot of the value's cell, as the date's window does.
  const open = (element: HTMLElement) => setEditorAnchor(valueCellFoot(propertyValueCell(element) ?? element));
  const trigger = editable ? {
    role: "button",
    tabIndex: 0,
    "data-formula-cell-editor": "",
    onClick: (event: MouseEvent<HTMLElement>) => {
      if (!(event.target as HTMLElement).closest("a")) open(event.currentTarget);
    },
    onKeyDown: (event: KeyboardEvent<HTMLElement>) => {
      if (!fieldEnter(event)) return;
      event.preventDefault(); open(event.currentTarget);
    },
  } : {};
  const editor = editorAnchor ? (
    <FormulaEditorDialog
      prop={prop}
      props={props}
      // It previews with the page it was opened on, a database template's too.
      pagesById={snapshot.pagesById[row.id] ? snapshot.pagesById : { ...snapshot.pagesById, [row.id]: row }}
      propsByDb={snapshot.propertiesByDataSourceId}
      bounds={snapshot.formulaBounds}
      rowId={row.id}
      anchor={editorAnchor}
      onSave={(formula) => void operations.updateProperty(prop.id, { config: { ...prop.config, formula } }, context)}
      onClose={() => setEditorAnchor(null)}
    />
  ) : null;
  // A true or false result is a checkbox a formula alone sets.
  if (display.checked !== undefined) {
    return (
      <span className={styles.cellCheck} {...trigger}>
        <input
          type="checkbox"
          checked={display.checked}
          disabled
          aria-label={t(display.checked ? "propValue:checkbox.checked" : "propValue:checkbox.unchecked")}
        />
        {editor}
      </span>
    );
  }
  // A list of true or false alone, a box each, in a row 8px apart, as Notion's (seen 2026-11-12).
  if (display.checks) {
    return (
      <span className={styles.cellCheck} data-formula-checks {...trigger}>
        {display.checks.map((checked, index) => <input key={index} type="checkbox" checked={checked} disabled readOnly
          aria-label={t(checked ? "propValue:checkbox.checked" : "propValue:checkbox.unchecked")} />)}
        {editor}
      </span>
    );
  }
  // People alone show as a person property's, pages alone as a relation's,
  // as Notion's (seen 2026-11-12); until each page is loaded, words.
  const pages = display.pages?.flatMap((id) => snapshot.pagesById[id] ? [snapshot.pagesById[id]] : []);
  if (display.people || (pages && pages.length === display.pages?.length)) {
    return (
      <span className={styles.rollupValue} {...trigger}>
        {display.people ? <span className={styles.peopleReadonly}>
          {display.people.map((id, index) => <PersonBadge key={`${id}:${index}`} id={id} currentUserId={snapshot.userId} />)}
        </span> : <RollupPageLinks pages={pages ?? []} />}
        {editor}
      </span>
    );
  }
  return (
    <span className={styles.rollupValue} data-number={display.number !== undefined || display.numeric ? "" : undefined}
      data-computed-empty={display.number === undefined && !display.spans?.length && !display.text ? "" : undefined} {...trigger}>
      {display.number !== undefined ? <NumberValueDisplay value={display.number} property={{ config: (prop.config ?? {}) as Record<string, unknown> }} />
        : display.spans ? <RichTextCellText spans={display.spans} interactive /> : display.text || " "}
      {editor}
    </span>
  );
}

export function RollupCell({ row, prop, onOpenPage, snapshot, operations, context }: {
  row: Page;
  prop: DbProperty;
  /** Opens a rolled-up page as the view opens its rows (RollupPageLinks). */
  onOpenPage?: (pageId: string) => void;
} & PropertyCellEnvironment) {
  const { t } = useTranslation(["propertyCell", "common", "propValue"]);
  const sourceProps = snapshot.propertiesByDataSourceId[prop.databaseId] ?? [];
  const relationProp = sourceProps.find((item) => item.id === prop.config?.rollupRelationPropertyId);
  const missingRelation = Boolean(prop.config?.rollupRelationPropertyId && !relationProp);
  const targetDbId = relationProp?.config?.relationDatabaseId ?? relationProp?.databaseId;
  const targetProps = targetDbId ? snapshot.propertiesByDataSourceId[targetDbId] ?? [] : [];
  // Every database a rollup of rollups reaches, each known once the one
  // before it loads.
  const hopDbIds = rollupHopDatabaseIds(
    prop,
    targetProps,
    snapshot.propertiesByDataSourceId,
    snapshot.formulaBounds.rollup,
  ).join("\u0000");

  useEffect(() => {
    const request = capturePropertyCellRequest(context);
    const reads: (Promise<void> | void)[] = [];
    if (targetDbId) reads.push(operations.loadRelation(targetDbId, { rows: false, values: true, signal: request.signal }, context));
    for (const hopDbId of hopDbIds ? hopDbIds.split("\u0000") : []) {
      reads.push(operations.loadRelation(hopDbId, { rows: false, signal: request.signal }, context));
    }
    return abortWhileRunning(request, reads);
  }, [context, operations, hopDbIds, targetDbId]);

  if (missingRelation) {
    return <span className={styles.rollupValue} data-rollup-broken="relation">{t("propertyCell:unavailable")}</span>;
  }
  if (relationProp && unreadableRelationIds(valueAsIds(row.properties?.[relationProp.id], snapshot.formulaBounds.rollup),
    snapshot.pagesById, snapshot.hydratedRelationTargetIds).length > 0) {
    return <span className={styles.rollupValue} data-rollup-no-access>{t("propertyCell:noAccess")}</span>;
  }
  const targetReady = !prop.config?.rollupTargetPropertyId
    || targetProps.some((property) => property.id === prop.config?.rollupTargetPropertyId);
  // Pages shown as they are are those pages, each with its icon (rolledUpPageIds),
  // named on one line as a relation's are (2026-11-12: names broke over lines in an unwrapped cell),
  // each a link to its page (RollupPageLinks).
  const rolledFunction = rollupShownFunction(prop.config) ?? "show_original";
  const rolledIds = relationProp && (rolledFunction === "show_original" || rolledFunction === "show_unique")
    ? rolledUpPageIds(row, relationProp, targetProps.find((property) => property.id === prop.config?.rollupTargetPropertyId),
      snapshot.pagesById, snapshot.formulaBounds.rollup) : null;
  if (rolledIds) {
    const ids = rolledFunction === "show_unique" ? [...new Set(rolledIds)] : rolledIds;
    const pages = ids.flatMap((id) => snapshot.pagesById[id] ? [snapshot.pagesById[id]] : []);
    // Until every page is loaded the names stand as text.
    if (pages.length && pages.length === ids.length) {
      return <RollupPageLinks pages={pages} onOpenPage={onOpenPage} />;
    }
  }
  // A status's or select's values shown as they are are their chips, as Notion's (seen 2026-11-05).
  const chips = rollupOptionValues(row, prop, relationProp,
    targetProps.find((property) => property.id === prop.config?.rollupTargetPropertyId), snapshot.pagesById, snapshot.formulaBounds.rollup);
  if (chips) {
    return <span className={styles.chips} data-rollup-options>
      {chips.map(({ key, option, status }) => (
        <span key={key} className={styles.chip} style={chipStyle(option.color)} data-status-chip={status ? "" : undefined}><span className={styles.chipText}>{option.name}</span></span>
      ))}
    </span>;
  }
  // Checkboxes shown as they are are their boxes, as Notion's (seen 2026-11-12).
  const checks = targetReady && rollupMayShowCheckboxes(targetProps, prop) ? evaluateRollupCheckboxes({
    row, prop, sourceProps, targetProps, pagesById: snapshot.pagesById, propsByDb: snapshot.propertiesByDataSourceId,
    bounds: snapshot.formulaBounds,
  }) : null;
  if (checks) {
    return <span className={styles.cellCheck} data-rollup-checks>
      {checks.map((checked, index) => <input key={index} type="checkbox" checked={checked} disabled readOnly
        aria-label={t(checked ? "propValue:checkbox.checked" : "propValue:checkbox.unchecked")} />)}
    </span>;
  }
  const text = rollupShownText(prop.config?.rollupFunction, targetReady ? evaluateRollupWithComputedDisplay({
    row,
    prop,
    sourceProps,
    targetProps,
    pagesById: snapshot.pagesById,
    propsByDb: snapshot.propertiesByDataSourceId,
    bounds: snapshot.formulaBounds,
  }) : "");
  const targetProperty = targetProps.find(property => property.id === prop.config?.rollupTargetPropertyId);
  const presentation = targetProperty ? rollupFormatProperty(targetProperty, targetProps, snapshot.propertiesByDataSourceId) : undefined;
  const value = rollupNumberText(prop.config?.rollupFunction, text, presentation, snapshot.formulaBounds.rollup);
  const shown = rollupNumberShown(prop, text, presentation);
  return (
    <span className={styles.rollupValue}
      data-number={shown || (value && rollupGivesNumber(prop.config?.rollupFunction, targetProperty, prop.config)) ? "" : undefined}
      data-computed-empty={targetReady && !shown && !value ? "" : undefined}>
      {!targetReady ? <span className={styles.cellEmpty}>…</span>
        : shown ? <NumberValueDisplay value={shown.value} property={{ config: shown.config }} /> : value || " "}
    </span>
  );
}
