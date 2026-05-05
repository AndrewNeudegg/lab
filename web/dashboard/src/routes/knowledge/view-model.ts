import type {
  HomelabdKnowledgeEvidence,
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

export const canResumeResearchRun = (run?: HomelabdKnowledgeResearchRun) =>
  (run?.status || '').trim().toLowerCase() === 'failed';

export const modelProvenanceLabel = (provider?: string, model?: string) => {
  const parts = [provider, model].map((part) => part?.trim()).filter(Boolean);
  return parts.length ? parts.join(' / ') : '';
};

const citationLabelPattern = /^[A-Za-z]+\d+(?:\.\d+)?$/;
const groupedCitationPattern =
  /\[([A-Za-z]+\d+(?:\.\d+)?(?:\s*(?:,|;)\s*[A-Za-z]+\d+(?:\.\d+)?)*)\]/g;

const normaliseCitationLabel = (value = '') => value.trim().toUpperCase();

const normaliseCitationGroupSpacing = (value = '') =>
  value.replace(groupedCitationPattern, (_match, group: string) => `[${group.replace(/\s*(,|;)\s*/g, '$1 ')}]`);

export const linkKnowledgeCitations = (
  content = '',
  evidence: HomelabdKnowledgeEvidence[] = [],
  sourceHref: (sourceId: string) => string
) => {
  const labels = new Map<string, string>();
  for (const item of evidence || []) {
    const href = item.source_id ? sourceHref(item.source_id) : '';
    if (item.citation_label && href) {
      labels.set(normaliseCitationLabel(item.citation_label), href);
    }
  }
  if (!labels.size) {
    return content;
  }

  return content.replace(groupedCitationPattern, (match, group: string, offset: number, whole: string) => {
    if (whole.slice(offset + match.length, offset + match.length + 1) === '(') {
      return match;
    }

    const parts = group.split(/(\s*(?:,|;)\s*)/);
    const linked = parts.map((part) => {
      const separator = part.match(/^\s*(,|;)\s*$/);
      if (separator) {
        return `${separator[1]} `;
      }
      if (!citationLabelPattern.test(part.trim())) {
        return part;
      }
      const href = labels.get(normaliseCitationLabel(part));
      return href ? `[${part.trim()}](${href})` : part;
    });

    if (linked.join('') === group) {
      return match;
    }

    return linked.join('');
  });
};

export const knowledgeMarkdownPreview = (value = '', maxLength = 180) => {
  const cleaned = normaliseCitationGroupSpacing(value)
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

const markdownLine = (label: string, value?: string | number) =>
  value === undefined || value === '' ? '' : `- ${label}: ${value}`;

export const knowledgeReportExportMarkdown = (
  report: HomelabdKnowledgeReport,
  run?: HomelabdKnowledgeResearchRun
) => {
  const metadata = [
    markdownLine('Report ID', report.id),
    run ? markdownLine('Research run ID', run.id) : '',
    markdownLine('Mode', report.mode),
    run ? markdownLine('Run status', researchRunStatusLabel(run)) : '',
    run ? markdownLine('Depth', run.depth || 'standard') : '',
    run ? markdownLine('Sources examined', run.sources_examined || 0) : '',
    markdownLine('Evidence chunks', report.evidence?.length || run?.evidence_count || 0),
    markdownLine('Model', modelProvenanceLabel(report.provider, report.model)),
    markdownLine('Total tokens', report.usage?.total_tokens || run?.usage?.total_tokens),
    markdownLine('Created', report.created_at),
    run ? markdownLine('Workspace', run.workspace_path) : ''
  ].filter(Boolean);

  const sections = [`# ${report.question || 'Knowledge report'}`.trim(), '', ...metadata, '', '## Answer', '', report.answer?.trim() || 'No answer recorded.'];

  if (report.key_findings?.length) {
    sections.push('', '## Key Findings', '', ...report.key_findings.map((finding) => `- ${finding}`));
  }

  if (report.gaps?.length) {
    sections.push('', '## Gaps', '', ...report.gaps.map((gap) => `- ${gap}`));
  }

  if (report.evidence?.length) {
    sections.push('', '## Evidence');
    for (const item of report.evidence) {
      sections.push(
        '',
        `### [${item.citation_label || item.id}] ${item.source_title || 'Source'}`,
        markdownLine('Source ID', item.source_id),
        markdownLine('URI', item.source_uri),
        markdownLine('Section', item.section_title),
        markdownLine('Retrieval', item.retrieval),
        markdownLine('Score', item.score),
        '',
        item.excerpt ? `> ${item.excerpt.replace(/\n/g, '\n> ')}` : ''
      );
    }
  }

  return `${sections.filter((section) => section !== undefined).join('\n').replace(/\n{3,}/g, '\n\n').trim()}\n`;
};

const filenameSlug = (value = '') =>
  value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 72) || 'knowledge-report';

export const knowledgeReportExportFilename = (
  report: HomelabdKnowledgeReport,
  extension: 'md' | 'pdf'
) => {
  const idPart = report.id ? `-${compactKnowledgeID(report.id)}` : '';
  return `${filenameSlug(report.question)}${idPart}.${extension}`;
};

const pdfCharacterReplacements: Record<string, string> = {
  '\u2018': "'",
  '\u2019': "'",
  '\u201c': '"',
  '\u201d': '"',
  '\u2013': '-',
  '\u2014': '-',
  '\u2011': '-',
  '\u00a0': ' ',
  '\u2022': '-',
  '\u2192': '->',
  '\u00b9': '1',
  '\u00b2': '2',
  '\u00b3': '3',
  '\u2074': '4',
  '\u2082': '2',
  '\u2083': '3'
};

const asciiPdfText = (value = '') =>
  value.replace(/[^\n\r\t\x20-\x7e]/g, (character) => {
    const replacement = pdfCharacterReplacements[character];
    if (replacement !== undefined) {
      return replacement;
    }
    const normalised = character.normalize('NFKD').replace(/[^\x20-\x7e]/g, '');
    return normalised || '?';
  });

const pdfPlainTextFromMarkdown = (value = '') =>
  asciiPdfText(value)
    .replace(/```[A-Za-z0-9_.+-]*\n([\s\S]*?)```/g, '$1')
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/^#{1,6}\s+/gm, '')
    .replace(/^\s{0,3}>\s?/gm, '  ')
    .replace(/^\s*[-*+]\s+/gm, '- ')
    .replace(/\*\*([^*\n]+)\*\*/g, '$1')
    .replace(/__([^_\n]+)__/g, '$1')
    .replace(/(^|[\s([])\*([^*\n]+)\*/g, '$1$2')
    .replace(/(^|[\s([])_([^_\n]+)_/g, '$1$2');

const wrapPdfLine = (line: string, maxLength: number) => {
  const words = line.replace(/\t/g, '  ').split(/\s+/).filter(Boolean);
  if (!words.length) {
    return [''];
  }
  const wrapped: string[] = [];
  let current = '';
  for (const word of words) {
    if (word.length > maxLength) {
      if (current) {
        wrapped.push(current);
        current = '';
      }
      for (let index = 0; index < word.length; index += maxLength) {
        wrapped.push(word.slice(index, index + maxLength));
      }
      continue;
    }
    const next = current ? `${current} ${word}` : word;
    if (next.length > maxLength) {
      wrapped.push(current);
      current = word;
    } else {
      current = next;
    }
  }
  if (current) {
    wrapped.push(current);
  }
  return wrapped;
};

const escapePdfString = (value: string) =>
  value.replaceAll('\\', '\\\\').replaceAll('(', '\\(').replaceAll(')', '\\)');

export const knowledgeReportExportPdf = (
  report: HomelabdKnowledgeReport,
  run?: HomelabdKnowledgeResearchRun
) => {
  const text = pdfPlainTextFromMarkdown(knowledgeReportExportMarkdown(report, run));
  const lines = text.split('\n').flatMap((line) => wrapPdfLine(line, 94));
  const linesPerPage = 52;
  const pages: string[][] = [];
  for (let index = 0; index < lines.length; index += linesPerPage) {
    pages.push(lines.slice(index, index + linesPerPage));
  }
  if (!pages.length) {
    pages.push(['Knowledge report']);
  }

  const objects: string[] = [];
  objects[0] = '<< /Type /Catalog /Pages 2 0 R >>';
  objects[2] = '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>';

  let nextObjectNumber = 4;
  const pageObjectNumbers: number[] = [];
  for (const pageLines of pages) {
    const pageObjectNumber = nextObjectNumber++;
    const contentObjectNumber = nextObjectNumber++;
    pageObjectNumbers.push(pageObjectNumber);
    const content = [
      'BT',
      '/F1 10 Tf',
      '54 738 Td',
      '13 TL',
      ...pageLines.map((line) => `(${escapePdfString(line)}) Tj T*`),
      'ET'
    ].join('\n');
    objects[pageObjectNumber - 1] =
      `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents ${contentObjectNumber} 0 R >>`;
    objects[contentObjectNumber - 1] = `<< /Length ${content.length} >>\nstream\n${content}\nendstream`;
  }

  objects[1] = `<< /Type /Pages /Kids [${pageObjectNumbers.map((number) => `${number} 0 R`).join(' ')}] /Count ${pageObjectNumbers.length} >>`;

  let pdf = '%PDF-1.4\n';
  const offsets = [0];
  for (let index = 0; index < objects.length; index += 1) {
    offsets[index + 1] = pdf.length;
    pdf += `${index + 1} 0 obj\n${objects[index] || '<<>>'}\nendobj\n`;
  }
  const xrefOffset = pdf.length;
  pdf += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  for (let index = 1; index <= objects.length; index += 1) {
    pdf += `${String(offsets[index]).padStart(10, '0')} 00000 n \n`;
  }
  pdf += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xrefOffset}\n%%EOF\n`;
  return pdf;
};
