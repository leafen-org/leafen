import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { ContentGraph } from "@/contentGraph";
import {
  ArrowDown, ArrowUp, ComposeIcon, DotsHorizontal, HashIcon, SlidersIcon, SmileIcon, TurnIntoIcon, X,
} from "@/icons/leafen";
import type { Session } from "@/types";
import type { SidebarLayoutView } from "@/workspaceMembershipWire.generated";
import { popoverPlacementStyle } from "@/lib/popoverPlacement";
import { sidebarTemplateChoiceRow, sidebarViewChoiceRow, useSidebarDatabaseChoices } from "./SidebarDatabaseShortcut";
import { SidebarActionMenu, useSidebarBesidePlacement, type SidebarBesidePlace, type SidebarMenuRow } from "./SidebarActionMenu";
import { SIDEBAR_SECTION_LIMITS, sidebarItemName, sidebarViewOpenMode } from "./sidebarLayoutModel";
import { SIDEBAR_OPEN_MODES, sidebarOpenModeIcon, sidebarOpenModeLabel } from "./SidebarShortcutMenu";
import { SidebarViewPicker, sidebarViewPickerAnchor } from "./SidebarViewPicker";
import { useSidebarCustomizing } from "./sidebarCustomizing";
import { styles } from "./sidebarStyles";
import { focusOpenerAfterPaint } from "@/lib/overlayLayer";
import { graphDataSourceName } from "@/lib/dataSourceGraphName";
import { sidebarViewHeading } from "./SidebarDatabaseViewSection";
import { fieldEnter, fieldEscape } from "@/lib/fieldKeyBoundary";

const MENU_WIDTH = 220;

/**
 * A database view section's "…" menu, as Notion's (seen 2026-10-02, 220px,
 * each row led by its icon): "이름 변경" (a field beside it, the name shown
 * as its placeholder), "다음에서 열기" (사이드 보기 at first, 중앙에서 보기,
 * 전체 페이지 보기), "표시", "페이지 아이콘 표시", "출처" (the data source
 * window), "보기" (the source's views with "보기 검색…"), "템플릿" (the
 * template a new row starts from, "비어 있음" when none), then "섹션 제거"
 * and "사이드바 사용자 지정" — Notion's "탭으로 이동" goes with its tabs (the
 * one-sidebar decision of 2026-10-29).
 */
