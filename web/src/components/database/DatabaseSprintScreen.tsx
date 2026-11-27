import { useContext, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DatabaseSprintSettings } from "@/databaseToolbarConfigWire.generated";
import { SwitchInput } from "@/components/Switch";
import { BoardIcon, CalendarIcon, ClockIcon, DatabaseCylinderIcon, RepeatIcon, SprintIcon, Trash } from "@/icons/leafen";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { pageHref } from "@/lib/navigation";
import { usePathname, useRouter } from "@/lib/router";
import { DatabaseSprintContext, type DatabaseSprintOutcome } from "./databaseSprintContext";
import { DatabaseTaskSettingsHeader } from "./DatabaseTaskSettingsHeader";
import { NotionSelect } from "./NotionSelect";
import { PanelFoot, usePanelFootShown } from "./panelHead";
import styles from "./DatabaseTaskSettings.module.css";
import sprintStyles from "./DatabaseSprintScreen.module.css";
import { activeDateLocale } from "@/lib/i18n";

type IncompleteTasks = DatabaseSprintSettings["incompleteTasks"];

// Notion's sprints start at two weeks from Monday, unfinished tasks going to
// the next sprint, each completed by hand (seen 2026-11-02).
const defaultSettings: Omit<DatabaseSprintSettings, "timeZone"> = {
  durationWeeks: 2, startWeekday: 1, incompleteTasks: "next", automatic: false,
};

/**
 * Sprints, as Notion's (seen 2026-11-02): off, "스프린트 설정" says what turning
 * them on adds — a sprint board page and a sprint database — over "스프린트
 * 켜기", which turns them on as they start and opens the board; on, its rows
 * change the cadence at once ("기간", "시작:", "미완료 작업", "자동화 스프린트"),
 * then "이 보기에서 스프린트 관리" and "스프린트 끄기".
 */
