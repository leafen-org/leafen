import { DatabaseSourcePicker } from "@/components/editor/DatabaseSourcePicker";
import type { useAuthenticatedEditorActions } from "./useAuthenticatedEditorActions";

type PageBodyDatabaseActions = Pick<
  ReturnType<typeof useAuthenticatedEditorActions>,
  "createDatabase" | "linkDatabase" | "loadDatabaseSources"
>;

/**
 * What a page body's inline database choice opens: a new database, or a
 * view of a database the reader can see, added to the body. A page's and a
 * database page's body offer it alike.
 */
export function PageBodyDatabasePicker({ actions, onClose }: {
  actions: PageBodyDatabaseActions;
  onClose: () => void;
}) {
  return (
    <DatabaseSourcePicker
      type="inline_database"
      loadSources={actions.loadDatabaseSources}
      onClose={onClose}
      onCreate={() => {
        onClose();
        actions.createDatabase();
      }}
      onLink={(source) => { void actions.linkDatabase(source); }}
    />
  );
}
