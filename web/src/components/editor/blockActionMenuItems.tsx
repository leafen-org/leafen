import type { TFunction } from "i18next";
import type { HTMLAttributes, ReactNode, RefObject } from "react";
import { SwitchMark } from "@/components/Switch";
import type { GraphBlockType } from "@/contentGraph";
import { menuKeyHint } from "@/lib/menuKeyHint";
import { detectKeyboardShortcutPlatform, type KeyboardShortcutId, shortcutCompactLabel, shortcutDisplayKeys } from "@/lib/keyboardShortcuts";
import type { TextDirection } from "@/lib/textDirection";
import { BulletedListIcon, TableHeaderColumnIcon, TableHeaderRowIcon } from "@/icons/leafen";
import { ACTION_ICONS } from "./blockActionMenuIcons";
import { BlockIcon } from "./BlockIcon";
import { BLOCK_DEFS } from "./blockDefinitions";
import { blockDefLabel } from "./blockDefinitionLabels";
import { BLOCK_HANDLE_BACKGROUND_COLORS, BLOCK_HANDLE_COLORS } from "./blockHandleModel";
import { blockMenuActionSearchNames, blockMenuColorSearchNames, blockMenuEnglishColorName, blockMenuFound, blockMenuGroupsBestFirst, blockTurnChoiceSearchNames } from "./blockMenuSearch";
import { backgroundListWords, recentColorListWords, recentColorWord } from "./blockSearchWords";
import { storedEditorColor } from "./colorMemory";
import { BlockColorSwatch, type BlockListFormat, type BlockSubmenu } from "./BlockMenuSubmenu";
import type { BlockMenuShape } from "./blockMenuShape";
import { blockTurnChoiceLabel } from "./blockItemLabels";
import type { BlockTurnChoice } from "./blockTurnInto";
import styles from "./editorStyles.generated.module.css";
import type { EditorBlockMenuAnchor } from "./editorCommands";
import type { BlockMenuFile } from "./useBlockFileMenu";
import type { BlockMenuDatabase } from "./useBlockDatabaseMenu";
import type { BlockMenuSeveral } from "./blockMenuSeveral";
import { addFileMenuRows } from "./blockActionMenuFileRows";
import { addImageMenuRows, type BlockMenuImage, type BlockMenuImageFields } from "./blockActionMenuImageRows";

export type BlockMenuPanel = "main" | "turnInline";

/** What stays with a full-page database's page as its database turns inline. */
export type DatabasePageLeftovers = { shares: number; comments: number };

export type BlockPageInAnchor = { row: DOMRect; menu: DOMRect;
  returnFocusRef: RefObject<HTMLButtonElement | null>; onLeaveList(): void };

/** A row of the block menu. */
export type BlockMenuAction = { id: string; label: string; run(): void; before?: ReactNode; content?: (labelProps: HTMLAttributes<HTMLElement>) => ReactNode;
  hint?: string; submenu?: BlockSubmenu; attributes?: HTMLAttributes<HTMLButtonElement> & Record<string, unknown>;
  /** The keys beside it, which run it while the menu is open. */
  shortcut?: KeyboardShortcutId };

