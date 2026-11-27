import { WorkspaceIconGlyph, workspaceIconKind, workspaceIconLetter } from "@/components/page/PageIcon";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronDown } from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { Workspace } from "@/types";
import styles from "./MoveWorkspacePicker.module.css";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";

/**
 * The workspace "옮기기" sends a page to, at the foot of the popover as
 * Notion's (seen 2026-10-01 and 2026-11-16): the workspace's icon, name and ⌄, whose menu lists
 * the reader's workspaces with ✓ by the chosen one. A page sent to another
 * workspace is copied there and stays where it is, as Notion's help says
 * (read 2026-10-01).
 */
export function MoveWorkspacePicker({
  workspaces,
  workspaceId,
  onChange,
}: {
  workspaces: readonly Workspace[];
  workspaceId: string;
  onChange: (workspaceId: string) => void;
}) {
  const { t } = useTranslation("moveToDialog");
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  // The menu opens under the button, over the page, lined up with its right
  // edge as Notion's — or over the button where only that side has room.
  const menuPlace = usePopoverPlacement(menuRef, open ? (size, viewport) => placeAround(
    popoverRectOf(triggerRef.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport,
    { gap: 4, align: "end" },
  ) : null, [open], undefined, { kind: "picker", inside: () => [triggerRef.current],
    onEscape: close, onOutsidePress: close });
  const current = workspaces.find((workspace) => workspace.id === workspaceId);

  function close() { setOpen(false); }
  const picker = useNativeMenu({ items: [...workspaces], open, menuRef, label: t("moveToDialog:workspace.menuLabel"),
    keyOf: workspace => workspace.id, textOf: workspace => workspace.name, firstKey: workspaceId,
    onAction: workspace => { onChange(workspace.id); close(); } });

  return (
    <div className={styles.footer}>
      <button
        ref={triggerRef}
        type="button"
        className={styles.trigger}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("moveToDialog:workspace.label", { name: current?.name ?? "" })}
        data-move-workspace={workspaceId}
        onClick={() => setOpen((value) => !value)}
      >
        {current ? (
          <span className={styles.triggerIcon} aria-hidden="true" data-workspace-icon={workspaceIconKind(current.icon)}>
            {workspaceIconKind(current.icon) === "letter" ? <span>{workspaceIconLetter(current.name)}</span>
              : <WorkspaceIconGlyph icon={current.icon} size={16} fallback="" />}
          </span>
        ) : null}
        <span className={styles.name}>{current?.name}</span>
        <ChevronDown size="badge" className={styles.chevron} aria-hidden="true" />
      </button>
      {open ? (
        <div
          {...picker.menuProps}
          ref={menuRef}
          className={styles.menu}
          style={popoverPlacementStyle(menuPlace)}
        >
          {workspaces.map((workspace) => {
            const chosen = workspace.id === workspaceId;
            return (
              <NativeMenuItem key={workspace.id} state={picker.state} itemKey={workspace.id}
                className={styles.item} role="menuitemradio" aria-checked={chosen}
                data-move-workspace-item={workspace.id}>
                {({ labelProps }) => <>
                <span className={styles.icon} aria-hidden="true">
                  <WorkspaceIconGlyph icon={workspace.icon} size={20} fallback={workspaceIconLetter(workspace.name)} />
                </span>
                <span {...labelProps} className={styles.itemName}>{workspace.name}</span>
                {chosen ? <CheckIcon size="small" aria-hidden="true" /> : null}
                </>}
              </NativeMenuItem>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
