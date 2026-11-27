import { useState } from "react";
import { useTranslation } from "react-i18next";
import { BlockIcon } from "./BlockIcon";
import { blockSubmenuChoices, type BlockSubmenuSource } from "./BlockMenuSubmenu";
import type { BlockTurnChoice } from "./blockTurnInto";
import { storedEditorColor } from "./colorMemory";
import { PhoneStripPanel, StripColorMark, type StripSection } from "./PhoneStripPanel";

/**
 * A line's "전환" and colors from a phone strip, in the keyboard's place
 * (PhoneStripPanel), as the Notion Android app's (seen 2026-11-21 on the A90
 * at 411): "전환" heads its cards — text, the four headings, page, the
 * bulleted, numbered, to-do and toggle lists, code, quote, then the rest —
 * with nothing ticked and no "페이지 위치:" between page and the lists; the
 * colors "텍스트 색상" then "배경 색상", from the menu's own choices
 * (blockSubmenuChoices). A choice acts as the block menu's does.
 */
export function BlockStripPanel({ kind, source, onClose, onColor, turnInto }: {
  kind: "turn" | "color"; source: BlockSubmenuSource;
  onClose(): void; onColor(color: string): void; turnInto(choice: BlockTurnChoice): void;
}) {
  const { t } = useTranslation("blockHandle");
  const [recent] = useState(() => storedEditorColor());
  const list = blockSubmenuChoices(kind, source, t, { phone: true, recent: kind === "color" ? recent : null });
  const sections: StripSection[] = [];
  for (const item of list.items) {
    if (item.turn?.placeIn || !item.turn && !item.color) continue;
    if (item.group || !sections.length) sections.push({ label: item.group ?? list.label, cards: [] });
    const turn = item.turn, color = item.backgroundDefault ? "default_background" : item.color;
    sections.at(-1)!.cards.push({ key: item.id, label: item.label,
      icon: turn ? <BlockIcon type={turn.type} size="large" /> : <StripColorMark color={color!} />,
      run: () => turn ? turnInto(turn) : onColor(item.color!),
      data: { "data-turn-type": turn?.key, "data-block-color-choice": item.color } });
  }
  return <PhoneStripPanel kind={kind} label={list.label} sections={sections} onClose={onClose} />;
}
