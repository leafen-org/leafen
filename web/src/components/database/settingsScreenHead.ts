import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";

/** What a settings screen says its heading is, in place of the panel's own: its title and where "←" goes. */
export type SettingsScreenHead = { title: string; onBack?(): void };

/** Names a heading while a screen shows it; what it answers takes the heading back. */
type RegisterSettingsScreenHead = (head: SettingsScreenHead) => () => void;

export const SettingsScreenHeadContext = createContext<RegisterSettingsScreenHead | null>(null);

/**
 * The panel's side: the heading named last of those still shown, so a step
 * inside a screen (a property's "유형" inside "속성 편집") heads the panel
 * while it shows and gives the heading back to its screen as it goes.
 */
export function useSettingsScreenHeads(): [SettingsScreenHead | null, RegisterSettingsScreenHead] {
  const [heads, setHeads] = useState<readonly { id: number; head: SettingsScreenHead }[]>([]);
  const next = useRef(0);
  const register = useCallback<RegisterSettingsScreenHead>((head) => {
    const id = ++next.current;
    setHeads((shown) => [...shown, { id, head }]);
    return () => setHeads((shown) => shown.filter((entry) => entry.id !== id));
  }, []);
  return [heads.at(-1)?.head ?? null, register];
}

/**
 * A screen inside the view settings panel names its heading while it shows,
 * as Notion's grouping screen becomes "← 그룹화 기준" while its properties are
 * listed (seen 2026-10-24); null leaves the panel's own.
 */
export function useSettingsScreenHead(head: SettingsScreenHead | null) {
  const register = useContext(SettingsScreenHeadContext);
  const back = useRef(head?.onBack);
  back.current = head?.onBack;
  const title = head?.title, shown = head !== null, backs = Boolean(head?.onBack);
  useEffect(() => {
    if (!register || !shown || !title) return undefined;
    return register({ title, onBack: backs ? () => back.current?.() : undefined });
  }, [register, shown, title, backs]);
}

/** Whether the screen shows inside the view settings panel, whose heading it can name. */
export function useInSettingsPanel() {
  return useContext(SettingsScreenHeadContext) !== null;
}

/**
 * Opens another screen of the view settings panel from inside one, as the
 * layout's "컴팩트 카드 설정 ›" and that screen's "속성 표시하기" do; null
 * where the screen shows outside the panel.
 */
export const SettingsScreenOpenContext = createContext<((screen: "compactCards" | "properties" | "layout" | "group" | "timelineTable") => void) | null>(null);

export function useOpenSettingsScreen() {
  return useContext(SettingsScreenOpenContext);
}
