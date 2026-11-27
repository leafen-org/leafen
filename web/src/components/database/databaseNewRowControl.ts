import { createContext, type ReactNode } from "react";

/** What stands for "새로 만들기" at a toolbar's end, where a page has its own: "내 작업"'s "새 작업". */
export const DatabaseNewRowControlContext = createContext<ReactNode>(null);

/** What stands for the "데이터 소스 설정" rows of a view's settings, where a page has its own: "내 작업"'s "작업 출처". */
export const DatabaseSourceSettingsContext = createContext<ReactNode>(null);

/**
 * Whether a view's "데이터 소스 설정" is its "속성 편집" alone, with no "데이터
 * 소스 관리" after it, where its source is no database to change, convert,
 * automate or archive from there: People's, as Notion's owner sees it on
 * People (seen 2026-11-15, three looks in two free workspaces) and in a
 * linked view of it (seen 2026-11-19), which databaseSourceRules.tsx decides.
 */
export const DatabaseSourcePropertiesOnlyContext = createContext(false);

/** What follows the lock in a view's settings, where a page has its own: "내 작업"'s "작업이 보이지 않나요?". */
export const DatabaseSettingsFootContext = createContext<ReactNode>(null);

/**
 * Whether a view's rows take a new one, where a page decides it — "내 작업"
 * only while one of its sources does — or its source does: People's never
 * (databaseSourceRules.tsx). Without it no layout offers one
 * (useViewOffersNewRows).
 */
export const DatabaseRowsAddableContext = createContext(true);

/**
 * Whether a view's rows can be searched, where a page decides it: "내 작업"
 * only once it has a source. Without one its search stays, faded
 * (DatabaseToolbarControls), as Notion's (seen 2026-11-15).
 */
export const DatabaseSearchableContext = createContext(true);

/**
 * What an empty list shows in place of its "+ 새 …" line, where a page has
 * its own: "내 작업"'s "일치하는 작업이 없습니다." with its "+ 새 작업" (Notion's,
 * seen 2026-11-08), given the list's own way to add a row when it takes one.
 */
export const DatabaseEmptyRowsContext = createContext<((addRow?: () => void) => ReactNode) | null>(null);

/**
 * A list row's value a page draws its own way, else nothing: "내 작업"'s
 * source, its database's icon and name.
 */
export const DatabaseOwnValueContext = createContext<((rowId: string, propertyId: string) => ReactNode) | null>(null);
