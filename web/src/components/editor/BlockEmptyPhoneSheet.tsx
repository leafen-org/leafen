import { createContext, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { PhoneSheet } from "../shell/PhoneSheet";

/** Whether an empty block's panes draw inside its phone screen (BlockEmptyPhoneSheet), not its window. */
export const BlockEmptyPhoneContext = createContext(false);

/**
 * An empty block's window on a phone, as Notion's (seen 2026-11-15 on a
 * Galaxy A90 at 411, "+ › 웹 북마크" and a page's empty rows): a whole
 * screen, its name ("북마크"; "임베드" for a picture, video or any embed;
 * "파일" for a file) over a line with "닫기" at its end, its tabs on a white
 * row under the head where it has them, then flat white rows 28px apart —
 * the link field, the blue "북마크 생성" with its grey hint under it, or an
 * upload's blue row — and the keyboard down until the field is tapped
 * (fieldFocusOnOpen). An HTML file's is a half sheet ("HTML"). Closed, the
 * block keeps its empty row.
 */
export function BlockEmptyPhoneSheet({ name, title, half = false, tabs, onClose, children }: {
  name: string;
  title: string;
  half?: boolean;
  tabs?: ReactNode;
  onClose(): void;
  children: ReactNode;
}) {
  const { t } = useTranslation("common");
  return (
    <PhoneSheet size={half ? "half" : "screen"} title={title} label={title} onClose={onClose} action={{ label: t("actions.close"), onPress: onClose }}
      data={{ "data-block-empty-sheet": name }}>
      {tabs}
      <BlockEmptyPhoneContext.Provider value>{children}</BlockEmptyPhoneContext.Provider>
    </PhoneSheet>
  );
}
