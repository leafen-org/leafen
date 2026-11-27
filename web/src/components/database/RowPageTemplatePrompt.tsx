import { Fragment, useEffect, useRef, useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { caretOnTitleLastLine } from "@/components/editor/titleBodyCaret";
import { DotsHorizontal, FileBlank, Plus } from "@/icons/leafen";
import type { DatabaseTemplateChoice } from "@/lib/databaseTemplateRequests";
import styles from "./DatabaseRowPage.module.css";
import touchStyles from "./rowPageTemplateTouch.module.css";
import { RowPageTemplateMenu } from "./RowPageTemplateMenu";
import { TemplateIconGlyph, templateBodyPlaceholder } from "./databaseViewShared";
import { useDatabaseUntitledName } from "./untitledRowName";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * A database page's empty body, while its database offers templates, as
 * Notion's (seen 2026-10-12): with none, a line offering Enter to start
 * writing or a new template — its last words make the database a new
 * template; with some, a line offering to pick one (↑ ↓) over a list of them,
 * "비어 있음" and "+ 새 템플릿" (32px lines, 16px), the one chosen applied to
 * the page and "비어 있음" leaving it empty to write in, the caret on its
 * first line (Notion's, seen 2026-11-13). A template's line
 * shows "···" under the pointer, its menu (RowPageTemplateMenu, seen
 * 2026-10-13). The prompt's words are the empty body's first line: pressed,
 * the prompt goes and the caret stands there to write, Enter beginning a
 * second line (Notion's, seen 2026-11-04). A touch screen's words say to
 * tap there, with no stop and no keys, for either case (Notion's iPad, seen
 * 2026-11-14 at 834) — over its templates a phone's says only to tap to go
 * on (Notion's, seen 2026-11-15 at 402pt); with none they fit one line at a
 * phone's width, and a tap leaves the caret on a first line with no hint
 * (Notion's, seen 2026-11-25 on a Galaxy A90's Chrome at 411 in two tabs; a
 * body left with that one empty line shows neither, there too). Where the reader can't use its
 * templates — its database in a locked page — it offers only Enter to start
 * writing, in the same gray line, and no page starter (Notion's, seen 2026-11-16 in Chrome, two
 * tabs: a locked page's inline database's empty row page, aside and full).
 */
export function RowPageTemplatePrompt({ dataSourceId, untitledName, templates, onCreate, onApply, onWrite }: {
  dataSourceId: string;
  /** What the page's database calls its untitled pages, as the row's own title's place reads it. */
  untitledName?: string;
  templates: readonly DatabaseTemplateChoice[];
  /** Makes a new template; none where the reader can't use the database's templates. */
  onCreate?: () => void;
  onApply(templateId: string): void;
  /** Leaves the prompt for the body's first line, the caret on it: its words or "비어 있음" pressed. */
  onWrite(): void;
}) {
  const { t } = useTranslation("databaseView");
  const { touch, phone } = useSidebarLayout(true);
  // One made with no name reads as its database's untitled pages do, as in its template menu
  // and the row's own title's place, which a row page opened alone knows from its read.
  const databaseName = useDatabaseUntitledName(templates, dataSourceId), untitled = untitledName?.trim() || databaseName;
  const list = useRef<HTMLDivElement>(null);
  // From the empty body's cursor ↑ and ↓ move the shaded line and Enter takes
  // it, as Notion's (seen 2026-10-12); until an arrow is pressed Enter writes.
  // From the page's title, its last line, the first ↓ shades the first
  // template, the caret staying in the title (a letter typed next goes there),
  // as Notion's (a side peek, measured twice on 2026-11-15 in Chrome 1440 in
  // different tabs, no script before the key, the screen and one typed letter
  // after it); before it, ↓ there goes on to the title's "댓글 추가". Enter in
  // the title then is the title's own (Notion's looked the same either way).
  const [active, setActive] = useState<{ index: number; engaged: boolean }>({ index: 0, engaged: false });
  const [menu, setMenu] = useState<{ template: DatabaseTemplateChoice; anchor: DOMRect } | null>(null);
  const choices = templates.length + 2, creatable = Boolean(onCreate);
  useEffect(() => {
    if (templates.length === 0 || !creatable) return;
    const onKey = (event: globalThis.KeyboardEvent) => {
      const target = event.target instanceof HTMLElement ? event.target : null;
      const title = target?.closest<HTMLElement>("[data-row-page-title]");
      const fromTitle = Boolean(title && list.current?.closest("[data-row-page]")?.contains(title));
      // Only the page's own body, the editor beside the list, or its title.
      if (!target?.isContentEditable || !(fromTitle || list.current?.closest("section")?.contains(target))
        || event.metaKey || event.ctrlKey || event.altKey || imeComposing(event)) return;
      if (fromTitle && (event.key === "ArrowDown" || event.key === "ArrowUp")) {
        if (event.shiftKey || !title || !caretOnTitleLastLine(title) || !active.engaged && event.key === "ArrowUp") return;
        event.preventDefault();
        event.stopPropagation();
        setActive((current) => ({ engaged: true,
          index: current.engaged ? (current.index + (event.key === "ArrowDown" ? 1 : choices - 1)) % choices : 0 }));
      } else if (!fromTitle && (event.key === "ArrowDown" || event.key === "ArrowUp")) {
        event.preventDefault();
        setActive((current) => ({ engaged: true,
          index: (current.index + (event.key === "ArrowDown" ? 1 : choices - 1)) % choices }));
      } else if (fieldEnter(event) && !event.shiftKey && !fromTitle) {
        setActive((current) => {
          if (current.engaged) {
            event.preventDefault();
            event.stopPropagation();
            list.current?.querySelectorAll<HTMLButtonElement>("[role=option]")[current.index]?.click();
          }
          return current;
        });
      } else if (event.key === "Escape") {
        setActive({ index: 0, engaged: false });
      }
    };
    document.addEventListener("keydown", onKey, true);
    return () => document.removeEventListener("keydown", onKey, true);
  }, [choices, templates.length, active.engaged, creatable]);
  // A press on the words, not on what they offer, starts the writing.
  const write = {
    onMouseDown: (event: React.MouseEvent) => { if (!(event.target as HTMLElement).closest("button")) event.preventDefault(); },
    onClick: (event: React.MouseEvent) => { if (!(event.target as HTMLElement).closest("button")) onWrite(); },
  };
  if (!onCreate) {
    return (
      // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/click-events-have-key-events -- Enter in the body does the same.
      <p className={`${styles.templatePrompt} ${touch ? touchStyles.touchLead : ""}`} data-row-page-template-prompt="write" {...write}>{templateBodyPlaceholder()}</p>
    );
  }
  if (templates.length === 0) {
    return (
      // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/click-events-have-key-events -- Enter in the body does the same.
      <p className={`${styles.templatePrompt} ${touch ? touchStyles.touchLead : ""}`} data-row-page-template-prompt {...write}>
        {t(touch ? "templatePromptTapLead" : "templatePromptLead")}
        <button type="button" className={styles.templatePromptAction} onClick={onCreate}>{t("templatePromptAction")}</button>
        {touch ? null : t("templatePromptEnd")}
      </p>
    );
  }
  // ↑ and ↓ move between the lines, as Notion's list: measured once
  // 2026-11-15 in Chrome 1440 with no script before the keys, ↓ went on
  // through a template, "비어 있음" and "+ 새 템플릿", the next back to the
  // first.
  const move = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    const rows = [...(list.current?.querySelectorAll<HTMLButtonElement>("[role=option]") ?? [])];
    const at = rows.indexOf(document.activeElement as HTMLButtonElement);
    const next = rows[(at + (event.key === "ArrowDown" ? 1 : rows.length - 1)) % rows.length];
    if (!next) return;
    event.preventDefault();
    next.focus();
  };
  return (
    <div data-row-page-template-prompt="choose">
      {/* eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/click-events-have-key-events -- Enter in the body does the same. */}
      <p className={`${styles.templatePrompt} ${touch ? touchStyles.touchPrompt : ""}`} {...write}>{t(phone ? "templateChooseTapPhoneLead" : touch ? "templateChooseTapLead" : "templateChooseLead")}</p>
      <div ref={list} role="listbox" aria-label={t("templateChooseLabel")} className={`${styles.templateList} ${touch ? touchStyles.touchList : ""}`} onKeyDown={move}
        data-engaged={active.engaged ? "true" : undefined}>
        {templates.map((template, index) => (
          <Fragment key={template.id}>
            <button type="button" role="option" aria-selected={active.engaged && active.index === index} className={styles.templateRow}
              data-row-page-template={template.id} onClick={() => onApply(template.id)}>
              <span className={styles.templateRowIcon} aria-hidden="true"><TemplateIconGlyph icon={template.icon} empty={!template.hasContent} /></span>
              <span>{template.name || untitled}</span>
            </button>
            <button type="button" className={styles.templateRowMore} aria-haspopup="menu" aria-expanded={menu?.template.id === template.id}
              aria-label={t("templateRowMenu.open", { name: template.name || untitled })} data-row-page-template-more={template.id}
              onClick={(event) => setMenu({ template, anchor: event.currentTarget.getBoundingClientRect() })}>
              <DotsHorizontal size="small" aria-hidden="true" />
            </button>
          </Fragment>
        ))}
        <button type="button" role="option" aria-selected={active.engaged && active.index === templates.length} className={styles.templateRow}
          data-row-page-template-empty onClick={onWrite}>
          <span className={styles.templateRowIcon} aria-hidden="true"><FileBlank size="small" /></span><span>{t("templateEmpty")}</span>
        </button>
        <button type="button" role="option" aria-selected={active.engaged && active.index === templates.length + 1} className={styles.templateRow}
          data-row-page-template-new onClick={onCreate}>
          <span className={styles.templateRowIcon} aria-hidden="true"><Plus size="small" /></span><span>{t("newTemplate")}</span>
        </button>
      </div>
      {menu ? <RowPageTemplateMenu anchor={menu.anchor} dataSourceId={dataSourceId} template={menu.template} onClose={() => setMenu(null)} /> : null}
    </div>
  );
}