export function DatabaseSprintScreen({
  backLabel,
  taskDatabase,
  writable,
  onBack,
  onOpenTaskDatabase,
}: {
  backLabel: string;
  taskDatabase: boolean;
  writable: boolean;
  onBack(): void;
  onOpenTaskDatabase(): void;
}) {
  const { t, i18n } = useTranslation("databaseTasks");
  const router = useRouter();
  const pathname = usePathname();
  const surface = useContext(DatabaseSprintContext);
  const state = surface?.state ?? null;
  const enabled = state?.enabled === true;
  const saved = enabled && state ? state.settings : defaultSettings;
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<Exclude<DatabaseSprintOutcome, "done" | "unanswered"> | null>(null);
  // Turned on here, the board opens once it is there.
  const [opening, setOpening] = useState(false);
  const footed = usePanelFootShown();
  useEffect(() => {
    if (!opening || !enabled || !state?.boardPageId) return;
    setOpening(false);
    router.push(pageHref(state.boardPageId));
  }, [enabled, opening, router, state?.boardPageId]);
  const weekdays = useMemo(() => {
    const format = new Intl.DateTimeFormat(activeDateLocale(), { weekday: "long", timeZone: "UTC" });
    return Array.from({ length: 7 }, (_, day) => format.format(new Date(Date.UTC(2023, 0, 1 + day))));
  }, [i18n.language]);

  // When the next sprint starts: the first start day after the current
  // sprint's end, as Notion's note says it (seen 2026-11-03).
  const nextStart = useMemo(() => {
    const end = state?.sprints.find(({ status }) => status === "current")?.end;
    if (!enabled || !end) return null;
    const day = new Date(`${end}T00:00:00Z`);
    do day.setUTCDate(day.getUTCDate() + 1); while (day.getUTCDay() !== saved.startWeekday);
    const now = new Date();
    const days = Math.round((day.getTime() - Date.UTC(now.getFullYear(), now.getMonth(), now.getDate())) / 86_400_000);
    const locale = activeDateLocale();
    const date = `${new Intl.DateTimeFormat(locale, { month: "long", day: "numeric", timeZone: "UTC" }).format(day)} (${
      new Intl.DateTimeFormat(locale, { weekday: "short", timeZone: "UTC" }).format(day)})`;
    return days > 0 ? { days, date } : null;
  }, [enabled, saved.startWeekday, state?.sprints, i18n.language]);

  const activeViewId = surface?.activeViewId ?? "";
  const canManageInView = enabled && writable && activeViewId !== "" && state?.currentViewId !== activeViewId;

  async function manageInView() {
    if (!surface || !canManageInView || busy) return;
    setBusy(true);
    setRefusal(null);
    const outcome = await surface.run({ action: "manageView", viewId: activeViewId });
    setBusy(false);
    setRefusal(outcome === "done" || outcome === "unanswered" ? null : outcome);
  }

  async function run(action: "enable" | "update" | "disable", patch: Partial<Omit<DatabaseSprintSettings, "timeZone">> = {}) {
    if (!surface || !writable || busy) return;
    setBusy(true);
    setRefusal(null);
    const settings = { ...saved, ...patch, timeZone: surface.timeZone };
    const outcome = await surface.run(action === "disable" ? { action } : { action, settings });
    setBusy(false);
    // Unanswered, it was said (useDatabaseSprintSurface), and what landed
    // shows once read again: sprints turned on open their board then.
    const sent = outcome === "done" || outcome === "unanswered";
    setRefusal(sent ? null : outcome);
    if (sent && action === "enable") setOpening(true);
    return outcome;
  }

  // Turning them off asks in the card in the middle, as Notion's (seen
  // 2026-11-08): "스프린트를 비활성화할까요?", a red-outlined "비활성화하고 삭제"
  // and "취소"; its words say the deletion is for good, as it is.
  // Turned off from the board or the sprints' own database, the task
  // database opens in its place rather than leaving a page that is gone —
  // without the server's answer too, as the pages may be gone (2026-11-17:
  // waiting for the sprints read again, the board was "페이지 찾지 못함" first).
  async function turnOff() {
    setRefusal(null);
    const sprintPages = [state?.boardPageId, state?.sprintsPageId].flatMap((id) => id ? [pageHref(id)] : []);
    const confirmed = await confirmAction({
      message: t("turnOffSprintsQuestion"), detail: t("turnOffSprintsDescription"),
      confirmLabel: t("turnOffSprintsConfirm"), danger: true,
    });
    const outcome = confirmed ? await run("disable") : undefined;
    if ((outcome === "done" || outcome === "unanswered") && surface && sprintPages.includes(pathname)) router.push(pageHref(surface.pageId));
  }

  if (!taskDatabase) {
    return (
      <div className={styles.panel} data-database-task-settings data-task-screen="sprints" aria-busy={busy}>
        <DatabaseTaskSettingsHeader title={t("sprints")} backLabel={backLabel} onBack={onBack} />
        <div className={styles.body}>
          <p>{t("sprintsNeedTaskDatabase")}</p>
          <button type="button" className={styles.primaryAction} disabled={!writable} onClick={onOpenTaskDatabase}>
            {t("convertToTaskDatabase")}
          </button>
        </div>
      </div>
    );
  }

  if (!enabled) {
    return (
      <div className={styles.panel} data-database-task-settings data-task-screen="sprints-setup" aria-busy={busy}>
        <DatabaseTaskSettingsHeader title={t("sprintSetupTitle")} backLabel={backLabel} onBack={onBack} />
        <div className={`${styles.body} ${footed ? sprintStyles.footedBody : styles.tightFoot}`}>
          <p className={sprintStyles.sprintLead}>{t("sprintSetupDescription")}</p>
          <SprintPicture />
          <p className={sprintStyles.sprintAdds}>{t("sprintSetupAdds")}</p>
          <p className={sprintStyles.sprintAdded}><SprintIcon size="regular" aria-hidden="true" />
            <span><strong>{t("sprintBoardPage")}</strong>: {t("sprintBoardPageDescription")}</span></p>
          <p className={sprintStyles.sprintAdded}><DatabaseCylinderIcon size="regular" aria-hidden="true" />
            <span><strong>{t("sprintDatabase")}</strong>: {t("sprintDatabaseDescription")}</span></p>
          {refusal && <p role="alert">{t("sprintActionFailed")}</p>}
          <PanelFoot>
            <button type="button" className={`${styles.primaryAction} ${footed ? "" : sprintStyles.sprintOn}`} disabled={!writable || busy || !surface}
              data-sprints-on onClick={() => void run("enable")}>{t("turnOnSprints")}</button>
          </PanelFoot>
        </div>
      </div>
    );
  }

  const incompleteLabel = (value: IncompleteTasks) =>
    t(value === "next" ? "incompleteNextShort" : value === "backlog" ? "incompleteBacklogShort" : "incompleteKeepShort");
  return (
    <div className={styles.panel} data-database-task-settings data-task-screen="sprints" aria-busy={busy}>
      <DatabaseTaskSettingsHeader title={t("sprints")} backLabel={backLabel} onBack={onBack} />
      <div className={styles.body}>
        {nextStart ? <p className={sprintStyles.sprintNext} data-next-sprint-start>
          {t("nextSprintStarts", { count: nextStart.days, date: nextStart.date })}
        </p> : null}
        <div className={styles.settingRows}>
          <NotionSelect ariaLabel={t("sprintLength")} settingLabel={t("sprintLength")} settingIcon={<ClockIcon size="small" />}
            value={String(saved.durationWeeks)} disabled={!writable || busy}
            options={Array.from({ length: 8 }, (_, index) => ({ value: String(index + 1), label: t("weeks", { count: index + 1 }) }))}
            onChange={(value) => void run("update", { durationWeeks: Number(value) })} />
          <NotionSelect ariaLabel={t("sprintStart")} settingLabel={t("sprintStart")} settingIcon={<CalendarIcon size="small" />}
            value={String(saved.startWeekday)} disabled={!writable || busy}
            options={weekdays.map((name, day) => ({ value: String(day), label: name }))}
            onChange={(value) => void run("update", { startWeekday: Number(value) })} />
          <NotionSelect ariaLabel={t("incompleteTasks")} settingLabel={t("incompleteTasks")} settingIcon={<CalendarIcon size="small" />}
            value={saved.incompleteTasks} disabled={!writable || busy}
            options={(["next", "backlog", "keep"] as const).map((value) => ({ value, label: incompleteLabel(value) }))}
            onChange={(value) => void run("update", { incompleteTasks: value as IncompleteTasks })} />
        </div>
        {/* Between lines, in the rows' own columns, as Notion's (seen
            2026-11-08): its mark beside its 14px name, its 12px grey note
            under the name, the switch at the end. */}
        <hr className={sprintStyles.sprintLine} />
        <label className={`${styles.switchRow} ${sprintStyles.sprintSwitch}`}>
          <span><strong><span className={styles.switchRowIcon}><RepeatIcon size="small" aria-hidden="true" /></span>{t("automaticSprints")}</strong>
            <small>{t("automaticSprintsDescription")}</small></span>
          <SwitchInput aria-label={t("automaticSprints")} disabled={!writable || busy}
            checked={saved.automatic} onChange={(event) => void run("update", { automatic: event.currentTarget.checked })} />
        </label>
        {refusal && <p role="alert">{t(refusal === "legal-hold" ? "turnOffSprintsLegalHold" : "sprintActionFailed")}</p>}
        <hr className={sprintStyles.sprintLine} />
        {canManageInView && (
          <button type="button" className={`${styles.turnOffRow} ${sprintStyles.sprintRow}`} disabled={busy}
            title={t("manageSprintsInViewDescription")} data-sprint-manage-view
            onClick={() => void manageInView()}>
            <BoardIcon size="small" aria-hidden="true" /><span>{t("manageSprintsInView")}</span>
          </button>
        )}
        <button type="button" className={`${styles.turnOffRow} ${sprintStyles.sprintRow}`} disabled={!writable || busy}
          onClick={() => void turnOff()}>
          <Trash size="small" aria-hidden="true" /><span>{t("turnOffSprints")}</span>
        </button>
      </div>
    </div>
  );
}

/** Leafen's own picture of a sprint board for the setup screen (DIV asset). */
function SprintPicture() {
  return (
    <div className={sprintStyles.sprintPicture} aria-hidden="true">
      <span className={sprintStyles.sprintPictureBar} />
      <div className={sprintStyles.sprintPictureColumns}>
        <span /><span />
      </div>
    </div>
  );
}
