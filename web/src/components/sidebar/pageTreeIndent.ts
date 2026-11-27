/**
 * Where a sidebar tree row starts: 8px in, then the tree's own start
 * (--tree-base-indent, a teamspace's pages on a wide touch screen) and one
 * step per level — 8px as Notion's desktop sidebar (seen 2026-10-17), a
 * phone's 18px and a wide touch screen's 22px (--tree-level-indent).
 */
export function treeRowIndent(depth: number) {
  return `calc(8px + var(--tree-base-indent, 0px) + ${depth} * var(--tree-level-indent, 8px))`;
}

/**
 * "하위 페이지 없음" under an open row at `depth` starts 6px before the row's
 * own title, as Notion's (seen 2026-10-17: 32 under a top row at 1440); a
 * touch screen's wider leading moves it (--tree-leading-extra — 30 under a
 * top row on Notion's iPad, seen 2026-11-17). Page rows and the favorites'
 * database rows and templates share it.
 */
export function treeEmptyChildrenIndent(depth: number) {
  return `calc(${treeRowIndent(depth)} + 24px + var(--tree-leading-extra, 0px))`;
}
