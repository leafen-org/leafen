import { useRef, useState } from "react";
import { ChevronDown, LinkIcon, ReplaceIcon, Trash, WarningTriangleIcon } from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import menuStyles from "@/components/RowMenu.module.css";
import { blockItemText } from "./blockItemLabels";
import { requestBlockSetup } from "./blockSetup";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./EmbedUnknownLink.module.css";

type Option = "copy" | "change" | "remove";
const OPTIONS: Option[] = ["copy", "change", "remove"];

/**
 * The notice on a service's embed given a link it can't draw, as Notion's
 * (seen 2026-11-05: a Figma community file): "⚠ 알 수 없는 URL ⌄" at the
 * row's right, which offers to copy the link, change it or remove the block.
 * Notion's first option, its help article, is left out.
 */
export function EmbedUnknownLink({ adapter, blockId, url }: { adapter: NativeEditorGraphAdapter; blockId: string; url: string }) {
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  usePopoverAppear(open, menuRef, () => buttonRef.current);
  const { zIndex, onKeyDown } = useOverlayLayer(open, { kind: "menu", ref: menuRef, onEscape: () => setOpen(false),
    onOutsidePress: () => setOpen(false), inside: () => [buttonRef.current] });
  const text = (option: Option) => blockItemText(option === "copy" ? "embed.copyLink" : option === "change" ? "embed.changeLink" : "embed.remove");
  const menu = useNativeMenu({ items: OPTIONS, open, menuRef, label: blockItemText("embed.unknownLinkOptions"),
    keyOf: option => option, textOf: text,
    onAction: (option) => {
      setOpen(false);
      if (option === "copy") void copyText(url);
      else if (option === "change") requestBlockSetup(blockId);
      else adapter.remove([blockId]);
    } });
  const iconOf = (option: Option) => option === "copy" ? <LinkIcon size="regular" aria-hidden="true" />
    : option === "change" ? <ReplaceIcon size="regular" aria-hidden="true" /> : <Trash size="regular" aria-hidden="true" />;

  if (adapter.readOnly) return <span className={styles.noticeText}>
    <WarningTriangleIcon size="tiny" className={styles.warning} aria-hidden="true" />{blockItemText("embed.unknownLink")}
  </span>;
  return (
    <span className={styles.notice} data-embed-unknown-link>
      <button ref={buttonRef} type="button" className={styles.noticeButton} aria-haspopup="menu" aria-expanded={open}
        onClick={() => setOpen(current => !current)}>
        <WarningTriangleIcon size="tiny" className={styles.warning} aria-hidden="true" />
        {blockItemText("embed.unknownLink")}
        <ChevronDown size="tiny" aria-hidden="true" />
      </button>
      {open ? (
        <div {...menu.menuProps} ref={menuRef} className={`${menuStyles.menu} ${styles.menu}`} style={{ zIndex }}
          onKeyDown={event => { menu.menuProps.onKeyDown?.(event); onKeyDown(event); }}>
          <div className={menuStyles.sectionLabel}>{blockItemText("embed.unknownLinkOptions")}</div>
          {OPTIONS.map(option => (
            <NativeMenuItem key={option} itemKey={option} state={menu.state}
              className={`${menuStyles.item}${option === "remove" ? ` ${menuStyles.dangerOnHover}` : ""}`}>{({ labelProps }) => <>
              {iconOf(option)}
              <span {...labelProps}>{text(option)}</span>
            </>}</NativeMenuItem>
          ))}
        </div>
      ) : null}
    </span>
  );
}
