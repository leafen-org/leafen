import type { Page } from "@/types";

type ImportedNotionUiPage = Page & {
  isFavorite?: boolean;
  kind?: string;
  parentType?: string;
  position?: number;
  properties?: Record<string, unknown>;
};

export function isSyntheticNotionImportRootPage(page: ImportedNotionUiPage) {
  const properties = page.properties ?? {};
  return (
    page.kind === "page" &&
    typeof properties.notionImportJobId === "string" &&
    typeof properties.notionPageId !== "string" &&
    typeof properties.notionDatabaseId !== "string" &&
    typeof properties.notionDataSourceId !== "string"
  );
}
