import { createContext, useCallback, useContext, useLayoutEffect, useState, useSyncExternalStore, type ReactNode } from "react";
import { ProductPagesContext, type ProductPages } from "./productPagesContext";

type ProductPagesStore = {
  current: ProductPages | null;
  listeners: Set<() => void>;
  subscribe(listener: () => void): () => void;
};

const ProductPagesStoreContext = createContext<ProductPagesStore | null>(null);
const noStore = () => () => undefined;

/**
 * The product pages for the app, as a context and as a store a drawing can
 * take one answer from (useProductPagesSelect): a database drawn with its
 * page's title, or with its linked sources' places, is drawn again when
 * those change, not whenever the page list is read again (2026-11-11: each
 * list refresh drew a table of 100,000 rows again).
 */
export function ProductPagesProvider({ value, children }: { value: ProductPages; children: ReactNode }) {
  const [store] = useState<ProductPagesStore>(() => {
    const listeners = new Set<() => void>();
    return {
      current: value, listeners,
      subscribe(listener) {
        listeners.add(listener);
        return () => listeners.delete(listener);
      },
    };
  });
  // What draws now reads the list given now; what doesn't hears of it once drawn.
  store.current = value;
  useLayoutEffect(() => {
    for (const listener of [...store.listeners]) listener();
  }, [store, value]);
  return <ProductPagesContext.Provider value={value}><ProductPagesStoreContext.Provider value={store}>
    {children}
  </ProductPagesStoreContext.Provider></ProductPagesContext.Provider>;
}

/** One answer from the product pages, which draws again only when it changes. */
export function useProductPagesSelect<T>(select: (pages: ProductPages | null) => T): T {
  const store = useContext(ProductPagesStoreContext);
  return useSyncExternalStore(store ? store.subscribe : noStore, () => select(store?.current ?? null));
}

/**
 * The product pages as they are when asked, for what a press does with them
 * (an import opened, a guest's choice): asking draws nothing again as the
 * list changes.
 */
export function useProductPagesNow(): () => ProductPages | null {
  const store = useContext(ProductPagesStoreContext);
  return useCallback(() => store?.current ?? null, [store]);
}
