import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { BuiltInIconGlyph } from "@/components/page/PageIcon";
import { parseNativeIconValue } from "@/components/page/nativeIcons";
import { useProductPages } from "@/components/product/productPagesContext";
import { newSidebarTabId } from "@/components/sidebar/sidebarLayoutModel";
import { ArrowDown, ArrowUp, BadgeChevronDown, CheckIcon, Copy, DotsHorizontal, Home, LayoutIcon, Pencil, Trash, UndoIcon, WarningTriangleIcon } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import {
  mergeSidebarPresets, readSidebarPresets, saveSidebarPresets, SIDEBAR_PRESETS_LIMIT, sidebarPresetPages, sidebarPresetsKey, type SidebarPreset,
} from "@/lib/sidebarPresets";
import { LeafenApiError } from "@/apiTransport";
import { useShownChanges } from "@/lib/shownChanges";
import type { Session } from "@/types";
import { workspaceMembershipErrorCodes, type SidebarLayoutSection } from "@/workspaceMembershipWire.generated";
import { SidebarPresetEditor } from "./SidebarPresetEditor";
import styles from "./SidebarPresetsSection.module.css";
import { ReadFailed } from "@/components/ReadFailed";
import { useSettingsUnsaved, type SettingsUnsavedWords } from "./settingsUnsaved";
import { useSidebarPresetReach } from "./useSidebarPresetReach";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

const HOME = "home";
// Leaving with changes not saved asks as Notion's settings ask for its sidebar (seen 2026-11-18).
const LEAVE_WORDS: SettingsUnsavedWords = {
  message: "workspaceSettingsDialog:sidebarPresetsLeave", detail: "workspaceSettingsDialog:sidebarPresetsLeaveDetail",
  discard: "workspaceSettingsDialog:sidebarPresetsLeaveDiscard", keep: "workspaceSettingsDialog:sidebarPresetsLeaveKeep",
};
// Leafen's own list: every section but the recent pages and the developer links, as a new sidebar starts.
const HOME_SECTIONS: SidebarLayoutSection[] = (["favorites", "teamspaces", "private", "shared"] as const)
  .map((id) => ({ id, sort: id === "favorites" || id === "teamspaces" ? "manual" : "edited", limit: 5, hidden: false }));

/** A small menu under its button, closed by Escape or a press outside it. */
function Popover({ onClose, children, align = "start" }: { onClose(): void; children: ReactNode; align?: "start" | "end" }) {
  const ref = useRef<HTMLDivElement>(null);
  usePopoverAppear(true, ref);
  useOverlayLayer(true, { kind: "picker", restoreFocus: false, ref, onEscape: onClose, onOutsidePress: onClose });
  return <div ref={ref} className={styles.popover} data-align={align} role="menu">{children}</div>;
}

/** A preset's icon: the one it was given, or a preset's own. */
function SidebarPresetIcon({ preset }: { preset: SidebarPreset | null }) {
  if (!preset) return <Home size="regular" aria-hidden="true" />;
  const own = parseNativeIconValue(preset.icon);
  return own ? <BuiltInIconGlyph icon={own} size={20} color={own.color === "lightgray" ? "currentColor" : undefined} />
    : <LayoutIcon size="regular" aria-hidden="true" />;
}

/**
 * The settings window's "사이드바", as Notion's "Sidebar" (seen 2026-10-11,
 * 2026-11-06 and 2026-11-09), whose presets are tabs; Leafen's sidebar is
 * one list without tabs (the one-sidebar decision of 2026-10-29), so a preset
 * is what it adds to the list (sidebar_presets.go). "탭 사전 설정" with "새
 * 탭": a new row "새 탭" with "워크스페이스에서 생성함" and "선택 항목",
 * chosen with its name's field ready; "기본" and "선택", each preset with
 * who made it, its ⚠ when an item names a page not every member opens,
 * "기본"/"선택 항목" and its "⋯" (변경 사항 초기화 · 이름 변경 · 복제 ·
 * 위로/아래로 옮기기 · 사전 설정 삭제); beside them the chosen preset's
 * editor (SidebarPresetEditor). Leafen's own list can't change and stays
 * first. Notion's 채팅 and 회의 presets are its AI and meetings. Every change
 * waits for "변경 사항 저장하기", greyed while a preset has no name: a
 * changed preset carries a dot, and a deleted one stays greyed under
 * "삭제됨", with ↩ to bring it back, until they are saved. Saving shows
 * them saved at once and goes to the server
 * behind it; a refused save brings the dots and the button back with why
 * (lib/shownChanges.ts).
 */
