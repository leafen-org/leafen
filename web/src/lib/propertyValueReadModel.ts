export type PropertyValueSchemaReadOwner = Readonly<{
  request: (databaseIds: readonly (string | undefined)[]) => boolean;
  retire: () => void;
}>;

export function collectPropertyValueSchemaDatabaseIds(
  databaseIds: readonly (string | undefined)[],
) {
  return Array.from(new Set(
    databaseIds
      .map((databaseId) => databaseId?.trim() ?? "")
      .filter(Boolean),
  )).sort();
}
