import { confirmAction } from "@/components/ConfirmActionDialog";
import { Copy, Pencil, SlidersIcon, Trash } from "@/icons/leafen";
import { useOptionalProductToast } from "@/lib/productToast";
import type { DatabaseViewAction } from "./DatabaseTabMenus";
import { databaseViewLabels } from "./databaseViewLabels";
import { CONTENT_TAB_KEY } from "./pageLayoutTabOrder";
import { focusPickedViewName } from "./pickedViewName";

/**
 * What a row's "⋯" does in the layout builder's "N개 더 보기", as Notion's
 * (seen 2026-11-24 in Chrome at 640 and 1440, two tabs at two times): the
 * content tab's closes the list and chooses it, its settings open under its
 * pill; a data tab's opens beside it "이름 바꾸기" and "보기 편집" — the
 * list closed, the tab chosen and its view settings open, its name chosen
 * whole for the first — then "보기 복제", the copy joining the end chosen
 * with its settings open and its name chosen, and "보기 삭제", which asks
 * first ("이 보기를 삭제하시겠습니까?" · "보기 제거") and leaves the list open
 * with the first tab chosen, saying "〈name〉 보기 삭제됨".
 */
export function useLayoutTabListActions({ nameOf, onChooseContent, onChoose, onSettings, onTabActions }: {
  nameOf(tabId: string): string;
  onChooseContent(): void;
  onChoose(tabId: string): void;
  /** Opens the chosen data tab's view settings. */
  onSettings(tabId: string): void;
  onTabActions(tabId: string): { duplicate(): string; remove(): void };
}) {
  const labels = databaseViewLabels(), toast = useOptionalProductToast();
  // The settings are drawn a few frames after they are asked for; the name takes the focus then, chosen whole.
  const nameFirst = () => focusPickedViewName(() => document.body);
  return (id: string, close: () => void): DatabaseViewAction[] | (() => void) => {
    if (id === CONTENT_TAB_KEY) return () => { close(); onChooseContent(); };
    const open = (named: boolean) => { close(); onChoose(id); onSettings(id); if (named) nameFirst(); };
    return [
      { id: "rename", label: labels.renameView, icon: <Pencil size="regular" />, act: () => open(true) },
      { id: "edit", label: labels.editView, icon: <SlidersIcon size="regular" />, act: () => open(false) },
      { id: "duplicate", label: labels.duplicateView, icon: <Copy size="regular" />, grouped: true, act: () => {
        close();
        onSettings(onTabActions(id).duplicate());
        nameFirst();
      } },
      { id: "delete", label: labels.deleteView, icon: <Trash size="regular" />, danger: true, act: () => void (async () => {
        const name = nameOf(id);
        if (!await confirmAction({ message: labels.deleteViewConfirm, confirmLabel: labels.removeView, danger: true })) return;
        onTabActions(id).remove();
        toast?.notify(labels.viewDeleted(name), "success");
      })() },
    ];
  };
}
