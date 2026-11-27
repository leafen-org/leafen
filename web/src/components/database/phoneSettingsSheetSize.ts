import { useState } from "react";
import { useInSheetWindow } from "../shell/sheetWindow";

/**
 * How high a phone's view settings sheet opens, as Notion's (seen 2026-11-18
 * on an iPhone 17 in Safari, 402pt, and a Galaxy A90 in Chrome, 411): a
 * chart's, its "새 보기" too, opens half high — its top 16px under the
 * screen's middle, 10px on an iPhone (PhoneSheet `size="half"`) — so its
 * chart shows above while it is set; pushed up, or dragged up by its rows, it
 * opens out to a whole sheet, and the screens opened inside it keep its
 * height. A table's, board's, list's, gallery's, calendar's, timeline's and
 * dashboard's open whole. Decided as it opens: a layout chosen inside the
 * sheet leaves it as high as it was (Notion's "새 보기" turned from 표 to
 * 차트 stayed whole). Inside a wider touch screen's middle window it is that
 * window (sheetWindow.ts).
 */
export function usePhoneSettingsSheetSize(viewType: string): "half" | "full" {
  const windowed = useInSheetWindow();
  const [chart] = useState(viewType === "chart");
  return chart && !windowed ? "half" : "full";
}
