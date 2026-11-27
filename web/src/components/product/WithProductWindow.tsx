import type { ReactNode } from "react";
import { type ProductWindowStore, useProductWindow } from "@/lib/productWindows";

/** One window over the product, drawn from its store as it opens and closes; the product is not drawn again then (lib/productWindows.ts). */
export function WithProductWindow<T>({ store, children }: { store: ProductWindowStore<T>; children: (value: T | null) => ReactNode }) {
  return <>{children(useProductWindow(store))}</>;
}
