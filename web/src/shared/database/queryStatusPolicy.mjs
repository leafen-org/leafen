function record(value) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value : null;
}

function nonempty(value) {
  return typeof value === 'string' && value.trim() ? value.trim() : '';
}

export function statusOptionsFromConfig(config) {
  const options = record(config)?.options;
  if (!Array.isArray(options)) return [];
  return options.flatMap((value) => {
    const option = record(value);
    const id = nonempty(option?.id);
    const name = nonempty(option?.name);
    if (!id || !name) return [];
    return [{
      ...option,
      id,
      name,
      color: nonempty(option.color) || 'default',
    }];
  });
}

export function statusGroupsFromConfig(config) {
  const source = record(config);
  const notion = record(source?.notion);
  const notionStatus = record(notion?.status);
  const rawGroups = Array.isArray(source?.statusGroups)
    ? source.statusGroups
    : Array.isArray(notionStatus?.groups)
      ? notionStatus.groups
      : [];
  const seen = new Set();
  return rawGroups.slice(0, 100).flatMap((value) => {
    const group = record(value);
    const id = nonempty(group?.id);
    const name = nonempty(group?.name);
    if (!id || !name || seen.has(id)) return [];
    seen.add(id);
    const rawOptionIds = Array.isArray(group.optionIds)
      ? group.optionIds
      : Array.isArray(group.option_ids)
        ? group.option_ids
        : [];
    const optionIds = Array.from(new Set(rawOptionIds.map(nonempty).filter(Boolean))).slice(0, 100);
    return [{
      id,
      name,
      color: nonempty(group.color) || 'default',
      optionIds,
    }];
  });
}

export function statusFilterOptionIds(config, value) {
  const rawValues = Array.isArray(value) ? value : [value];
  const options = statusOptionsFromConfig(config);
  const groups = statusGroupsFromConfig(config);
  const groupByName = new Map();
  const groupByLowerId = new Map();
  const groupByLowerName = new Map();
  for (const group of groups) {
    if (!groupByName.has(group.name)) groupByName.set(group.name, group);
    const lowerId = group.id.toLowerCase();
    const lowerName = group.name.toLowerCase();
    if (!groupByLowerId.has(lowerId)) groupByLowerId.set(lowerId, group);
    if (!groupByLowerName.has(lowerName)) groupByLowerName.set(lowerName, group);
  }
  const optionByLowerId = new Map();
  const optionByLowerName = new Map();
  const optionIdsByGroup = new Map(groups.map((group) => [group.id, []]));
  for (const option of options) {
    const lowerId = option.id.toLowerCase();
    const lowerName = option.name.toLowerCase();
    if (!optionByLowerId.has(lowerId)) optionByLowerId.set(lowerId, option);
    if (!optionByLowerName.has(lowerName)) optionByLowerName.set(lowerName, option);
    let group = groupByName.get(nonempty(option.group));
    if (!group) group = groups.find((candidate) => candidate.optionIds.includes(option.id));
    group ??= groups[0];
    if (group) optionIdsByGroup.get(group.id)?.push(option.id);
  }
  const resolved = new Set();
  for (const valueItem of rawValues) {
    const raw = nonempty(valueItem);
    if (!raw) continue;
    const lower = raw.toLowerCase();
    const exactOption = optionByLowerId.get(lower) ?? optionByLowerName.get(lower);
    if (exactOption) {
      resolved.add(exactOption.id);
      continue;
    }
    const exactGroup = groupByLowerId.get(lower) ?? groupByLowerName.get(lower);
    if (exactGroup) {
      for (const optionId of optionIdsByGroup.get(exactGroup.id) ?? []) resolved.add(optionId);
      continue;
    }
    resolved.add(raw);
  }
  return Array.from(resolved);
}
