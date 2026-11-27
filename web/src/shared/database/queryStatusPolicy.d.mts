export function statusOptionsFromConfig(
  config: unknown,
): Array<Record<string, unknown> & { id: string; name: string; color: string }>;
export function statusGroupsFromConfig(
  config: unknown,
): Array<Record<string, unknown> & { id: string; name: string; color: string; optionIds: string[] }>;
export function statusFilterOptionIds(config: unknown, value: unknown): string[];
