<script lang="ts">
  import { browser } from '$app/environment';
  import { afterNavigate, goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { onMount, tick } from 'svelte';
  import ResearchPromptList from './ResearchPromptList.svelte';
  import {
    createHomelabdClient,
    knowledgeSpaceURL,
    Markdown,
    Navbar,
    type HomelabdKnowledgeAskResult,
    type HomelabdKnowledgeEvidence,
    type HomelabdKnowledgeReport,
    type HomelabdKnowledgeResearchRun,
    type HomelabdKnowledgeSpace
  } from '@homelab/shared';
  import {
    canResumeResearchRun,
    compactKnowledgeID,
    filterKnowledgeSpaces,
    knowledgeMarkdownPreview,
    knowledgeSpacesFromResponse,
    latestReport,
    modelProvenanceLabel,
    panelLabel,
    panelItemCount,
    researchRunStatusLabel,
    researchRunStatusTone,
    selectKnowledgeSpace,
    spaceSourceCount,
    spaceWordCount,
    sourceStatusLabel,
    sourceStatusTone,
    sourceSelectionSummary,
    type KnowledgePanel
  } from './view-model';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const client = createHomelabdClient({ baseUrl: apiBase });
  const panels: KnowledgePanel[] = ['sources', 'runs', 'artefacts'];

  let spaces: HomelabdKnowledgeSpace[] = [];
  let selectedSpaceId = '';
  let lastAppliedRouteSpaceId = '';
  let lastAppliedAnchorId = '';
  let lastSelectedSpaceId = '';
  let activePanel: KnowledgePanel = 'sources';
  let search = '';
  let loading = false;
  let creating = false;
  let addingSource = false;
  let asking = false;
  let creatingRun = false;
  let querying = false;
  let updatingSpace = false;
  let deletingSpace = false;
  let deletingSourceId = '';
  let resumingRunId = '';
  let error = '';
  let notice = '';
  let lastRefresh = '';
  let selectedSourceIds: string[] = [];
  let ready = false;
  let detailEl: HTMLElement | undefined;
  let createSpaceOpen = false;
  let addSourceOpen = false;
  let editingSpace = false;
  let confirmDeleteSpace = false;
  let confirmDeleteSourceId = '';
  let mobileSpacesOpen = false;
  let mobileOptionsOpen = false;
  let researchFormOpen = true;

  let titleDraft = '';
  let objectiveDraft = '';
  let descriptionDraft = '';
  let editTitleDraft = '';
  let editObjectiveDraft = '';
  let editDescriptionDraft = '';
  let sourceTitleDraft = '';
  let sourceKindDraft = 'text';
  let sourceURIDraft = '';
  let sourceContentDraft = '';
  let corpusQueryDraft = '';
  let questionDraft = '';
  let researchActionDraft = 'research';
  let researchModeDraft = 'research';
  let researchDepthDraft = 'standard';
  let runObjectiveDraft = '';
  let discoverSourcesDraft = true;
  let activeReport: HomelabdKnowledgeReport | undefined;
  let activeAskResult: HomelabdKnowledgeAskResult | undefined;
  let activeRun: HomelabdKnowledgeResearchRun | undefined;
  let highlightedSourceId = '';

  let visibleSpaces: HomelabdKnowledgeSpace[] = [];
  let selectedSpace: HomelabdKnowledgeSpace | undefined;
  let latestSelectedReport: HomelabdKnowledgeReport | undefined;
  let latestSelectedRun: HomelabdKnowledgeResearchRun | undefined;
  let selectedRunReport: HomelabdKnowledgeReport | undefined;
  let displayedAskResult: HomelabdKnowledgeAskResult | undefined;
  let totalSourceCount = 0;
  let selectedSourceCount = 0;
  let selectedSourceSummary = '';
  let researchRunSourceSummary = '';
  let canStartResearchRun = false;
  let totalReportCount = 0;
  let totalSpaceSourceCount = 0;
  let sourceReady = false;
  let sourceReferenceAccepted = false;
  let canSubmitKnowledgeAction = false;

  $: visibleSpaces = filterKnowledgeSpaces(spaces, search);
  $: selectedSpaceId = selectKnowledgeSpace(
    spaces,
    visibleSpaces,
    selectedSpaceId,
    browser ? currentRouteSpaceId() : ''
  );
  $: selectedSpace = spaces.find((space) => space.id === selectedSpaceId);
  $: latestSelectedReport = activeReport || latestReport(selectedSpace);
  $: displayedAskResult = activeAskResult;
  $: latestSelectedRun = activeRun;
  $: selectedRunReport = reportForRun(selectedSpace, latestSelectedRun);
  $: totalSourceCount = selectedSpace?.sources?.length || 0;
  $: selectedSourceCount = selectedSourceIds.length;
  $: selectedSourceSummary = sourceSelectionSummary(selectedSourceCount, totalSourceCount);
  $: researchRunSourceSummary = discoverSourcesDraft
    ? `${selectedSourceSummary}; web and academic discovery will gather and evaluate sources`
    : selectedSourceSummary;
  $: canStartResearchRun = !!runObjectiveDraft.trim() && (discoverSourcesDraft || selectedSourceIds.length > 0);
  $: canSubmitKnowledgeAction =
    researchActionDraft === 'ask'
      ? !!questionDraft.trim() && selectedSourceIds.length > 0
      : researchActionDraft === 'search'
        ? !!corpusQueryDraft.trim() && selectedSourceIds.length > 0
        : canStartResearchRun;
  $: totalReportCount = spaces.reduce((total, space) => total + (space.reports?.length || 0), 0);
  $: totalSpaceSourceCount = spaces.reduce((total, space) => total + spaceSourceCount(space), 0);
  $: sourceReferenceAccepted = sourceKindDraft.trim() === 'url';
  $: sourceReady =
    sourceReferenceAccepted
      ? !!sourceURIDraft.trim() || !!sourceContentDraft.trim()
      : !!sourceContentDraft.trim();
  $: if (activeRun && selectedSpace?.research_runs?.length) {
    const refreshedRun = selectedSpace.research_runs.find((run) => run.id === activeRun?.id);
    if (refreshedRun && refreshedRun !== activeRun) {
      activeRun = refreshedRun;
    }
  }
  $: if (activeReport && selectedSpace?.reports?.length) {
    const refreshedReport = selectedSpace.reports.find((report) => report.id === activeReport?.id);
    if (refreshedReport && refreshedReport !== activeReport) {
      activeReport = refreshedReport;
    }
  }
  $: if (selectedSpace && selectedSpace.id !== lastSelectedSpaceId) {
    lastSelectedSpaceId = selectedSpace.id;
    activeReport = undefined;
    activeAskResult = undefined;
    activeRun = undefined;
    editingSpace = false;
    confirmDeleteSpace = false;
    confirmDeleteSourceId = '';
    mobileSpacesOpen = false;
    mobileOptionsOpen = false;
    highlightedSourceId = '';
    researchFormOpen = !(selectedSpace.research_runs?.length);
    selectedSourceIds = (selectedSpace.sources || []).map((source) => source.id);
    addSourceOpen = !(selectedSpace.sources?.length);
  }

  const currentRouteSpaceId = () => (browser ? $page.url.searchParams.get('space') || '' : '');

  const routeSpaceIdFromLocation = () =>
    typeof window !== 'undefined'
      ? new URL(window.location.href).searchParams.get('space') || ''
      : '';

  const currentRoutePath = () =>
    browser ? `${$page.url.pathname}${$page.url.search}${$page.url.hash}` : '';

  const currentRouteHash = () => (browser ? $page.url.hash.replace(/^#/, '') : '');

  const syncTimeLabel = () =>
    new Date().toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    });

  const compactTime = (value?: string) => {
    if (!value) {
      return 'unknown';
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return value;
    }
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  };

  const evidenceTraceLabel = (evidence: HomelabdKnowledgeEvidence) => {
    const method = evidence.retrieval ? `${evidence.retrieval} retrieval` : 'retrieval';
    const scores = [
      evidence.lexical_score !== undefined ? `lexical ${evidence.lexical_score}` : '',
      evidence.semantic_score !== undefined ? `semantic ${evidence.semantic_score}` : ''
    ].filter(Boolean);
    return scores.length ? `${method}; ${scores.join(', ')}` : method;
  };

  const plural = (count: number, singular: string, pluralLabel = `${singular}s`) =>
    `${count} ${count === 1 ? singular : pluralLabel}`;

  const compactPanelLabel = (panel: KnowledgePanel) => (panel === 'artefacts' ? 'Reports' : panelLabel(panel));

  const anchorPart = (value = '') => value.trim().replace(/[^A-Za-z0-9_-]+/g, '-') || 'item';

  const knowledgeAnchorId = (...parts: string[]) => `knowledge-${parts.map(anchorPart).join('-')}`;

  const panelAnchorId = (panel: KnowledgePanel) => knowledgeAnchorId('panel', panel);

  const panelFromAnchor = (anchorId: string): KnowledgePanel | undefined => {
    const panel = anchorId.replace(/^knowledge-panel-/, '') as KnowledgePanel;
    return panels.includes(panel) ? panel : undefined;
  };

  const sourceAnchorId = (sourceId = '') => knowledgeAnchorId('source', sourceId || 'source');

  const reportAnchorId = (reportId = '') => knowledgeAnchorId('report', reportId || 'report');

  const runAnchorId = (runId = '') => knowledgeAnchorId('research', runId || 'run');

  const evidenceAnchorId = (ownerId = '', evidenceId = '') =>
    knowledgeAnchorId('reference', ownerId || 'owner', evidenceId || 'evidence');

  const reportGapAnchorId = (reportId = '', index = 0) =>
    knowledgeAnchorId('gap', reportId || 'report', `${index + 1}`);

  const runLoopAnchorId = (runId = '', loopId = '') => knowledgeAnchorId('loop', runId || 'run', loopId || 'loop');

  const runLoopGapAnchorId = (runId = '', loopId = '', index = 0) =>
    knowledgeAnchorId('gap', runId || 'run', loopId || 'loop', `${index + 1}`);

  const runCoverageAnchorId = (runId = '', coverageId = '') =>
    knowledgeAnchorId('coverage', runId || 'run', coverageId || 'coverage');

  const runCandidateAnchorId = (runId = '', candidateId = '') =>
    knowledgeAnchorId('candidate', runId || 'run', candidateId || 'candidate');

  const runEventAnchorId = (runId = '', eventId = '') => knowledgeAnchorId('event', runId || 'run', eventId || 'event');

  const sourceQuestionAnchorId = (sourceId = '', index = 0) =>
    knowledgeAnchorId('question', sourceId || 'source', `${index + 1}`);

  const spaceQuestionAnchorId = (spaceId = '', index = 0) =>
    knowledgeAnchorId('question', spaceId || 'space', `${index + 1}`);

  const knowledgeHashHref = (anchorId = '') =>
    selectedSpace?.id && anchorId ? `${knowledgeSpaceURL(selectedSpace.id)}#${anchorId}` : `#${anchorId}`;

  const promptItem = (anchorId: string, text: string) => ({
    id: anchorId,
    href: knowledgeHashHref(anchorId),
    text
  });

  const sourceForAnchor = (anchorId: string) =>
    (selectedSpace?.sources || []).find((source) => sourceAnchorId(source.id) === anchorId);

  const sourceAnchorHref = (sourceId = '') =>
    (selectedSpace?.sources || []).some((source) => source.id === sourceId) ? knowledgeHashHref(sourceAnchorId(sourceId)) : '';

  const reportHref = (report?: HomelabdKnowledgeReport) => (report ? knowledgeHashHref(reportAnchorId(report.id)) : '#');

  const runHref = (run?: HomelabdKnowledgeResearchRun) => (run ? knowledgeHashHref(runAnchorId(run.id)) : '#');

  const knowledgeLinkAnchorId = (link: HTMLAnchorElement) => {
    const href = link.getAttribute('href') || '';
    if (href.startsWith('#')) {
      return href.slice(1);
    }
    try {
      return new URL(link.href).hash.replace(/^#/, '');
    } catch {
      return '';
    }
  };

  const shouldHandlePlainClick = (event: MouseEvent) =>
    !event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;

  const focusKnowledgeTarget = (target: HTMLElement) => {
    const focusTarget = target.matches('a, button, summary, input, textarea, select, [tabindex]')
      ? target
      : target.querySelector<HTMLElement>('a, button, summary, [tabindex]');
    (focusTarget || target).focus({ preventScroll: true });
  };

  const scrollKnowledgeAnchorIntoView = (anchorId: string) => {
    requestAnimationFrame(() => {
      const target = document.getElementById(anchorId);
      if (!(target instanceof HTMLElement)) {
        return;
      }
      if (target instanceof HTMLDetailsElement) {
        target.open = true;
      }
      let parent = target.parentElement;
      while (parent) {
        if (parent instanceof HTMLDetailsElement) {
          parent.open = true;
        }
        parent = parent.parentElement;
      }
      target.scrollIntoView({ block: 'start' });
      focusKnowledgeTarget(target);
    });
  };

  const pushKnowledgeHash = (anchorId: string, replaceState = false) => {
    if (!browser || !selectedSpace?.id || !anchorId) {
      return;
    }
    const next = knowledgeHashHref(anchorId);
    if (currentRoutePath() === next) {
      return;
    }
    void goto(next, { keepFocus: true, noScroll: true, replaceState });
  };

  const openSourceFromAnchor = (anchorId: string) => {
    const source = sourceForAnchor(anchorId);
    if (!source) {
      return;
    }
    activePanel = 'sources';
    highlightedSourceId = source.id;
    scrollKnowledgeAnchorIntoView(anchorId);
  };

  const openPanelFromAnchor = (anchorId: string) => {
    const panel = panelFromAnchor(anchorId);
    if (!panel) {
      return false;
    }
    activePanel = panel;
    return true;
  };

  const openReportFromAnchor = (anchorId: string) => {
    const report = (selectedSpace?.reports || []).find((item) => reportAnchorId(item.id) === anchorId);
    if (!report) {
      return false;
    }
    activeReport = report;
    activePanel = 'artefacts';
    scrollKnowledgeAnchorIntoView(anchorId);
    return true;
  };

  const openRunFromAnchor = (anchorId: string) => {
    const run = (selectedSpace?.research_runs || []).find((item) => runAnchorId(item.id) === anchorId);
    if (!run) {
      return false;
    }
    activeRun = run;
    activePanel = 'runs';
    scrollKnowledgeAnchorIntoView(anchorId);
    return true;
  };

  const openReferenceFromAnchor = (anchorId: string) => {
    for (const report of selectedSpace?.reports || []) {
      if ((report.evidence || []).some((evidence) => evidenceAnchorId(report.id, evidence.id) === anchorId)) {
        activeReport = report;
        activePanel = 'artefacts';
        scrollKnowledgeAnchorIntoView(anchorId);
        return true;
      }
    }
    for (const run of selectedSpace?.research_runs || []) {
      if ((reportForRun(selectedSpace, run)?.evidence || []).some((evidence) => evidenceAnchorId(run.id, evidence.id) === anchorId)) {
        activeRun = run;
        activePanel = 'runs';
        scrollKnowledgeAnchorIntoView(anchorId);
        return true;
      }
    }
    return false;
  };

  const openPromptFromAnchor = (anchorId: string) => {
    const source = (selectedSpace?.sources || []).find((item) =>
      (item.questions || []).some((_, index) => sourceQuestionAnchorId(item.id, index) === anchorId)
    );
    if (source) {
      activePanel = 'sources';
      highlightedSourceId = source.id;
      scrollKnowledgeAnchorIntoView(anchorId);
      return true;
    }
    if ((selectedSpace?.insight?.suggested_questions || []).some((_, index) => spaceQuestionAnchorId(selectedSpace?.id, index) === anchorId)) {
      mobileOptionsOpen = true;
      scrollKnowledgeAnchorIntoView(anchorId);
      return true;
    }
    for (const run of selectedSpace?.research_runs || []) {
      for (const loop of run.research_loops || []) {
        if ((loop.gaps || []).some((_, index) => runLoopGapAnchorId(run.id, loop.id, index) === anchorId)) {
          activeRun = run;
          activePanel = 'runs';
          scrollKnowledgeAnchorIntoView(anchorId);
          return true;
        }
      }
    }
    for (const report of selectedSpace?.reports || []) {
      if ((report.gaps || []).some((_, index) => reportGapAnchorId(report.id, index) === anchorId)) {
        activeReport = report;
        activePanel = 'artefacts';
        scrollKnowledgeAnchorIntoView(anchorId);
        return true;
      }
    }
    return false;
  };

  const openResearchDetailFromAnchor = (anchorId: string) => {
    for (const run of selectedSpace?.research_runs || []) {
      const runMatches =
        (run.research_loops || []).some((loop) => runLoopAnchorId(run.id, loop.id) === anchorId) ||
        (run.coverage || []).some((coverage) => runCoverageAnchorId(run.id, coverage.id) === anchorId) ||
        (run.source_candidates || []).some((candidate) => runCandidateAnchorId(run.id, candidate.id) === anchorId) ||
        (run.events || []).some((event) => runEventAnchorId(run.id, event.id) === anchorId);
      if (runMatches) {
        activeRun = run;
        activePanel = 'runs';
        scrollKnowledgeAnchorIntoView(anchorId);
        return true;
      }
    }
    return false;
  };

  const openKnowledgeAnchor = (anchorId: string) => {
    if (!anchorId.startsWith('knowledge-')) {
      return false;
    }
    if (anchorId.startsWith('knowledge-panel-')) {
      return openPanelFromAnchor(anchorId);
    }
    if (anchorId.startsWith('knowledge-source-')) {
      openSourceFromAnchor(anchorId);
      return true;
    }
    if (anchorId.startsWith('knowledge-report-')) {
      return openReportFromAnchor(anchorId);
    }
    if (anchorId.startsWith('knowledge-research-')) {
      return openRunFromAnchor(anchorId);
    }
    if (anchorId.startsWith('knowledge-reference-')) {
      return openReferenceFromAnchor(anchorId);
    }
    if (anchorId.startsWith('knowledge-question-') || anchorId.startsWith('knowledge-gap-')) {
      return openPromptFromAnchor(anchorId);
    }
    return openResearchDetailFromAnchor(anchorId);
  };

  const applyKnowledgeAnchor = (anchorId: string, force = false) => {
    if (!anchorId || (!force && anchorId === lastAppliedAnchorId)) {
      return false;
    }
    const applied = openKnowledgeAnchor(anchorId);
    if (applied) {
      lastAppliedAnchorId = anchorId;
    }
    return applied;
  };

  const handleKnowledgeCitationClick = (event: MouseEvent) => {
    if (!shouldHandlePlainClick(event)) {
      return;
    }
    const link = event.target instanceof Element
      ? event.target.closest('a[href*="#knowledge-"]')
      : null;
    if (!(link instanceof HTMLAnchorElement)) {
      return;
    }
    const anchorId = knowledgeLinkAnchorId(link);
    if (!anchorId || !applyKnowledgeAnchor(anchorId, true)) {
      return;
    }
    event.preventDefault();
    pushKnowledgeHash(anchorId);
  };

  const citationLinkedMarkdown = (content = '', evidence: HomelabdKnowledgeEvidence[] = []) => {
    const labels = new Map<string, string>();
    for (const item of evidence || []) {
      if (item.citation_label && sourceAnchorHref(item.source_id)) {
        labels.set(item.citation_label, sourceAnchorHref(item.source_id));
      }
    }
    if (!labels.size) {
      return content;
    }
    return content.replace(/\[([A-Za-z]+\d+(?:\.\d+)?)\]/g, (match, label, offset, whole) => {
      if (whole.slice(offset + match.length, offset + match.length + 1) === '(') {
        return match;
      }
      const href = labels.get(label);
      return href ? `[${label}](${href})` : match;
    });
  };

  const reportForRun = (
    space?: HomelabdKnowledgeSpace,
    run?: HomelabdKnowledgeResearchRun
  ): HomelabdKnowledgeReport | undefined => {
    if (!space || !run?.report_id) {
      return undefined;
    }
    return (space.reports || []).find((report) => report.id === run.report_id);
  };

  const navigateToSpace = (spaceId: string, replaceState = false) => {
    if (!browser || !spaceId) {
      return;
    }
    const next = knowledgeSpaceURL(spaceId);
    if (currentRoutePath() === next) {
      return;
    }
    lastAppliedRouteSpaceId = spaceId;
    void goto(next, { keepFocus: true, noScroll: true, replaceState });
  };

  const navigateToKnowledgeRoot = (replaceState = false) => {
    if (!browser || currentRoutePath() === '/knowledge') {
      return;
    }
    void goto('/knowledge', { keepFocus: true, noScroll: true, replaceState });
  };

  const isCompactKnowledgeViewport = () =>
    typeof window !== 'undefined' && window.matchMedia('(max-width: 760px)').matches;

  const revealDetailIfCompact = () => {
    if (!isCompactKnowledgeViewport()) {
      return;
    }
    requestAnimationFrame(() => {
      if (!detailEl) {
        return;
      }
      const navbarBottom = document.querySelector('.navbar')?.getBoundingClientRect().bottom || 0;
      const detailTop = detailEl.getBoundingClientRect().top + window.scrollY;
      window.scrollTo({ top: Math.max(0, detailTop - navbarBottom - 8) });
    });
  };

  const applyRouteSpaceSelection = (spaceId: string) => {
    if (!spaceId) {
      return;
    }
    selectedSpaceId = spaceId;
    search = '';
  };

  const selectSpace = (spaceId: string) => {
    selectedSpaceId = spaceId;
    navigateToSpace(spaceId);
    revealDetailIfCompact();
  };

  const handleMobileSpaceSelect = (event: Event) => {
    const target = event.currentTarget as HTMLSelectElement;
    if (target.value) {
      selectSpace(target.value);
    }
  };

  const handleSpaceRowClick = (event: MouseEvent, spaceId: string) => {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    event.preventDefault();
    selectSpace(spaceId);
  };

  const handleKnowledgePopState = () => {
    window.setTimeout(() => {
      const spaceId = routeSpaceIdFromLocation();
      if (!spaceId) {
        return;
      }
      applyRouteSpaceSelection(spaceId);
      lastAppliedRouteSpaceId = spaceId;
    }, 0);
  };

  afterNavigate(({ to }) => {
    if (!browser || to?.url.pathname !== '/knowledge') {
      return;
    }
    const spaceId = to.url.searchParams.get('space') || '';
    if (spaceId && spaceId !== selectedSpaceId) {
      applyRouteSpaceSelection(spaceId);
      lastAppliedRouteSpaceId = spaceId;
    }
    const anchorId = to.url.hash.replace(/^#/, '');
    if (anchorId) {
      requestAnimationFrame(() => applyKnowledgeAnchor(anchorId, true));
    } else {
      lastAppliedAnchorId = '';
    }
  });

  const updateSpace = (space: HomelabdKnowledgeSpace) => {
    const existing = spaces.some((item) => item.id === space.id);
    spaces = existing
      ? spaces.map((item) => (item.id === space.id ? space : item))
      : [space, ...spaces];
    selectedSpaceId = space.id;
    if (currentRouteSpaceId() !== space.id) {
      navigateToSpace(space.id);
    }
  };

  const refreshSpaces = async () => {
    loading = true;
    error = '';
    try {
      const response = await client.listKnowledgeSpaces();
      spaces = [...knowledgeSpacesFromResponse(response)].sort(
        (left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at)
      );
      const routeSpaceId = currentRouteSpaceId();
      if (routeSpaceId && spaces.some((space) => space.id === routeSpaceId)) {
        selectedSpaceId = routeSpaceId;
        search = '';
      }
      if (!spaces.some((space) => space.id === selectedSpaceId)) {
        selectedSpaceId = spaces[0]?.id || '';
      }
      if (!spaces.length) {
        createSpaceOpen = true;
      }
      await tick();
      const anchorId = currentRouteHash();
      if (anchorId) {
        applyKnowledgeAnchor(anchorId);
      } else {
        lastAppliedAnchorId = '';
      }
      lastRefresh = syncTimeLabel();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to load Knowledge Space data.';
    } finally {
      loading = false;
      ready = true;
    }
  };

  const createSpace = async () => {
    const title = titleDraft.trim();
    if (!title || creating) {
      return;
    }
    creating = true;
    error = '';
    notice = '';
    try {
      const response = await client.createKnowledgeSpace({
        title,
        objective: objectiveDraft.trim() || undefined,
        description: descriptionDraft.trim() || undefined
      });
      updateSpace(response.space);
      titleDraft = '';
      objectiveDraft = '';
      descriptionDraft = '';
      notice = response.reply || 'Knowledge Space created.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to create Knowledge Space.';
    } finally {
      creating = false;
    }
  };

  const beginEditSpace = () => {
    if (!selectedSpace) {
      return;
    }
    editTitleDraft = selectedSpace.title || '';
    editObjectiveDraft = selectedSpace.objective || '';
    editDescriptionDraft = selectedSpace.description || '';
    editingSpace = true;
    confirmDeleteSpace = false;
    confirmDeleteSourceId = '';
    mobileOptionsOpen = false;
    revealDetailIfCompact();
  };

  const cancelEditSpace = () => {
    editingSpace = false;
    editTitleDraft = '';
    editObjectiveDraft = '';
    editDescriptionDraft = '';
  };

  const saveSpace = async () => {
    if (!selectedSpace || updatingSpace || !editTitleDraft.trim()) {
      return;
    }
    updatingSpace = true;
    error = '';
    notice = '';
    try {
      const response = await client.updateKnowledgeSpace(selectedSpace.id, {
        title: editTitleDraft.trim(),
        objective: editObjectiveDraft.trim(),
        description: editDescriptionDraft.trim()
      });
      updateSpace(response.space);
      editingSpace = false;
      notice = response.reply || 'Knowledge Space updated.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to update Knowledge Space.';
    } finally {
      updatingSpace = false;
    }
  };

  const beginDeleteSpace = () => {
    confirmDeleteSpace = true;
    editingSpace = false;
    confirmDeleteSourceId = '';
    mobileOptionsOpen = false;
    revealDetailIfCompact();
  };

  const deleteSelectedSpace = async () => {
    if (!selectedSpace || deletingSpace) {
      return;
    }
    const deletedId = selectedSpace.id;
    deletingSpace = true;
    error = '';
    notice = '';
    try {
      const response = await client.deleteKnowledgeSpace(deletedId);
      const remaining = spaces
        .filter((space) => space.id !== deletedId)
        .sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at));
      spaces = remaining;
      const nextSpaceId = remaining[0]?.id || '';
      selectedSpaceId = nextSpaceId;
      lastSelectedSpaceId = '';
      editingSpace = false;
      confirmDeleteSpace = false;
      activeReport = undefined;
      activeAskResult = undefined;
      activeRun = undefined;
      selectedSourceIds = [];
      if (nextSpaceId) {
        navigateToSpace(nextSpaceId, true);
      } else {
        navigateToKnowledgeRoot(true);
        createSpaceOpen = true;
      }
      notice = response.reply || 'Knowledge Space deleted.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to delete Knowledge Space.';
    } finally {
      deletingSpace = false;
    }
  };

  const addSource = async () => {
    if (!selectedSpace || addingSource || !sourceReady) {
      return;
    }
    addingSource = true;
    error = '';
    notice = '';
    try {
      const response = await client.addKnowledgeSource(selectedSpace.id, {
        title: sourceTitleDraft.trim(),
        kind: sourceKindDraft,
        uri: sourceURIDraft.trim() || undefined,
        content: sourceContentDraft.trim() || undefined
      });
      updateSpace(response.space);
      activePanel = 'sources';
      sourceTitleDraft = '';
      sourceURIDraft = '';
      sourceContentDraft = '';
      selectedSourceIds = (response.space.sources || []).map((source) => source.id);
      addSourceOpen = false;
      notice = response.reply || 'Source analysed.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to add source.';
    } finally {
      addingSource = false;
    }
  };

  const deleteSource = async (sourceId: string) => {
    if (!selectedSpace || deletingSourceId) {
      return;
    }
    deletingSourceId = sourceId;
    error = '';
    notice = '';
    try {
      const response = await client.deleteKnowledgeSource(selectedSpace.id, sourceId);
      updateSpace(response.space);
      selectedSourceIds = selectedSourceIds.filter((id) => id !== sourceId);
      confirmDeleteSourceId = '';
      notice = response.reply || 'Source deleted.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to delete source.';
    } finally {
      deletingSourceId = '';
    }
  };

  const askKnowledge = async () => {
    if (!selectedSpace || asking || !questionDraft.trim() || !selectedSourceIds.length) {
      return;
    }
    asking = true;
    error = '';
    notice = '';
    try {
      const response = await client.askKnowledgeSpace(selectedSpace.id, {
        question: questionDraft.trim(),
        source_ids: selectedSourceIds.length ? selectedSourceIds : undefined
      });
      activeAskResult = undefined;
      activeReport = response.report;
      updateSpace(response.space);
      activePanel = 'artefacts';
      await tick();
      if (response.report) {
        pushKnowledgeHash(reportAnchorId(response.report.id), true);
      }
      notice = response.reply || 'Grounded answer saved.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to ask Knowledge Space.';
    } finally {
      asking = false;
    }
  };

  const queryCorpus = async () => {
    if (!selectedSpace || querying || !corpusQueryDraft.trim() || !selectedSourceIds.length) {
      return;
    }
    querying = true;
    error = '';
    notice = '';
    try {
      const response = await client.queryKnowledgeSpace(selectedSpace.id, {
        query: corpusQueryDraft.trim(),
        source_ids: selectedSourceIds.length ? selectedSourceIds : undefined,
        limit: 8
      });
      activeAskResult = {
        question: corpusQueryDraft.trim(),
        answer: `Found ${response.result.evidence.length} matching evidence chunk${response.result.evidence.length === 1 ? '' : 's'}.`,
        evidence: response.result.evidence,
        gaps: response.result.evidence.length ? [] : ['No stored source chunks matched this query.'],
        created_at: response.result.created_at
      };
      activePanel = 'runs';
      notice = response.reply || 'Corpus query completed.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to query corpus.';
    } finally {
      querying = false;
    }
  };

  const startResearchRun = async (
    objective: string,
    discoverSources = discoverSourcesDraft,
    mode = researchModeDraft,
    depth = researchDepthDraft
  ) => {
    const trimmedObjective = objective.trim();
    if (!selectedSpace || creatingRun || !trimmedObjective || (!discoverSources && !selectedSourceIds.length)) {
      return;
    }
    runObjectiveDraft = trimmedObjective;
    discoverSourcesDraft = discoverSources;
    researchModeDraft = mode;
    researchDepthDraft = depth;
    activePanel = 'runs';
    creatingRun = true;
    error = '';
    notice = '';
    try {
      const response = await client.createKnowledgeResearchRun(selectedSpace.id, {
        objective: trimmedObjective,
        mode,
        depth,
        source_ids: selectedSourceIds.length ? selectedSourceIds : undefined,
        discover_sources: discoverSources || undefined
      });
      activeRun = response.run;
      activeReport = response.report;
      updateSpace(response.space);
      activePanel = 'runs';
      await tick();
      pushKnowledgeHash(runAnchorId(response.run.id), true);
      researchFormOpen = false;
      notice = response.report ? 'Research completed.' : 'Research queued.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to create research run.';
    } finally {
      creatingRun = false;
    }
  };

  const createResearchRun = async () => {
    if (!canStartResearchRun) {
      return;
    }
    await startResearchRun(runObjectiveDraft, discoverSourcesDraft, researchModeDraft, researchDepthDraft);
  };

  const resumeResearchRun = async (run: HomelabdKnowledgeResearchRun) => {
    if (!selectedSpace || resumingRunId || !canResumeResearchRun(run)) {
      return;
    }
    resumingRunId = run.id;
    error = '';
    notice = '';
    try {
      const response = await client.resumeKnowledgeResearchRun(selectedSpace.id, run.id);
      activeRun = response.run;
      activeReport = response.report;
      updateSpace(response.space);
      activePanel = 'runs';
      await tick();
      pushKnowledgeHash(runAnchorId(response.run.id), true);
      notice = response.reply || 'Research run resumed.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to resume research run.';
    } finally {
      resumingRunId = '';
    }
  };

  const submitKnowledgeAction = async () => {
    if (!canSubmitKnowledgeAction) {
      return;
    }
    if (researchActionDraft === 'ask') {
      await askKnowledge();
      return;
    }
    if (researchActionDraft === 'search') {
      await queryCorpus();
      return;
    }
    await createResearchRun();
  };

  const researchPrompt = (prompt: string) => {
    mobileSpacesOpen = false;
    mobileOptionsOpen = false;
    revealDetailIfCompact();
    void startResearchRun(prompt, true, 'research');
  };

  const toggleSourceSelection = (sourceId: string) => {
    selectedSourceIds = selectedSourceIds.includes(sourceId)
      ? selectedSourceIds.filter((id) => id !== sourceId)
      : [...selectedSourceIds, sourceId];
  };

  const selectAllSources = () => {
    selectedSourceIds = (selectedSpace?.sources || []).map((source) => source.id);
  };

  const clearSourceSelection = () => {
    selectedSourceIds = [];
  };

  const clearSearch = () => {
    search = '';
  };

  const toggleMobileSpaces = () => {
    mobileSpacesOpen = !mobileSpacesOpen;
    if (mobileSpacesOpen) {
      mobileOptionsOpen = false;
    }
  };

  const toggleMobileOptions = () => {
    mobileOptionsOpen = !mobileOptionsOpen;
    if (mobileOptionsOpen) {
      mobileSpacesOpen = false;
    }
  };

  const openMobileCreateSpace = () => {
    mobileSpacesOpen = true;
    mobileOptionsOpen = false;
    createSpaceOpen = true;
    requestAnimationFrame(() => {
      document.getElementById('mobile-space-title')?.focus();
    });
  };

  const selectReport = (report: HomelabdKnowledgeReport, updateHash = true) => {
    activeReport = report;
    activePanel = 'artefacts';
    if (updateHash) {
      pushKnowledgeHash(reportAnchorId(report.id));
    }
    revealDetailIfCompact();
  };

  const selectRun = (run: HomelabdKnowledgeResearchRun, updateHash = true) => {
    activeRun = run;
    activePanel = 'runs';
    if (updateHash) {
      pushKnowledgeHash(runAnchorId(run.id));
    }
    revealDetailIfCompact();
  };

  const clearSelectedRun = (updateHash = true) => {
    activeRun = undefined;
    activePanel = 'runs';
    if (updateHash) {
      pushKnowledgeHash(panelAnchorId('runs'), true);
    }
    revealDetailIfCompact();
  };

  const selectPanel = (panel: KnowledgePanel, updateHash = true) => {
    activePanel = panel;
    if (updateHash) {
      pushKnowledgeHash(panelAnchorId(panel));
    }
  };

  const handleReportRowClick = (event: MouseEvent, report: HomelabdKnowledgeReport) => {
    if (!shouldHandlePlainClick(event)) {
      return;
    }
    event.preventDefault();
    selectReport(report);
  };

  const handleRunRowClick = (event: MouseEvent, run: HomelabdKnowledgeResearchRun) => {
    if (!shouldHandlePlainClick(event)) {
      return;
    }
    event.preventDefault();
    selectRun(run);
  };

  const handleTabKeydown = (event: KeyboardEvent, panel: KnowledgePanel) => {
    const index = panels.indexOf(panel);
    const nextPanel = (nextIndex: number) => {
      const panelId = panels[(nextIndex + panels.length) % panels.length];
      selectPanel(panelId);
      requestAnimationFrame(() => document.getElementById(`knowledge-tab-${panelId}`)?.focus());
    };
    if (event.key === 'ArrowRight') {
      event.preventDefault();
      nextPanel(index + 1);
    } else if (event.key === 'ArrowLeft') {
      event.preventDefault();
      nextPanel(index - 1);
    } else if (event.key === 'Home') {
      event.preventDefault();
      nextPanel(0);
    } else if (event.key === 'End') {
      event.preventDefault();
      nextPanel(panels.length - 1);
    }
  };

  onMount(() => {
    void refreshSpaces();
    const interval = window.setInterval(() => {
      void refreshSpaces();
    }, 10000);
    window.addEventListener('popstate', handleKnowledgePopState);
    document.addEventListener('click', handleKnowledgeCitationClick, true);
    return () => {
      window.clearInterval(interval);
      window.removeEventListener('popstate', handleKnowledgePopState);
      document.removeEventListener('click', handleKnowledgeCitationClick, true);
    };
  });
</script>

<svelte:head>
  <title>homelabd Knowledge Space</title>
  <meta name="description" content="Organise and research source-grounded Knowledge Space material" />
</svelte:head>

<div class="knowledge-shell">
  <Navbar title="Knowledge Space" subtitle="homelabd" current="/knowledge" taskApiBase={apiBase} />

  <main
    class="knowledge-page"
    class:has-selection={!!selectedSpace}
    class:loading-state={!ready && loading}
    data-ready={ready ? 'true' : 'false'}
  >
    {#if !ready && loading}
      <section class="knowledge-loading-state" aria-label="Loading Knowledge Space" aria-busy="true">
        <header class="loading-topline">
          <div>
            <span class="eyebrow">Knowledge Space</span>
            <h1>Loading research corpus</h1>
            <p>Syncing spaces, sources, reports, and research records.</p>
          </div>
          <span class="status-pill active">Syncing</span>
        </header>

        <div class="loading-mobile-toolbar" aria-hidden="true">
          <span class="loading-control wide"></span>
          <span class="loading-control"></span>
          <span class="loading-control"></span>
          <span class="loading-control"></span>
        </div>

        <div class="loading-tabs" aria-hidden="true">
          <span class="active">Sources</span>
          <span>Research</span>
          <span>Reports</span>
        </div>

        <div class="loading-grid">
          <section class="loading-card" aria-hidden="true">
            <span class="loading-label"></span>
            <span class="loading-title"></span>
            <span class="loading-bar large"></span>
            <span class="loading-bar"></span>
            <span class="loading-bar short"></span>
          </section>
          <section class="loading-card compact" aria-hidden="true">
            <span class="loading-label"></span>
            <span class="loading-row"></span>
            <span class="loading-row"></span>
            <span class="loading-row short"></span>
          </section>
        </div>
      </section>
    {:else}
    <section class="space-list" aria-label="Knowledge Space list">
      <header class="space-header">
        <div>
          <h1>Knowledge Space</h1>
          <span>{lastRefresh ? `Synced ${lastRefresh}` : loading ? 'Loading spaces' : 'Not synced'}</span>
        </div>
        <button
          type="button"
          class="sync-button"
          disabled={loading}
          aria-label={loading ? 'Syncing Knowledge Spaces' : 'Sync Knowledge Spaces'}
          title="Sync Knowledge Spaces"
          on:click={() => void refreshSpaces()}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M20 12a8 8 0 0 1-13.7 5.7M4 12A8 8 0 0 1 17.7 6.3M7 18H4v-3M17 6h3v3" />
          </svg>
          <span>{loading ? 'Syncing' : 'Sync'}</span>
        </button>
      </header>

      <div class="space-metrics" aria-label="Knowledge Space totals">
        <span><strong>{spaces.length}</strong> {spaces.length === 1 ? 'space' : 'spaces'}</span>
        <span><strong>{totalSpaceSourceCount}</strong> {totalSpaceSourceCount === 1 ? 'source' : 'sources'}</span>
        <span><strong>{totalReportCount}</strong> {totalReportCount === 1 ? 'report' : 'reports'}</span>
      </div>

      <label class="hidden" for="knowledge-search">Search Knowledge Space</label>
      <span class="search-control">
        <input
          id="knowledge-search"
          class="search"
          type="search"
          bind:value={search}
          placeholder="Search spaces"
        />
        {#if search}
          <button
            type="button"
            class="icon-button"
            aria-label="Clear search input"
            title="Clear search"
            on:click={clearSearch}
          >
            <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
              <path d="M6 6l12 12M18 6 6 18" />
            </svg>
          </button>
        {/if}
      </span>

      <details class="create-space" bind:open={createSpaceOpen}>
        <summary>New space</summary>
        <form on:submit|preventDefault={() => void createSpace()}>
          <label for="space-title">Title</label>
          <input id="space-title" bind:value={titleDraft} autocomplete="off" />

          <label for="space-objective">Objective</label>
          <textarea id="space-objective" bind:value={objectiveDraft} rows="3"></textarea>

          <label for="space-description">Description</label>
          <textarea id="space-description" bind:value={descriptionDraft} rows="2"></textarea>

          <div class="form-footer">
            <span>{titleDraft.trim() ? 'Ready' : 'Title required'}</span>
            <button type="submit" disabled={creating || !titleDraft.trim()}>
              {creating ? 'Creating' : 'Create'}
            </button>
          </div>
        </form>
      </details>

      {#if error}
        <p class="notice error" role="alert">{error}</p>
      {/if}
      {#if notice}
        <p class="notice success">{notice}</p>
      {/if}

      <div class="rows" aria-label="Knowledge Space rows">
        {#if visibleSpaces.length}
          {#each visibleSpaces as space (space.id)}
            <a
              href={knowledgeSpaceURL(space.id)}
              class="space-row"
              class:selected={selectedSpace?.id === space.id}
              aria-current={selectedSpace?.id === space.id ? 'page' : undefined}
              on:click={(event) => handleSpaceRowClick(event, space.id)}
            >
              <span class="dot"></span>
              <span>
                <strong>{space.title}</strong>
                <small>{compactKnowledgeID(space.id)} · {spaceSourceCount(space)} sources</small>
              </span>
              <em>{plural(spaceWordCount(space), 'word')}</em>
            </a>
          {/each}
        {:else if loading}
          <p class="empty">Loading Knowledge Spaces...</p>
        {:else}
          <div class="empty">
            <p>{search ? 'No Knowledge Space matches this search.' : 'No Knowledge Spaces yet.'}</p>
            {#if search}
              <button type="button" class="text-action" on:click={clearSearch}>Clear search</button>
            {/if}
          </div>
        {/if}
      </div>
    </section>

    <section class="space-detail" aria-label="Knowledge Space detail" bind:this={detailEl}>
      {#if selectedSpace}
        <div class="mobile-corpus-bar" aria-label="Knowledge Space mobile controls">
          <div class="mobile-corpus-primary">
            <label class="hidden" for="knowledge-space-switcher">Space</label>
            <select
              id="knowledge-space-switcher"
              aria-label="Space"
              bind:value={selectedSpaceId}
              on:change={handleMobileSpaceSelect}
            >
              {#each spaces as space (space.id)}
                <option value={space.id}>{space.title}</option>
              {/each}
            </select>
            <button
              type="button"
              class="icon-action"
              disabled={loading}
              aria-label={loading ? 'Syncing Knowledge Spaces' : 'Sync Knowledge Spaces'}
              title="Sync"
              on:click={() => void refreshSpaces()}
            >
              <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <path d="M20 12a8 8 0 0 1-13.7 5.7M4 12A8 8 0 0 1 17.7 6.3M7 18H4v-3M17 6h3v3" />
              </svg>
            </button>
            <button
              type="button"
              class="icon-action"
              aria-expanded={mobileSpacesOpen}
              aria-controls={mobileSpacesOpen ? 'mobile-space-browser' : undefined}
              aria-label={mobileSpacesOpen ? 'Hide Knowledge Space browser' : 'Browse Knowledge Spaces'}
              title="Browse spaces"
              on:click={toggleMobileSpaces}
            >
              <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <path d="M4 6h16M4 12h16M4 18h16" />
              </svg>
            </button>
            <button
              type="button"
              class="icon-action"
              aria-label="Create Knowledge Space"
              title="New space"
              on:click={openMobileCreateSpace}
            >
              <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <path d="M12 5v14M5 12h14" />
              </svg>
            </button>
            <button
              type="button"
              class="icon-action"
              aria-expanded={mobileOptionsOpen}
              aria-controls={mobileOptionsOpen ? 'mobile-space-options' : undefined}
              aria-label={mobileOptionsOpen ? 'Hide Knowledge Space options' : 'More Knowledge Space options'}
              title="More"
              on:click={toggleMobileOptions}
            >
              <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <path d="M12 7.2v.1M12 12v.1M12 16.8v.1" />
              </svg>
            </button>
          </div>
          <div class="mobile-stat-row" aria-label="Selected Knowledge Space totals">
            <span>{plural(spaceSourceCount(selectedSpace), 'source')}</span>
            <span>{plural(spaceWordCount(selectedSpace), 'word')}</span>
            <span>{plural(selectedSpace.reports?.length || 0, 'report')}</span>
            {#if lastRefresh}
              <span>Synced {lastRefresh}</span>
            {/if}
          </div>
        </div>

        {#if mobileSpacesOpen}
          <section id="mobile-space-browser" class="mobile-space-browser" aria-label="Browse Knowledge Spaces">
            <header>
              <strong>Spaces</strong>
              <span>{plural(spaces.length, 'space')} · {plural(totalSpaceSourceCount, 'source')}</span>
            </header>

            <label class="hidden" for="knowledge-mobile-search">Search Knowledge Space</label>
            <span class="search-control">
              <input
                id="knowledge-mobile-search"
                class="search"
                type="search"
                bind:value={search}
                placeholder="Search spaces"
              />
              {#if search}
                <button
                  type="button"
                  class="icon-button"
                  aria-label="Clear search input"
                  title="Clear search"
                  on:click={clearSearch}
                >
                  <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                    <path d="M6 6l12 12M18 6 6 18" />
                  </svg>
                </button>
              {/if}
            </span>

            <details class="create-space mobile-create-space" bind:open={createSpaceOpen}>
              <summary>New space</summary>
              <form on:submit|preventDefault={() => void createSpace()}>
                <label for="mobile-space-title">Title</label>
                <input id="mobile-space-title" bind:value={titleDraft} autocomplete="off" />

                <label for="mobile-space-objective">Objective</label>
                <textarea id="mobile-space-objective" bind:value={objectiveDraft} rows="3"></textarea>

                <label for="mobile-space-description">Description</label>
                <textarea id="mobile-space-description" bind:value={descriptionDraft} rows="2"></textarea>

                <div class="form-footer">
                  <span>{titleDraft.trim() ? 'Ready' : 'Title required'}</span>
                  <button type="submit" disabled={creating || !titleDraft.trim()}>
                    {creating ? 'Creating' : 'Create'}
                  </button>
                </div>
              </form>
            </details>

            <div class="mobile-space-rows" aria-label="Mobile Knowledge Space rows">
              {#if visibleSpaces.length}
                {#each visibleSpaces as space (space.id)}
                  <a
                    href={knowledgeSpaceURL(space.id)}
                    class="space-row"
                    class:selected={selectedSpace?.id === space.id}
                    aria-current={selectedSpace?.id === space.id ? 'page' : undefined}
                    on:click={(event) => handleSpaceRowClick(event, space.id)}
                  >
                    <span class="dot"></span>
                    <span>
                      <strong>{space.title}</strong>
                      <small>{compactKnowledgeID(space.id)} · {spaceSourceCount(space)} sources</small>
                    </span>
                    <em>{plural(spaceWordCount(space), 'word')}</em>
                  </a>
                {/each}
              {:else}
                <div class="empty">
                  <p>{search ? 'No Knowledge Space matches this search.' : 'No Knowledge Spaces yet.'}</p>
                  {#if search}
                    <button type="button" class="text-action" on:click={clearSearch}>Clear search</button>
                  {/if}
                </div>
              {/if}
            </div>
          </section>
        {/if}

        {#if mobileOptionsOpen}
          <section id="mobile-space-options" class="mobile-space-options" aria-label="Knowledge Space options">
            <header>
              <div>
                <strong>{selectedSpace.title}</strong>
                <span>{compactKnowledgeID(selectedSpace.id)}</span>
              </div>
              <div class="mobile-option-actions">
                <button type="button" class="text-action" on:click={beginEditSpace}>Rename</button>
                <button type="button" class="danger-action" on:click={beginDeleteSpace}>Delete space</button>
              </div>
            </header>
            {#if selectedSpace.insight?.key_terms?.length}
              <div class="chips" aria-label="Mobile Knowledge Space key terms">
                {#each selectedSpace.insight.key_terms.slice(0, 6) as term}
                  <span>{term}</span>
                {/each}
              </div>
            {/if}
            {#if selectedSpace.insight?.suggested_questions?.length}
              <ResearchPromptList
                items={selectedSpace.insight.suggested_questions
                  .slice(0, 3)
                  .map((question, index) => promptItem(spaceQuestionAnchorId(selectedSpace.id, index), question))}
                label="Mobile research suggestions"
                disabled={creatingRun}
                onResearch={researchPrompt}
              />
            {/if}
          </section>
        {/if}

        {#if error}
          <p class="notice error mobile-notice" role="alert">{error}</p>
        {/if}
        {#if notice}
          <p class="notice success mobile-notice">{notice}</p>
        {/if}

        <header class="detail-header">
          <div>
            <span class="eyebrow">{compactKnowledgeID(selectedSpace.id)}</span>
            <h2>{selectedSpace.title}</h2>
            <div class="detail-summary">
              <Markdown content={selectedSpace.objective || selectedSpace.description || 'No objective recorded.'} />
            </div>
          </div>
          <div class="detail-actions" aria-label="Knowledge Space actions">
            <span>{plural(spaceSourceCount(selectedSpace), 'source')}</span>
            <span>{plural(spaceWordCount(selectedSpace), 'word')}</span>
            <button type="button" class="text-action" on:click={beginEditSpace}>
              Rename
            </button>
            <button
              type="button"
              class="danger-action"
              aria-expanded={confirmDeleteSpace}
              on:click={beginDeleteSpace}
            >
              Delete space
            </button>
          </div>
        </header>

        {#if editingSpace}
          <section class="management-panel" aria-label="Edit Knowledge Space">
            <form class="edit-space-form" on:submit|preventDefault={() => void saveSpace()}>
              <div class="form-grid space-edit-grid">
                <label for="edit-space-title">Space title</label>
                <input id="edit-space-title" bind:value={editTitleDraft} autocomplete="off" />

                <label for="edit-space-objective">Objective</label>
                <textarea id="edit-space-objective" bind:value={editObjectiveDraft} rows="3"></textarea>

                <label for="edit-space-description">Description</label>
                <textarea id="edit-space-description" bind:value={editDescriptionDraft} rows="2"></textarea>
              </div>
              <div class="form-footer">
                <span>{editTitleDraft.trim() ? 'Ready to save' : 'Title required'}</span>
                <div class="button-row">
                  <button type="button" class="text-action" on:click={cancelEditSpace}>Cancel</button>
                  <button type="submit" disabled={updatingSpace || !editTitleDraft.trim()}>
                    {updatingSpace ? 'Saving' : 'Save changes'}
                  </button>
                </div>
              </div>
            </form>
          </section>
        {/if}

        {#if confirmDeleteSpace}
          <section class="danger-panel" aria-label="Delete Knowledge Space confirmation">
            <div>
              <strong>Delete {selectedSpace.title}?</strong>
              <p>This removes the active corpus, source snapshots, retrieval index, and run workspaces for this space.</p>
            </div>
            <div class="button-row">
              <button type="button" class="text-action" on:click={() => (confirmDeleteSpace = false)}>
                Cancel
              </button>
              <button type="button" class="danger-action solid" disabled={deletingSpace} on:click={() => void deleteSelectedSpace()}>
                {deletingSpace ? 'Deleting' : 'Delete space'}
              </button>
            </div>
          </section>
        {/if}

        <div class="insight-bar" aria-label="Knowledge Space insight">
          <div class="insight-card">
            <span>Key terms</span>
            {#if selectedSpace.insight?.key_terms?.length}
              <div class="chips">
                {#each selectedSpace.insight.key_terms.slice(0, 6) as term}
                  <span>{term}</span>
                {/each}
              </div>
            {:else}
              <strong>None yet</strong>
            {/if}
          </div>
          <div class="insight-card">
            <span>Suggested questions</span>
            {#if selectedSpace.insight?.suggested_questions?.length}
              <ResearchPromptList
                items={selectedSpace.insight.suggested_questions
                  .slice(0, 3)
                  .map((question, index) => promptItem(spaceQuestionAnchorId(selectedSpace.id, index), question))}
                label="Research suggestions"
                disabled={creatingRun}
                onResearch={researchPrompt}
              />
            {:else}
              <strong>No suggestions yet</strong>
            {/if}
          </div>
        </div>

        <div class="tabs" role="tablist" aria-label="Knowledge Space panels">
          {#each panels as panel}
            <button
              id={`knowledge-tab-${panel}`}
              type="button"
              role="tab"
              aria-label={`${panelLabel(panel)} ${panelItemCount(panel, selectedSpace)}`}
              aria-selected={activePanel === panel}
              aria-controls={panelAnchorId(panel)}
              class:active={activePanel === panel}
              tabindex={activePanel === panel ? 0 : -1}
              on:click={() => selectPanel(panel)}
              on:keydown={(event) => handleTabKeydown(event, panel)}
            >
              <span class="panel-label-full">{panelLabel(panel)}</span>
              <span class="panel-label-short">{compactPanelLabel(panel)}</span>
              <small>{panelItemCount(panel, selectedSpace)}</small>
            </button>
          {/each}
        </div>

        {#if activePanel === 'sources'}
          <div
            id="knowledge-panel-sources"
            class="panel sources-panel"
            role="tabpanel"
            aria-labelledby="knowledge-tab-sources"
          >
            <section class="source-list-section" aria-label="Processed sources">
              <header class="panel-title">
                <div>
                  <h3>Processed sources</h3>
                  <p>{spaceSourceCount(selectedSpace)} source{spaceSourceCount(selectedSpace) === 1 ? '' : 's'} available for research.</p>
                </div>
              </header>
              <div class="source-list">
                {#if selectedSpace.sources?.length}
                  {#each selectedSpace.sources as source (source.id)}
                    <details
                      id={sourceAnchorId(source.id)}
                      class="source-card source-card-collapsible"
                      class:highlighted={highlightedSourceId === source.id}
                    >
                      <summary class="source-summary">
                        <span class="source-summary-main">
                          <span class="source-kind">{source.kind}</span>
                          <h3>{source.title}</h3>
                        </span>
                        <span class="source-state">
                          <span class={`status-pill ${sourceStatusTone(source)}`}>{sourceStatusLabel(source)}</span>
                          <strong>{source.word_count} words</strong>
                        </span>
                      </summary>
                      <div class="source-card-body">
                        <div class="source-card-actions">
                          <button
                            type="button"
                            class="danger-action compact source-delete-action"
                            aria-expanded={confirmDeleteSourceId === source.id}
                            aria-label={`Delete source ${source.title}`}
                            on:click={() => {
                              confirmDeleteSourceId = source.id;
                              confirmDeleteSpace = false;
                            }}
                          >
                            <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                              <path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7l1-3h4l1 3" />
                            </svg>
                            <span>Delete</span>
                          </button>
                        </div>
                        {#if confirmDeleteSourceId === source.id}
                          <section class="danger-panel source-delete-panel" aria-label={`Delete source ${source.title} confirmation`}>
                            <div>
                              <strong>Delete {source.title}?</strong>
                              <p>This removes the source from the active corpus and retrieval index. Saved reports remain as historical artefacts.</p>
                            </div>
                            <div class="button-row">
                              <button type="button" class="text-action" on:click={() => (confirmDeleteSourceId = '')}>
                                Cancel
                              </button>
                              <button
                                type="button"
                                class="danger-action solid"
                                disabled={deletingSourceId === source.id}
                                on:click={() => void deleteSource(source.id)}
                              >
                                {deletingSourceId === source.id ? 'Deleting' : 'Delete source'}
                              </button>
                            </div>
                          </section>
                        {/if}
                      {#if source.summary}
                        <div class="markdown-block compact">
                          <Markdown content={source.summary} />
                        </div>
                      {/if}
                      <details class="source-details">
                        <summary>Evidence, metadata, and full text</summary>
                        <div class="source-details-body">
                          {#if source.claims?.length}
                            <div class="claims-list" aria-label={`${source.title} claims`}>
                              {#each source.claims.slice(0, 3) as claim}
                                <section>
                                  <strong>{claim.importance || 'Claim'}</strong>
                                  <div class="markdown-block compact">
                                    <Markdown content={claim.text} />
                                  </div>
                                </section>
                              {/each}
                            </div>
                          {/if}
                          {#if source.content}
                            <details class="source-content">
                              <summary>Source content</summary>
                              <div class="markdown-block source-body">
                                <Markdown content={source.content} headingIds />
                              </div>
                            </details>
                          {/if}
                          {#if source.uri || source.provenance?.canonical_uri || source.provenance?.snapshot_path || source.provenance?.extractor}
                            <dl class="source-meta">
                              {#if source.provenance?.canonical_uri || source.uri}
                                <div>
                                  <dt>Reference</dt>
                                  <dd>{source.provenance?.canonical_uri || source.uri}</dd>
                                </div>
                              {/if}
                              {#if source.provenance?.snapshot_path}
                                <div>
                                  <dt>Snapshot</dt>
                                  <dd>{source.provenance.snapshot_path}</dd>
                                </div>
                              {/if}
                              {#if source.chunks?.length}
                                <div>
                                  <dt>Chunks</dt>
                                  <dd>{source.chunks.length}</dd>
                                </div>
                              {/if}
                              {#if source.sections?.length}
                                <div>
                                  <dt>Sections</dt>
                                  <dd>{source.sections.length}</dd>
                                </div>
                              {/if}
                              {#if source.provenance?.extractor}
                                <div>
                                  <dt>Extractor</dt>
                                  <dd>{source.provenance.extractor}</dd>
                                </div>
                              {/if}
                            </dl>
                          {/if}
                          {#if source.ingestion?.error}
                            <p class="source-error">{source.ingestion.error}</p>
                          {/if}
                          {#if source.entities?.length || source.reliability_notes?.length}
                            <div class="source-analysis">
                              {#if source.entities?.length}
                                <div>
                                  <strong>Entities</strong>
                                  <p>{source.entities.slice(0, 4).map((entity) => entity.name).join(', ')}</p>
                                </div>
                              {/if}
                              {#if source.reliability_notes?.length}
                                <div>
                                  <strong>Reliability</strong>
                                  <p>{source.reliability_notes.slice(0, 2).join(' ')}</p>
                                </div>
                              {/if}
                            </div>
                          {/if}
                          {#if source.key_terms?.length}
                            <div class="chips" aria-label={`${source.title} key terms`}>
                              {#each source.key_terms.slice(0, 6) as term}
                                <span>{term}</span>
                              {/each}
                            </div>
                          {/if}
                          {#if source.sections?.length}
                            <div class="chips" aria-label={`${source.title} sections`}>
                              {#each source.sections.slice(0, 5) as section}
                                <span>{section.heading}</span>
                              {/each}
                            </div>
                          {/if}
                          {#if source.questions?.length}
                            <ResearchPromptList
                              items={source.questions
                                .slice(0, 2)
                                .map((question, index) => promptItem(sourceQuestionAnchorId(source.id, index), question))}
                              label={`${source.title} suggested questions`}
                              disabled={creatingRun}
                              onResearch={researchPrompt}
                            />
                          {/if}
                        </div>
                      </details>
                      </div>
                    </details>
                  {/each}
                {:else}
                  <p class="empty">No sources have been analysed. Add text or a URL before asking questions.</p>
                {/if}
              </div>
            </section>

            <details class="add-source" bind:open={addSourceOpen}>
              <summary>Add source</summary>
              <form class="source-form" on:submit|preventDefault={() => void addSource()}>
                <div class="form-grid">
                  <label for="source-title">Source title</label>
                  <input id="source-title" bind:value={sourceTitleDraft} autocomplete="off" />

                  <label for="source-kind">Source type</label>
                  <select id="source-kind" bind:value={sourceKindDraft}>
                    <option value="text">Text</option>
                    <option value="url">URL</option>
                    <option value="file">File</option>
                    <option value="note">Note</option>
                    <option value="email">Email</option>
                    <option value="mcp">Connected resource</option>
                  </select>

                  <label for="source-uri">Reference</label>
                  <input
                    id="source-uri"
                    bind:value={sourceURIDraft}
                    autocomplete="off"
                    placeholder={sourceKindDraft === 'url' ? 'https://example.com/source' : ''}
                  />
                </div>

                <label for="source-content">Source text</label>
                <textarea
                  id="source-content"
                  bind:value={sourceContentDraft}
                  rows="8"
                ></textarea>

                <div class="form-footer">
                  <span>
                    {sourceKindDraft === 'url' && !sourceContentDraft.trim()
                      ? 'Server fetch'
                      : `${sourceContentDraft.trim().split(/\s+/).filter(Boolean).length} words`}
                  </span>
                  <button
                    type="submit"
                    disabled={addingSource || !sourceReady}
                  >
                    {addingSource ? 'Indexing' : 'Index source'}
                  </button>
                </div>
              </form>
            </details>
          </div>
        {:else if activePanel === 'runs'}
          <div
            id="knowledge-panel-runs"
            class="panel run-panel"
            role="tabpanel"
            aria-labelledby="knowledge-tab-runs"
          >
            {#if !latestSelectedRun}
            <div class="research-sidebar">
              <details class="new-research-panel" aria-label="New research" bind:open={researchFormOpen}>
                <summary>
                  <span>
                    <strong>New Knowledge work</strong>
                    <span>{researchRunSourceSummary}</span>
                  </span>
                </summary>
                <form class="research-form" on:submit|preventDefault={() => void submitKnowledgeAction()}>
                  <div class="panel-title">
                    <div>
                      <h3>Research action</h3>
                      <p>{researchRunSourceSummary}</p>
                    </div>
                  </div>

                  <fieldset class="choice-group compact" aria-label="Research action">
                    <legend>Action</legend>
                    <label class:checked={researchActionDraft === 'ask'}>
                      <input type="radio" name="research-action" value="ask" bind:group={researchActionDraft} />
                      <span>
                        <strong>Ask a question</strong>
                        <small>Answer from selected stored sources and save the result as a report.</small>
                      </span>
                    </label>
                    <label class:checked={researchActionDraft === 'research'}>
                      <input type="radio" name="research-action" value="research" bind:group={researchActionDraft} />
                      <span>
                        <strong>Run research</strong>
                        <small>Create a durable research run that can gather sources online.</small>
                      </span>
                    </label>
                    <label class:checked={researchActionDraft === 'search'}>
                      <input type="radio" name="research-action" value="search" bind:group={researchActionDraft} />
                      <span>
                        <strong>Search corpus</strong>
                        <small>Retrieve matching chunks from selected stored sources.</small>
                      </span>
                    </label>
                  </fieldset>

                  {#if researchActionDraft === 'search'}
                    <label for="corpus-query">Stored-source search</label>
                    <input id="corpus-query" bind:value={corpusQueryDraft} autocomplete="off" />
                  {:else if researchActionDraft === 'ask'}
                    <label for="research-question">Question</label>
                    <textarea id="research-question" bind:value={questionDraft} rows="3"></textarea>
                  {:else}
                    <label for="run-objective">Question or research goal</label>
                    <textarea id="run-objective" bind:value={runObjectiveDraft} rows="3"></textarea>
                  {/if}

                  {#if researchActionDraft === 'research'}
                    <fieldset class="choice-group" aria-label="Research effort">
                      <legend>Research effort</legend>
                      <label class:checked={researchDepthDraft === 'quick'}>
                        <input type="radio" name="research-depth" value="quick" bind:group={researchDepthDraft} />
                        <span>
                          <strong>Quick</strong>
                          <small>Fast scan of the selected corpus and a small discovery pass.</small>
                        </span>
                      </label>
                      <label class:checked={researchDepthDraft === 'standard'}>
                        <input type="radio" name="research-depth" value="standard" bind:group={researchDepthDraft} />
                        <span>
                          <strong>Standard</strong>
                          <small>Balanced source gathering, reading, and synthesis.</small>
                        </span>
                      </label>
                      <label class:checked={researchDepthDraft === 'deep'}>
                        <input type="radio" name="research-depth" value="deep" bind:group={researchDepthDraft} />
                        <span>
                          <strong>Deep</strong>
                          <small>More searches, broader evidence review, and longer synthesis.</small>
                        </span>
                      </label>
                    </fieldset>

                    <fieldset class="choice-group compact" aria-label="Research output">
                      <legend>Output</legend>
                      <label class:checked={researchModeDraft === 'research'}>
                        <input type="radio" name="research-output" value="research" bind:group={researchModeDraft} />
                        <span>
                          <strong>Report</strong>
                          <small>Full answer with evidence and gaps.</small>
                        </span>
                      </label>
                      <label class:checked={researchModeDraft === 'brief'}>
                        <input type="radio" name="research-output" value="brief" bind:group={researchModeDraft} />
                        <span>
                          <strong>Brief</strong>
                          <small>Shorter synthesis for review.</small>
                        </span>
                      </label>
                      <label class:checked={researchModeDraft === 'study'}>
                        <input type="radio" name="research-output" value="study" bind:group={researchModeDraft} />
                        <span>
                          <strong>Study</strong>
                          <small>Questions and learning-oriented notes.</small>
                        </span>
                      </label>
                    </fieldset>
                  {/if}

                  <div class="research-controls">
                    {#if researchActionDraft === 'research'}
                      <label class="inline-check">
                        <input type="checkbox" bind:checked={discoverSourcesDraft} />
                        <span>Search web and academic sources</span>
                      </label>
                    {/if}
                    <button
                      type="submit"
                      disabled={creatingRun || asking || querying || !canSubmitKnowledgeAction}
                    >
                      {researchActionDraft === 'ask'
                        ? asking ? 'Answering' : 'Ask question'
                        : researchActionDraft === 'search'
                          ? querying ? 'Searching' : 'Search corpus'
                          : creatingRun ? 'Starting' : 'Start research'}
                    </button>
                  </div>

                  {#if selectedSpace.sources?.length}
                    <details class="source-picker">
                      <summary>{selectedSourceSummary}</summary>
                      <div class="source-picker-actions">
                        <button type="button" disabled={!selectedSpace.sources?.length} on:click={selectAllSources}>
                          Select all
                        </button>
                        <button type="button" disabled={!selectedSourceIds.length} on:click={clearSourceSelection}>
                          Clear
                        </button>
                      </div>
                      <div class="source-select" aria-label="Research source selection">
                        {#each selectedSpace.sources as source (source.id)}
                          <label>
                            <input
                              type="checkbox"
                              checked={selectedSourceIds.includes(source.id)}
                              on:change={() => toggleSourceSelection(source.id)}
                            />
                            <span>{source.title}</span>
                          </label>
                        {/each}
                      </div>
                    </details>
                  {/if}
                </form>
              </details>

              {#if displayedAskResult && researchActionDraft === 'search'}
                <article class="report-card corpus-result-card" aria-label="Corpus search result">
                  <header>
                    <div>
                      <span>Search result</span>
                      <h3>{displayedAskResult.question}</h3>
                    </div>
                    <strong>{compactTime(displayedAskResult.created_at)}</strong>
                  </header>
                  <details class="knowledge-disclosure" aria-label="Corpus search evidence" open>
                    <summary>
                      <span>
                        <strong>Evidence</strong>
                        <span>{displayedAskResult.evidence?.length || 0} chunks</span>
                      </span>
                    </summary>
                    <div class="disclosure-body">
                      <div class="evidence-list">
                        {#each displayedAskResult.evidence || [] as evidence (evidence.id)}
                          <section id={evidenceAnchorId('search', evidence.id)}>
                            <div class="evidence-heading">
                              {#if sourceAnchorHref(evidence.source_id)}
                                <a class="source-reference-link" href={sourceAnchorHref(evidence.source_id)}>[{evidence.citation_label}] {evidence.source_title}</a>
                              {:else}
                                <strong>[{evidence.citation_label}] {evidence.source_title}</strong>
                              {/if}
                            </div>
                            <div class="markdown-block evidence-body">
                              <Markdown content={evidence.excerpt} />
                            </div>
                          </section>
                        {/each}
                      </div>
                    </div>
                  </details>
                </article>
              {/if}

              <section class="record-inventory" aria-label="Research records">
                <header>
                  <div>
                    <h3>Research records</h3>
                    <p>{plural(selectedSpace.research_runs?.length || 0, 'run')}</p>
                  </div>
                </header>
                <div class="record-table" aria-label="Research table">
                  {#if selectedSpace.research_runs?.length}
                    {#each selectedSpace.research_runs as run (run.id)}
                      <div class="record-row-wrap">
                        <a
                          id={`${runAnchorId(run.id)}-row`}
                          class="record-row research-record-row"
                          href={runHref(run)}
                          on:click={(event) => handleRunRowClick(event, run)}
                        >
                          <span
                            class={`record-status-dot ${researchRunStatusTone(run)}`}
                            class:pulse={researchRunStatusTone(run) === 'active'}
                            aria-hidden="true"
                          ></span>
                          <span class="record-row-main">
                            <strong>{run.objective}</strong>
                            <small>
                              {researchRunStatusLabel(run)} · {run.depth || 'standard'} · {run.discover_sources ? 'web and academic' : 'stored corpus'}
                            </small>
                          </span>
                          <span class="record-row-meta">
                            <strong>{run.evidence_count || 0}</strong>
                            <small>evidence</small>
                          </span>
                          <span class="record-row-time">{compactTime(run.created_at)}</span>
                        </a>
                        {#if canResumeResearchRun(run)}
                          <button
                            type="button"
                            class="run-resume-action"
                            disabled={!!resumingRunId}
                            aria-label={`Resume failed research ${run.objective}`}
                            title="Resume failed research"
                            on:click={() => void resumeResearchRun(run)}
                          >
                            <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                              <path d="M20 12a8 8 0 0 1-13.7 5.7M4 12A8 8 0 0 1 17.7 6.3M7 18H4v-3M17 6h3v3" />
                            </svg>
                          </button>
                        {/if}
                      </div>
                    {/each}
                  {:else}
                    <p class="empty">No research runs yet.</p>
                  {/if}
                </div>
              </section>
            </div>
            {/if}

            {#if latestSelectedRun}
            <div class="runs-list" aria-label="Research">
                <article id={runAnchorId(latestSelectedRun.id)} class="report-card" aria-label="Selected research">
                  <header>
                    <div>
                      <button
                        type="button"
                        class="back-to-records"
                        aria-label="Back to research records"
                        on:click={() => clearSelectedRun()}
                      >
                        <svg viewBox="0 0 20 20" aria-hidden="true" focusable="false">
                          <path d="M12.5 4.5 7 10l5.5 5.5" />
                        </svg>
                        <span>Back to research</span>
                      </button>
                      <span class="run-status-actions">
                        <span class={`status-pill ${researchRunStatusTone(latestSelectedRun)}`}>{researchRunStatusLabel(latestSelectedRun)}</span>
                        {#if canResumeResearchRun(latestSelectedRun)}
                          <button
                            type="button"
                            class="run-resume-action inline"
                            disabled={!!resumingRunId}
                            aria-label={`Resume failed research ${latestSelectedRun.objective}`}
                            title="Resume failed research"
                            on:click={() => void resumeResearchRun(latestSelectedRun)}
                          >
                            <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                              <path d="M20 12a8 8 0 0 1-13.7 5.7M4 12A8 8 0 0 1 17.7 6.3M7 18H4v-3M17 6h3v3" />
                            </svg>
                          </button>
                        {/if}
                      </span>
                      <h3>{latestSelectedRun.objective}</h3>
                    </div>
                    <span class="header-link-group">
                      <strong>{compactTime(latestSelectedRun.created_at)}</strong>
                      <a
                        class="permalink"
                        href={runHref(latestSelectedRun)}
                        aria-label={`Link to research ${latestSelectedRun.objective}`}
                        title="Link to research"
                      >
                        #
                      </a>
                    </span>
                  </header>
                  <dl class="source-meta">
                    <div>
                      <dt>Sources</dt>
                      <dd>{latestSelectedRun.sources_examined || 0}</dd>
                    </div>
                    <div>
                      <dt>Evidence</dt>
                      <dd>{latestSelectedRun.evidence_count || 0}</dd>
                    </div>
                    <div>
                      <dt>Discovery</dt>
                      <dd>{latestSelectedRun.discover_sources ? 'web, academic, and corpus' : 'stored corpus only'}</dd>
                    </div>
                    {#if modelProvenanceLabel(latestSelectedRun.provider, latestSelectedRun.model)}
                      <div>
                        <dt>Model</dt>
                        <dd>{modelProvenanceLabel(latestSelectedRun.provider, latestSelectedRun.model)}</dd>
                      </div>
                    {/if}
                    {#if latestSelectedRun.usage?.total_tokens}
                      <div>
                        <dt>Tokens</dt>
                        <dd>{latestSelectedRun.usage.total_tokens}</dd>
                      </div>
                    {/if}
                    {#if latestSelectedRun.workspace_path}
                      <div>
                        <dt>Workspace</dt>
                        <dd>{latestSelectedRun.workspace_path}</dd>
                      </div>
                    {/if}
                  </dl>
                  {#if selectedRunReport}
                    <details class="knowledge-disclosure run-answer" aria-label="Research final answer" open>
                      <summary>
                        <span>
                          <strong>Final answer</strong>
                          <span>{selectedRunReport.evidence?.length || 0} citations</span>
                        </span>
                      </summary>
                      <div class="disclosure-body">
                        <a
                          class="text-action"
                          href={reportHref(selectedRunReport)}
                          on:click={(event) => handleReportRowClick(event, selectedRunReport!)}
                        >
                          Open report
                        </a>
                        <div class="markdown-block answer-body">
                          <Markdown content={citationLinkedMarkdown(selectedRunReport.answer, selectedRunReport.evidence)} headingIds />
                        </div>
                        {#if selectedRunReport.key_findings?.length}
                          <details class="knowledge-disclosure nested" aria-label="Research key findings">
                            <summary>
                              <span>
                                <strong>Key findings</strong>
                                <span>{selectedRunReport.key_findings.length}</span>
                              </span>
                            </summary>
                            <div class="disclosure-body">
                              <div class="claims-list">
                                {#each selectedRunReport.key_findings as finding}
                                  <section>
                                    <strong>Finding</strong>
                                    <div class="markdown-block compact">
                                      <Markdown content={citationLinkedMarkdown(finding, selectedRunReport.evidence)} />
                                    </div>
                                  </section>
                                {/each}
                              </div>
                            </div>
                          </details>
                        {/if}
                        {#if selectedRunReport.gaps?.length}
                          <details class="knowledge-disclosure nested" aria-label="Research gaps">
                            <summary>
                              <span>
                                <strong>Gaps</strong>
                                <span>{selectedRunReport.gaps.length}</span>
                              </span>
                            </summary>
                            <div class="disclosure-body">
                              <ResearchPromptList
                                items={selectedRunReport.gaps.map((gap, index) =>
                                  promptItem(reportGapAnchorId(selectedRunReport.id, index), gap)
                                )}
                                label="Research gap prompts"
                                disabled={creatingRun}
                                onResearch={researchPrompt}
                              />
                            </div>
                          </details>
                        {/if}
                        {#if selectedRunReport.evidence?.length}
                          <details class="knowledge-disclosure nested" aria-label="Research evidence">
                            <summary>
                              <span>
                                <strong>Evidence</strong>
                                <span>{selectedRunReport.evidence.length} cited chunks</span>
                              </span>
                            </summary>
                            <div class="disclosure-body">
                              <div class="evidence-list">
                                {#each selectedRunReport.evidence.slice(0, 8) as evidence (evidence.id)}
                                  <section id={evidenceAnchorId(latestSelectedRun.id, evidence.id)}>
                                    <div class="evidence-heading">
                                      {#if sourceAnchorHref(evidence.source_id)}
                                        <a class="source-reference-link" href={sourceAnchorHref(evidence.source_id)}>[{evidence.citation_label}] {evidence.source_title}</a>
                                      {:else}
                                        <strong>[{evidence.citation_label}] {evidence.source_title}</strong>
                                      {/if}
                                      <a
                                        class="permalink"
                                        href={knowledgeHashHref(evidenceAnchorId(latestSelectedRun.id, evidence.id))}
                                        aria-label={`Link to research reference ${evidence.citation_label}`}
                                        title="Link to reference"
                                      >
                                        #
                                      </a>
                                    </div>
                                    <div class="markdown-block evidence-body">
                                      <Markdown content={evidence.excerpt} />
                                    </div>
                                    <dl class="candidate-meta evidence-trace">
                                      {#if evidence.section_title}
                                        <div>
                                          <dt>Section</dt>
                                          <dd>{evidence.section_title}</dd>
                                        </div>
                                      {/if}
                                      <div>
                                        <dt>Trace</dt>
                                        <dd>{evidenceTraceLabel(evidence)}</dd>
                                      </div>
                                      <div>
                                        <dt>Score</dt>
                                        <dd>{evidence.score}</dd>
                                      </div>
                                    </dl>
                                    {#if evidence.source_summary}
                                      <div class="markdown-block compact">
                                        <Markdown content={evidence.source_summary} />
                                      </div>
                                    {/if}
                                    {#if evidence.source_uri}
                                      <small>{evidence.source_uri}</small>
                                    {/if}
                                  </section>
                                {/each}
                              </div>
                            </div>
                          </details>
                        {/if}
                      </div>
                    </details>
                  {/if}
                  {#if latestSelectedRun.plan?.rewritten_objective || latestSelectedRun.plan?.search_queries?.length || latestSelectedRun.plan?.steps?.length}
                    <details class="knowledge-disclosure run-plan" aria-label="Research plan">
                      <summary>
                        <span>
                          <strong>Plan</strong>
                          <span>{latestSelectedRun.plan?.search_queries?.length || 0} queries</span>
                        </span>
                      </summary>
                      <div class="disclosure-body">
                        {#if latestSelectedRun.plan?.rewritten_objective}
                          <section>
                            <strong>Objective</strong>
                            <div class="markdown-block compact">
                              <Markdown content={latestSelectedRun.plan.rewritten_objective} />
                            </div>
                          </section>
                        {/if}
                        {#if latestSelectedRun.plan?.search_queries?.length}
                          <section>
                            <strong>Queries</strong>
                            <div class="chips">
                              {#each latestSelectedRun.plan.search_queries as query}
                                <span>{query}</span>
                              {/each}
                            </div>
                          </section>
                        {/if}
                        {#if latestSelectedRun.plan?.steps?.length}
                          <section>
                            <strong>Steps</strong>
                            <ol>
                              {#each latestSelectedRun.plan.steps as step}
                                <li>{step}</li>
                              {/each}
                            </ol>
                          </section>
                        {/if}
                      </div>
                    </details>
                  {/if}
                  {#if latestSelectedRun.stop_reason}
                    <details class="knowledge-disclosure run-note" aria-label="Research stop reason">
                      <summary>
                        <span>
                          <strong>Stop reason</strong>
                        </span>
                      </summary>
                      <div class="disclosure-body">
                        <div class="markdown-block compact">
                          <Markdown content={latestSelectedRun.stop_reason} />
                        </div>
                      </div>
                    </details>
                  {/if}
                  {#if latestSelectedRun.research_loops?.length}
                    <div class="research-loops" aria-label="Research loops">
                      {#each latestSelectedRun.research_loops as loop (loop.id)}
                        <details id={runLoopAnchorId(latestSelectedRun.id, loop.id)} class="knowledge-disclosure">
                          <summary>
                            <span>
                              <strong>Loop {loop.index}</strong>
                              <small>{loop.queries?.length || 0} searches · {loop.evidence_count || 0} cited chunks</small>
                            </span>
                            <span class={`candidate-status ${loop.decision || loop.status}`}>
                              {loop.decision || loop.status}
                            </span>
                          </summary>
                          <div class="disclosure-body">
                            {#if loop.queries?.length}
                              <div class="chips" aria-label={`Loop ${loop.index} queries`}>
                                {#each loop.queries as query}
                                  <span>{query}</span>
                                {/each}
                              </div>
                            {/if}
                            <dl class="candidate-meta">
                              <div>
                                <dt>Accepted</dt>
                                <dd>{loop.accepted_count || 0}</dd>
                              </div>
                              <div>
                                <dt>Rejected</dt>
                                <dd>{loop.rejected_count || 0}</dd>
                              </div>
                              <div>
                                <dt>Failed</dt>
                                <dd>{loop.failed_count || 0}</dd>
                              </div>
                              {#if loop.source_ids?.length}
                                <div>
                                  <dt>Sources</dt>
                                  <dd>{loop.source_ids.length}</dd>
                                </div>
                              {/if}
                            </dl>
                            {#if loop.stop_reason}
                              <div class="markdown-block compact">
                                <Markdown content={loop.stop_reason} />
                              </div>
                            {/if}
                            {#if loop.supported_claims?.length}
                              <div class="loop-subsection">
                                <strong>Supported</strong>
                                {#each loop.supported_claims as claim}
                                  <div class="markdown-block compact"><Markdown content={claim} /></div>
                                {/each}
                              </div>
                            {/if}
                            {#if loop.gaps?.length}
                              <ResearchPromptList
                                items={loop.gaps.map((gap, index) =>
                                  promptItem(runLoopGapAnchorId(latestSelectedRun.id, loop.id, index), gap)
                                )}
                                label={`Loop ${loop.index} gap prompts`}
                                disabled={creatingRun}
                                onResearch={researchPrompt}
                              />
                            {/if}
                            {#if loop.follow_up_queries?.length}
                              <div class="loop-subsection">
                                <strong>Follow-up</strong>
                                <div class="chips">
                                  {#each loop.follow_up_queries as query}
                                    <span>{query}</span>
                                  {/each}
                                </div>
                              </div>
                            {/if}
                          </div>
                        </details>
                      {/each}
                    </div>
                  {/if}
                  {#if latestSelectedRun.coverage?.length}
                    <details class="knowledge-disclosure run-coverage" aria-label="Research coverage">
                      <summary>
                        <span>
                          <strong>Coverage</strong>
                          <span>{latestSelectedRun.coverage.length} topics</span>
                        </span>
                      </summary>
                      <div class="disclosure-body">
                        {#each latestSelectedRun.coverage as item (item.id)}
                          <section id={runCoverageAnchorId(latestSelectedRun.id, item.id)}>
                            <header>
                              <strong>{item.topic}</strong>
                              <span class={`candidate-status ${item.status}`}>{item.status}</span>
                            </header>
                            <small>{item.evidence_count || 0} cited chunks{item.source_ids?.length ? ` from ${item.source_ids.length} source${item.source_ids.length === 1 ? '' : 's'}` : ''}</small>
                            {#if item.notes}
                              <div class="markdown-block compact">
                                <Markdown content={item.notes} />
                              </div>
                            {/if}
                          </section>
                        {/each}
                      </div>
                    </details>
                  {/if}
                  {#if latestSelectedRun.source_candidates?.length}
                    <details class="knowledge-disclosure source-candidates" aria-label="Discovered source candidates">
                      <summary>
                        <span>
                          <strong>Discovered sources</strong>
                          <span>{latestSelectedRun.source_candidates.length} candidates</span>
                        </span>
                      </summary>
                      <div class="disclosure-body">
                        {#each latestSelectedRun.source_candidates as candidate (candidate.id)}
                          <section id={runCandidateAnchorId(latestSelectedRun.id, candidate.id)}>
                            <header>
                              <strong>{candidate.title || candidate.url}</strong>
                              <span class={`candidate-status ${candidate.status}`}>{candidate.status}</span>
                            </header>
                            {#if candidate.url}
                              <a href={candidate.url} target="_blank" rel="noreferrer">
                                {candidate.domain || candidate.url}
                              </a>
                            {/if}
                            <dl class="candidate-meta">
                              {#if candidate.query}
                                <div>
                                  <dt>Query</dt>
                                  <dd>{candidate.query}</dd>
                                </div>
                              {/if}
                              {#if candidate.content_type}
                                <div>
                                  <dt>Type</dt>
                                  <dd>{candidate.content_type}</dd>
                                </div>
                              {/if}
                              {#if candidate.extraction_state}
                                <div>
                                  <dt>Extraction</dt>
                                  <dd>{candidate.extraction_state}</dd>
                                </div>
                              {/if}
                              {#if candidate.usefulness}
                                <div>
                                  <dt>Usefulness</dt>
                                  <dd>{candidate.usefulness}</dd>
                                </div>
                              {/if}
                              {#if candidate.word_count}
                                <div>
                                  <dt>Words</dt>
                                  <dd>{candidate.word_count}</dd>
                                </div>
                              {/if}
                              {#if candidate.relevance_score !== undefined}
                                <div>
                                  <dt>Relevance</dt>
                                  <dd>{candidate.relevance_score}/100</dd>
                                </div>
                              {/if}
                            </dl>
                            {#if candidate.snippet}
                              <div class="markdown-block compact">
                                <Markdown content={candidate.snippet} />
                              </div>
                            {/if}
                            {#if candidate.extraction_message}
                              <div class="markdown-block compact">
                                <Markdown content={candidate.extraction_message} />
                              </div>
                            {/if}
                            {#if candidate.coverage?.length}
                              <div class="chips">
                                {#each candidate.coverage as topic}
                                  <span>{topic}</span>
                                {/each}
                              </div>
                            {/if}
                            {#if candidate.source_id}
                              <small>
                                Imported as
                                {#if sourceAnchorHref(candidate.source_id)}
                                  <a class="source-reference-link" href={sourceAnchorHref(candidate.source_id)}>{candidate.source_id}</a>
                                {:else}
                                  {candidate.source_id}
                                {/if}
                              </small>
                            {/if}
                            {#if candidate.error}
                              <p class="source-error">{candidate.error}</p>
                            {/if}
                          </section>
                        {/each}
                      </div>
                    </details>
                  {/if}
                  {#if latestSelectedRun.error}
                    <p class="source-error">{latestSelectedRun.error}</p>
                  {/if}
                  {#if latestSelectedRun.events?.length}
                    <details class="knowledge-disclosure run-events" aria-label="Research events">
                      <summary>
                        <span>
                          <strong>Events</strong>
                          <span>{latestSelectedRun.events.length}</span>
                        </span>
                      </summary>
                      <div class="disclosure-body">
                        {#each latestSelectedRun.events as event (event.id)}
                          <section id={runEventAnchorId(latestSelectedRun.id, event.id)}>
                            <strong>{event.stage}</strong>
                            <div class="markdown-block compact">
                              <Markdown content={event.message} />
                            </div>
                          </section>
                        {/each}
                      </div>
                    </details>
                  {/if}
                </article>
            </div>
            {/if}
          </div>
        {:else}
          <div
            id="knowledge-panel-artefacts"
            class="panel reports-panel"
            role="tabpanel"
            aria-labelledby="knowledge-tab-artefacts"
          >
            <div class="record-workspace reports-workspace">
            {#if latestSelectedReport}
              <article id={reportAnchorId(latestSelectedReport.id)} class="report-card record-detail" aria-label="Selected report">
                <header>
                  <div>
                    <span>{latestSelectedReport.mode}</span>
                    <h3>{latestSelectedReport.question}</h3>
                  </div>
                  <span class="header-link-group">
                    <strong>{compactTime(latestSelectedReport.created_at)}</strong>
                    <a
                      class="permalink"
                      href={reportHref(latestSelectedReport)}
                      aria-label={`Link to report ${latestSelectedReport.question}`}
                      title="Link to report"
                    >
                      #
                    </a>
                  </span>
                </header>
                <details class="knowledge-disclosure report-answer" aria-label="Report answer" open>
                  <summary>
                    <span>
                      <strong>Answer</strong>
                      <span>{latestSelectedReport.evidence?.length || 0} citations</span>
                    </span>
                  </summary>
                  <div class="disclosure-body">
                    <div class="markdown-block answer-body">
                      <Markdown content={citationLinkedMarkdown(latestSelectedReport.answer, latestSelectedReport.evidence)} headingIds />
                    </div>
                  </div>
                </details>
                {#if latestSelectedReport.key_findings?.length}
                  <details class="knowledge-disclosure" aria-label="Report key findings">
                    <summary>
                      <span>
                        <strong>Key findings</strong>
                        <span>{latestSelectedReport.key_findings.length}</span>
                      </span>
                    </summary>
                    <div class="disclosure-body">
                      <div class="claims-list">
                        {#each latestSelectedReport.key_findings as finding}
                          <section>
                            <strong>Finding</strong>
                            <div class="markdown-block compact">
                              <Markdown content={citationLinkedMarkdown(finding, latestSelectedReport.evidence)} />
                            </div>
                          </section>
                        {/each}
                      </div>
                    </div>
                  </details>
                {/if}
                {#if modelProvenanceLabel(latestSelectedReport.provider, latestSelectedReport.model) || latestSelectedReport.usage?.total_tokens}
                  <dl class="source-meta">
                    {#if modelProvenanceLabel(latestSelectedReport.provider, latestSelectedReport.model)}
                      <div>
                        <dt>Model</dt>
                        <dd>{modelProvenanceLabel(latestSelectedReport.provider, latestSelectedReport.model)}</dd>
                      </div>
                    {/if}
                    {#if latestSelectedReport.usage?.total_tokens}
                      <div>
                        <dt>Tokens</dt>
                        <dd>{latestSelectedReport.usage.total_tokens}</dd>
                      </div>
                    {/if}
                  </dl>
                {/if}
                {#if latestSelectedReport.evidence?.length}
                  <details class="knowledge-disclosure" aria-label="Report evidence">
                    <summary>
                      <span>
                        <strong>Evidence</strong>
                        <span>{latestSelectedReport.evidence.length} cited chunks</span>
                      </span>
                    </summary>
                    <div class="disclosure-body">
                      <div class="evidence-list">
                        {#each latestSelectedReport.evidence as evidence (evidence.id)}
                          <section id={evidenceAnchorId(latestSelectedReport.id, evidence.id)}>
                            <div class="evidence-heading">
                              {#if sourceAnchorHref(evidence.source_id)}
                                <a class="source-reference-link" href={sourceAnchorHref(evidence.source_id)}>[{evidence.citation_label}] {evidence.source_title}</a>
                              {:else}
                                <strong>[{evidence.citation_label}] {evidence.source_title}</strong>
                              {/if}
                              <a
                                class="permalink"
                                href={knowledgeHashHref(evidenceAnchorId(latestSelectedReport.id, evidence.id))}
                                aria-label={`Link to report reference ${evidence.citation_label}`}
                                title="Link to reference"
                              >
                                #
                              </a>
                            </div>
                            <div class="markdown-block evidence-body">
                              <Markdown content={evidence.excerpt} />
                            </div>
                            <dl class="candidate-meta evidence-trace">
                              {#if evidence.section_title}
                                <div>
                                  <dt>Section</dt>
                                  <dd>{evidence.section_title}</dd>
                                </div>
                              {/if}
                            <div>
                              <dt>Trace</dt>
                              <dd>{evidenceTraceLabel(evidence)}</dd>
                            </div>
                              <div>
                                <dt>Score</dt>
                                <dd>{evidence.score}</dd>
                              </div>
                            </dl>
                            {#if evidence.source_summary}
                              <div class="markdown-block compact">
                                <Markdown content={evidence.source_summary} />
                              </div>
                            {/if}
                          </section>
                        {/each}
                      </div>
                    </div>
                  </details>
                {/if}
                {#if latestSelectedReport.gaps?.length}
                  <details class="knowledge-disclosure" aria-label="Report gaps">
                    <summary>
                      <span>
                        <strong>Gaps</strong>
                        <span>{latestSelectedReport.gaps.length}</span>
                      </span>
                    </summary>
                    <div class="disclosure-body">
                      <ResearchPromptList
                        items={latestSelectedReport.gaps.map((gap, index) =>
                          promptItem(reportGapAnchorId(latestSelectedReport.id, index), gap)
                        )}
                        label="Report gap prompts"
                        disabled={creatingRun}
                        onResearch={researchPrompt}
                      />
                    </div>
                  </details>
                {/if}
              </article>
            {/if}
            <div class="record-inventory reports-list" aria-label="Knowledge Space reports">
              <header>
                <div>
                  <h3>Report records</h3>
                  <p>{plural(selectedSpace.reports?.length || 0, 'report')}</p>
                </div>
              </header>
              <div class="record-table" aria-label="Reports table">
                {#if selectedSpace.reports?.length}
                  {#each selectedSpace.reports as report (report.id)}
                    <a
                      id={`${reportAnchorId(report.id)}-row`}
                      class="record-row"
                      class:active={latestSelectedReport?.id === report.id}
                      href={reportHref(report)}
                      aria-current={latestSelectedReport?.id === report.id ? 'true' : undefined}
                      on:click={(event) => handleReportRowClick(event, report)}
                    >
                      <span class="record-row-main">
                        <span class="record-type">{report.mode}</span>
                        <strong>{report.question}</strong>
                        <small>{knowledgeMarkdownPreview(report.key_findings?.[0] || report.answer, 96)}</small>
                      </span>
                      <span class="record-row-meta">
                        <strong>{report.evidence?.length || 0}</strong>
                        <small>citations</small>
                      </span>
                      <span class="record-row-time">{compactTime(report.created_at)}</span>
                    </a>
                  {/each}
                {:else}
                  <p class="empty">No reports are stored.</p>
                {/if}
              </div>
            </div>
            </div>
          </div>
        {/if}
      {:else}
        <div class="empty-detail">
          <h2>No Knowledge Space selected</h2>
          <p>Create or sync spaces to begin.</p>
        </div>
      {/if}
    </section>
    {/if}
  </main>
</div>

<style>
  :global(html),
  :global(body),
  :global(body > div) {
    min-height: 100%;
  }

  :global(body) {
    margin: 0;
    color: var(--text, #172033);
    background: var(--bg, #eef2f7);
    overflow-x: hidden;
    overscroll-behavior-x: none;
    font-family:
      Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
      sans-serif;
  }

  :global(:root) {
    --knowledge-muted: #475569;
    --knowledge-primary-bg: #172554;
    --knowledge-primary-text: #ffffff;
    --knowledge-warning-text: #92400e;
  }

  :global(html[data-theme='dark']) {
    --knowledge-muted: #b7c6da;
    --knowledge-primary-bg: #172554;
    --knowledge-primary-text: #ffffff;
    --knowledge-warning-text: #fde68a;
  }

  button,
  input,
  textarea,
  select {
    box-sizing: border-box;
    font: inherit;
  }

  button,
  summary,
  select,
  input,
  textarea {
    border-radius: 8px;
  }

  button,
  summary {
    cursor: pointer;
  }

  h1,
  h2,
  h3,
  p {
    margin: 0;
  }

  .knowledge-shell {
    min-height: 100dvh;
    background: var(--bg, #eef2f7);
    overflow-x: clip;
    overscroll-behavior-x: none;
  }

  .knowledge-page {
    display: grid;
    grid-template-columns: minmax(20rem, 25rem) minmax(0, 1fr);
    min-height: calc(100dvh - 4.15rem);
    min-width: 0;
    max-width: 100%;
    overflow-x: clip;
    overscroll-behavior-x: none;
  }

  .knowledge-page.loading-state {
    grid-template-columns: minmax(0, 1fr);
    align-items: start;
    padding: 1rem;
  }

  .knowledge-loading-state {
    display: grid;
    gap: 0.9rem;
    width: min(100%, 72rem);
    min-width: 0;
    margin: 0 auto;
  }

  .loading-topline,
  .loading-card {
    min-width: 0;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .loading-topline {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 1rem;
    padding: 1rem;
  }

  .loading-topline h1 {
    color: var(--text-strong, #0f172a);
    font-size: 1.45rem;
    line-height: 1.15;
  }

  .loading-topline p {
    margin-top: 0.35rem;
    color: var(--knowledge-muted, #475569);
  }

  .loading-topline .status-pill {
    align-self: flex-start;
    width: fit-content;
  }

  .loading-mobile-toolbar {
    display: none;
  }

  .loading-tabs {
    display: flex;
    flex-wrap: wrap;
    gap: 0.45rem;
  }

  .loading-tabs span,
  .loading-control {
    min-height: 2.35rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .loading-tabs span {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0.45rem 0.9rem;
    color: var(--text, #172033);
    font-weight: 800;
  }

  .loading-tabs span.active {
    border-color: var(--knowledge-primary-bg, #172554);
    background: var(--knowledge-primary-bg, #172554);
    color: var(--knowledge-primary-text, #ffffff);
  }

  .loading-grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(18rem, 0.75fr);
    gap: 0.9rem;
    min-width: 0;
  }

  .loading-card {
    display: grid;
    gap: 0.7rem;
    padding: 1rem;
  }

  .loading-label,
  .loading-title,
  .loading-bar,
  .loading-row {
    display: block;
    border-radius: 999px;
    background: color-mix(in srgb, var(--border, #cbd5e1) 72%, var(--panel, #ffffff));
  }

  .loading-label {
    width: 8rem;
    height: 0.75rem;
  }

  .loading-title {
    width: min(100%, 24rem);
    height: 1.25rem;
  }

  .loading-bar {
    width: min(100%, 34rem);
    height: 0.85rem;
  }

  .loading-bar.large {
    width: min(100%, 42rem);
  }

  .loading-bar.short,
  .loading-row.short {
    width: min(62%, 18rem);
  }

  .loading-row {
    width: 100%;
    height: 2.1rem;
    border-radius: 8px;
  }

  .space-list {
    display: flex;
    flex-direction: column;
    gap: 0.85rem;
    min-width: 0;
    padding: 1rem;
    border-right: 1px solid var(--border-soft, #dbe3ef);
    background: var(--panel, #f8fafc);
  }

  .space-detail {
    min-width: 0;
    max-width: 100%;
    padding: 1.2rem;
    background: var(--bg, #eef2f7);
    overflow-x: clip;
    overscroll-behavior-x: none;
  }

  .space-header,
  .detail-header,
  .form-footer,
  .research-controls,
  .report-card header,
  .record-inventory header,
  .header-link-group,
  .detail-actions {
    display: flex;
    align-items: center;
    gap: 0.7rem;
  }

  .space-header,
  .detail-header,
  .form-footer,
  .report-card header,
  .record-inventory header {
    justify-content: space-between;
  }

  .header-link-group {
    flex: 0 0 auto;
    justify-content: flex-end;
  }

  .space-header h1 {
    color: var(--text-strong, #0f172a);
    font-size: 1.45rem;
    line-height: 1.15;
  }

  .space-header span,
  .panel-title p,
  .source-card p,
  .empty,
  .empty-detail p {
    color: var(--knowledge-muted, #475569);
  }

  .space-header button,
  .form-footer button,
  .research-controls button,
  .tabs button {
    min-height: 2.4rem;
    border: 1px solid var(--border, #cbd5e1);
    background: var(--panel, #ffffff);
    color: var(--text, #172033);
    font-weight: 700;
  }

  .space-header button,
  .form-footer button,
  .research-controls button {
    padding: 0.45rem 0.75rem;
  }

  .sync-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    color: var(--knowledge-primary-text, #ffffff) !important;
    border-color: var(--knowledge-primary-bg, #172554) !important;
    background: var(--knowledge-primary-bg, #172554) !important;
  }

  .sync-button span {
    color: var(--knowledge-primary-text, #ffffff);
  }

  .sync-button svg,
  .icon-button svg,
  .icon-action svg,
  .run-resume-action svg,
  .source-delete-action svg {
    width: 1rem;
    height: 1rem;
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  button:disabled {
    cursor: not-allowed;
    opacity: 0.55;
  }

  .space-metrics,
  .insight-bar {
    display: grid;
    gap: 0.7rem;
  }

  .space-metrics {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .insight-bar {
    grid-template-columns: minmax(0, 1fr) minmax(18rem, 0.9fr);
    margin: 1rem 0;
  }

  .insight-card {
    min-width: 0;
    padding: 0.8rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    background: var(--panel, #ffffff);
    border-radius: 8px;
  }

  .space-metrics strong,
  .insight-bar strong {
    display: block;
    overflow-wrap: anywhere;
    color: var(--text-strong, #0f172a);
    font-size: 1.15rem;
  }

  .space-metrics span,
  .insight-bar span,
  .eyebrow,
  .report-card header span,
  .form-footer span {
    color: var(--knowledge-muted, #475569);
    font-size: 0.78rem;
    font-weight: 800;
    letter-spacing: 0;
    text-transform: uppercase;
  }

  .space-metrics span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-transform: none;
  }

  .search-control {
    position: relative;
    display: block;
  }

  .search,
  input,
  textarea,
  select {
    width: 100%;
    min-width: 0;
    box-sizing: border-box;
    border: 1px solid var(--border, #cbd5e1);
    background: var(--panel, #ffffff);
    color: var(--text, #172033);
  }

  .search,
  input,
  select {
    min-height: 2.5rem;
    padding: 0.5rem 0.65rem;
  }

  .search-control .search {
    padding-right: 2.8rem;
  }

  .icon-button {
    position: absolute;
    top: 0.25rem;
    right: 0.25rem;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2rem;
    min-height: 2rem;
    padding: 0;
  }

  textarea {
    padding: 0.65rem;
    resize: vertical;
  }

  label {
    color: var(--text-strong, #0f172a);
    font-weight: 700;
  }

  .hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  .create-space,
  .add-source {
    border: 1px solid var(--border-soft, #dbe3ef);
    background: var(--panel, #ffffff);
    border-radius: 8px;
  }

  .create-space summary,
  .add-source summary {
    padding: 0.75rem;
    color: var(--text-strong, #0f172a);
    font-weight: 800;
  }

  .create-space form,
  .source-form,
  .research-form {
    display: grid;
    gap: 0.7rem;
  }

  .create-space form,
  .add-source form {
    padding: 0 0.75rem 0.75rem;
  }

  .notice {
    padding: 0.65rem 0.75rem;
    border-radius: 8px;
    border: 1px solid var(--border, #cbd5e1);
    background: var(--panel, #ffffff);
    font-weight: 700;
  }

  .notice.error {
    color: var(--danger, #dc2626);
    border-color: color-mix(in srgb, var(--danger, #dc2626) 35%, var(--border, #cbd5e1));
  }

  .notice.success {
    color: #166534;
    border-color: color-mix(in srgb, var(--success, #16a34a) 35%, var(--border, #cbd5e1));
  }

  .text-action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: fit-content;
    min-height: 2.35rem;
    padding: 0.4rem 0.75rem;
    color: var(--knowledge-primary-bg, #172554);
    border: 1px solid var(--knowledge-primary-bg, #172554);
    background: var(--panel, #ffffff);
    font-weight: 800;
    text-decoration: none;
  }

  .permalink {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    width: 1.75rem;
    min-width: 1.75rem;
    height: 1.75rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 999px;
    color: var(--knowledge-muted, #475569);
    background: var(--panel, #ffffff);
    font-size: 0.78rem;
    font-weight: 850;
    text-decoration: none;
  }

  .permalink:hover,
  .permalink:focus-visible {
    border-color: var(--primary, #2563eb);
    box-shadow: 0 0 0 1px var(--primary, #2563eb);
    color: var(--primary, #2563eb);
  }

  .danger-action {
    width: fit-content;
    min-height: 2.35rem;
    padding: 0.4rem 0.75rem;
    border: 1px solid color-mix(in srgb, var(--danger, #dc2626) 55%, var(--border, #cbd5e1));
    background: var(--panel, #ffffff);
    color: var(--danger, #dc2626);
    font-weight: 800;
  }

  .danger-action.solid {
    border-color: var(--danger, #dc2626);
    background: var(--danger, #dc2626);
    color: #ffffff;
  }

  .danger-action.compact {
    min-height: 2rem;
    padding: 0.3rem 0.55rem;
    font-size: 0.82rem;
  }

  .source-delete-action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.35rem;
  }

  .button-row {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 0.55rem;
    min-width: 0;
  }

  .mobile-corpus-bar,
  .mobile-space-browser,
  .mobile-space-options,
  .mobile-notice {
    display: none;
  }

  .mobile-corpus-primary {
    display: grid;
    grid-template-columns: minmax(0, 1fr) repeat(4, 2.35rem);
    gap: 0.35rem;
    align-items: center;
  }

  .icon-action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.35rem;
    min-height: 2.35rem;
    padding: 0;
    border: 1px solid var(--border, #cbd5e1);
    background: var(--panel, #ffffff);
    color: var(--text, #172033);
  }

  .icon-action[aria-expanded='true'] {
    border-color: var(--knowledge-primary-bg, #172554);
    color: var(--knowledge-primary-bg, #172554);
    box-shadow: 0 0 0 1px var(--knowledge-primary-bg, #172554);
  }

  .mobile-stat-row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
  }

  .mobile-stat-row span,
  .mobile-space-browser header span,
  .mobile-space-options header span {
    color: var(--knowledge-muted, #475569);
    font-size: 0.74rem;
    font-weight: 800;
  }

  .mobile-stat-row span {
    padding: 0.18rem 0.42rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 999px;
    background: var(--bg, #eef2f7);
  }

  .mobile-space-browser,
  .mobile-space-options {
    gap: 0.65rem;
    margin-bottom: 0.75rem;
    padding: 0.7rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .mobile-space-browser header,
  .mobile-space-options header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.65rem;
    min-width: 0;
  }

  .mobile-space-browser header strong,
  .mobile-space-options header strong {
    color: var(--text-strong, #0f172a);
    overflow-wrap: anywhere;
  }

  .mobile-space-rows {
    display: grid;
    gap: 0.55rem;
    max-height: 14rem;
    overflow: auto;
  }

  .mobile-option-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 0.45rem;
  }

  .management-panel,
  .danger-panel {
    box-sizing: border-box;
    min-width: 0;
    margin-top: 0.8rem;
    padding: 0.85rem;
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .management-panel {
    border: 1px solid var(--border-soft, #dbe3ef);
  }

  .danger-panel {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.9rem;
    border: 1px solid color-mix(in srgb, var(--danger, #dc2626) 35%, var(--border, #cbd5e1));
  }

  .danger-panel > div {
    min-width: 0;
  }

  .danger-panel strong {
    color: var(--text-strong, #0f172a);
    overflow-wrap: anywhere;
  }

  .danger-panel p {
    margin-top: 0.3rem;
    color: var(--knowledge-muted, #475569);
    line-height: 1.45;
  }

  .source-delete-panel {
    margin-bottom: 0.75rem;
  }

  .rows,
  .source-list,
  .reports-list,
  .runs-list,
  .run-events,
  .run-plan,
  .research-loops,
  .run-coverage,
  .claims-list,
  .evidence-list {
    display: grid;
    gap: 0.7rem;
  }

  .space-row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.65rem;
    min-width: 0;
    padding: 0.75rem;
    border: 1px solid transparent;
    border-radius: 8px;
    color: inherit;
    text-decoration: none;
    background: var(--panel, #ffffff);
  }

  .space-row:hover,
  .space-row.selected {
    border-color: var(--primary, #2563eb);
    box-shadow: 0 0 0 1px var(--primary, #2563eb);
  }

  .space-row strong,
  .space-row small,
  .space-row em {
    overflow-wrap: anywhere;
  }

  .space-row strong {
    display: block;
    color: var(--text-strong, #0f172a);
  }

  .space-row small,
  .space-row em {
    color: var(--knowledge-muted, #475569);
    font-style: normal;
  }

  .dot {
    width: 0.65rem;
    height: 0.65rem;
    border-radius: 999px;
    background: var(--secondary, #0f766e);
  }

  .detail-header {
    align-items: flex-start;
    gap: 1rem;
  }

  .detail-header h2 {
    color: var(--text-strong, #0f172a);
    font-size: clamp(1.35rem, 2vw, 2rem);
    line-height: 1.12;
  }

  .detail-actions {
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .detail-actions span {
    padding: 0.35rem 0.6rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 999px;
    background: var(--panel, #ffffff);
    color: var(--knowledge-muted, #475569);
    font-weight: 800;
  }

  .tabs {
    display: flex;
    flex-wrap: wrap;
    gap: 0.45rem;
    min-width: 0;
    max-width: 100%;
    overflow-x: clip;
    padding-bottom: 0.2rem;
  }

  .tabs button {
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
    flex: 0 0 auto;
    max-width: 100%;
    padding: 0.45rem 0.9rem;
  }

  .tabs button span {
    min-width: 0;
    overflow-wrap: anywhere;
    white-space: normal;
  }

  .panel-label-short {
    display: none;
  }

  .tabs button small {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 1.35rem;
    min-height: 1.35rem;
    padding: 0 0.25rem;
    border-radius: 999px;
    color: var(--knowledge-muted, #475569);
    background: var(--bg, #eef2f7);
    font-size: 0.75rem;
    font-weight: 850;
  }

  .tabs button.active {
    border-color: var(--knowledge-primary-bg, #172554);
    background: var(--knowledge-primary-bg, #172554);
    color: var(--knowledge-primary-text, #ffffff);
  }

  .tabs button.active small {
    color: var(--knowledge-primary-bg, #172554);
    background: var(--knowledge-primary-text, #ffffff);
  }

  .panel {
    margin-top: 0.8rem;
    min-width: 0;
    max-width: 100%;
    overflow-x: clip;
  }

  .panel-title {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.75rem;
  }

  .panel-title h3 {
    color: var(--text-strong, #0f172a);
    font-size: 1rem;
  }

  .sources-panel {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(18rem, 25rem);
    gap: 1rem;
    align-items: start;
  }

  .source-list-section {
    min-width: 0;
    max-width: 100%;
    overflow-x: clip;
    overscroll-behavior-x: none;
  }

  .form-grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(8rem, 12rem);
    gap: 0.7rem;
  }

  .form-grid label {
    grid-column: span 1;
  }

  .form-grid label[for='source-uri'],
  .form-grid input#source-uri {
    grid-column: 1 / -1;
  }

  .space-edit-grid {
    grid-template-columns: 1fr;
  }

  .space-edit-grid label,
  .space-edit-grid input,
  .space-edit-grid textarea {
    grid-column: 1 / -1;
  }

  .source-card,
  .report-card,
  .record-row,
  .evidence-list section {
    min-width: 0;
    max-width: 100%;
    box-sizing: border-box;
    padding: 0.9rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .report-card,
  .record-row,
  .evidence-list section,
  .source-candidates section,
  .run-events section {
    scroll-margin-top: 8rem;
  }

  .source-card-collapsible {
    padding: 0;
    overflow: hidden;
    overscroll-behavior-x: none;
    scroll-margin-top: 5rem;
  }

  .source-card-collapsible.highlighted {
    border-color: var(--primary, #2563eb);
    box-shadow: 0 0 0 1px var(--primary, #2563eb);
  }

  .source-summary {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.65rem;
    min-width: 0;
    padding: 0.85rem 0.9rem;
    color: var(--text, #172033);
  }

  .source-summary:focus-visible {
    outline: 2px solid var(--primary, #2563eb);
    outline-offset: -2px;
  }

  .source-summary-main {
    display: grid;
    gap: 0.15rem;
    min-width: 0;
  }

  .source-kind {
    color: var(--knowledge-muted, #475569);
    font-size: 0.78rem;
    font-weight: 800;
    letter-spacing: 0;
    text-transform: uppercase;
  }

  .source-card-body {
    min-width: 0;
    overflow-x: clip;
    overscroll-behavior-x: none;
    padding: 0 0.9rem 0.9rem;
    border-top: 1px solid var(--border-soft, #dbe3ef);
  }

  .source-card-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin: 0.75rem 0 0.65rem;
    min-width: 0;
  }

  .source-card-body > .markdown-block.compact {
    margin-top: 0;
  }

  .record-row {
    width: 100%;
    color: inherit;
    text-align: left;
  }

  .record-row-wrap,
  .run-status-actions {
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
    min-width: 0;
    max-width: 100%;
  }

  .report-card,
  .record-workspace,
  .record-inventory,
  .record-table,
  .record-row-wrap,
  .record-row,
  .record-row-main,
  .record-row-meta,
  .source-card,
  .source-card-body,
  .source-summary,
  .run-panel,
  .research-panel,
  .sources-panel,
  .runs-list,
  .reports-list,
  .knowledge-disclosure,
  .disclosure-body {
    min-width: 0;
    max-width: 100%;
  }

  .record-row:hover,
  .record-row:focus-visible,
  .record-row.active {
    border-color: var(--primary, #2563eb);
    box-shadow: 0 0 0 1px var(--primary, #2563eb);
  }

  .source-card h3,
  .report-card h3,
  .record-row strong {
    color: var(--text-strong, #0f172a);
    font-size: 1rem;
    line-height: 1.25;
    overflow-wrap: anywhere;
  }

  .source-card p,
  .source-analysis p,
  .record-row small {
    margin-top: 0.55rem;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .record-workspace {
    display: grid;
    grid-template-columns: minmax(18rem, 24rem) minmax(0, 1fr);
    gap: 1rem;
    align-items: start;
  }

  .reports-workspace .record-inventory {
    order: 1;
  }

  .reports-workspace .record-detail {
    order: 2;
  }

  .research-sidebar,
  .record-inventory,
  .record-table,
  .record-row-main {
    display: grid;
    gap: 0.65rem;
  }

  .research-sidebar .record-inventory {
    order: 3;
  }

  .new-research-panel {
    order: 1;
    min-width: 0;
    max-width: 100%;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
    overflow: hidden;
  }

  .corpus-result-card {
    order: 2;
  }

  .new-research-panel > summary {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: 0.5rem;
    padding: 0.7rem;
    color: var(--text-strong, #0f172a);
    font-weight: 850;
    list-style: none;
  }

  .new-research-panel > summary::-webkit-details-marker {
    display: none;
  }

  .new-research-panel > summary::before {
    content: '';
    width: 0;
    height: 0;
    border-top: 0.32rem solid transparent;
    border-bottom: 0.32rem solid transparent;
    border-left: 0.42rem solid currentColor;
    transition: transform 120ms ease;
  }

  .new-research-panel[open] > summary::before {
    transform: rotate(90deg);
  }

  .new-research-panel > summary > span {
    display: grid;
    gap: 0.1rem;
    min-width: 0;
  }

  .new-research-panel > summary span span {
    color: var(--knowledge-muted, #475569);
    font-size: 0.78rem;
    font-weight: 800;
    overflow-wrap: anywhere;
  }

  .new-research-panel .research-form {
    padding: 0 0.7rem 0.7rem;
  }

  .record-inventory {
    padding: 0.75rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: color-mix(in srgb, var(--panel, #ffffff) 72%, var(--bg, #eef2f7));
  }

  .record-inventory h3 {
    color: var(--text-strong, #0f172a);
    font-size: 1rem;
  }

  .record-inventory p {
    color: var(--knowledge-muted, #475569);
    font-weight: 750;
  }

  .record-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(4rem, auto) auto;
    align-items: center;
    gap: 0.65rem;
    text-decoration: none;
  }

  .record-row-wrap {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .research-record-row {
    grid-template-columns: auto minmax(0, 1fr) minmax(4rem, auto) auto;
  }

  .record-status-dot {
    --dot-ring: color-mix(in srgb, var(--knowledge-muted, #475569) 20%, transparent);
    display: inline-block;
    position: relative;
    width: 0.72rem;
    height: 0.72rem;
    border-radius: 999px;
    background: var(--knowledge-muted, #475569);
    box-shadow: 0 0 0 3px var(--dot-ring);
  }

  .record-status-dot.success {
    --dot-ring: color-mix(in srgb, var(--success, #16a34a) 20%, transparent);
    background: var(--success, #16a34a);
  }

  .record-status-dot.active {
    --dot-ring: color-mix(in srgb, var(--primary, #2563eb) 20%, transparent);
    background: var(--primary, #2563eb);
  }

  .record-status-dot.danger {
    --dot-ring: color-mix(in srgb, var(--danger, #dc2626) 20%, transparent);
    background: var(--danger, #dc2626);
  }

  .record-status-dot.pulse {
    animation: knowledge-activity-ring 2.4s ease-in-out infinite;
  }

  .run-resume-action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.2rem;
    min-width: 2.2rem;
    min-height: 2.2rem;
    padding: 0;
    border: 1px solid color-mix(in srgb, var(--danger, #dc2626) 35%, var(--border, #cbd5e1));
    border-radius: 999px;
    background: var(--panel, #ffffff);
    color: var(--danger, #dc2626);
  }

  .run-resume-action:hover,
  .run-resume-action:focus-visible {
    border-color: var(--danger, #dc2626);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--danger, #dc2626) 18%, transparent);
  }

  .run-resume-action.inline {
    width: 1.85rem;
    min-width: 1.85rem;
    min-height: 1.85rem;
  }

  @keyframes knowledge-activity-ring {
    0%,
    100% {
      box-shadow: 0 0 0 3px var(--dot-ring);
    }
    50% {
      box-shadow: 0 0 0 6px color-mix(in srgb, var(--primary, #2563eb) 10%, transparent);
    }
  }

  .record-row-main {
    gap: 0.25rem;
  }

  .record-row-main small,
  .record-row-time,
  .record-row-meta small {
    color: var(--knowledge-muted, #475569);
    font-size: 0.78rem;
    font-weight: 800;
  }

  .record-row-main small {
    margin-top: 0;
  }

  .record-row-meta {
    display: grid;
    justify-items: end;
    gap: 0.1rem;
  }

  .record-row-meta strong {
    font-size: 0.95rem;
  }

  .record-type {
    width: fit-content;
    padding: 0.18rem 0.42rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 999px;
    color: var(--knowledge-muted, #475569);
    background: var(--bg, #eef2f7);
    font-size: 0.72rem;
    font-weight: 850;
    text-transform: uppercase;
  }

  .detail-summary,
  .markdown-block {
    min-width: 0;
    max-width: 100%;
  }

  .markdown-block :global(.markdown),
  .markdown-block :global(.markdown *) {
    max-width: 100%;
    overflow-wrap: anywhere;
  }

  .markdown-block :global(.markdown pre),
  .markdown-block :global(.markdown table) {
    max-width: 100%;
    overflow-x: clip;
    overscroll-behavior-x: none;
  }

  .markdown-block :global(.markdown pre) {
    white-space: pre-wrap;
    word-break: break-word;
  }

  .markdown-block :global(.markdown pre code) {
    min-width: 0;
    width: 100%;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .markdown-block :global(.markdown table) {
    display: table;
    table-layout: fixed;
    width: 100%;
  }

  .markdown-block :global(a[href^="#knowledge-source-"]) {
    color: var(--knowledge-primary-bg, #172554);
    font-weight: 800;
    text-decoration-thickness: 0.08em;
    text-underline-offset: 0.16em;
  }

  .detail-summary {
    margin-top: 0.35rem;
    max-width: 62rem;
  }

  .detail-summary :global(.markdown),
  .markdown-block :global(.markdown) {
    color: var(--text, #172033);
  }

  .detail-summary :global(.markdown p),
  .markdown-block :global(.markdown p),
  .markdown-block :global(.markdown li) {
    color: var(--text, #172033);
  }

  .detail-summary :global(.markdown h1),
  .detail-summary :global(.markdown h2),
  .detail-summary :global(.markdown h3),
  .detail-summary :global(.markdown h4),
  .detail-summary :global(.markdown h5),
  .detail-summary :global(.markdown h6),
  .markdown-block :global(.markdown h1),
  .markdown-block :global(.markdown h2),
  .markdown-block :global(.markdown h3),
  .markdown-block :global(.markdown h4),
  .markdown-block :global(.markdown h5),
  .markdown-block :global(.markdown h6),
  .markdown-block :global(.markdown strong) {
    color: var(--text-strong, #0f172a);
  }

  .markdown-block.compact {
    margin-top: 0.55rem;
  }

  .markdown-block.compact :global(.markdown h1),
  .markdown-block.compact :global(.markdown h2),
  .markdown-block.compact :global(.markdown h3) {
    font-size: 1rem;
  }

  .answer-body {
    margin-top: 0.75rem;
  }

  .evidence-body {
    margin-top: 0.55rem;
  }

  .evidence-heading {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.55rem;
    min-width: 0;
  }

  .evidence-heading strong,
  .evidence-heading .source-reference-link {
    min-width: 0;
  }

  .source-details {
    margin-top: 0.75rem;
    max-width: 100%;
    overflow-x: clip;
    overscroll-behavior-x: none;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--bg, #eef2f7);
  }

  .source-details > summary {
    padding: 0.55rem 0.7rem;
    color: var(--text-strong, #0f172a);
    font-weight: 800;
  }

  .source-details-body {
    display: grid;
    gap: 0.65rem;
    min-width: 0;
    max-width: 100%;
    overflow-x: clip;
    overscroll-behavior-x: none;
    padding: 0 0.7rem 0.7rem;
  }

  .source-details-body > * {
    margin-top: 0;
  }

  .source-content {
    margin-top: 0.75rem;
    max-width: 100%;
    overflow-x: clip;
    overscroll-behavior-x: none;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--bg, #eef2f7);
  }

  .source-details .source-content {
    background: var(--panel, #ffffff);
  }

  .source-content summary {
    padding: 0.55rem 0.7rem;
    color: var(--text-strong, #0f172a);
    font-weight: 800;
  }

  .source-body {
    min-width: 0;
    max-width: 100%;
    overflow-x: clip;
    overscroll-behavior-x: none;
    padding: 0 0.7rem 0.7rem;
  }

  .source-state {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: 0.45rem;
    min-width: 0;
  }

  .status-pill {
    display: inline-flex;
    align-items: center;
    min-height: 1.6rem;
    padding: 0.25rem 0.5rem;
    border-radius: 999px;
    border: 1px solid var(--border, #cbd5e1);
    background: var(--bg, #eef2f7);
    color: var(--knowledge-muted, #475569);
    font-size: 0.76rem;
    font-weight: 850;
    text-transform: none;
  }

  .report-card header .status-pill,
  .record-row .status-pill {
    color: var(--knowledge-muted, #475569);
    font-size: 0.76rem;
    text-transform: none;
  }

  .report-card header .status-pill.success,
  .record-row .status-pill.success {
    border-color: color-mix(in srgb, var(--success, #16a34a) 35%, var(--border, #cbd5e1));
    color: #166534;
  }

  .report-card header .status-pill.danger,
  .record-row .status-pill.danger,
  .source-error {
    color: var(--danger, #dc2626);
  }

  .report-card header .status-pill.danger,
  .record-row .status-pill.danger {
    border-color: color-mix(in srgb, var(--danger, #dc2626) 35%, var(--border, #cbd5e1));
  }

  .report-card header .status-pill.active,
  .record-row .status-pill.active {
    border-color: color-mix(in srgb, var(--primary, #2563eb) 35%, var(--border, #cbd5e1));
    color: var(--primary, #2563eb);
  }

  .source-meta {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(9rem, 1fr));
    gap: 0.45rem;
    margin: 0.75rem 0 0;
  }

  .source-meta div {
    min-width: 0;
  }

  .source-meta dt {
    color: var(--knowledge-muted, #475569);
    font-size: 0.75rem;
    font-weight: 850;
  }

  .source-meta dd {
    margin: 0.15rem 0 0;
    color: var(--text, #172033);
    overflow-wrap: anywhere;
  }

  .source-details-body > .source-meta {
    margin: 0;
  }

  .source-list,
  .source-list-section {
    margin-top: 1rem;
  }

  .claims-list,
  .source-analysis,
  .run-plan,
  .run-answer,
  .run-note,
  .research-loops,
  .run-coverage {
    margin-top: 0.75rem;
  }

  .claims-list section,
  .source-analysis,
  .run-plan .disclosure-body > section,
  .run-coverage .disclosure-body > section {
    min-width: 0;
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--bg, #eef2f7);
  }

  .source-analysis {
    display: grid;
    gap: 0.55rem;
    grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
  }

  .source-details-body > .claims-list,
  .source-details-body > .source-analysis,
  .source-details-body > .chips,
  .source-details-body > .source-content {
    margin-top: 0;
  }

  .run-answer {
    background: color-mix(in srgb, var(--primary, #2563eb) 7%, var(--panel, #ffffff));
  }

  .loop-subsection {
    display: grid;
    gap: 0.35rem;
    margin-top: 0.65rem;
    min-width: 0;
  }

  .research-loops small,
  .run-coverage small {
    color: var(--knowledge-muted, #475569);
    font-size: 0.78rem;
    font-weight: 800;
  }

  .claims-list strong,
  .source-analysis strong,
  .run-plan strong,
  .run-answer strong,
  .run-note strong,
  .research-loops strong,
  .run-coverage strong {
    color: var(--text-strong, #0f172a);
    overflow-wrap: anywhere;
  }

  .run-plan ol {
    margin: 0.45rem 0 0;
    padding-left: 1.25rem;
  }

  .run-plan li {
    margin-top: 0.25rem;
    overflow-wrap: anywhere;
  }

  .knowledge-disclosure {
    box-sizing: border-box;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--bg, #eef2f7);
    overflow: hidden;
  }

  .knowledge-disclosure.nested {
    margin-top: 0.65rem;
    background: var(--panel, #ffffff);
  }

  .knowledge-disclosure > summary {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.55rem;
    min-width: 0;
    padding: 0.65rem;
    color: var(--text-strong, #0f172a);
    font-weight: 850;
    list-style: none;
  }

  .knowledge-disclosure > summary::-webkit-details-marker {
    display: none;
  }

  .knowledge-disclosure > summary::before {
    content: '';
    width: 0;
    height: 0;
    border-top: 0.32rem solid transparent;
    border-bottom: 0.32rem solid transparent;
    border-left: 0.42rem solid currentColor;
    transition: transform 120ms ease;
  }

  .knowledge-disclosure[open] > summary::before {
    transform: rotate(90deg);
  }

  .knowledge-disclosure > summary > span:first-of-type {
    display: grid;
    gap: 0.12rem;
    min-width: 0;
  }

  .knowledge-disclosure > summary strong {
    overflow-wrap: anywhere;
  }

  .knowledge-disclosure > summary > span:first-of-type > span,
  .knowledge-disclosure > summary > span:first-of-type > small {
    color: var(--knowledge-muted, #475569);
    font-size: 0.78rem;
    font-weight: 800;
    overflow-wrap: anywhere;
  }

  .knowledge-disclosure > summary .candidate-status {
    justify-self: end;
    max-width: 100%;
  }

  .disclosure-body {
    display: grid;
    gap: 0.65rem;
    padding: 0 0.65rem 0.65rem;
  }

  .disclosure-body > * {
    min-width: 0;
    max-width: 100%;
  }

  .disclosure-body > .markdown-block,
  .disclosure-body > .claims-list,
  .disclosure-body > .evidence-list,
  .disclosure-body > .chips,
  .disclosure-body > .candidate-meta {
    margin-top: 0;
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem;
    margin-top: 0.65rem;
  }

  .chips span {
    max-width: 100%;
    padding: 0.3rem 0.5rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 999px;
    color: var(--knowledge-muted, #475569);
    background: var(--bg, #eef2f7);
    font-size: 0.82rem;
    font-weight: 700;
    overflow-wrap: anywhere;
  }

  .research-panel {
    display: grid;
    grid-template-columns: minmax(18rem, 24rem) minmax(0, 1fr);
    gap: 1rem;
    align-items: start;
  }

  .run-panel {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 1rem;
    align-items: start;
  }

  .back-to-records {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    width: fit-content;
    min-height: 2rem;
    margin-bottom: 0.55rem;
    padding: 0.3rem 0.55rem;
    border: 1px solid var(--border, #cbd5e1);
    background: var(--panel, #ffffff);
    color: var(--knowledge-primary-bg, #172554);
    font-size: 0.82rem;
    font-weight: 850;
  }

  .back-to-records svg {
    width: 1rem;
    height: 1rem;
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .research-controls {
    flex-wrap: wrap;
  }

  .choice-group {
    display: grid;
    gap: 0.45rem;
    min-width: 0;
    margin: 0;
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .choice-group legend {
    padding: 0 0.2rem;
    color: var(--text-strong, #0f172a);
    font-size: 0.86rem;
    font-weight: 850;
  }

  .choice-group label {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: 0.5rem;
    align-items: flex-start;
    min-width: 0;
    padding: 0.5rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--bg, #eef2f7);
  }

  .choice-group label.checked {
    border-color: var(--primary, #2563eb);
    box-shadow: 0 0 0 1px var(--primary, #2563eb);
    background: color-mix(in srgb, var(--primary, #2563eb) 7%, var(--panel, #ffffff));
  }

  .choice-group input {
    width: 1rem;
    height: 1rem;
    margin-top: 0.1rem;
  }

  .choice-group span {
    display: grid;
    gap: 0.12rem;
    min-width: 0;
  }

  .choice-group strong {
    color: var(--text-strong, #0f172a);
  }

  .choice-group small {
    color: var(--knowledge-muted, #475569);
    line-height: 1.35;
    overflow-wrap: anywhere;
  }

  .choice-group.compact {
    grid-template-columns: 1fr;
  }

  .choice-group.compact legend {
    grid-column: 1 / -1;
  }

  .primary-actions {
    justify-content: flex-end;
  }

  .research-controls label {
    min-width: 100%;
  }

  .inline-check {
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
    min-width: min(100%, 16rem);
    box-sizing: border-box;
    padding: 0.45rem 0.55rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--bg, #eef2f7);
    color: var(--text, #172033);
    font-weight: 800;
  }

  .inline-check input {
    width: 1rem;
    min-width: 1rem;
    height: 1rem;
  }

  .inline-check span {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .source-picker {
    min-width: 0;
    max-width: 100%;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .source-picker > summary {
    padding: 0.6rem 0.7rem;
    color: var(--text-strong, #0f172a);
    font-weight: 800;
    overflow-wrap: anywhere;
  }

  .source-picker-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    min-width: 0;
    padding: 0 0.65rem 0.65rem;
  }

  .source-picker-actions button {
    min-height: 2.25rem;
    padding: 0.35rem 0.65rem;
    border: 1px solid var(--border, #cbd5e1);
    background: var(--panel, #ffffff);
    color: var(--text, #172033);
    font-weight: 800;
  }

  .source-select {
    display: grid;
    gap: 0.45rem;
    max-height: 16rem;
    overflow: auto;
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .source-picker .source-select {
    margin: 0 0.65rem 0.65rem;
    max-height: 12rem;
  }

  .source-select label {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    min-width: 0;
    font-weight: 600;
  }

  .source-select input {
    width: 1rem;
    min-height: 1rem;
  }

  .source-select span {
    overflow-wrap: anywhere;
  }

  .evidence-list {
    margin-top: 0.8rem;
  }

  .evidence-list strong {
    overflow-wrap: anywhere;
  }

  .source-reference-link {
    display: inline-block;
    max-width: 100%;
    color: var(--knowledge-primary-bg, #172554);
    font-weight: 850;
    overflow-wrap: anywhere;
    text-decoration-thickness: 0.08em;
    text-underline-offset: 0.16em;
  }

  .evidence-list small {
    display: block;
    margin-top: 0.4rem;
    color: var(--knowledge-muted, #475569);
    overflow-wrap: anywhere;
  }

  .run-events section {
    min-width: 0;
    padding: 0.65rem 0;
    border-top: 1px solid var(--border-soft, #dbe3ef);
  }

  .run-events strong {
    color: var(--text-strong, #0f172a);
    text-transform: capitalize;
  }

  .source-candidates {
    display: grid;
    gap: 0.55rem;
    margin-top: 0.75rem;
  }

  .source-candidates section {
    min-width: 0;
    padding: 0.7rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .source-candidates header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.6rem;
    min-width: 0;
  }

  .source-candidates strong,
  .source-candidates a,
  .source-candidates small {
    overflow-wrap: anywhere;
  }

  .candidate-meta {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr));
    gap: 0.45rem;
    margin: 0.6rem 0 0;
  }

  .candidate-meta div {
    min-width: 0;
  }

  .candidate-meta dt {
    color: var(--knowledge-muted, #475569);
    font-size: 0.72rem;
    font-weight: 850;
  }

  .candidate-meta dd {
    margin: 0.12rem 0 0;
    color: var(--text, #172033);
    overflow-wrap: anywhere;
  }

  .evidence-trace {
    grid-template-columns: repeat(auto-fit, minmax(7.5rem, 1fr));
    padding-top: 0.1rem;
  }

  .source-candidates a,
  .source-candidates small {
    display: block;
    margin-top: 0.4rem;
    color: var(--knowledge-muted, #475569);
  }

  .source-candidates small .source-reference-link {
    display: inline;
    margin-top: 0;
  }

  .candidate-status {
    display: inline-flex;
    align-items: center;
    min-height: 1.45rem;
    padding: 0.18rem 0.45rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 999px;
    color: var(--knowledge-muted, #475569);
    font-size: 0.72rem;
    font-weight: 850;
    text-transform: capitalize;
  }

  .candidate-status.imported,
  .candidate-status.accepted,
  .candidate-status.covered,
  .candidate-status.complete,
  .candidate-status.completed {
    border-color: color-mix(in srgb, var(--success, #16a34a) 35%, var(--border, #cbd5e1));
    color: #166534;
  }

  .candidate-status.failed,
  .candidate-status.rejected,
  .candidate-status.gap {
    border-color: color-mix(in srgb, var(--danger, #dc2626) 35%, var(--border, #cbd5e1));
    color: var(--danger, #dc2626);
  }

  .candidate-status.partial,
  .candidate-status.continue,
  .candidate-status.searching,
  .candidate-status.reading,
  .candidate-status.evaluating {
    border-color: color-mix(in srgb, var(--warning, #d97706) 35%, var(--border, #cbd5e1));
    color: var(--knowledge-warning-text, #92400e);
  }

  .empty,
  .empty-detail {
    display: grid;
    gap: 0.65rem;
    padding: 1rem;
    border: 1px dashed var(--border, #cbd5e1);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .empty-detail {
    display: grid;
    place-content: center;
    min-height: 60vh;
    text-align: center;
  }

  :global([data-theme='dark']) .tabs button.active {
    color: var(--knowledge-primary-text, #ffffff);
  }

  :global([data-theme='dark']) .notice.success {
    color: var(--success, #4ade80);
  }

  @media (max-width: 1080px) {
    .knowledge-page,
    .run-panel,
    .research-panel,
    .sources-panel,
    .record-workspace {
      grid-template-columns: 1fr;
    }

    .space-list {
      border-right: 0;
      border-bottom: 1px solid var(--border-soft, #dbe3ef);
    }
  }

  @media (max-width: 760px) {
    .knowledge-page {
      min-height: auto;
    }

    .knowledge-page.loading-state {
      min-height: calc(100dvh - 4.15rem);
      padding: 0.65rem;
    }

    .loading-topline {
      align-items: flex-start;
      flex-direction: column;
      padding: 0.8rem;
    }

    .loading-topline h1 {
      font-size: 1.2rem;
    }

    .loading-topline .status-pill {
      display: none;
    }

    .loading-mobile-toolbar {
      display: grid;
      grid-template-columns: minmax(0, 1fr) repeat(3, 2.35rem);
      gap: 0.35rem;
      padding: 0.55rem;
      border: 1px solid var(--border-soft, #dbe3ef);
      border-radius: 8px;
      background: var(--panel, #ffffff);
    }

    .loading-control.wide {
      width: 100%;
    }

    .loading-tabs {
      position: static;
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 0.35rem;
      margin: 0;
      padding: 0;
      background: transparent;
    }

    .loading-tabs span {
      min-width: 0;
      padding: 0.42rem 0.3rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .loading-grid {
      grid-template-columns: minmax(0, 1fr);
    }

    .knowledge-page.has-selection .space-list {
      display: none;
    }

    .space-list,
    .space-detail {
      padding: 0.65rem;
    }

    .space-list .rows {
      max-height: 14rem;
      overflow: auto;
    }

    .mobile-corpus-bar {
      display: grid;
      gap: 0.5rem;
      margin-bottom: 0.65rem;
      padding: 0.55rem;
      border: 1px solid var(--border-soft, #dbe3ef);
      border-radius: 8px;
      background: var(--panel, #ffffff);
    }

    .mobile-space-browser,
    .mobile-space-options {
      display: grid;
    }

    .mobile-space-browser header,
    .mobile-space-options header {
      flex-direction: column;
    }

    .mobile-option-actions {
      justify-content: flex-start;
      width: 100%;
    }

    .mobile-option-actions button {
      flex: 1 1 8rem;
    }

    .mobile-notice {
      display: block;
      margin-bottom: 0.65rem;
    }

    .space-header,
    .detail-header,
    .form-footer,
    .report-card header,
    .record-inventory header {
      align-items: flex-start;
      flex-direction: column;
    }

    .detail-header {
      gap: 0.45rem;
      margin-bottom: 0.55rem;
    }

    .detail-header .eyebrow {
      display: none;
    }

    .detail-header h2 {
      font-size: 1.08rem;
    }

    .detail-summary {
      display: none;
    }

    .detail-actions,
    .button-row,
    .danger-panel {
      width: 100%;
    }

    .detail-actions {
      display: none;
    }

    .button-row,
    .danger-panel {
      flex-direction: column;
    }

    .button-row button,
    .danger-panel button {
      width: 100%;
    }

    .space-header button,
    .form-footer button,
    .research-controls button {
      width: 100%;
    }

    .space-metrics,
    .form-grid {
      grid-template-columns: 1fr;
    }

    .insight-bar {
      display: none;
    }

    .form-grid label,
    .form-grid input,
    .form-grid select {
      grid-column: 1;
    }

    .tabs {
      position: sticky;
      top: 4rem;
      z-index: 6;
      display: flex;
      gap: 0.45rem;
      margin: 0 -0.1rem;
      padding: 0.45rem 0.1rem;
      overflow-x: clip;
      background: var(--bg, #eef2f7);
      box-shadow: 0 -0.75rem 0 0 var(--bg, #eef2f7);
    }

    .tabs button {
      justify-content: center;
      flex: 1 1 0;
      gap: 0.3rem;
      min-width: 0;
      padding: 0.42rem 0.35rem;
    }

    .tabs button small {
      display: none;
    }

    .panel-label-full {
      display: none;
    }

    .panel-label-short {
      display: inline;
    }

    .panel {
      margin-top: 0.55rem;
    }

    .run-panel .runs-list,
    .reports-workspace .record-detail {
      order: 1;
    }

    .run-panel .research-sidebar,
    .reports-workspace .record-inventory {
      order: 2;
    }

    .panel-title {
      gap: 0.45rem;
    }

    .source-list,
    .source-list-section {
      margin-top: 0.55rem;
    }

    .source-card,
    .report-card,
    .record-row,
    .evidence-list section {
      padding: 0.7rem;
    }

    .record-inventory {
      padding: 0.65rem;
    }

    .record-row {
      grid-template-columns: minmax(0, 1fr);
      gap: 0.45rem;
    }

    .record-row-meta,
    .record-row-time {
      justify-items: start;
      text-align: left;
    }

    .choice-group.compact {
      grid-template-columns: 1fr;
    }

    .source-summary {
      grid-template-columns: 1fr;
      align-items: flex-start;
      padding: 0.7rem;
    }

    .source-card-body {
      padding: 0 0.7rem 0.7rem;
    }

    .source-card-actions {
      justify-content: flex-start;
    }

    .source-state {
      justify-content: flex-start;
    }

    .source-delete-action {
      width: 2rem;
      min-width: 2rem;
      padding: 0;
    }

    .source-delete-action span {
      display: none;
    }

    .source-details {
      margin-top: 0.6rem;
    }

    .source-details > summary {
      min-height: 2.25rem;
      padding: 0.5rem 0.6rem;
    }

    .source-details-body {
      padding: 0 0.6rem 0.6rem;
    }

    .inline-check {
      width: 100%;
    }

  }
</style>