export function SidebarPresetsSection({ session, workspaceId }: { session: Session; workspaceId: string }) {
  const { t } = useTranslation(["workspaceSettingsDialog", "sidebar"]);
  const product = useProductPages();
  // As saved, and as being changed.
  const [presets, setPresets] = useState<SidebarPreset[] | null>(null);
  const [draft, setDraft] = useState<SidebarPreset[] | null>(null);
  const saving = useShownChanges<{ presets: SidebarPreset[] }>();
  const [canEdit, setCanEdit] = useState(false);
  const [chosen, setChosen] = useState(HOME);
  const [menu, setMenu] = useState<{ id: string; kind: "role" | "more" } | null>(null);
  const [error, setError] = useState("");
  // A failed read of the presets, said in the list's place with a way to
  // read again, never as no optional presets; a save that failed is `error`.
  const [readFailed, setReadFailed] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  const [names, setNames] = useState<Record<string, string>>({});
  const nameRef = useRef<HTMLInputElement>(null);
  // The presets as the server last answered, at their revision: what a save
  // is made on, and what it is merged from when another saved meanwhile.
  const kept = useRef<{ presets: SidebarPreset[]; revision: number }>({ presets: [], revision: 0 });

  useEffect(() => {
    const controller = new AbortController();
    readSidebarPresets(session, workspaceId, controller.signal).then((read) => {
      kept.current = { presets: read.presets, revision: read.revision ?? 0 };
      setPresets(read.presets);
      setDraft(read.presets);
      setCanEdit(read.canEdit);
    }, () => { if (!controller.signal.aborted) setReadFailed(t("sidebarPresetsLoadFailed")); });
    product?.workspacePeople?.(controller.signal).then((people) => {
      setNames(Object.fromEntries(people.map((person) => [person.id, person.name])));
    }, () => undefined);
    return () => controller.abort();
  }, [product, readTurn, session, t, workspaceId]);

  const untitled = t("sidebarPresetUntitled");
  const list = draft ?? [];
  const savedList = saving.shown.presets ?? presets ?? [];
  const savedOf = (id: string) => savedList.find((preset) => preset.id === id);
  const changed = (preset: SidebarPreset) => sidebarPresetsKey(savedOf(preset.id)) !== sidebarPresetsKey(preset);
  const deleted = savedList.filter((preset) => !list.some((candidate) => candidate.id === preset.id));
  const dirty = sidebarPresetsKey(list) !== sidebarPresetsKey(savedList);
  // "변경 사항 저장하기" stays greyed while a preset has no name, empty or spaces alone, as Notion's (seen 2026-11-18).
  const unnamed = list.some((preset) => !preset.name.trim());
  useSettingsUnsaved(dirty, undefined, LEAVE_WORDS);
  // The pages the presets name that not every member opens, and the presets with any.
  const named = [...list, ...deleted].flatMap((preset) => sidebarPresetPages(preset).map((item) => ({ ...item, presetId: preset.id })));
  const notEveryone = useSidebarPresetReach(session, named.map((item) => item.pageId));
  const warnedItems = new Set(named.filter((item) => notEveryone.has(item.pageId)).map((item) => `${item.presetId}:${item.key}`));
  const warns = (id: string) => named.some((item) => item.presetId === id && notEveryone.has(item.pageId));

  const save = (next: SidebarPreset[]) => setDraft(next);
  const commit = () => {
    if (!draft || unnamed) return;
    setError("");
    const sent = draft;
    void saving.change({ presets: sent }, async () => {
      // Saved by another owner, window or agent since it was read (409), the
      // change goes again on what they are now, both kept (2026-11-13: the
      // last save replaced them all).
      const base = kept.current;
      const saved = await saveSidebarPresets(session, workspaceId, sent, base.revision).catch(async (failure: unknown) => {
        if (!(failure instanceof LeafenApiError) || failure.code !== workspaceMembershipErrorCodes.sidebarPresetsRevisionConflict) throw failure;
        const now = await readSidebarPresets(session, workspaceId);
        return saveSidebarPresets(session, workspaceId, mergeSidebarPresets(base.presets, sent, now.presets), now.revision ?? 0);
      });
      kept.current = { presets: saved.presets, revision: saved.revision ?? 0 };
      setPresets(saved.presets);
      // Changes made since it went stay to be saved.
      setDraft((current) => (sidebarPresetsKey(current ?? []) === sidebarPresetsKey(sent) ? saved.presets : current));
    }).catch(() => setError(t("sidebarPresetsSaveFailed")));
  };
  // A preset's change undone: as saved, or gone when it is new, as Notion's.
  const reset = (id: string) => {
    const before = savedOf(id);
    if (!before && chosen === id) setChosen(HOME);
    save(before ? list.map((preset) => (preset.id === id ? before : preset)) : list.filter((preset) => preset.id !== id));
  };
  // A deleted preset back where it was saved.
  const restore = (preset: SidebarPreset) => {
    const at = Math.min(savedList.indexOf(preset), list.length);
    save([...list.slice(0, at), preset, ...list.slice(at)]);
  };
  const full = list.length >= SIDEBAR_PRESETS_LIMIT;
  const focusName = () => focusOpenedField(() => nameRef.current, { select: true });
  const add = (preset: Omit<SidebarPreset, "id" | "createdBy">) => {
    if (full) return;
    const id = newSidebarTabId();
    save([...list, { ...preset, id, createdBy: "" }]);
    setChosen(id);
    focusName();
  };
  const update = (next: SidebarPreset) => save(list.map((preset) => (preset.id === next.id ? next : preset)));
  // A preset moves past the next one in its own group, default or optional,
  // as the list shows them apart.
  const neighbour = (id: string, by: -1 | 1) => {
    const at = list.findIndex((preset) => preset.id === id);
    if (at < 0) return -1;
    for (let to = at + by; to >= 0 && to < list.length; to += by) {
      if (Boolean(list[to].optional) === Boolean(list[at].optional)) return to;
    }
    return -1;
  };
  const move = (id: string, by: -1 | 1) => {
    const at = list.findIndex((preset) => preset.id === id), to = neighbour(id, by);
    if (at < 0 || to < 0) return;
    const next = [...list];
    [next[at], next[to]] = [next[to], next[at]];
    save(next);
  };
  const duplicate = (preset: SidebarPreset | null) => add(preset
    ? { name: t("sidebarPresetCopyName", { name: preset.name || untitled }), icon: preset.icon, optional: true, sections: preset.sections, shortcuts: preset.shortcuts }
    : { name: t("sidebarPresetCopyName", { name: t("sidebar:home") }), optional: true, sections: HOME_SECTIONS, shortcuts: [] });
  const madeBy = (preset: SidebarPreset) => (preset.createdBy ? (names[preset.createdBy] ? t("sidebarPresetBy", { name: names[preset.createdBy] }) : "")
    : t("sidebarPresetByWorkspace"));
  const warnMark = (id: string) => (warns(id) ? (
    <span className={styles.warn} title={t("sidebarPresetWarning")} aria-label={t("sidebarPresetWarning")} role="img" data-sidebar-preset-warning="">
      <WarningTriangleIcon size="small" aria-hidden="true" />
    </span>
  ) : null);

  const row = (preset: SidebarPreset | null) => {
    const id = preset?.id ?? HOME, own = preset !== null;
    const roleOpen = menu?.id === id && menu.kind === "role", moreOpen = menu?.id === id && menu.kind === "more";
    const close = () => setMenu(null);
    const item = (key: string, label: string, icon: ReactNode, run: () => void, disabled = false, danger = false) => (
      <button key={key} type="button" role="menuitem" className={styles.item} disabled={disabled} data-danger={danger ? "" : undefined}
        data-sidebar-preset-action={key} onClick={() => { close(); run(); }}>{icon}<span>{label}</span></button>
    );
    return (
      <div key={id} className={styles.row} data-chosen={chosen === id ? "true" : undefined} data-sidebar-preset={id}>
        <button type="button" className={styles.rowMain} onClick={() => setChosen(id)}>
          <span className={styles.rowIcon}><SidebarPresetIcon preset={preset} /></span>
          <span className={styles.rowText}>
            <span>{preset ? preset.name || untitled : t("sidebar:home")}
              {preset && changed(preset) ? <span className={styles.unsaved} aria-hidden="true" data-sidebar-preset-unsaved /> : null}</span>
            <span className={styles.rowMeta}>{preset ? madeBy(preset) : t("sidebarPresetByLeafen")}</span>
          </span>
        </button>
        {warnMark(id)}
        <span className={styles.menuWrap}>
          <button type="button" className={styles.role} aria-haspopup="menu" aria-expanded={roleOpen} disabled={!own || !canEdit}
            title={own ? undefined : t("sidebarPresetHomeAlways")} data-sidebar-preset-role={preset?.optional ? "optional" : "default"}
            onClick={() => setMenu(roleOpen ? null : { id, kind: "role" })}>
            {preset?.optional ? t("sidebarPresetOptional") : t("sidebarPresetDefault")}<BadgeChevronDown aria-hidden="true" />
          </button>
          {roleOpen && preset ? (
            // Opened toward the list, as "⋯" beside it: from the button's left its
            // hints ran past the window's edge (2026-11-06).
            <Popover onClose={close} align="end">
              {([false, true] as const).map((optional) => (
                <button key={String(optional)} type="button" role="menuitemradio" aria-checked={preset.optional === optional} className={styles.choice}
                  onClick={() => { close(); update({ ...preset, optional }); }}>
                  <span className={styles.choiceText}>
                    <span>{optional ? t("sidebarPresetOptional") : t("sidebarPresetDefault")}</span>
                    <span>{optional ? t("sidebarPresetOptionalHint") : t("sidebarPresetDefaultHint")}</span>
                  </span>
                  {preset.optional === optional ? <CheckIcon size="small" aria-hidden="true" /> : null}
                </button>
              ))}
            </Popover>
          ) : null}
        </span>
        <span className={styles.menuWrap}>
          <button type="button" className={styles.more} aria-label={t("sidebarPresetActions", { name: preset ? preset.name || untitled : t("sidebar:home") })}
            aria-haspopup="menu" aria-expanded={moreOpen} disabled={!canEdit} onClick={() => setMenu(moreOpen ? null : { id, kind: "more" })}>
            <DotsHorizontal size="regular" aria-hidden="true" />
          </button>
          {moreOpen ? (
            // As Notion's: a move with nowhere to go isn't offered.
            <Popover onClose={close} align="end">
              {preset && changed(preset) ? item("reset", t("sidebarPresetResetChanges"), <UndoIcon size="small" aria-hidden="true" />, () => reset(preset.id)) : null}
              {item("rename", t("sidebarPresetRename"), <Pencil size="small" aria-hidden="true" />, () => { setChosen(id); focusName(); }, !own)}
              {item("duplicate", t("sidebarPresetDuplicate"), <Copy size="small" aria-hidden="true" />, () => duplicate(preset), full)}
              {own && neighbour(id, -1) >= 0 ? item("move-up", t("sidebarPresetMoveUp"), <ArrowUp size="small" aria-hidden="true" />, () => move(id, -1)) : null}
              {own && neighbour(id, 1) >= 0 ? item("move-down", t("sidebarPresetMoveDown"), <ArrowDown size="small" aria-hidden="true" />, () => move(id, 1)) : null}
              {item("delete", t("sidebarPresetDelete"), <Trash size="small" aria-hidden="true" />, () => {
                if (chosen === id) setChosen(HOME);
                save(list.filter((candidate) => candidate.id !== id));
              }, !own, true)}
            </Popover>
          ) : null}
        </span>
      </div>
    );
  };

  const deletedRow = (preset: SidebarPreset) => (
    <div key={preset.id} className={styles.row} data-deleted="true" data-sidebar-preset={preset.id}>
      <span className={styles.rowMain}>
        <span className={styles.rowIcon}><SidebarPresetIcon preset={preset} /></span>
        <span className={styles.rowText}>
          <span>{preset.name}<span className={styles.unsaved} aria-hidden="true" /></span>
          <span className={styles.rowMeta}>{madeBy(preset)}</span>
        </span>
      </span>
      {warnMark(preset.id)}
      <span className={styles.role} aria-disabled="true" data-sidebar-preset-role={preset.optional ? "optional" : "default"}>
        {preset.optional ? t("sidebarPresetOptional") : t("sidebarPresetDefault")}<BadgeChevronDown aria-hidden="true" />
      </span>
      <button type="button" className={styles.more} aria-label={t("sidebarPresetRestore", { name: preset.name })} title={t("sidebarPresetRestore", { name: preset.name })}
        disabled={!canEdit || full} onClick={() => restore(preset)} data-sidebar-preset-restore>
        <UndoIcon size="small" aria-hidden="true" />
      </button>
    </div>
  );

  const current = list.find((preset) => preset.id === chosen) ?? null;
  const warnedHere = new Set([...warnedItems].flatMap((key) => (current && key.startsWith(`${current.id}:`) ? [key.slice(current.id.length + 1)] : [])));

  return (
    <section className={styles.section} aria-label={t("sidebarPresetsTitle")} data-sidebar-presets>
      <div className={styles.head}>
        <span>{t("sidebarPresetsTitle")}</span>
        {canEdit ? (
          <span className={styles.headActions}>
            {dirty ? (
              <button type="button" className={styles.save} disabled={unnamed} onClick={commit} data-sidebar-preset-save>{t("sidebarPresetSave")}</button>
            ) : null}
            <button type="button" className={styles.newTab} disabled={full || !presets} data-sidebar-preset-new
              onClick={() => add({ name: "", optional: true, sections: [], shortcuts: [] })}>{t("sidebarPresetNew")}</button>
          </span>
        ) : null}
      </div>
      {error ? <div className={styles.error} role="alert">{error}</div> : null}
      <div className={styles.body}>
        <div className={styles.list}>
          <div className={styles.group}>{t("sidebarPresetDefaultGroup")}</div>
          {row(null)}
          {/* Until they are read, nothing is said about them but a read that failed. */}
          {presets === null ? (readFailed ? (
            <div className={styles.empty}>
              <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); setReadTurn((turn) => turn + 1); }} />
            </div>
          ) : null) : <>
            {list.filter((preset) => !preset.optional).map(row)}
            <div className={styles.group}>{t("sidebarPresetOptionalGroup")}</div>
            {list.some((preset) => preset.optional) ? list.filter((preset) => preset.optional).map(row)
              : <div className={styles.empty}>{t("sidebarPresetNoOptional")}</div>}
            {deleted.length ? <div className={styles.group}>{t("sidebarPresetDeletedGroup")}</div> : null}
            {deleted.map(deletedRow)}
          </>}
        </div>
        <SidebarPresetEditor key={chosen} session={session} workspaceId={workspaceId} preset={current} homeSections={HOME_SECTIONS}
          icon={<SidebarPresetIcon preset={current} />} untitled={untitled} changed={current ? changed(current) : false}
          editable={canEdit} canDuplicate={canEdit && !full} warned={warnedHere} nameRef={nameRef}
          onChange={update} onReset={() => current && reset(current.id)} onDuplicate={() => duplicate(current)} />
      </div>
    </section>
  );
}
