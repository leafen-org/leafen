import { i18next } from "@/i18n";

// A new Status has Notion's three groups with one option each, named in the
// language of the person who makes it. Its first option has no color chosen
// and is drawn in its group's grey, as Notion's (seen 2026-11-02).
const STATUS_DEFAULTS = [
  { group: "todo", color: "gray", option: "todo", optionColor: "gray", fromGroup: true },
  { group: "inProgress", color: "blue", option: "doing", optionColor: "blue" },
  { group: "complete", color: "green", option: "done", optionColor: "green" },
] as const;

export function statusDefaultsForLanguage(language = "en") {
  const label = (key: string) => String(i18next.t(`common:generated.${key}`, { lng: language }));
  return STATUS_DEFAULTS.map((status) => ({
    name: label(`statusGroups.${status.group}`),
    color: status.color,
    option: { name: label(`statusOptions.${status.option}`), color: status.optionColor, fromGroup: "fromGroup" in status },
  }));
}

export function createDefaultStatusConfig(
  idFactory: () => string,
  language = "en",
) {
  if (typeof idFactory !== "function") {
    throw new TypeError("createDefaultStatusConfig requires an id factory");
  }
  const definitions = statusDefaultsForLanguage(language);
  const options = definitions.map((group) => ({
    id: idFactory(),
    name: group.option.name,
    color: group.option.color,
    description: null,
    group: group.name,
    ...(group.option.fromGroup ? { colorFromGroup: true } : {}),
  }));
  const statusGroups = definitions.map((group, index) => ({
    id: idFactory(),
    name: group.name,
    color: group.color,
    optionIds: [options[index].id],
  }));
  return { options, statusGroups };
}

type CarriedOption = { id: string; name: string; color?: string; description?: string | null };

/**
 * A property made a status from another type, as Notion's "유형 변경" makes it
 * (seen 2026-11-08): a new status's three groups and options. A select's or
 * multi-select's options come along into "To-do" after its first, each in
 * place, one named as a new option is (시작 전, 진행 중, 완료) taking that
 * one's place and group with its own color (one drawn in its group's before,
 * as a status's made a select is, still is); such a status has no default
 * (default_option null), so its rows that set none read empty. Any other
 * type's status reads its first option while a row sets none. The server
 * gives a status asked for without groups the same (databaseStatusConfigMadeFrom).
 */
export function statusConfigMadeFrom(
  fromType: string | undefined,
  carried: readonly CarriedOption[],
  idFactory: () => string,
  language = "en",
) {
  const made = createDefaultStatusConfig(idFactory, language);
  if (fromType !== "select" && fromType !== "multi_select") return made;
  const taken = new Map<number, CarriedOption>();
  const rest: CarriedOption[] = [];
  for (const option of carried) {
    const at = made.options.findIndex((candidate, index) => candidate.name === option.name && !taken.has(index));
    if (at >= 0) taken.set(at, option);
    else rest.push(option);
  }
  const placed = made.options.map((option, index) => {
    const own = taken.get(index);
    return own ? { ...own, color: own.color ?? option.color, description: own.description ?? null, group: option.group } : option;
  });
  const toDo = made.statusGroups[0];
  const joined = rest.map((option) => {
    const { colorFromGroup: _fromGroup, ...kept } = option as CarriedOption & { colorFromGroup?: unknown; group?: unknown };
    return { ...kept, color: option.color ?? "default", description: option.description ?? null, group: toDo.name };
  });
  return {
    options: [placed[0], ...joined, ...placed.slice(1)],
    statusGroups: made.statusGroups.map((group, index) => ({
      ...group,
      optionIds: index === 0 ? [placed[0].id, ...joined.map((option) => option.id)] : [placed[index].id],
    })),
    defaultOptionId: "",
  };
}
