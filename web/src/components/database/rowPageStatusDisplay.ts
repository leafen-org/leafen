import { propertyFirstInUnorderedPages } from "./rowPropertiesModel";

/**
 * A status shown on its database's pages as a checkbox: Notion's "다음과 같이
 * 표시: 체크박스" chosen in "속성 편집" from a page's property menu (seen
 * 2026-11-16 from a side peek and from a full page opened by its address,
 * "선택" by default). It is the pages' own, every page of the database
 * showing it so; the table the side peek was opened from kept its column as
 * it was. So it is held on the property, as its place in the pages' order is
 * (rowPropertiesModel.ts), not on a view as a table's column is
 * (tablePropertyMenuModel.ts `statusCheckboxColumns`).
 */
const PAGE_STATUS_CHECKBOX = "pageStatusCheckbox";

/** Whether the database's pages show this status as a checkbox. */
export function pageShowsStatusCheckbox(property: { type?: string; config?: unknown }): boolean {
  return property.type === "status" && (property.config as Record<string, unknown> | undefined)?.[PAGE_STATUS_CHECKBOX] === true;
}

/**
 * The property its pages show as a checkbox, or as its options again; shown
 * as a checkbox, it leads its pages' properties where the database keeps no
 * page order yet (propertyFirstInUnorderedPages), as Notion's.
 */
export function propertyAfterPageStatusDisplay<T extends { config: Record<string, unknown> }>(
  property: T, checkbox: boolean, properties: readonly { config?: unknown }[],
): T {
  const { [PAGE_STATUS_CHECKBOX]: _kept, ...config } = property.config;
  return checkbox ? propertyFirstInUnorderedPages({ ...property, config: { ...config, [PAGE_STATUS_CHECKBOX]: true } }, properties)
    : { ...property, config };
}