export function SidebarViewSectionMenu({
  name,
  view,
  limit,
  graph,
  session,
  workspaceId,
  onChange,
  onLimit,
  canMoveUp,
  canMoveDown,
  onMove,
  onRemove,
  onCustomize,
}: {
  /**
   * The section's name, for the button's label and the name's placeholder:
   * its own, else what it goes by while its database isn't read
   * (sidebarViewHeading names it once the menu has it).
   */
  name: string;
  view: SidebarLayoutView;
  limit: number;
  /** The holder page's content the section read; the menu reads it itself without one. */
  graph: ContentGraph | null;
  session: Session;
  workspaceId: string;
  onChange: (change: Partial<Omit<SidebarLayoutView, "key">>) => void;
  onLimit: (limit: number) => void;
  /** Moves with nowhere to go aren't offered, as a page section's menu. */
  canMoveUp: boolean;
  canMoveDown: boolean;
  onMove: (step: -1 | 1) => void;
  onRemove: () => void;
  onCustomize?: () => void;
}) {
  const { t } = useTranslation("sidebar");
  const button = useRef<HTMLButtonElement>(null);
  const [opened, setOpened] = useState(false);
  const customizing = useSidebarCustomizing();
  const [viewQuery, setViewQuery] = useState("");
  const choices = useSidebarDatabaseChoices(session, view.pageId, view.dataSourceId, opened, graph);
  const { source } = choices;
  // Untitled, it goes by its heading, read here once the menu has its
  // database — the customize list's menu reads it as it opens.
  const shownName = view.name.trim() ? name : sidebarViewHeading(view, choices.content, name);
  const mode = sidebarViewOpenMode(view);

  function close(restoreFocus = true) {
    setOpened(false);
    if (restoreFocus) focusOpenerAfterPaint(button.current, { preventScroll: true });
  }

  const action = (key: string) => ({ "data-sidebar-section-action": key });
  const rows: SidebarMenuRow[] = [
    { key: "rename", label: t("viewRename"), icon: <ComposeIcon size="regular" aria-hidden="true" />, data: action("rename"), submenu: {
      label: t("viewRename"), custom: (place) => <SidebarViewRename place={place} label={t("viewRename")} placeholder={shownName}
        value={view.name} onRename={(value) => { onChange({ name: sidebarItemName(value) }); close(); }} /> } },
    { key: "open-in", label: t("openIn"), icon: sidebarOpenModeIcon(mode), value: sidebarOpenModeLabel(mode, t), data: action("open-in"),
      submenu: { label: t("openIn"), radio: true, data: { "data-sidebar-section-choices": "open" }, choices: SIDEBAR_OPEN_MODES.map((choice) => ({
        key: choice, label: sidebarOpenModeLabel(choice, t), icon: sidebarOpenModeIcon(choice), checked: mode === choice,
        data: { "data-sidebar-view-open": choice }, run: () => { onChange({ openIn: choice }); close(); } })) } },
    { key: "limit", label: t("sectionShow"), icon: <HashIcon size="regular" aria-hidden="true" />, value: String(limit), data: action("limit"),
      submenu: { label: t("sectionShow"), radio: true, data: { "data-sidebar-section-choices": "limit" }, choices: SIDEBAR_SECTION_LIMITS.map((choice) => ({
        key: String(choice), label: String(choice), checked: limit === choice, data: { "data-sidebar-section-choice": String(choice) },
        run: () => { onLimit(choice); close(); } })) } },
    { key: "show-icons", label: t("showPageIcons"), icon: <SmileIcon size="regular" aria-hidden="true" />, checked: view.showIcons, asSwitch: true,
      data: action("show-icons"), run: () => onChange({ showIcons: !view.showIcons }) },
    { key: "source", label: t("viewSource"), icon: <TurnIntoIcon size="regular" aria-hidden="true" />, value: source && choices.content ? graphDataSourceName(choices.content, source) : undefined,
      data: action("source"), submenu: { label: t("viewSource"), custom: ({ main, trigger, back, byKeys }) => (
        <SidebarViewPicker session={session} workspaceId={workspaceId} checkedSourceId={view.dataSourceId} searchFirst={byKeys}
          anchor={sidebarViewPickerAnchor({ main, trigger })}
          onClose={back}
          onChoose={(chosen) => {
            close();
            onChange({ pageId: chosen.pageId, dataSourceId: chosen.dataSourceId, viewId: chosen.viewId, templateId: "" });
          }} />
      ) } },
    sidebarViewChoiceRow(t, { key: "views", label: t("viewChoice"), data: action("views"), choices, viewId: view.viewId,
      query: viewQuery, onQuery: setViewQuery, onChoose: (viewId) => { onChange({ viewId }); close(); } }),
    sidebarTemplateChoiceRow(t, { data: action("templates"), choices, templateId: view.templateId ?? "",
      onChoose: (templateId) => { onChange({ templateId }); close(); } }),
    ...(canMoveUp ? [{ key: "move-up", label: t("sectionMoveUp"), icon: <ArrowUp size="regular" aria-hidden="true" />, separatorBefore: true, data: action("move-up"), run: () => { onMove(-1); close(); } }] : []),
    ...(canMoveDown ? [{ key: "move-down", label: t("sectionMoveDown"), icon: <ArrowDown size="regular" aria-hidden="true" />, separatorBefore: !canMoveUp, data: action("move-down"), run: () => { onMove(1); close(); } }] : []),
    ...(customizing ? [] : [{ key: "remove", label: t("sectionRemove"), icon: <X size="regular" aria-hidden="true" />, separatorBefore: !canMoveUp && !canMoveDown, data: action("remove"), run: () => { close(); onRemove(); } }]),
    ...(onCustomize ? [{ key: "customize", label: t("customizeSidebar"), icon: <SlidersIcon size="regular" aria-hidden="true" />, separatorBefore: true,
      data: action("customize"), run: () => { close(); onCustomize(); } }] : []),
  ];

  return (
    <>
      <button ref={button} type="button" className={styles.sectionAction} data-sidebar-section-menu="view"
        aria-label={t("sectionMenu", { name: shownName })} aria-haspopup="menu" aria-expanded={opened}
        onClick={() => {
          if (opened) { close(); return; }
          setViewQuery("");
          setOpened(true);
        }}>
        {customizing ? <SlidersIcon size="regular" aria-hidden="true" /> : <DotsHorizontal size="regular" aria-hidden="true" />}
      </button>
      {opened ? createPortal(<SidebarActionMenu button={button} label={t("sectionMenu", { name: shownName })} width={MENU_WIDTH} rows={rows}
        backdropLabel={t("closeSectionMenu")} close={close} />, document.body) : null}
    </>
  );
}

/**
 * "이름 변경": a field beside the row, the name shown as its placeholder.
 * Enter or Escape keeps the name typed, as Notion's rename fields do (its
 * tab's "이름 변경" and its page rename box, seen 2026-11-16: a letter typed
 * over the name stayed on Escape); Escape is heard before the menu's own
 * (useMenuEscape), which would close the field without it.
 */
function SidebarViewRename({ place, label, placeholder, value, onRename }: {
  place: SidebarBesidePlace; label: string; placeholder: string; value: string; onRename(value: string): void;
}) {
  const box = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLInputElement>(null);
  const rename = useRef(onRename);
  rename.current = onRename;
  const position = useSidebarBesidePlacement(box, place);
  useEffect(() => {
    const keep = (event: KeyboardEvent) => {
      if (!fieldEscape(event) || !field.current || document.activeElement !== field.current) return;
      event.preventDefault();
      event.stopPropagation();
      rename.current(field.current.value);
    };
    window.addEventListener("keydown", keep, true);
    return () => window.removeEventListener("keydown", keep, true);
  }, []);
  return (
    <div ref={box} className={`${styles.sectionMenu} ${styles.viewRename}`} role="dialog" aria-label={label}
      style={{ ...popoverPlacementStyle(position), width: MENU_WIDTH }}>
      <input ref={field} autoFocus placeholder={placeholder} defaultValue={value} aria-label={label} onFocus={(event) => event.currentTarget.select()}
        onKeyDown={(event) => {
          if (!fieldEnter(event)) return;
          event.preventDefault();
          onRename(event.currentTarget.value);
        }} />
    </div>
  );
}
