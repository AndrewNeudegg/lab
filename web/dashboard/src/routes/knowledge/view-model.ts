import type {
  HomelabdKnowledgeReport,
  HomelabdKnowledgeResearchRun,
  HomelabdKnowledgeSource,
  HomelabdKnowledgeSpace
} from '@homelab/shared';

export type KnowledgePanel = 'sources' | 'runs' | 'artefacts';

type KnowledgeSpacesResponseLike = {
  spaces?: HomelabdKnowledgeSpace[] | null;
};

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

export const latestResearchRun = (space?: HomelabdKnowledgeSpace): HomelabdKnowledgeResearchRun | undefined =>
  [...(space?.research_runs || [])].sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))[0];

export const latestAskReport = (space?: HomelabdKnowledgeSpace): HomelabdKnowledgeReport | undefined =>
  [...(space?.reports || [])]
    .filter((report) => report.mode === 'ask')
    .sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))[0];

export const researchRunsExceptSelected = (
  space?: HomelabdKnowledgeSpace,
  selectedRun?: HomelabdKnowledgeResearchRun
): HomelabdKnowledgeResearchRun[] => {
  const selectedID = selectedRun?.id || '';
  return (space?.research_runs || []).filter((run) => run.id !== selectedID);
};

export const knowledgeSpacesFromResponse = (
  response?: KnowledgeSpacesResponseLike | null
): HomelabdKnowledgeSpace[] => {
  if (response?.spaces == null) {
    return [];
  }
  if (!Array.isArray(response.spaces)) {
    throw new TypeError('Knowledge Space response did not include a spaces array.');
  }
  return response.spaces;
};

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
    case 'runs':
      return 'Research';
    case 'artefacts':
      return 'Reports';
    default:
      return 'Sources';
  }
};

export const panelItemCount = (panel: KnowledgePanel, space?: HomelabdKnowledgeSpace) => {
  switch (panel) {
    case 'runs':
      return space?.research_runs?.length || 0;
    case 'artefacts':
      return space?.reports?.length || 0;
    default:
      return spaceSourceCount(space);
  }
};

export const sourceSelectionSummary = (selectedCount: number, totalCount: number) => {
  if (totalCount <= 0) {
    return 'No sources available';
  }
  if (selectedCount <= 0) {
    return 'No sources selected';
  }
  if (selectedCount === totalCount) {
    return `All ${totalCount} ${totalCount === 1 ? 'source' : 'sources'} selected`;
  }
  return `${selectedCount}/${totalCount} sources selected`;
};

export const sourceStatusLabel = (source?: HomelabdKnowledgeSource) => {
  const state = (source?.ingestion?.state || 'ready').trim().toLowerCase();
  switch (state) {
    case 'failed':
      return 'Failed';
    case 'processing':
      return 'Processing';
    case 'ready':
      return 'Ready';
    default:
      return state || 'Ready';
  }
};

export const sourceStatusTone = (source?: HomelabdKnowledgeSource) => {
  const state = (source?.ingestion?.state || 'ready').trim().toLowerCase();
  if (state === 'failed') {
    return 'danger';
  }
  if (state === 'processing') {
    return 'active';
  }
  return 'success';
};

export const researchRunStatusLabel = (run?: HomelabdKnowledgeResearchRun) => {
  const status = (run?.status || 'queued').trim().toLowerCase();
  switch (status) {
    case 'queued':
      return 'Queued';
    case 'planning':
      return 'Planning';
    case 'discovering':
      return 'Discovering';
    case 'retrieving':
      return 'Retrieving';
    case 'reading':
      return 'Reading';
    case 'synthesizing':
      return 'Synthesising';
    case 'reviewing':
      return 'Reviewing';
    case 'completed':
      return 'Completed';
    case 'failed':
      return 'Failed';
    case 'cancelled':
      return 'Cancelled';
    default:
      return status || 'Queued';
  }
};

export const researchRunStatusTone = (run?: HomelabdKnowledgeResearchRun) => {
  const status = (run?.status || 'queued').trim().toLowerCase();
  if (status === 'completed') {
    return 'success';
  }
  if (status === 'failed' || status === 'cancelled') {
    return 'danger';
  }
  return 'active';
};

export const modelProvenanceLabel = (provider?: string, model?: string) => {
  const parts = [provider, model].map((part) => part?.trim()).filter(Boolean);
  return parts.length ? parts.join(' / ') : '';
};

export const knowledgeMarkdownPreview = (value = '', maxLength = 180) => {
  const cleaned = value
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/^#{1,6}\s+/gm, '')
    .replace(/^\s{0,3}>\s?/gm, '')
    .replace(/^\s*[-*+]\s+/gm, '')
    .replace(/[*_~]/g, '')
    .replace(/\s+/g, ' ')
    .trim();
  if (cleaned.length <= maxLength) {
    return cleaned;
  }
  return `${cleaned.slice(0, Math.max(0, maxLength - 1)).trim()}...`;
};
