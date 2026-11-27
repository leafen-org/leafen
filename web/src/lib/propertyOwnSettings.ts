const PROPERTY_OWN_SETTINGS = ["hideWhenEmpty", "hideInPagePanel", "description", "icon", "pagePosition"] as const;

/**
 * A property's settings that are its own whatever its type: how a page shows
 * it, its description, its icon and its place in its database's page order
 * (pagePosition, rowPropertiesModel.ts). A change of type, by any way, keeps
 * them; the server keeps them alike (database_property_own_settings.go).
 */
export function propertyOwnSettings(config: object | undefined): Record<string, unknown> {
  const own = (config ?? {}) as Record<string, unknown>;
  return Object.fromEntries(PROPERTY_OWN_SETTINGS.flatMap((key) => own[key] !== undefined ? [[key, own[key]]] : []));
}
