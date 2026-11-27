import { useRef, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "@/icons/leafen";
import styles from "./FormQuestionGap.module.css";

/**
 * Notion's "질문 추가" between a form's questions (seen 2026-11-12): pointing
 * anywhere in the 24px between the audience notice and a question, or two
 * questions, shows a blue drop with its "+" 24 before the questions' start;
 * pressed, a 36 tall slot holds the new question's place and the menu of
 * question types opens beside it.
 */
export function FormQuestionGap({ open, onOpen, menu }: {
  open: boolean;
  onOpen: () => void;
  /** The menu of question types, placed by the drop it opens from. */
  menu: (anchor: RefObject<HTMLButtonElement | null>) => ReactNode;
}) {
  const { t } = useTranslation("formView");
  const button = useRef<HTMLButtonElement>(null);
  return (
    <div className={styles.gap} data-open={open ? "true" : undefined} data-form-question-gap>
      <button ref={button} type="button" className={styles.add} aria-label={t("addQuestion")} title={t("addQuestion")}
        aria-haspopup="menu" aria-expanded={open} onClick={onOpen}>
        <Plus size="regular" aria-hidden="true" />
      </button>
      {open ? <><FormQuestionSlot />{menu(button)}</> : null}
    </div>
  );
}

/** The 36 tall place a question being added holds, below the questions too (Notion's, seen 2026-11-12). */
export function FormQuestionSlot({ ref }: { ref?: RefObject<HTMLDivElement | null> }) {
  return <div ref={ref} className={styles.slot} aria-hidden="true" data-form-question-slot />;
}
