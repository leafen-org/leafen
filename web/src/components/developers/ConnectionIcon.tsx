import { useState } from "react";
import { useTranslation } from "react-i18next";
import { AnchoredIconPicker, PICKER_WIDTH } from "@/components/page/AnchoredEmojiPicker";
import { IconImage } from "@/components/page/PageIcon";
import { safePresentationImage } from "@/components/page/pagePresentation";
import styles from "./DeveloperTools.module.css";
import own from "./ConnectionIcon.module.css";

// A connection's square, as Notion's: its first letter on a color its name keeps.
const MARK_COLORS = ["#9a6dd7", "#2383e2", "#448361", "#d9730d", "#cb912f", "#c14c8a", "#d44c47"];
export function markColor(name: string) {
  let sum = 0;
  for (const char of name) sum = (sum * 31 + char.codePointAt(0)!) % 9973;
  return MARK_COLORS[sum % MARK_COLORS.length];
}

/**
 * A connection's square, 56px in its lists and 64px on its page's 정보 표시:
 * the icon its person picked (connection_icon.go) — a picture filling it, or
 * an emoji — or else its name's first letter on its color.
 */
export function ConnectionMark({ name, icon = "", size = "list" }: { name: string; icon?: string; size?: "list" | "display" }) {
  const clean = icon.trim();
  const image = safePresentationImage(clean);
  if (image) return <span className={`${styles.mark} ${own.picture}`} data-size={size} data-connection-icon="image" aria-hidden="true"><IconImage src={image} /></span>;
  if (clean) return <span className={styles.mark} data-size={size} data-connection-icon="emoji" aria-hidden="true">{clean}</span>;
  return <span className={styles.mark} data-size={size} style={{ background: markColor(name) }} aria-hidden="true">{[...name.trim()][0]?.toUpperCase()}</span>;
}

/**
 * The square on a connection's 정보 표시 with Notion's "편집" tag from its
 * bottom middle (an MCP token's, seen 2026-11-09; an internal connection's
 * "편집하기", 2026-10-30 and 2026-11-13). Either opens the icon picker a workspace's icon
 * has — an emoji, or a picture's link, which a connection keeps as it owns
 * no file — and "제거" puts the first letter back.
 */
export function ConnectionIconEdit({ kind, name, icon, onChange }: {
  kind: "mcpToken" | "internal";
  name: string;
  icon: string;
  onChange: (icon: string) => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const edit = t(kind === "mcpToken" ? "developerTools.connection.iconEdit" : "developerTools.internal.iconEdit");
  const label = t(kind === "mcpToken" ? "developerTools.connection.iconEditLabel" : "developerTools.internal.iconEditLabel");
  const [anchor, setAnchor] = useState<{ top: number; bottom: number; left: number } | null>(null);
  const pick = (value: string) => { setAnchor(null); if (value !== icon) onChange(value); };
  return (
    <>
      <button type="button" className={own.edit} aria-label={label} aria-haspopup="dialog" aria-expanded={anchor !== null}
        data-connection-icon-edit onClick={(event) => {
          // Its middle under the square's, as a page's icon picker opens.
          const rect = event.currentTarget.getBoundingClientRect();
          setAnchor({ top: rect.top, bottom: rect.bottom, left: rect.left + (rect.width - PICKER_WIDTH) / 2 });
        }}>
        <ConnectionMark name={name} icon={icon} size="display" />
        <span className={own.tag} data-kind={kind} aria-hidden="true">{edit}</span>
      </button>
      {anchor ? (
        <AnchoredIconPicker anchor={anchor} marker="connection-icon-picker" onClose={() => setAnchor(null)}
          picker={{ onPick: pick, onPickImage: pick, onRemove: icon ? () => pick("") : undefined }} />
      ) : null}
    </>
  );
}
