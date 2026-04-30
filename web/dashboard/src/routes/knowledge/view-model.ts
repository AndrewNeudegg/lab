import type { HomelabdKnowledgeReport, HomelabdKnowledgeSpace } from '@homelab/shared';

export type KnowledgePanel = 'sources' | 'research' | 'reports';

export const compactKnowledgeID = (id = '') => {
  const trimmed = id.trim();
  if (!trimmed) {
    return 'space';
  }
  const parts = trimmed.split('_');
  return parts.length > 1 ? parts[parts.length - 1] : trimmed.slice(-8);
};

export const spaceSourceCount = (space?: HomelabdKnowledgeSpace) =>
  space?.insight?.source_count ?? space?.sources?.length ?? 0;

export const spaceWordCount = (space?: HomelabdKnowledgeSpace) =>
  space?.insight?.word_count ?? (space?.sources || []).reduce((total, source) => total + (source.word_count || 0), 0);

export const latestReport = (space?: HomelabdKnowledgeSpace): HomelabdKnowledgeReport | undefined =>
  [...(space?.reports || [])].sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))[0];

export const filterKnowledgeSpaces = (
  spaces: HomelabdKnowledgeSpace[],
  search: string
): HomelabdKnowledgeSpace[] => {
  const query = search.trim().toLowerCase();
  const sorted = [...spaces].sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at));
  if (!query) {
    return sorted;
  }
  return sorted.filter((space) => {
    const haystack = [
      space.title,
      space.description,
      space.objective,
      ...(space.insight?.key_terms || []),
      ...(space.sources || []).map((source) => source.title)
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();
    return haystack.includes(query);
  });
};

export const selectKnowledgeSpace = (
  spaces: HomelabdKnowledgeSpace[],
  visibleSpaces: HomelabdKnowledgeSpace[],
  selectedSpaceId: string,
  routedSpaceId = ''
) => {
  const routed = routedSpaceId.trim();
  if (routed && spaces.some((space) => space.id === routed)) {
    return routed;
  }
  if (selectedSpaceId && spaces.some((space) => space.id === selectedSpaceId)) {
    return selectedSpaceId;
  }
  return visibleSpaces[0]?.id || spaces[0]?.id || '';
};

export const panelLabel = (panel: KnowledgePanel) => {
  switch (panel) {
    case 'research':
      return 'Research';
    case 'reports':
      return 'Reports';
    default:
      return 'Sources';
  }
};
