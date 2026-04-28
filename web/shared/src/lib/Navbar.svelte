<script lang="ts">
  import { onMount } from 'svelte';
  import { createHomelabdClient } from './client';
  import ThemeToggle from './ThemeToggle.svelte';
  import { taskAttentionCounts, type TaskAttentionCounts } from './tasks';

  export let title = 'homelabd';
  export let subtitle = 'Dashboard';
  export let current = '';
  export let apiBase = '';
  export let taskApiBase = '';
  export let taskAttention: TaskAttentionCounts | undefined = undefined;
  export let links: { href: string; label: string }[] = [
    { href: '/chat', label: 'Chat' },
    { href: '/tasks', label: 'Tasks' },
    { href: '/workflows', label: 'Workflows' },
    { href: '/docs', label: 'Docs' },
    { href: '/terminal', label: 'Terminal' },
    { href: '/supervisord', label: 'Supervisor' },
    { href: '/healthd', label: 'Health' }
  ];

  let mobileMenuOpen = false;
  let mobileMenuElement: HTMLDetailsElement | undefined;
  let fetchedTaskAttention: TaskAttentionCounts = { red: 0, amber: 0, total: 0 };
  let currentTaskAttention: TaskAttentionCounts = { red: 0, amber: 0, total: 0 };

  const isActive = (href: string) => current === href;
  const isTasksLink = (href: string) => href === '/tasks';
  const badgeCount = (count: number) => (count > 99 ? '99+' : String(count));
  const attentionParts = (counts: TaskAttentionCounts) => [
    ...(counts.red ? [`${counts.red} urgent ${counts.red === 1 ? 'item' : 'items'}`] : []),
    ...(counts.amber ? [`${counts.amber} review ${counts.amber === 1 ? 'item' : 'items'}`] : [])
  ];
  const attentionPhrase = (counts: TaskAttentionCounts) =>
    `${attentionParts(counts).join(', ')} ${counts.total === 1 ? 'needs' : 'need'} attention`;
  const linkTitle = (link: { href: string; label: string }) =>
    isTasksLink(link.href) && currentTaskAttention.total
      ? `${link.label}: ${attentionPhrase(currentTaskAttention)}`
      : undefined;
  const setMobileMenuOpen = (open: boolean) => {
    mobileMenuOpen = open;
    if (mobileMenuElement && mobileMenuElement.open !== open) {
      mobileMenuElement.open = open;
    }
  };
  const toggleMobileMenu = (event: MouseEvent) => {
    event.preventDefault();
    setMobileMenuOpen(!(mobileMenuElement?.open ?? mobileMenuOpen));
  };
  const syncMobileMenuOpen = (event: Event) => {
    if (event.currentTarget instanceof HTMLDetailsElement) {
      mobileMenuOpen = event.currentTarget.open;
    }
  };

  $: currentTaskAttention = taskAttention || fetchedTaskAttention;

  const refreshTaskAttention = async () => {
    if (taskAttention !== undefined) {
      return;
    }
    const client = createHomelabdClient({ baseUrl: taskApiBase || apiBase || '/api' });
    const [tasksResult, approvalsResult] = await Promise.allSettled([
      client.listTasks(),
      client.listApprovals()
    ]);
    if (tasksResult.status !== 'fulfilled') {
      return;
    }
    const tasks = Array.isArray(tasksResult.value.tasks) ? tasksResult.value.tasks : [];
    const approvals =
      approvalsResult.status === 'fulfilled' && Array.isArray(approvalsResult.value.approvals)
        ? approvalsResult.value.approvals
        : [];
    fetchedTaskAttention = taskAttentionCounts(tasks, approvals);
  };

  onMount(() => {
    if (mobileMenuElement?.open) {
      mobileMenuOpen = true;
    }
    void refreshTaskAttention().catch(() => undefined);
    const interval = window.setInterval(() => {
      void refreshTaskAttention().catch(() => undefined);
    }, 15000);
    return () => window.clearInterval(interval);
  });
</script>

