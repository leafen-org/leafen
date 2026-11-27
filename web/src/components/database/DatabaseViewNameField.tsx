import { useEffect, useRef, useState } from "react";
import type { GraphView } from "@/contentGraph";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { useTypedKeptOnLeave } from "@/lib/typedKeptOnLeave";
import { AnchoredIconPicker } from "../page/AnchoredEmojiPicker";
import { databaseViewLabels } from "./databaseViewLabels";
import { DescribeButton } from "./describeButton";
import { menuIconButtonProps } from "./menuIconButton";
import { ViewIcon, viewOwnName } from "./databaseViewShared";
import styles from "./DatabaseViewSettingsPanel.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/**
 * The view settings' first line, as Notion's (seen 2026-10-05): the view's
 * icon, its name, and an "i" that opens a line for its description under it.
 */
export function DatabaseViewNameField({ view, newView, locked, describable = true, onRename, onDone, onUpdateConfig }: {
  view: GraphView;
  newView: boolean;
  locked: boolean;
  /** Whether its name takes a description ("i"): a form's doesn't, as Notion's "폼 설정" (seen 2026-11-09). */
  describable?: boolean;
  onRename(name: string): void;
  /** A new view's "완료" (or a chart's "차트 편집"), which Enter in its name presses too. */
  onDone?(): void;
  onUpdateConfig?(patch: Record<string, unknown>): void;
}) {
  const labels = databaseViewLabels();
  // A view never named has no name of its own: the field is empty over its
  // placeholder, as Notion's (seen 2026-11-04 and 2026-11-06).
  const ownName = viewOwnName(view);
  const [viewName, setViewName] = useState(newView ? "" : ownName);
  const description = typeof view.config.description === "string" ? view.config.description : "";
  // Once opened, the description's line stays while the panel is open.
  const [describing, setDescribing] = useState(false);
  const showDescription = describable && (describing || description !== "");
  const descriptionRef = useRef<HTMLTextAreaElement>(null);
  const [iconAnchor, setIconAnchor] = useState<{ top: number; bottom: number; left: number; right: number } | null>(null);
  function describe(value: string) {
    const next = value.trim();
    if (next !== description) onUpdateConfig?.({ description: next || undefined });
  }
  // The name is kept as typed, spaces too, as Notion's (seen 2026-11-11).
  function rename(value: string) {
    namedKept.settled();
    if (value !== ownName) onRename(value);
  }
  // It is kept too when the settings close another way, or turn to another view (useTypedKeptOnLeave).
  // A new view's name isn't: its "새 보기" closed by a press outside or Escape leaves it unnamed, its
  // layout's name on its tab, as Notion's (seen 2026-11-18 in Chrome 1440: "nv" typed, then a press
  // outside; "nw" typed, then Escape — the tab "표" each time); Enter or "완료" names it.
  const namedKept = useTypedKeptOnLeave(view.id, (value) => { if (!newView) rename(value); });
  useEffect(() => { if (!newView) { setViewName(ownName); namedKept.settled(); } }, [ownName, newView]);
  return (
    <>
      {/* The view's icon (Notion's icons, "제거" back to its type's; named "아이콘 바꾸기" with no tip,
          menuIconButtonProps), its name, and an "i" that opens a line for its description under it (seen 2026-10-05). */}
      <div className={styles.viewName}>
        <button type="button" className={styles.viewIconButton} disabled={locked || !onUpdateConfig}
          {...menuIconButtonProps(labels.changeIcon, iconAnchor !== null)} data-view-icon-button onClick={(event) => {
            const rect = event.currentTarget.getBoundingClientRect();
            setIconAnchor({ top: rect.top, bottom: rect.bottom, left: rect.left, right: rect.right });
          }}>
          <ViewIcon view={view} size="regular" />
        </button>
        <span className={styles.viewNameBox}>
          {/* Enter or leaving the field keeps the name, as the view's tab does; in a new view Enter is its
              "완료" too, the panel turning to the view's settings (Notion's, seen 2026-11-12 and 2026-11-16 by
              the audit and 2026-11-17 in Chrome 1440: "z" typed after it went nowhere). A new view's name takes the
              focus, but not on a touch screen: Notion's phone "새 보기" opens with the keyboard down (seen
              2026-11-15 on a Galaxy A90, no script before the press: one letter typed after it went nowhere). */}
          <input key={view.id} autoFocus={newView && fieldFocusOnOpen()} data-view-settings-name placeholder={labels.viewName} value={viewName} onChange={event => { setViewName(event.currentTarget.value); namedKept.typed(event.currentTarget.value); }} aria-label={labels.viewName} disabled={locked}
            onFocus={event => event.currentTarget.select()}
            onKeyDown={(event) => {
              if (!fieldEnter(event)) return;
              event.preventDefault();
              rename(event.currentTarget.value);
              if (newView) onDone?.();
            }}
            onBlur={(event) => rename(event.target.value)} />
          {!newView && describable && <DescribeButton className={styles.describe} disabled={locked || !onUpdateConfig}
            name={labels.addViewDescription} open={showDescription} data-view-description-button onClick={() => {
              setDescribing(true);
              focusOpenedField(() => descriptionRef.current);
            }} />}
        </span>
      </div>
      {showDescription && (
        <textarea ref={descriptionRef} key={`${view.id}:description`} className={styles.viewDescription} rows={1} maxLength={2000}
          defaultValue={description} disabled={locked} aria-label={labels.viewDescription} placeholder={labels.viewDescriptionPlaceholder}
          data-view-description
          onKeyDown={(event) => {
            if (!fieldEnter(event) || event.shiftKey) return;
            event.preventDefault();
            describe(event.currentTarget.value);
          }}
          onBlur={(event) => describe(event.target.value)} />
      )}
      {iconAnchor && onUpdateConfig ? (
        <AnchoredIconPicker anchor={iconAnchor} marker="view-icon-picker" onClose={() => setIconAnchor(null)}
          picker={{
            label: labels.viewIcon, current: typeof view.config.icon === "string" ? view.config.icon : undefined,
            // An emoji too, as Notion's view icons take one (2026-11-05).
            onPick: (emoji: string) => { onUpdateConfig({ icon: emoji }); setIconAnchor(null); },
            onPickBuiltInIcon: (icon) => { onUpdateConfig({ icon }); setIconAnchor(null); },
            onRemove: () => { if (typeof view.config.icon === "string") onUpdateConfig({ icon: undefined }); setIconAnchor(null); },
          }} />
      ) : null}
    </>
  );
}
