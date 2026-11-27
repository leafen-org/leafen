import type { ProductStatusSink } from "@/lib/productStatus";
import { createContext, useContext, type Dispatch, type SetStateAction } from "react";
import type { ImportDestination } from "@/components/import/ImportDialog";
import type { NativeGraphMutationOutbox, NativePageMutationBrowser } from "@/lib/nativePageMutationBrowser";
import type { PageMadeHereOutbox } from "@/lib/pageMadeHere";
import type { WorkspacePerson } from "@/lib/workspacePeopleChoices";
import type { Bootstrap, Page, Session, Workspace } from "@/types";
import type { DatabaseFavoriteProjection, InlineDatabaseProjection } from "@/workspaceMembershipWire.generated";
import type { DatabaseFavoriteTarget } from "./useDatabaseFavorites";

/**
 * The workspace pages the product shows and how a change to them is kept,
 * for a page body edited away from the page view, such as a database page's.
 */
export interface ProductPages {
  pages: Page[];
  mentionPages: Page[];
  /** The reader's workspaces, which a page may be copied to. */
  workspaces?: Workspace[];
  /** The pages others shared with the reader, which aren't their private pages. */
  sharedPageIds?: readonly string[];
  /** Shared pages everyone in the workspace can open, which the Shared section doesn't list. */
  workspaceWidePageIds?: readonly string[];
  organizationId?: string;
  /** The workspace the reader is in, whose databases an action may reach. */
  workspaceId?: string;
  /** The signed-in session, for screens drawn outside the page views, such as an app's. */
  session?: Session;
  setData: Dispatch<SetStateAction<Bootstrap>>;
  /** Shows what such a body did, where the page view shows its own. */
  onStatus?: ProductStatusSink;
  /** Reads the page list again, once such a body's page blocks changed pages. */
  refreshPages?: () => Promise<unknown>;
  /** Opens the import screen for files that go into the given page. */
  openImport?: (destination: ImportDestination) => void;
  /** Where such a body's structure changes, a page made here and a page's settings wait for a server that can't be reached. */
  mutationOutbox?: NativeGraphMutationOutbox & PageMadeHereOutbox & Pick<NativePageMutationBrowser<unknown>, "admitAndSendMetadata">;
  /** The reader and the workspace's people, whom an action's notification may go to. */
  workspacePeople?: (signal: AbortSignal) => Promise<WorkspacePerson[]>;
  /** A peek's saved date command belongs to the page that opened that peek. */
  recordDateCommand?: (key: string, step: (forward: boolean) => Promise<boolean>) => boolean;
  /** The reader's favorite inline databases, and how one is added or taken out. */
  databaseFavorites?: readonly DatabaseFavoriteProjection[];
  /** The databases the pages hold in their bodies, which the sidebar lists under their page. */
  inlineDatabases?: readonly InlineDatabaseProjection[];
  setDatabaseFavorite?: (target: DatabaseFavoriteTarget, favorite: boolean) => Promise<void>;
}

export const ProductPagesContext = createContext<ProductPages | null>(null);

export function useProductPages() {
  return useContext(ProductPagesContext);
}