<header class="navbar">
  <a class="brand" href="/chat" on:click={() => setMobileMenuOpen(false)}>
    <span>{subtitle}</span>
    <strong>{title}</strong>
  </a>

  <nav class="desktop-nav" aria-label="Primary">
    {#each links as link}
      <a
        href={link.href}
        aria-current={isActive(link.href) ? 'page' : undefined}
        aria-label={isTasksLink(link.href) && currentTaskAttention.total > 0
          ? `${link.label}, ${attentionPhrase(currentTaskAttention)}`
          : undefined}
        title={linkTitle(link)}
        class:has-attention={isTasksLink(link.href) && currentTaskAttention.total > 0}
      >
        <span class="nav-label">{link.label}</span>
        {#if isTasksLink(link.href) && currentTaskAttention.total > 0}
          <span class="sr-only">, {attentionPhrase(currentTaskAttention)}</span>
          <span class="attention-badges" aria-hidden="true">
            {#if currentTaskAttention.red > 0}
              <span class="attention-badge critical">{badgeCount(currentTaskAttention.red)}</span>
            {/if}
            {#if currentTaskAttention.amber > 0}
              <span class="attention-badge warning">{badgeCount(currentTaskAttention.amber)}</span>
            {/if}
          </span>
        {/if}
      </a>
    {/each}
  </nav>

  <div class="right">
    {#if apiBase}
      <span class="api">{apiBase}</span>
    {/if}
    <div class="desktop-theme">
      <ThemeToggle />
    </div>
    <details bind:this={mobileMenuElement} class="mobile-menu" on:toggle={syncMobileMenuOpen}>
      <!-- svelte-ignore a11y_no_redundant_roles -- Chromium exposes this styled summary consistently with an explicit role. -->
      <summary
        class="menu-button"
        role="button"
        aria-controls="primary-mobile-nav"
        aria-expanded={mobileMenuOpen}
        on:click={toggleMobileMenu}
      >
        <span aria-hidden="true">☰</span>
        Menu
      </summary>
      <nav id="primary-mobile-nav" class="mobile-nav" aria-label="Primary mobile">
        {#each links as link}
          <a
            href={link.href}
            aria-current={isActive(link.href) ? 'page' : undefined}
            aria-label={isTasksLink(link.href) && currentTaskAttention.total > 0
              ? `${link.label}, ${attentionPhrase(currentTaskAttention)}`
              : undefined}
            title={linkTitle(link)}
            class:has-attention={isTasksLink(link.href) && currentTaskAttention.total > 0}
            on:click={() => setMobileMenuOpen(false)}
          >
            <span class="nav-label">{link.label}</span>
            {#if isTasksLink(link.href) && currentTaskAttention.total > 0}
              <span class="sr-only">, {attentionPhrase(currentTaskAttention)}</span>
              <span class="attention-badges" aria-hidden="true">
                {#if currentTaskAttention.red > 0}
                  <span class="attention-badge critical">{badgeCount(currentTaskAttention.red)}</span>
                {/if}
                {#if currentTaskAttention.amber > 0}
                  <span class="attention-badge warning">{badgeCount(currentTaskAttention.amber)}</span>
                {/if}
              </span>
            {/if}
          </a>
        {/each}
        <ThemeToggle compact />
      </nav>
    </details>
  </div>
</header>

<style>
  :global(:root) {
    --bg: #f5f7fb;
    --panel: #f8fafc;
    --surface: #ffffff;
    --surface-muted: #f8fafc;
    --surface-hover: #eef5ff;
    --text: #172033;
    --text-strong: #0f172a;
    --muted: #64748b;
    --border: #cbd5e1;
    --border-soft: #dbe3ef;
    --accent: #2563eb;
    --accent-hover: #1d4ed8;
    --shadow: rgb(15 23 42 / 0.08);
    --success-bg: #f0fdf4;
    --success-border: #bbf7d0;
    --danger-bg: #fef2f2;
    --danger-text: #991b1b;
    --warning-bg: #fffbeb;
    --warning-border: #fde68a;
    --warning-text: #92400e;
  }

  :global(html[data-theme='dark']) {
    --bg: #0b1120;
    --panel: #111827;
    --surface: #172033;
    --surface-muted: #1f2937;
    --surface-hover: #243047;
    --text: #dbe7f6;
    --text-strong: #f8fafc;
    --muted: #9fb0c7;
    --border: #334155;
    --border-soft: #263244;
    --accent: #60a5fa;
    --accent-hover: #3b82f6;
    --shadow: rgb(0 0 0 / 0.35);
    --success-bg: #0f2f22;
    --success-border: #1f6f4a;
    --danger-bg: #3a1418;
    --danger-text: #fecaca;
    --warning-bg: #33270d;
    --warning-border: #854d0e;
    --warning-text: #fde68a;
  }

  :global(html[data-theme='dark'] body),
  :global(html[data-theme='light'] body) {
    color: var(--text) !important;
    background: var(--bg) !important;
  }

  :global(html[data-theme='dark'] .chat-card),
  :global(html[data-theme='dark'] .task-pane),
  :global(html[data-theme='dark'] .task-record),
  :global(html[data-theme='dark'] .record-header),
  :global(html[data-theme='dark'] .record-summary),
  :global(html[data-theme='dark'] .decision-panel),
  :global(html[data-theme='dark'] .message),
  :global(html[data-theme='dark'] .toolbar),
	  :global(html[data-theme='dark'] .metric),
	  :global(html[data-theme='dark'] .chart-panel),
	  :global(html[data-theme='dark'] .slo),
	  :global(html[data-theme='dark'] .process),
	  :global(html[data-theme='dark'] .check),
	  :global(html[data-theme='dark'] .notification),
	  :global(html[data-theme='dark'] .notifications),
	  :global(html[data-theme='dark'] .empty-record),
	  :global(html[data-theme='dark'] .task-plan),
	  :global(html[data-theme='dark'] .task-result),
	  :global(html[data-theme='dark'] .workspace-path),
	  :global(html[data-theme='dark'] .activity),
	  :global(html[data-theme='dark'] .record-summary div),
	  :global(html[data-theme='dark'] .toolbar-actions span),
	  :global(html[data-theme='dark'] .budget),
	  :global(html[data-theme='dark'] .empty),
	  :global(html[data-theme='dark'] .composer),
	  :global(html[data-theme='dark'] .triage button),
	  :global(html[data-theme='dark'] .task-header button),
	  :global(html[data-theme='dark'] .record-actions button),
	  :global(html[data-theme='dark'] .message-actions button),
  :global(html[data-theme='dark'] .prompt-actions button),
  :global(html[data-theme='dark'] .command-header-actions button),
  :global(html[data-theme='dark'] .terminal-panel),
  :global(html[data-theme='dark'] .terminal-header),
  :global(html[data-theme='dark'] .terminal-notice),
  :global(html[data-theme='dark'] .terminal-composer),
  :global(html[data-theme='dark'] .terminal-actions button),
  :global(html[data-theme='dark'] input),
  :global(html[data-theme='dark'] select),
  :global(html[data-theme='dark'] textarea) {
    color: var(--text) !important;
    border-color: var(--border-soft) !important;
    background: var(--surface) !important;
  }

  :global(html[data-theme='dark'] .workbench),
  :global(html[data-theme='dark'] .shell),
  :global(html[data-theme='dark'] main),
  :global(html[data-theme='dark'] .app-shell) {
    color: var(--text) !important;
    background: var(--bg) !important;
  }

  :global(html[data-theme='dark'] .record-header h2),
  :global(html[data-theme='dark'] .record-summary strong),
  :global(html[data-theme='dark'] .task-header h1),
  :global(html[data-theme='dark'] .task-copy strong),
  :global(html[data-theme='dark'] .next-step h3),
  :global(html[data-theme='dark'] .task-plan h3),
  :global(html[data-theme='dark'] .task-plan li strong),
  :global(html[data-theme='dark'] .task-result h3),
  :global(html[data-theme='dark'] .activity h3),
	  :global(html[data-theme='dark'] .activity li strong),
	  :global(html[data-theme='dark'] .message .meta span),
	  :global(html[data-theme='dark'] .prompt-actions strong),
	  :global(html[data-theme='dark'] .triage strong),
	  :global(html[data-theme='dark'] .toolbar h2),
	  :global(html[data-theme='dark'] .section-title h2),
	  :global(html[data-theme='dark'] .metric strong),
	  :global(html[data-theme='dark'] .panel-title h2),
	  :global(html[data-theme='dark'] .slo h3),
	  :global(html[data-theme='dark'] .process h3),
	  :global(html[data-theme='dark'] .check h3),
  :global(html[data-theme='dark'] .terminal-header h1),
  :global(html[data-theme='dark'] .notification h3) {
    color: var(--text-strong) !important;
  }

  :global(html[data-theme='dark'] .task-header p),
  :global(html[data-theme='dark'] .task-header span),
  :global(html[data-theme='dark'] .record-header p),
  :global(html[data-theme='dark'] .record-summary span),
  :global(html[data-theme='dark'] .workspace-path span),
  :global(html[data-theme='dark'] .task-plan header p),
  :global(html[data-theme='dark'] .plan-risks > strong),
  :global(html[data-theme='dark'] .activity header p),
  :global(html[data-theme='dark'] .activity time),
  :global(html[data-theme='dark'] .activity li span),
  :global(html[data-theme='dark'] .task-copy small),
  :global(html[data-theme='dark'] .meta),
  :global(html[data-theme='dark'] .prompt-actions span),
  :global(html[data-theme='dark'] .toolbar p),
  :global(html[data-theme='dark'] .metric p),
  :global(html[data-theme='dark'] .metric span),
  :global(html[data-theme='dark'] .chart-panel p),
  :global(html[data-theme='dark'] .slo p),
  :global(html[data-theme='dark'] .slo-stats span),
  :global(html[data-theme='dark'] .process p),
  :global(html[data-theme='dark'] .process small),
  :global(html[data-theme='dark'] .check p),
  :global(html[data-theme='dark'] .check small),
  :global(html[data-theme='dark'] .notification time),
  :global(html[data-theme='dark'] .terminal-header p),
  :global(html[data-theme='dark'] .terminal-header .shell-meta),
  :global(html[data-theme='dark'] .terminal-notice),
  :global(html[data-theme='dark'] .muted) {
    color: var(--muted) !important;
  }

  :global(html[data-theme='dark'] .markdown),
  :global(html[data-theme='dark'] .markdown p),
  :global(html[data-theme='dark'] .markdown li),
  :global(html[data-theme='dark'] .task-result p),
  :global(html[data-theme='dark'] .task-plan li),
  :global(html[data-theme='dark'] .task-plan li p),
  :global(html[data-theme='dark'] .plan-risks li),
  :global(html[data-theme='dark'] .plan-review),
  :global(html[data-theme='dark'] .workspace-path code),
  :global(html[data-theme='dark'] .activity li p),
  :global(html[data-theme='dark'] .next-step p),
  :global(html[data-theme='dark'] .empty-record p) {
    color: var(--text) !important;
  }

  :global(html[data-theme='dark'] .markdown h1),
  :global(html[data-theme='dark'] .markdown h2),
  :global(html[data-theme='dark'] .markdown h3),
  :global(html[data-theme='dark'] .markdown h4),
  :global(html[data-theme='dark'] .markdown h5),
  :global(html[data-theme='dark'] .markdown h6),
  :global(html[data-theme='dark'] .markdown strong) {
    color: var(--text-strong) !important;
  }

  :global(html[data-theme='dark'] .markdown blockquote) {
    color: var(--muted) !important;
    border-color: var(--border-soft) !important;
  }

  :global(html[data-theme='dark'] .markdown code) {
    color: #e2e8f0 !important;
    background: #1f2937 !important;
  }

  :global(html[data-theme='dark'] .markdown pre) {
    color: #e2e8f0 !important;
    border-color: var(--border-soft) !important;
    background: #020617 !important;
  }

	  :global(html[data-theme='dark'] .message.user) {
	    border-color: var(--success-border) !important;
	    background: var(--success-bg) !important;
	  }

	  :global(html[data-theme='dark'] .triage button.active) {
	    border-color: #1e3a8a !important;
	    background: #10254a !important;
	  }

	  :global(html[data-theme='dark'] .triage button.active span),
	  :global(html[data-theme='dark'] .triage button.active strong) {
	    color: #bfdbfe !important;
	  }

	  :global(html[data-theme='dark'] .notifications .muted) {
	    color: var(--muted) !important;
	    background: transparent !important;
	  }

  :global(html[data-theme='dark'] .task-row:hover),
  :global(html[data-theme='dark'] .task-row.selected) {
    border-color: var(--border) !important;
    background: var(--surface-hover) !important;
  }

  :global(html[data-theme='dark'] .status-pill) {
    color: #fed7aa !important;
    border-color: #854d0e !important;
    background: #33270d !important;
  }

  :global(html[data-theme='dark'] .status-pill.connected) {
    color: #bbf7d0 !important;
    border-color: var(--success-border) !important;
    background: var(--success-bg) !important;
  }

  :global(html[data-theme='dark'] .notice) {
    color: #bbf7d0 !important;
    border-color: var(--success-border) !important;
    background: var(--success-bg) !important;
  }

	  :global(html[data-theme='dark'] .error) {
	    color: var(--danger-text) !important;
	    border-color: #7f1d1d !important;
	    background: var(--danger-bg) !important;
  }

	  :global(html[data-theme='dark'] .approval-list article),
	  :global(html[data-theme='dark'] .next-step.amber) {
	    border-color: var(--warning-border) !important;
	    background: var(--warning-bg) !important;
	  }

	  :global(html[data-theme='dark'] .next-step.green) {
	    border-color: var(--success-border) !important;
	    background: var(--success-bg) !important;
	  }

	  :global(html[data-theme='dark'] .next-step.blue) {
	    border-color: #1e3a8a !important;
	    background: #10254a !important;
	  }

	  :global(html[data-theme='dark'] .next-step.red) {
	    border-color: #7f1d1d !important;
	    background: var(--danger-bg) !important;
	  }

  :global(html[data-theme='dark'] .status.red),
  :global(html[data-theme='dark'] .pill.critical),
  :global(html[data-theme='dark'] .pill.page) {
    color: #fecaca !important;
    background: #7f1d1d !important;
  }

  :global(html[data-theme='dark'] .status.amber),
  :global(html[data-theme='dark'] .pill.warning),
  :global(html[data-theme='dark'] .pill.warn) {
    color: #fde68a !important;
    background: #854d0e !important;
  }

  :global(html[data-theme='dark'] .status.blue) {
    color: #bfdbfe !important;
    background: #1e3a8a !important;
  }

	  :global(html[data-theme='dark'] .status.green),
	  :global(html[data-theme='dark'] .pill.healthy),
	  :global(html[data-theme='dark'] .pill.info) {
	    color: #bbf7d0 !important;
	    background: #166534 !important;
	  }

	  :global(html[data-theme='dark'] .status.gray),
	  :global(html[data-theme='dark'] .pill:not(.healthy):not(.info):not(.warning):not(.warn):not(.critical):not(.page)) {
	    color: #dbe7f6 !important;
	    background: #334155 !important;
	  }

  .navbar {
    box-sizing: border-box;
    position: sticky;
    top: 0;
    z-index: 20;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 1rem;
    min-height: 4rem;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border-soft, #dbe3ef);
    background: var(--surface, #ffffff);
    box-shadow: 0 1px 2px var(--shadow, rgb(15 23 42 / 0.04));
  }

  .brand,
  .desktop-nav a,
  .mobile-nav a {
    color: inherit;
    text-decoration: none;
  }

  .brand {
    display: grid;
    gap: 0.12rem;
    min-width: 0;
  }

  .brand span {
    overflow: hidden;
    color: var(--muted, #64748b);
    font-size: 0.72rem;
    font-weight: 850;
    letter-spacing: 0.08em;
    text-overflow: ellipsis;
    text-transform: uppercase;
    white-space: nowrap;
  }

  .brand strong {
    overflow: hidden;
    color: var(--text-strong, #0f172a);
    font-size: 1.15rem;
    line-height: 1.1;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .desktop-nav,
  .right {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .desktop-nav a,
  .mobile-nav a {
    display: inline-flex;
    align-items: center;
    min-width: 0;
    border: 1px solid transparent;
    border-radius: 0.65rem;
    color: var(--text, #334155);
    font-size: 0.88rem;
    font-weight: 800;
  }

  .desktop-nav a {
    gap: 0.42rem;
    padding: 0.45rem 0.75rem;
  }

  .nav-label {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .sr-only {
    position: absolute;
    overflow: hidden;
    width: 1px;
    height: 1px;
    margin: -1px;
    padding: 0;
    border: 0;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
  }

  .attention-badges {
    display: inline-flex;
    flex: 0 0 auto;
    align-items: center;
    gap: 0.18rem;
  }

  .attention-badge {
    box-sizing: border-box;
    display: inline-grid;
    place-items: center;
    min-width: 1.05rem;
    height: 1.05rem;
    padding: 0 0.26rem;
    border: 1px solid rgb(255 255 255 / 0.72);
    border-radius: 999px;
    color: #ffffff;
    font-size: 0.62rem;
    font-variant-numeric: tabular-nums;
    font-weight: 900;
    line-height: 1;
    box-shadow: 0 0 0 1px var(--surface, #ffffff);
  }

  .attention-badge.critical {
    background: #dc2626;
  }

  .attention-badge.warning {
    background: #ea580c;
  }

  .desktop-nav a:hover,
  .mobile-nav a:hover {
    border-color: var(--border, #cbd5e1);
    background: var(--surface-muted, #f8fafc);
  }

  .desktop-nav a[aria-current='page'],
  .mobile-nav a[aria-current='page'] {
    border-color: var(--accent, #2563eb);
    color: #ffffff;
    background: var(--accent, #2563eb);
  }

  .api {
    max-width: 16rem;
    overflow: hidden;
    padding: 0.42rem 0.65rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 999px;
    color: var(--muted, #475569);
    background: var(--surface-muted, #f8fafc);
    font-size: 0.76rem;
    font-weight: 750;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .mobile-menu {
    display: none;
  }

  .mobile-menu summary {
    list-style: none;
  }

  .mobile-menu summary::-webkit-details-marker {
    display: none;
  }

  .menu-button {
    display: none;
    min-height: 2.4rem;
    padding: 0 0.75rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 0.65rem;
    color: var(--text, #243047);
    background: var(--surface, #ffffff);
    font: inherit;
    font-size: 0.88rem;
    font-weight: 850;
    cursor: pointer;
  }

  .menu-button span {
    margin-right: 0.25rem;
  }

  .mobile-nav {
    position: absolute;
    top: calc(100% + 0.35rem);
    right: 0.75rem;
    left: 0.75rem;
    display: none;
    gap: 0.4rem;
    padding: 0.55rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.9rem;
    background: var(--surface, #ffffff);
    box-shadow: 0 18px 40px var(--shadow, rgb(15 23 42 / 0.16));
  }

  .mobile-nav a {
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.8rem 0.9rem;
  }

  @media (max-width: 760px) {
    .navbar {
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 0.75rem;
      min-height: 3.75rem;
    }

    .desktop-nav {
      display: none;
    }

    .desktop-theme {
      display: none;
    }

    .right {
      justify-content: flex-end;
    }

    .api {
      display: none;
    }

    .mobile-menu {
      display: block;
    }

    .menu-button {
      display: inline-flex;
      align-items: center;
      justify-content: center;
    }

    .mobile-nav {
      display: none;
    }

    .mobile-menu[open] .mobile-nav {
      display: grid;
    }
  }
</style>
