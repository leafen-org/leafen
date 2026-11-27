import { useProductPagesSelect } from "@/components/product/productPagesStore";
import { pageLookup } from "@/lib/pageIndex";
import { pageTrashRoot } from "@/lib/pageTrash";
import { dataSourceInTrash } from "@/lib/dataSourcePage";

/**
 * Whether a database shows from the trash: its page is there, itself or with
 * a page above it (a full-page database, an inline one's page), or its data
 * source is, as the server reads the source. Only the answer is selected
 * from the page list, so a change elsewhere in it draws nothing again.
 */
export function useDatabaseInTrash(check: {
  pageId?: string;
  source?: { sourcePageInTrash?: boolean; sourceInTrash?: boolean };
}): boolean {
  const { pageId = "", source } = check;
  const pageInTrash = useProductPagesSelect((pages) => {
    const list = pages?.pages;
    if (!list || !pageId) return false;
    const lookup = pageLookup(list);
    const page = lookup(pageId);
    return Boolean(page && pageTrashRoot(page, lookup));
  });
  return pageInTrash || Boolean(source && dataSourceInTrash(source));
}
