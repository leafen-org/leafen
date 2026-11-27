// The row's tools by the page's text column, which a full-width database's
// row reaches past (Notion's, seen 2026-11-03 with the sidebar open, 1000 to
// 768px): from 480px all of them; under it settings and "새로 만들기 ⌄"; under
// 320px settings alone.
export const NARROW_DATABASE_ROW = 480;
export const CRAMPED_DATABASE_ROW = 320;
const COLUMN_CRAMPED_ROW = 400;

/**
 * The row's width as far as its tools go by it: the least of its step
 * (cramped, narrow or all), so the toolbar is drawn again only as the row
 * crosses a step, not for each width the row takes as the page widens or
 * narrows (the sidebar folding, a side peek's edge dragged — each frame
 * drew every database's toolbar again and laid the page out after it).
 */
function rowWidthStep(width: number) {
  if (width <= 0) return 0;
  return width < CRAMPED_DATABASE_ROW ? CRAMPED_DATABASE_ROW - 1 : width < NARROW_DATABASE_ROW ? NARROW_DATABASE_ROW - 1 : NARROW_DATABASE_ROW;
}

/**
 * Reads the width a database's tools go by from its title row: the element
 * whose size also moves it (`column`, watched beside the row) and the
 * reading, by its step (rowWidthStep).
 */
export function databaseToolsRowWidth(row: HTMLElement): { column: HTMLElement | null; read(): number } {
  // Notion's block box: the text column and its 6px either side, an inline
  // row sitting 4px in from the column and a full-width one cut back to it.
  // In a page's column block its own row is all the room it has, and a
  // 337px column already leaves settings alone (Notion's, seen 2026-11-03).
  const inColumn = Boolean(row.closest("[data-column-id]"));
  const column = inColumn ? null : row.closest<HTMLElement>("[data-native-editor-content]");
  // A row page's data tab, under the title: Notion's tab row is its
  // database's box, tabs and tools both 8 in, and folds at 480 of it (seen
  // 2026-11-24 twice in Chrome: a 479px row at 686 and 477 at 684 kept
  // "새로 만들기 ⌄", 480 at 687 all its tools); its view is that box less the
  // 8 before the text (rowPageTabDatabaseBox.css), and moves as the text does.
  const tabText = column || inColumn ? null : row.closest<HTMLElement>("[data-row-layout-tab-panel]");
  const tabBox = tabText ? row.closest<HTMLElement>("[data-database-view]") : null;
  return {
    column: column ?? tabText,
    read: () => rowWidthStep(column ? Math.min(column.clientWidth, row.clientWidth + 8) + 12
      : tabBox ? tabBox.clientWidth + 8
      : inColumn && row.clientWidth < COLUMN_CRAMPED_ROW ? Math.min(row.clientWidth, CRAMPED_DATABASE_ROW - 1) : row.clientWidth),
  };
}