export type BlockActionMenuProps = {
  activeColor: string;
  activeType: GraphBlockType | "mixed";
  // Only blocks that carry a caption offer to show it.
  captionVisible?: boolean;
  // Text blocks' "텍스트 방향", offered in a right-to-left language or when always shown.
  direction?: { active: TextDirection | null; onSelect: (direction: TextDirection) => void };
  image?: BlockMenuImage;
  emptyImage?: BlockMenuImageFields;
  file?: BlockMenuFile;
  // An inline database's own rows, replacing a block's turn, color, comment and suggest.
  database?: BlockMenuDatabase;
  // A list's "목록 형식", offered for a numbered or bulleted list item; it
  // changes the whole list. A quote's "인용 크기" takes the same place.
  listFormat?: BlockListFormat;
  selectionCount: number;
  /** Several chosen blocks' head and the rows they all take, as the first of them leads (blockMenuSeveral). */
  several?: BlockMenuSeveral;
  /** Whether "전환" offers a synced block, which it doesn't for blocks inside one (blockTurnChoices). */
  turnIntoSynced?: boolean;
  onClose: () => void;
  onColor: (color: string) => void;
  onComment?: () => void;
  /** "편집 내용 제안": turns the page's suggesting mode on or off, as Notion's block menu (seen 2026-10-07). */
  onSuggest?: () => void;
  /** "여기에서 발표": presents the page from the slide holding the block, as Notion's (seen 2026-10-12). */
  onPresent?: () => void;
  onCopyLink?: () => void;
  // Several blocks copy all their links instead.
  onCopyLinks?: () => void;
  onDelete: () => void;
  onDuplicate: () => void;
  // Offered for a database, whose copy may leave its rows behind.
  onDuplicateWithoutContent?: () => void;
  /** A phone's "아래에 삽입": a new line under the block, the caret in it (Notion's "작업" sheet, seen 2026-11-11). */
  onInsertBelow?: () => void;
  onMove: () => void;
  // A synced block offers a copy; a copy offers to unsync.
  onSyncedBlock?: () => void;
  syncedBlock?: "copy" | "unsync";
  // An original offers to unsync all its copies.
  onUnsyncAll?: () => void;
  toggleCaption: () => void;
  turnInto: (choice: BlockTurnChoice) => void;
  /** Opens or closes the pages "페이지 위치:" puts the blocks in, beside the turn list. */
  onPageIn?: (place: BlockPageInAnchor | null) => void;
  // An inline database offers to become a full-page database, and a
  // full-page database's page block to become an inline one, first saying
  // what stays with the page when anything does.
  onTurnDatabase?: { into: "page" | "inline"; run: () => void; leftovers?: () => Promise<DatabasePageLeftovers | null> };
  /** A simple table's "데이터베이스로 전환", first in its menu as Notion's (seen 2026-10-11). */
  onTurnTableDatabase?: () => void;
  /** A code block's own rows, after its caption, as Notion's (seen 2026-10-11): copy, wrap and language. */
  code?: { onCopy(): void; wrap: boolean; onToggleWrap(): void; language: BlockListFormat; onFormat?: () => void };
  /** A callout's "아이콘 편집", after its color, as Notion's (seen 2026-10-11). */
  onEditIcon?: () => void;
  /** A bookmark becomes a mention of its link, and copies the link, as Notion's (seen 2026-10-11). */
  bookmark?: { onTurnMention(): void; onCopySource(): void };
  /** A link to a page opens it in a new tab or a side peek, as Notion's (seen 2026-10-11). */
  openPage?: { onNewTab(): void; onSidePeek(): void };
  /** A simple table's header row and column, switched from its menu. */
  table?: { headerRow: boolean; headerColumn: boolean; onToggleHeaderRow(): void; onToggleHeaderColumn(): void; onFitWidth(): void };
  // The color shortcut opens the menu at its colors.
  initialPanel?: "main" | "color" | "turn";
  // Right-clicking a block opens the menu at the pointer, kept on screen.
  anchor?: EditorBlockMenuAnchor;
  // Who last edited the blocks, when, and their words, at the menu's foot.
  footer?: ReactNode;
  returnFocusRef?: RefObject<HTMLElement | null>;
  /** A locked page's short menu, as Notion's (seen 2026-10-31): the block's kind, its link, comment and present only. */
  readOnly?: boolean;
  /** That short menu's search, which only someone who may edit the page has, while suggesting (ReadOnlyBlockHandle). */
  searchable?: boolean;
  /** In that short menu while suggesting, "삭제" suggests the block away, as Notion's (seen 2026-11-18). */
  onSuggestRemoval?: () => void;
};

/** What the open menu lends its rows: its panel and search, the blocks' common rows and its turn into a page. */
export type BlockActionMenuState = {
  t: TFunction<["blockHandle", "common", "editor"]>;
  panel: BlockMenuPanel;
  setPanel(panel: BlockMenuPanel): void;
  query: string;
  many: boolean;
  shape: BlockMenuShape;
  turnChoices: BlockTurnChoice[];
  fileMore: boolean;
  kindHead: ReactNode;
  sheet: boolean;
  touch: boolean;
  leftovers: DatabasePageLeftovers | null;
  turnDatabase(): Promise<void>;
};

export const blockTypeLabel = (type: GraphBlockType) => { const def = BLOCK_DEFS.find((candidate) => candidate.type === type); return def ? blockDefLabel(def) : type; };

// A Mac's keys in Notion's order, ⇧ before ⌥ ("⌘⇧⌥X", seen 2026-10-04).
export const blockMenuKeys = (id: KeyboardShortcutId) => {
  const platform = detectKeyboardShortcutPlatform();
  return platform === "mac" ? shortcutCompactLabel(id, platform) : shortcutDisplayKeys(id, platform)[0]?.join("+");
};

/**
 * The block menu's rows for the blocks in hand, in Notion's order: a locked
 * page's short menu; what its search finds; an image's, a file's, an inline
 * database's or a simple table's own menu; any other block's rows; or the
 * question before a full-page database turns inline.
 */
