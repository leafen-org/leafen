import type { Bootstrap, Page } from "@/types";

export function upsertBootstrapPage(current: Bootstrap, incoming: Page): Bootstrap {
  const previous = current.pages.find((candidate) => candidate.id === incoming.id);
  // A page's own answers don't say whether its body holds anything, so the
  // known answer stays; a page arriving under a parent is a block in the
  // parent's body, which then holds something.
  const page = incoming.hasContent === undefined && previous?.hasContent !== undefined
    ? { ...incoming, hasContent: previous.hasContent }
    : incoming;
  const newParentId = page.parentId && page.parentId !== previous?.parentId && !page.inTrash ? page.parentId : null;
  const removesWikiRoot = Boolean(
    previous?.isWiki
    && previous.wikiRootId === previous.id
    && !page.isWiki
    && !page.wikiRootId,
  );
  return {
    ...current,
    pages: current.pages.some((item) => item.id === page.id)
      ? current.pages.map((item) => {
          if (item.id === page.id) return page;
          if (item.id === newParentId && item.hasContent === false) return { ...item, hasContent: true };
          if (!removesWikiRoot || item.wikiRootId !== page.id) return item;
          return {
            ...item,
            isWiki: false,
            wikiRootId: undefined,
            wikiRevision: 0,
            verifiedAt: "",
            verifiedBy: "",
            verificationExpiresAt: "",
          };
        })
      : [...current.pages.map((item) => item.id === newParentId && item.hasContent === false ? { ...item, hasContent: true } : item), page],
  };
}
