import { SwitchMark } from "@/components/Switch";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { SlidersIcon } from "@/icons/leafen";
import styles from "./SearchControls.module.css";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { detectKeyboardShortcutPlatform } from "@/lib/keyboardShortcuts";
import { menuKeyHint } from "@/lib/menuKeyHint";
import { CommandSearchHint, useCommandSearch } from "./CommandSearchHint";

// "⌘↵" on a Mac; elsewhere the keys as words, as every menu's (menuKeyHint).
const mac = detectKeyboardShortcutPlatform() === "mac";
const newTabKeys = mac ? ["⌘", "↵"] : menuKeyHint("⌘↵").split("+");
const copyLinkKeys = mac ? ["⌘", "L"] : menuKeyHint("⌘L").split("+");

/**
 * The search window's foot, as Notion's (seen 2026-09-30): "⌘↵ 새 탭에서
 * 열기" on the left — and in the Mac and Windows apps "⌘L 링크 복사", the
 * chosen result's link, 20px after it, and in the Mac and Windows apps
 * the command search's key, "⇧⌘K 명령어 검색", while it is on, as Notion's
 * app's (its source read 2026-11-13; CommandSearchHint) — and "검색 설정"
 * on the right, whose switch keeps the filters from one search to the next.
 */
export function SearchFooter({ keepFilters, onKeepFilters, copyLink = false, onEditShortcut }: {
  keepFilters: boolean;
  onKeepFilters: (keep: boolean) => void;
  /** The app's ⌘L hint (SearchDialog copies the chosen result's link). */
  copyLink?: boolean;
  /** The command search hint pressed: its key's setting opens. */
  onEditShortcut: () => void;
}) {
  const { t } = useTranslation(["searchDialog"]);
  const [open, setOpen] = useState(false);
  const commandSearch = useCommandSearch();
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  const menuRef = useRef<HTMLDivElement>(null);
  usePopoverAppear(open, menuRef);
  const { zIndex, onKeyDown } = useOverlayLayer(open, { kind: "menu", ref: menuRef, onEscape: () => setOpen(false),
    onOutsidePress: () => setOpen(false), inside: () => [buttonRef.current] });
  const menu = useNativeMenu({ items: ["keep"], open, menuRef, label: t("searchDialog:settings.label"),
    keyOf: item => item, textOf: () => t("searchDialog:settings.keepFilters"), onAction: () => onKeepFilters(!keepFilters) });

  return (
    <div ref={rootRef} className={styles.footer} data-search-footer>
      <span className={styles.footerHints}>
        {/* ↵ turns over with a right-to-left screen, as Notion's Enter sign (styles.css). */}
        <span className={styles.footerHint} aria-hidden="true">{newTabKeys.map(key => <kbd key={key}
          data-reading-turn={key === "↵" || undefined}>{key}</kbd>)} {t("searchDialog:footer.openInNewTab")}</span>
        {copyLink ? <span className={styles.footerHint} aria-hidden="true" data-search-footer-copy-link>
          {copyLinkKeys.map(key => <kbd key={key}>{key}</kbd>)} {t("searchDialog:footer.copyLink")}</span> : null}
        {commandSearch?.on ? <CommandSearchHint shortcut={commandSearch.shortcut} onEdit={onEditShortcut} /> : null}
      </span>
      <button ref={buttonRef} type="button" className={styles.footerButton} aria-label={t("searchDialog:settings.label")}
        title={t("searchDialog:settings.label")} aria-haspopup="menu" aria-expanded={open}
        onClick={() => setOpen((current) => !current)}>
        <SlidersIcon size="small" aria-hidden="true" />
      </button>
      {open ? (
        <div {...menu.menuProps} ref={menuRef} className={styles.settingsMenu} style={{ zIndex }}
          onKeyDown={event => { menu.menuProps.onKeyDown?.(event); onKeyDown(event); }}>
          <NativeMenuItem itemKey="keep" state={menu.state} role="menuitemcheckbox" aria-checked={keepFilters}
            className={styles.settingsItem} data-search-keep-filters>{({ labelProps }) => <>
            <span {...labelProps}>{t("searchDialog:settings.keepFilters")}</span>
            <SwitchMark on={keepFilters} />
          </>}</NativeMenuItem>
        </div>
      ) : null}
    </div>
  );
}