export function blockActionMenuItems(props: BlockActionMenuProps, menu: BlockActionMenuState): BlockMenuAction[] {
  const { activeType, captionVisible, direction, image, emptyImage, file, database, listFormat, readOnly, onColor, onComment, onSuggest, onPresent,
    onCopyLink, onCopyLinks, onDelete, onDuplicate, onDuplicateWithoutContent, onInsertBelow, onMove, onSyncedBlock, syncedBlock, onUnsyncAll,
    toggleCaption, turnInto, onTurnDatabase, onTurnTableDatabase, code, onEditIcon, bookmark, openPage, table, onSuggestRemoval, several } = props;
  const { t, panel, setPanel, query, many, shape, turnChoices, fileMore, kindHead, sheet, touch, leftovers, turnDatabase } = menu;
  const keysFor = blockMenuKeys;
  // The delete row: "삭제" with its key; a page's or a database's own menu, and several chosen led by a sub-page,
  // "휴지통으로 이동" with none, which its search finds by "trash", not "delete", as Notion's (seen 2026-11-05;
  // several and the search, 2026-11-26).
  const trashing = Boolean(database) || many && several?.deleteToTrash === true;
  const deleteTitle = t(trashing ? "blockHandle:database.moveToTrash" : "blockHandle:menu.delete"), deleteHint = trashing ? undefined : keysFor("deleteBlock");
  const deleteEnglish = trashing ? "move to trash" : "delete";
  const colorName = (color: string) => t(`blockHandle:colors.${color}`, { defaultValue: t("blockHandle:colors.default") });
  const colorTitle = (group: "text" | "background", color: string) => group === "text" ? t("blockHandle:menu.textColorNamed", { color: colorName(color) })
    : color === "default_background" ? t("blockHandle:colorMenu.defaultBackground") : colorName(color);
  const actions: BlockMenuAction[] = [];
  const add = (id: string, label: string, run: () => void, attributes?: BlockMenuAction["attributes"], hint?: string) => {
    const action: BlockMenuAction = { id, label, run, attributes, hint }; actions.push(action); return action;
  };
  const divider = <div className={styles.menuDivider} role="separator" />;
  // ⌘/ search's groups are apart by a line, their names closer set (EditorActionsAndMarks).
  const searchLabel = (value: string) => <div className={`${styles.menuLabel} ${styles.blockMenuSearchLabel}`}>{value}</div>;
  const searchDivider = <div className={`${styles.menuDivider} ${styles.blockMenuSearchDivider}`} role="separator" />;
  const back = (title: string) => add("back", title, () => setPanel("main"), { className: `${styles.menuItem} ${styles.submenuBack}` });
  const child = (id: BlockSubmenu, title: string) => { const action = add(id, title, () => {}); action.submenu = id; return action; };
  const keyed = (action: BlockMenuAction, id: KeyboardShortcutId) => { action.shortcut = id; action.hint = keysFor(id); return action; };
  // "여기에서 발표 [베타] ⌘⌥P" on its own after the comments, as Notion's (seen 2026-10-12).
  const presentHere = () => {
    if (!onPresent || many) return;
    const title = t("blockHandle:menu.presentHere");
    const action = add("present", title, onPresent, { "data-block-present": true }, keysFor("presentPage"));
    action.before = divider;
    action.content = labelProps => <><span aria-hidden="true">{ACTION_ICONS.present}</span>
      <span {...labelProps} className={styles.menuBadged}><span>{title}</span><span className={styles.menuBadge}>{t("blockHandle:menu.beta")}</span></span></>;
  };
  if (readOnly && query.trim()) {
    // Its search finds its own rows, best match first, without heads or lines
    // (Notion's while suggesting, seen 2026-11-18: "제" found 편집 내용 제안, then 삭제).
    const found = blockMenuFound(query, [
      onCopyLink && !many ? { id: "copy-link", title: t("blockHandle:menu.copyLinkToBlock"), en: "Copy link to block", run: onCopyLink, hint: keysFor("copyBlockLink") } : null,
      onSuggestRemoval ? { id: "delete", title: t("blockHandle:menu.delete"), en: "delete", run: onSuggestRemoval, hint: keysFor("deleteBlock") } : null,
      onComment && !many && shape.comment ? { id: "comment", title: t("blockHandle:menu.comment"), en: "comment", run: onComment, hint: keysFor("addComment") } : null,
      onSuggest && shape.suggest ? { id: "suggest", title: t("blockHandle:menu.suggestEdits"), en: "suggest edits", run: onSuggest, hint: keysFor("suggestEdits") } : null,
      onPresent && !many ? { id: "present", title: t("blockHandle:menu.presentHere"), en: "present from here", run: onPresent, hint: keysFor("presentPage") } : null,
    ].filter((action) => action !== null), (action) => blockMenuActionSearchNames(action.title, action.en));
    for (const action of found.items) add(action.id, action.title, action.run, undefined, action.hint);
  } else if (readOnly) {
    if (onCopyLink && !many) add("copy-link", t("blockHandle:menu.copyLinkToBlock"),
      onCopyLink, { "data-copy-block-link": true }, keysFor("copyBlockLink"));
    if (onSuggestRemoval) add("delete", t("blockHandle:menu.delete"), onSuggestRemoval,
      { className: `${styles.menuItem} ${styles.menuItemDanger}`, "data-suggest-removal": true }, keysFor("deleteBlock"));
    // Comment and "편집 내용 제안" are a group of their own, as in the full menu
    // (Notion's while suggesting or for whoever may comment, seen 2026-11-18).
    const talk = actions.length;
    if (onComment && !many && shape.comment) add("comment", t("blockHandle:menu.comment"), onComment, { "data-block-comment": true }, keysFor("addComment"));
    if (onSuggest && shape.suggest) add("suggest", t("blockHandle:menu.suggestEdits"), onSuggest, { "data-block-suggest": true }, keysFor("suggestEdits"));
    if (actions[talk] && talk > 0) actions[talk].before = divider;
    presentHere();
    if (actions[0]) actions[0].before = kindHead;
  } else if (panel === "main" && query.trim()) {
    // Each group lists after those with a better match (blockMenuFound).
    const groups: { best: number; list(): void }[] = [];
    // Only what the menu itself shows is found: no turn or color where it has none (Notion's, seen 2026-11-13).
    const turns = blockMenuFound(query, shape.turn || !many && (image || file || database) ? turnChoices : [], choice => blockTurnChoiceSearchNames(choice, blockTurnChoiceLabel(choice)),
      [t("blockHandle:menu.turnInto"), "Turn into"]);
    groups.push({ best: turns.best, list: () => { for (const [index, choice] of turns.items.entries()) {
      const action = add(`turn:${choice.key}`, blockTurnChoiceLabel(choice), () => turnInto(choice),
        { className: styles.turnItem, "data-turn-type": choice.key });
      if (index === 0) action.before = searchLabel(t("blockHandle:menu.turnInto"));
      action.content = labelProps => <><span aria-hidden="true"><BlockIcon type={choice.placeIn ? "link_to_page" : choice.type} size="regular" /></span><span {...labelProps}>{action.label}</span></>;
    } } });
    // "최근 사용": the color last used, found by its own words under its list's (recentColorWord, recentColorListWords).
    const recent = image || database || file && activeType !== "file" || !shape.color ? null : storedEditorColor();
    const last = blockMenuFound(query, recent ? [recent] : [], color => blockMenuActionSearchNames(recentColorWord(), "Highlight",
      colorTitle(color.endsWith("_background") ? "background" : "text", color)), recentColorListWords(t("blockHandle:colorMenu.recent")));
    groups.push({ best: last.best, list: () => { for (const color of last.items) {
      const title = colorTitle(color.endsWith("_background") ? "background" : "text", color);
      const action = add(`recent:${color}`, title, () => onColor(color), { className: styles.colorItem, "data-block-color-recent": true }, keysFor("blockColor"));
      action.before = searchLabel(t("blockHandle:colorMenu.recent"));
      action.content = labelProps => <><BlockColorSwatch color={color} /><span {...labelProps}>{title}</span></>;
    } } });
    for (const group of image || database || file && activeType !== "file" || !shape.color ? [] : ["text", "background"] as const) {
      // "기본 배경" heads the backgrounds, as in the color submenu (Notion's search finds it, seen 2026-11-13).
      const colors = blockMenuFound(query, group === "text" ? BLOCK_HANDLE_COLORS : ["default_background", ...BLOCK_HANDLE_BACKGROUND_COLORS],
        color => blockMenuColorSearchNames(color, [colorTitle(group, color), colorName(color), t(`editor:colors.${color}`, { defaultValue: "" }), blockMenuEnglishColorName(color)]),
        group === "text" ? [t("blockHandle:colorMenu.textColor"), "Text color"] : backgroundListWords(t("blockHandle:colorMenu.backgroundColor")));
      groups.push({ best: colors.best, list: () => { for (const [index, color] of colors.items.entries()) {
        const title = colorTitle(group, color), chosen = color === "default_background" ? "default" : color;
        const action = add(`${group}:${chosen}`, title, () => onColor(chosen), { className: styles.colorItem, "data-block-color-choice": chosen,
          "data-block-color-group": color === "default_background" ? "background" : undefined });
        if (index === 0) action.before = searchLabel(t(group === "text" ? "blockHandle:colorMenu.textColor" : "blockHandle:colorMenu.backgroundColor"));
        action.content = labelProps => <><BlockColorSwatch color={color} /><span {...labelProps}>{title}</span></>;
      } } });
    }
    const foundActions = [
      // A database's turn into a page (or inline) is found as its menu shows it first (2026-11-07: it was left out).
      onTurnDatabase && !many && !database?.structureLocked ? { id: "turn-database",
        title: t(onTurnDatabase.into === "page" ? "blockHandle:menu.turnIntoPage" : "blockHandle:menu.turnIntoInline"),
        en: onTurnDatabase.into === "page" ? "turn into page" : "turn into inline", run: () => void turnDatabase() } : null,
      ...(!many && (image || file) ? [
        { id: "replace", title: t("blockHandle:media.replace"), en: "replace", run: (image ?? file)!.onReplace },
        { id: "download", title: t("blockHandle:media.download"), en: "download", run: (image ?? file)!.onDownload },
        { id: "view-original", title: t("blockHandle:media.viewOriginal"), en: "view original", run: (image ?? file)!.onViewOriginal },
        image?.onCrop ? { id: "crop", title: t("blockHandle:media.crop"), en: "crop", run: image.onCrop } : null,
        (image ?? file)?.onFullScreen ? { id: "full-screen", title: t("blockHandle:media.fullScreen"), en: "full screen", run: (image ?? file)!.onFullScreen!,
          hint: keysFor("fullScreen") } : null,
        file?.onRename ? { id: "rename", title: t("blockHandle:media.rename"), en: "rename", run: file.onRename } : null,
      ] : []),
      // Whatever block has them: a bookmark's or a code block's were left out of the search.
      captionVisible !== undefined && !many ? { id: "caption", title: t("blockHandle:media.caption"), en: "caption", run: toggleCaption,
        shortcut: "toggleCaption" as const } : null,
      bookmark && !many ? { id: "turn-mention", title: t("blockHandle:menu.turnIntoMention"), en: "turn into mention", run: bookmark.onTurnMention } : null,
      bookmark && !many ? { id: "copy-source", title: t("blockHandle:media.copySourceLink"), en: "copy source link", run: bookmark.onCopySource } : null,
      ...(database && !many ? database.rows.filter(row => !row.disabled).map(row => ({ id: row.id, title: row.label, en: row.en, run: row.run })) : []),
      onTurnTableDatabase && !many ? { id: "table-database", title: t("blockHandle:menu.turnIntoDatabase"), en: "turn into database", run: onTurnTableDatabase } : null,
      table && !many ? { id: "fit-width", title: t("blockHandle:menu.fitWidth"), en: "fit to width", run: table.onFitWidth } : null,
      onCopyLink && !many ? { id: "copy-link", title: t(database ? "blockHandle:database.copyLink" : "blockHandle:menu.copyLinkToBlock"), en: database ? "copy link" : "Copy link to block",
        run: database?.onCopyLink ?? onCopyLink, hint: database?.onCopyLink ? undefined : keysFor("copyBlockLink") } : null,
      onCopyLinks && many ? { id: "copy-links", title: t("blockHandle:menu.copyAllLinks"), en: "copy all links", run: onCopyLinks } : null,
      ...(database?.structureLocked ? [] : [
        { id: "duplicate", title: t("blockHandle:menu.duplicate"), en: "duplicate", run: onDuplicate, hint: keysFor("duplicateBlock") },
        { id: "move", title: t("blockHandle:menu.moveTo"), en: "move to", run: onMove, hint: keysFor("moveBlock") },
        { id: "delete", title: deleteTitle, en: deleteEnglish, run: onDelete, hint: deleteHint }]),
      onComment && !many && !database && shape.comment ? { id: "comment", title: t("blockHandle:menu.comment"), en: "comment", run: onComment, hint: keysFor("addComment") } : null,
      onSuggest && !database && shape.suggest ? { id: "suggest", title: t("blockHandle:menu.suggestEdits"), en: "suggest edits", run: onSuggest, hint: keysFor("suggestEdits") } : null,
      onPresent && !many ? { id: "present", title: t("blockHandle:menu.presentHere"), en: "present from here", run: onPresent, hint: keysFor("presentPage") } : null,
    ];
    const found = blockMenuFound(query, foundActions.filter(action => action !== null), action => blockMenuActionSearchNames(action.title, action.en));
    groups.push({ best: found.best, list: () => { for (const action of found.items) {
      const listed = add(action.id, action.title, action.run, undefined, action.hint);
      if ("shortcut" in action && action.shortcut) keyed(listed, action.shortcut);
    } } });
    // "이 앱으로 열기"'s apps, where the menu has it: each by its name and its app's, as a menu's app lines are found,
    // and every one by the row's own name, after those found by their own (blockMenuFound); after the menu's own rows
    // where they match as well, as the other menus' app lines stand (AppMenuLines).
    const openWith = t("blockHandle:media.openWith");
    const opening = blockMenuFound(query, !many && file?.openWith?.length ? file.openWith : [], app => ({ names: [], appLine: `${app.name} ${app.appName}` }),
      [openWith, "Open with"]);
    groups.push({ best: opening.best, list: () => { for (const [index, app] of opening.items.entries()) {
      const action = add(`open-with:${app.id}`, app.name, app.run, { "data-open-with-app": app.id });
      if (index === 0) action.before = searchLabel(openWith);
      action.content = labelProps => <><span aria-hidden="true">{ACTION_ICONS.openWith}</span><span {...labelProps}>{app.name}</span></>;
    } } });
    // Each group after the first opens with a line, as Notion's (seen 2026-11-13).
    let listed = 0;
    for (const group of blockMenuGroupsBestFirst(groups)) {
      const start = actions.length;
      group.list();
      if (actions.length > start && listed++ > 0) actions[start].before = <>{searchDivider}{actions[start].before}</>;
    }
  } else if (panel === "main" && image && !many) {
    addImageMenuRows({ add, child, keyed, keysFor, presentHere, t, divider, head: <div className={styles.menuLabel} data-block-menu-type>{blockTypeLabel("image")}</div> },
      { image, turns: turnChoices.length, captionVisible, toggleCaption, onCopyLink, onDuplicate, onMove, onDelete, onComment, onSuggest,
        insertBelow: sheet || touch ? onInsertBelow : undefined, sheet }, actions);
  } else if (panel === "main" && file && !many) {
    addFileMenuRows({ add, child, keyed, keysFor, presentHere, t, divider,
      head: <div className={styles.menuLabel} data-block-menu-type>{file.label ?? blockTypeLabel(activeType as GraphBlockType)}</div> },
    { file, activeType: activeType as GraphBlockType, fileMore, turns: turnChoices.length, captionVisible, toggleCaption,
      insertBelow: sheet || touch ? onInsertBelow : undefined, sheet, onCopyLink, onDuplicate, onMove, onDelete, onComment, onSuggest }, actions);
  } else if (panel === "main" && database && !many) {
    // An inline database's own menu, as Notion's (seen 2026-10-10): into a page,
    // its icon, lock and "추가 옵션"; opening it alone; its link, duplicate,
    // move and "휴지통으로 이동". No turn, color, comment or suggest.
    if (onTurnDatabase && !database.structureLocked) add("turn-database", t(onTurnDatabase.into === "page" ? "blockHandle:menu.turnIntoPage" : "blockHandle:menu.turnIntoInline"),
      () => void turnDatabase(), { "data-turn-database": onTurnDatabase.into });
    // A sub-page's menu turns and colors first, as Notion's (seen 2026-10-12); a locked page's neither.
    if (database.page && !database.structureLocked) {
      if (turnChoices.length) child("turn", t("blockHandle:menu.turnInto"));
      child("color", t("blockHandle:menu.color"));
    }
    const rowHint = (row: (typeof database.rows)[number]) => row.shortcut ? keysFor(row.shortcut) : row.hint;
    for (const row of database.rows) if (row.place === "top") {
      const action = add(row.id, row.label, row.run, row.attributes, rowHint(row));
      if (row.switched !== undefined) {
        const on = row.switched;
        action.attributes = { ...action.attributes, role: "menuitemcheckbox", "aria-checked": on };
        action.content = (labelProps) => <><span aria-hidden="true">{ACTION_ICONS[row.id]}</span><span {...labelProps}>{row.label}</span>
          <SwitchMark on={on} className={styles.menuSwitch} /></>;
      }
    }
    if (database.rows.some(row => row.place === "more")) child("more", t("blockHandle:media.moreOptions"));
    const opens = actions.length;
    for (const row of database.rows) if (row.place === "open") add(row.id, row.label, row.run, row.attributes, rowHint(row));
    if (actions[opens]) actions[opens].before = divider;
    const links = actions.length;
    if (database.onCopyLink ?? onCopyLink) add("copy-link", t("blockHandle:database.copyLink"), (database.onCopyLink ?? onCopyLink)!,
      { "data-copy-block-link": true }, database.onCopyLink ? undefined : keysFor("copyBlockLink"));
    if (!database.structureLocked) {
      // With its content or without, as Notion's (seen 2026-11-05).
      if (onDuplicateWithoutContent) child("duplicate", t("blockHandle:menu.duplicate"));
      else add("duplicate", t("blockHandle:menu.duplicate"), onDuplicate, undefined, keysFor("duplicateBlock"));
      add("move", t("blockHandle:menu.moveTo"), onMove, undefined, keysFor("moveBlock"));
      for (const row of database.rows) if (row.place === "trash") add(row.id, row.label, row.run, row.attributes);
      add("delete", deleteTitle, onDelete, { className: `${styles.menuItem} ${styles.menuItemDanger}` });
    }
    if (links > 0) actions[links].before = divider;
    // A sub-page's too, after "휴지통으로 이동" (Notion, seen 2026-10-31).
    presentHere();
    actions[0].before = <div className={styles.menuLabel} data-block-menu-type>{database.header}</div>;
  } else if (panel === "main" && table && !many) {
    // A simple table's own menu, as Notion's (seen 2026-10-11): into a database,
    // fit to width, its header row and column switched; link, duplicate, move,
    // delete; comment and suggest. No turn or color.
    if (onTurnTableDatabase) add("table-database", t("blockHandle:menu.turnIntoDatabase"), onTurnTableDatabase, { "data-turn-table-database": true });
    add("fit-width", t("blockHandle:menu.fitWidth"), table.onFitWidth, { "data-table-fit-width": true });
    for (const [id, on, toggle, icon] of [["header-row", table.headerRow, table.onToggleHeaderRow, <TableHeaderRowIcon size="regular" />],
      ["header-column", table.headerColumn, table.onToggleHeaderColumn, <TableHeaderColumnIcon size="regular" />]] as const) {
      const text = t(id === "header-row" ? "blockHandle:menu.headerRow" : "blockHandle:menu.headerColumn");
      add(id, text, toggle, { role: "menuitemcheckbox", "aria-checked": on, "data-table-header": id }).content = (labelProps) => <>
        <span aria-hidden="true">{icon}</span><span {...labelProps}>{text}</span>
        <SwitchMark on={on} className={styles.menuSwitch} /></>;
    }
    const start = actions.length;
    if (onCopyLink) add("copy-link", t("blockHandle:menu.copyLinkToBlock"),
      onCopyLink, { "data-copy-block-link": true }, keysFor("copyBlockLink"));
    add("duplicate", t("blockHandle:menu.duplicate"), onDuplicate, undefined, keysFor("duplicateBlock"));
    add("move", t("blockHandle:menu.moveTo"), onMove, undefined, keysFor("moveBlock"));
    add("delete", t("blockHandle:menu.delete"), onDelete, { className: `${styles.menuItem} ${styles.menuItemDanger}` }, keysFor("deleteBlock"));
    actions[start].before = divider;
    const notes = actions.length;
    if (onComment) add("comment", t("blockHandle:menu.comment"), onComment, { "data-block-comment": true }, keysFor("addComment"));
    if (onSuggest) add("suggest", t("blockHandle:menu.suggestEdits"), onSuggest, { "data-block-suggest": true }, keysFor("suggestEdits"));
    if (actions[notes]) actions[notes].before = divider;
    presentHere();
    actions[0].before = <div className={styles.menuLabel} data-block-menu-type>{blockTypeLabel("table")}</div>;
  } else if (panel === "main") {
    // The rows a block's type keeps (blockMenuShape.ts); a list's format
    // follows its color, and a synced block's unsync leads, as Notion's.
    if (emptyImage && !many) keyed(add("imageLink", t("blockHandle:media.addImageLink"), emptyImage.onEditLink), "imageLink");
    if (onTurnTableDatabase && !many) add("table-database", t("blockHandle:menu.turnIntoDatabase"), onTurnTableDatabase, { "data-turn-table-database": true });
    if (shape.turn) child("turn", t("blockHandle:menu.turnInto"));
    if (bookmark && !many) add("turn-mention", t("blockHandle:menu.turnIntoMention"), bookmark.onTurnMention, { "data-turn-mention": true });
    if (shape.color) child("color", t("blockHandle:menu.color"));
    // A touch screen's menu has it too, as Notion's iPad (seen 2026-11-13: a bookmark's "⋯").
    if ((sheet || touch) && onInsertBelow && !many) add("insert-below", t("blockHandle:menu.insertBelow"), onInsertBelow);
    if (listFormat && !many) {
      const title = listFormat.label ?? t("blockHandle:menu.listFormat");
      child("listFormat", title);
      // A list's "목록 형식" is marked with a bulleted list, as Notion's (seen 2026-11-10).
      if (!listFormat.label) actions[actions.length - 1].content = labelProps => <>
        <span aria-hidden="true"><BulletedListIcon size="regular" /></span><span {...labelProps}>{title}</span></>;
    }
    // Several of a kind edit their icons together, and code blocks their language, after their color (blockMenuSeveral).
    const editIcon = many ? several?.onEditIcon : onEditIcon;
    if (editIcon) add("edit-icon", t("blockHandle:database.editIcon"), editIcon, many ? undefined : { "data-edit-callout-icon": true });
    if (many && several?.language) child("language", t("blockHandle:menu.codeLanguage"));
    if (direction) child("direction", t("blockHandle:menu.textDirection"));
    if (openPage && !many) {
      add("open-tab", t("blockHandle:database.openInNewTab"), openPage.onNewTab, undefined, keysFor("openInNewTab"));
      add("open-side", t("blockHandle:database.openInSidePeek"), openPage.onSidePeek, undefined,
        menuKeyHint("⌥Click"));
    }
    if (captionVisible !== undefined && !many && !emptyImage) keyed(add("caption", t("blockHandle:media.caption"), toggleCaption,
      { "data-block-caption": captionVisible ? "on" : "off" }), "toggleCaption");
    if (code && !many) {
      add("copy-code", t("blockHandle:menu.copyCode"), code.onCopy, { "data-copy-code": true });
      const wrapLabel = t("blockHandle:menu.wrapCode");
      add("code-wrap", wrapLabel, code.onToggleWrap, { role: "menuitemcheckbox", "aria-checked": code.wrap, "data-code-wrap": true }).content = (labelProps) => <>
        <span aria-hidden="true">{ACTION_ICONS["code-wrap"]}</span>
        <span {...labelProps}>{wrapLabel}</span><SwitchMark on={code.wrap} className={styles.menuSwitch} /></>;
      // On a phone its language and formatting are under "추가 옵션", as Notion's sheet (seen 2026-11-11).
      if (sheet) child("more", t("blockHandle:media.moreOptions"));
      else {
        child("language", t("blockHandle:menu.codeLanguage"));
        if (code.onFormat) add("code-format", t("blockHandle:menu.formatCode"), code.onFormat, { "data-code-format": true });
      }
    }
    if (syncedBlock === "copy" && onUnsyncAll && !many) keyed(add("unsync-all", t("blockHandle:menu.unsyncAllSyncedBlocks"), onUnsyncAll), "unsyncAllSynced");
    if (syncedBlock === "unsync" && onSyncedBlock && !many) add("synced", t("blockHandle:menu.unsyncSyncedBlock"), onSyncedBlock);
    const start = actions.length;
    // The link rows together, its source's first, as a file's (Notion's bookmark, seen 2026-11-06).
    if (bookmark && !many) add("copy-source", t("blockHandle:media.copySourceLink"), bookmark.onCopySource);
    if (onCopyLink && !many) add("copy-link", t("blockHandle:menu.copyLinkToBlock"),
      onCopyLink, { "data-copy-block-link": true }, keysFor("copyBlockLink"));
    if (onCopyLinks && many) add("copy-links", t("blockHandle:menu.copyAllLinks"), onCopyLinks, { "data-copy-all-links": true });
    // Several led by a database duplicate as a database's menu does, "복제 ›"; led by another block, "복제" alone (blockMenuSeveral).
    if (many && several?.duplicateKinds) child("duplicate", t("blockHandle:menu.duplicate"));
    else add("duplicate", t("blockHandle:menu.duplicate"), onDuplicate, undefined, keysFor("duplicateBlock"));
    if (onDuplicateWithoutContent && !many) add("duplicate-empty", t("blockHandle:menu.duplicateWithoutContent"), onDuplicateWithoutContent, { "data-duplicate-without-content": true });
    add("move", t("blockHandle:menu.moveTo"), onMove, undefined, keysFor("moveBlock"));
    add("delete", deleteTitle, onDelete, { className: `${styles.menuItem} ${styles.menuItemDanger}` }, deleteHint);
    // Comment and suggest are a group of their own, as Notion's (seen 2026-10-12).
    const talk = actions.length;
    if (onComment && !many && shape.comment) add("comment", t("blockHandle:menu.comment"), onComment, { "data-block-comment": true }, keysFor("addComment"));
    if (onSuggest && shape.suggest) add("suggest", t("blockHandle:menu.suggestEdits"), onSuggest, { "data-block-suggest": true }, keysFor("suggestEdits"));
    if (actions[talk]) actions[talk].before = divider;
    presentHere();
    if (onTurnDatabase && !many) add("turn-database", t(onTurnDatabase.into === "page" ? "blockHandle:menu.turnIntoPage" : "blockHandle:menu.turnIntoInline"),
      () => void turnDatabase(), { "data-turn-database": onTurnDatabase.into });
    if (start > 0) actions[start].before = divider;
    actions[0].before = <>{kindHead}{start > 0 ? null : actions[0].before}</>;
  } else if (panel === "turnInline" && leftovers && onTurnDatabase) {
    back(t("blockHandle:turnInline.title"));
    add("confirm-inline", t("blockHandle:turnInline.confirm"), onTurnDatabase.run, { "data-turn-inline-confirm-action": true });
  }
  return actions;
}
