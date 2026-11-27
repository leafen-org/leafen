import { useRef, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { i18next } from "@/i18n";
import { DotsHorizontal, X } from "@/icons/leafen";
import { placeAround, placeBeside, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import type { DbProperty, PropertyType } from "@/lib/types";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { localizedPropertyTypeLabel } from "./propertyTypes";
import styles from "./FormView.module.css";

// The kinds of new question, in the order and with the names of Notion's
// list (seen 2026-10-08, its files and place 2026-11-02).
const EXISTING_SHOWN = 5;

export const NEW_QUESTION_TYPES = [
  ["rich_text", "text"], ["multi_select", "choice"], ["date", "date"], ["person", "person"], ["files", "files"], ["number", "number"],
  ["checkbox", "checkbox"], ["email", "email"], ["url", "url"], ["phone", "phone"], ["place", "place"],
] as const satisfies readonly (readonly [PropertyType, string])[];

export type NewQuestionType = typeof NEW_QUESTION_TYPES[number][0];

/**
 * Whether a question asks a property of a kind the form makes questions of,
 * which it can copy. A title's or a relation's question has no "질문 복제",
 * as Notion's (seen 2026-11-06); its "질문 유형" still turns it (FormView).
 */
export function formQuestionTypeMadeHere(type: string) {
  return type === "select" || NEW_QUESTION_TYPES.some(([value]) => value === type);
}

/** A new question's name: its kind, as Notion's, numbered while the database has one so named. */
export function newQuestionName(type: NewQuestionType, taken: readonly string[]) {
  const kind = NEW_QUESTION_TYPES.find(([value]) => value === type)![1];
  const base = i18next.t(`formView:newQuestionTypes.${kind}`);
  const names = new Set(taken);
  for (let count = 1; ; count += 1) {
    const name = count === 1 ? base : `${base} ${count}`;
    if (!names.has(name)) return name;
  }
}

/**
 * "+" under the questions, as Notion's "유형 선택" (seen 2026-10-08): the
 * database's properties without a question, then a new question of a kind,
 * which makes its property.
 */
export function FormAddQuestionMenu({
  anchor, questions = [], beside = false, besideGap = 24, properties, labels, canCreate, onQuestion, onExisting, onNew, onClose,
}: {
  anchor: RefObject<HTMLElement | null>;
  /** The form's questions a condition group can take in, listed first as "기존 질문" (Notion's, seen 2026-11-05). */
  questions?: readonly { id: string; name: string; type: PropertyType }[];
  /** Left of a condition group's add button, 24 from it, as Notion's. */
  beside?: boolean;
  /** How far left of its button it stands beside: a gap's drop it overlaps, its end at the drop's middle (Notion's, seen 2026-11-12). */
  besideGap?: number;
  onQuestion?: (questionId: string) => void;
  properties: readonly DbProperty[];
  labels: readonly string[];
  canCreate: boolean;
  onExisting: (propertyId: string) => void;
  onNew: (type: NewQuestionType) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation(["formView", "common"]);
  // A form's title question is "제목", as Notion's lists call it (seen 2026-11-05).
  const kindName = (type: PropertyType) => type === "title" ? t("formView:titleKind") : localizedPropertyTypeLabel(type);
  const menu = useRef<HTMLDivElement>(null);
  // Five of the properties, then "N개 더 보기" for the rest, as Notion's (seen 2026-11-02).
  const [allShown, setAllShown] = useState(false);
  const shown = allShown ? properties : properties.slice(0, EXISTING_SHOWN);
  // Its first row is marked as it opens, the mark then following the pointer (Notion's, seen 2026-11-12).
  const [pointed, setPointed] = useState<string | null>(null);
  const marked = pointed ?? questions[0]?.id ?? shown[0]?.id ?? (canCreate ? NEW_QUESTION_TYPES[0][0] : null);
  // At most 80% of the window tall, as Notion's (seen 2026-11-12: 578 in a 723 window).
  const place = usePopoverPlacement(menu, (size, viewport) => beside
    ? placeBeside(popoverRectOf(anchor.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport,
      { prefer: "left", gap: besideGap, cap: viewport.height * 0.8 })
    : placeAround(popoverRectOf(anchor.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 4, cap: viewport.height * 0.8 }),
  [anchor, beside, besideGap], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose, inside: () => [anchor.current] });
  const item = (key: string, type: PropertyType, name: string, detail: string | null, onClick: () => void, data: Record<string, string>) => (
    <button key={key} type="button" role="menuitem" className={styles.addMenuItem} onClick={onClick} {...data}
      data-active={key === marked ? "" : undefined} onMouseEnter={() => setPointed(key)}>
      <span className={styles.addMenuIcon}><PropertyTypeIcon type={type} /></span>
      <span className={styles.addMenuText}><span>{name}</span>{detail ? <small>{detail}</small> : null}</span>
    </button>
  );
  return (
    <PopoverLayer><div ref={menu} className={styles.addMenu} role="menu" aria-label={t("formView:chooseType")} style={popoverPlacementStyle(place)}>
      <div className={styles.addMenuHeader}>
        <span>{t("formView:chooseType")}</span>
        <button type="button" aria-label={t("common:actions.close")} onClick={onClose}><X size="tiny" aria-hidden="true" /></button>
      </div>
      {/* The head stays while the rows under it scroll. */}
      <div className={styles.addMenuList}>
        {questions.length > 0 ? <div className={styles.addMenuSection}>{t("formView:existingQuestions")}</div> : null}
        {questions.map((question) => item(question.id, question.type, question.name, kindName(question.type),
          () => onQuestion?.(question.id), { "data-form-add-existing-question": question.id }))}
        {properties.length > 0 ? <div className={styles.addMenuSection}>{t("formView:existingProperties")}</div> : null}
        {shown.map((property, index) => item(property.id, property.type, labels[index], kindName(property.type),
          () => onExisting(property.id), { "data-form-add-property": property.id }))}
        {shown.length < properties.length ? (
          <button type="button" role="menuitem" className={styles.addMenuMore} data-form-add-more onClick={() => setAllShown(true)}>
            <DotsHorizontal aria-hidden="true" />
            {t("formView:moreProperties", { count: properties.length - shown.length })}
          </button>
        ) : null}
        {/* "새로운 질문" heads the new kinds under other rows; alone they have no head (Notion's, seen 2026-11-15). */}
        {canCreate && (questions.length > 0 || properties.length > 0) ? <div className={styles.addMenuSection}>{t("formView:newQuestion")}</div> : null}
        {canCreate ? NEW_QUESTION_TYPES.map(([type, kind]) => item(type, type, t(`formView:newQuestionTypes.${kind}`), null,
          () => onNew(type), { "data-form-new-question": type })) : null}
      </div>
    </div></PopoverLayer>
  );
}
