import type { AssistantActivity, AssistantCapability, AssistantUXPattern } from '@homelab/shared';

const areaLabels: Record<string, string> = {
  communication: 'Communication',
  execution: 'Execution',
  focus: 'Daily focus',
  memory: 'Memory',
  planning: 'Planning',
  research: 'Research',
  systems: 'Systems'
};

const autonomyLabels: Record<string, string> = {
  automatic: 'Automatic after approval',
  confirm: 'Act with confirmation',
  observe: 'Observe and suggest',
  plan: 'Plan and propose'
};

export const assistantAreaLabel = (area = '') => areaLabels[area] || area.replaceAll('-', ' ');

export const assistantAutonomyLabel = (autonomy = '') =>
  autonomyLabels[autonomy] || autonomy.replaceAll('_', ' ');

export const assistantAutonomyTone = (autonomy = '') => {
  switch (autonomy) {
    case 'automatic':
      return 'red';
    case 'confirm':
      return 'amber';
    case 'plan':
      return 'blue';
    default:
      return 'green';
  }
};

export const selectAssistantCapability = (
  capabilities: AssistantCapability[],
  selectedCapabilityId: string
) =>
  capabilities.find((capability) => capability.id === selectedCapabilityId) || capabilities[0];

export const patternsForCapability = (
  capability: AssistantCapability | undefined,
  patterns: AssistantUXPattern[]
) => {
  const ids = new Set(capability?.ux_pattern_ids || []);
  return patterns.filter((pattern) => ids.has(pattern.id));
};

export const activityCountForCapability = (
  capability: AssistantCapability | undefined,
  activities: AssistantActivity[]
) => {
  if (!capability) {
    return 0;
  }
  return activities.filter((activity) => activity.capability_ids.includes(capability.id)).length;
};

export const primaryCapabilityForActivity = (
  activity: AssistantActivity | undefined,
  capabilities: AssistantCapability[]
) => {
  const ids = new Set(activity?.capability_ids || []);
  return capabilities.find((capability) => ids.has(capability.id));
};

export const activityForCapability = (
  capability: AssistantCapability | undefined,
  activities: AssistantActivity[]
) => activities.find((activity) => Boolean(capability && activity.capability_ids.includes(capability.id)));
