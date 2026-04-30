<script lang="ts">
  import { onMount, tick } from 'svelte';
  import {
    dataUrlAttachment,
    formatAttachmentSize,
    textAttachment
  } from './attachments';
  import { createHomelabdClient } from './client';
  import ThemeToggle from './ThemeToggle.svelte';
  import { taskAttentionCounts, type TaskAttentionCounts } from './tasks';
  import type { HomelabdTaskAttachment } from './types';

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

  type RecentAction = {
    time: string;
    type: string;
    label: string;
    path: string;
  };

  type BeforeInstallPromptEvent = Event & {
    prompt: () => Promise<void>;
    userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>;
  };

  type PwaWindow = Window & {
    __homelabdPwaInstallPrompt?: BeforeInstallPromptEvent;
  };

  const actionStorageKey = 'homelabd.dashboard.recentActions.v1';
  const helpClientBase = () => apiBase || '/api';
  const skipWaitingMessage = 'SKIP_WAITING';

  let mobileMenuOpen = false;
  let mobileMenu: HTMLDetailsElement | undefined;
  let fetchedTaskAttention: TaskAttentionCounts = { red: 0, amber: 0, total: 0 };
  let currentTaskAttention: TaskAttentionCounts = { red: 0, amber: 0, total: 0 };
  let helpDialog: HTMLDialogElement | undefined;
  let helpDetails = '';
  let helpStatus = '';
  let helpError = '';
  let helpSubmitting = false;
  let helpCapturing = false;
  let helpDialogOpen = false;
  let helpReady = false;
  let helpAttachments: HomelabdTaskAttachment[] = [];
  let pwaInstallPrompt: BeforeInstallPromptEvent | undefined;
  let pwaInstallReady = false;
  let pwaInstalling = false;
  let pwaInstalled = false;
  let pwaUpdateWorker: ServiceWorker | null = null;
  let pwaUpdateReady = false;
  let pwaRefreshing = false;
  let navbarElement: HTMLElement | undefined;
  let brandElement: HTMLAnchorElement | undefined;
  let navMeasureElement: HTMLElement | undefined;
  let rightElement: HTMLDivElement | undefined;
  let compactNav = false;
  let expandedRightWidth = 0;
  const screenCaptureUnavailableMessage =
    'Browser context captured. Screenshot capture is unavailable in this browser, so the report will submit without an image.';

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

  const closeMobileMenu = () => {
    mobileMenuOpen = false;
    mobileMenu?.removeAttribute('open');
  };

  const isStandaloneDisplay = () =>
    window.matchMedia?.('(display-mode: standalone)').matches ||
    (navigator as Navigator & { standalone?: boolean }).standalone === true;

  const pwaWindow = () => window as PwaWindow;

  const elementLabel = (target: EventTarget | null) => {
    if (!(target instanceof Element)) {
      return 'page';
    }
    const element = target.closest('button, a, input, textarea, select, summary, [role="button"]');
    if (!element) {
      return target.tagName.toLowerCase();
    }
    const aria = element.getAttribute('aria-label') || element.getAttribute('title');
    const text = element.textContent?.trim().replace(/\s+/g, ' ');
    const name = aria || text || element.getAttribute('id') || element.tagName.toLowerCase();
    return name.slice(0, 120);
  };

  const recentActions = (): RecentAction[] => {
    try {
      const parsed = JSON.parse(localStorage.getItem(actionStorageKey) || '[]');
      return Array.isArray(parsed) ? parsed.filter((item) => item && typeof item === 'object') : [];
    } catch {
      return [];
    }
  };

  const recordAction = (type: string, target: EventTarget | null) => {
    try {
      const item: RecentAction = {
        time: new Date().toISOString(),
        type,
        label: elementLabel(target),
        path: `${location.pathname}${location.search}`
      };
      localStorage.setItem(actionStorageKey, JSON.stringify([...recentActions(), item].slice(-12)));
    } catch {
      // Recent actions are helpful context, not required for normal navigation.
    }
  };

  const visiblePageText = () =>
    (document.body.innerText || document.body.textContent || '')
      .replace(/\s+\n/g, '\n')
      .replace(/\n{3,}/g, '\n\n')
      .slice(0, 5000);

  const activeElementLabel = () =>
    document.activeElement ? elementLabel(document.activeElement) : 'none';

  const buildBrowserContextAttachment = () => {
    const context = {
      captured_at: new Date().toISOString(),
      page: current || location.pathname,
      url: location.href,
      title: document.title,
      viewport: {
        width: window.innerWidth,
        height: window.innerHeight,
        device_pixel_ratio: window.devicePixelRatio
      },
      screen: {
        width: window.screen.width,
        height: window.screen.height
      },
      user_agent: navigator.userAgent,
      language: navigator.language,
      theme: document.documentElement.dataset.theme || '',
      active_element: activeElementLabel(),
      selected_text: String(window.getSelection?.() || '').slice(0, 1000),
      recent_actions: recentActions(),
      visible_text: visiblePageText()
    };
    return textAttachment('browser-context.json', 'application/json', JSON.stringify(context, null, 2));
  };

  const stopStream = (stream?: MediaStream) => {
    stream?.getTracks().forEach((track) => track.stop());
  };

  const screenCaptureSupported = () =>
    window.isSecureContext && typeof navigator.mediaDevices?.getDisplayMedia === 'function';

  const screenCaptureUnavailable = (err: unknown) =>
    err instanceof Error && /screen capture is not available/i.test(err.message);

  const updateCompactNav = () => {
    if (!navbarElement || !brandElement || !navMeasureElement || !rightElement) {
      return;
    }
    const style = getComputedStyle(navbarElement);
    const available =
      navbarElement.clientWidth -
      (Number.parseFloat(style.paddingLeft) || 0) -
      (Number.parseFloat(style.paddingRight) || 0);
    const gap = Number.parseFloat(style.columnGap || style.gap) || 0;
    if (!compactNav) {
      expandedRightWidth = rightElement.scrollWidth;
    }
    const required =
      brandElement.scrollWidth +
      navMeasureElement.scrollWidth +
      (expandedRightWidth || rightElement.scrollWidth) +
      gap * 2;
    const nextCompact = required > available;
    if (compactNav !== nextCompact) {
      compactNav = nextCompact;
    }
  };

  const captureScreenshotAttachment = async () => {
    let stream: MediaStream | undefined;
    try {
      stream = await navigator.mediaDevices?.getDisplayMedia({ video: true, audio: false });
      if (!stream) {
        return undefined;
      }
      const video = document.createElement('video');
      video.muted = true;
      video.srcObject = stream;
      await video.play();
      if (!video.videoWidth || !video.videoHeight) {
        await new Promise((resolve) => {
          video.onloadedmetadata = () => resolve(undefined);
          window.setTimeout(resolve, 350);
        });
      }
      const canvas = document.createElement('canvas');
      canvas.width = video.videoWidth || window.innerWidth;
      canvas.height = video.videoHeight || window.innerHeight;
      canvas.getContext('2d')?.drawImage(video, 0, 0, canvas.width, canvas.height);
      return dataUrlAttachment('dashboard-screenshot.png', 'image/png', canvas.toDataURL('image/png'));
    } finally {
      stopStream(stream);
    }
  };

  const showHelpDialog = async () => {
    helpDialogOpen = true;
    await tick();
    if (!helpDialog || helpDialog.open) {
      return;
    }
    if (typeof helpDialog.showModal === 'function') {
      helpDialog.showModal();
      return;
    }
    helpDialog.setAttribute('open', '');
  };

  const openHelpTaskDialog = async () => {
    if (helpCapturing || helpSubmitting) {
      return;
    }
    closeMobileMenu();
    helpDetails = '';
    helpError = '';
    helpStatus = 'Capturing page context.';
    helpCapturing = true;
    try {
      helpAttachments = [buildBrowserContextAttachment()];
    } catch (err) {
      helpAttachments = [
        textAttachment(
          'browser-context.json',
          'application/json',
          JSON.stringify(
            {
              captured_at: new Date().toISOString(),
              url: location.href,
              error: err instanceof Error ? err.message : 'Unable to capture browser context.'
            },
            null,
            2
          )
        )
      ];
    }
    void showHelpDialog();
    if (!screenCaptureSupported()) {
      helpStatus = screenCaptureUnavailableMessage;
      helpCapturing = false;
      return;
    }
    try {
      helpStatus = 'Choose this tab or screen to attach a screenshot.';
      const screenshot = await captureScreenshotAttachment();
      if (screenshot) {
        helpAttachments = [...helpAttachments, screenshot];
        helpStatus = 'Screenshot and browser context captured.';
      } else {
        helpStatus = screenCaptureUnavailableMessage;
      }
    } catch (err) {
      helpStatus =
        screenCaptureUnavailable(err)
          ? screenCaptureUnavailableMessage
          : err instanceof DOMException && err.name === 'NotAllowedError'
          ? 'Browser context captured. Screenshot capture was cancelled.'
          : 'Browser context captured. Screenshot capture was skipped.';
    } finally {
      helpCapturing = false;
      void showHelpDialog();
    }
  };

  const closeHelpDialog = () => {
    if (helpSubmitting) {
      return;
    }
    helpDialogOpen = false;
    if (helpDialog?.open) {
      helpDialog.close();
    }
  };

  const helpTaskGoal = () => {
    const detail = helpDetails.trim() || 'No extra detail provided.';
    const attachmentList = helpAttachments
      .map((attachment) => `- ${attachment.name} (${attachment.content_type}, ${formatAttachmentSize(attachment.size)})`)
      .join('\n');
    return [
      `Dashboard help task from ${current || location.pathname}`,
      '',
      'Operator detail:',
      detail,
      '',
      `Captured URL: ${location.href}`,
      'Attachments:',
      attachmentList || '- browser context unavailable'
    ].join('\n');
  };

  const submitHelpTask = async () => {
    if (helpSubmitting) {
      return;
    }
    helpSubmitting = true;
    helpError = '';
    try {
      const client = createHomelabdClient({ baseUrl: helpClientBase() });
      const response = await client.createTask({
        goal: helpTaskGoal(),
        attachments: helpAttachments
      });
      helpStatus = response.reply || 'Help task submitted.';
      helpDialogOpen = false;
      if (helpDialog?.open) {
        helpDialog.close();
      }
    } catch (err) {
      helpError = err instanceof Error ? err.message : 'Unable to submit help task.';
    } finally {
      helpSubmitting = false;
    }
  };

  const installDashboard = async () => {
    if (!pwaInstallPrompt || pwaInstalling) {
      return;
    }
    pwaInstalling = true;
    const prompt = pwaInstallPrompt;
    pwaInstallPrompt = undefined;
    pwaWindow().__homelabdPwaInstallPrompt = undefined;
    pwaInstallReady = false;
    try {
      await prompt.prompt();
      const choice = await prompt.userChoice;
      pwaInstalled = choice.outcome === 'accepted' || isStandaloneDisplay();
    } finally {
      pwaInstalling = false;
    }
  };

  const showDashboardInstall = (prompt: BeforeInstallPromptEvent | undefined) => {
    if (!prompt || pwaInstalled) {
      return;
    }
    pwaInstallPrompt = prompt;
    pwaInstallReady = true;
  };

  const applyDashboardUpdate = () => {
    if (!pwaUpdateWorker || pwaRefreshing) {
      return;
    }
    pwaRefreshing = true;
    pwaUpdateWorker.postMessage({ type: skipWaitingMessage });
  };

  const queueDashboardUpdate = (worker: ServiceWorker | null) => {
    if (!worker || !navigator.serviceWorker?.controller) {
      return;
    }
    pwaUpdateWorker = worker;
    pwaUpdateReady = true;
  };

  onMount(() => {
    helpReady = true;
    pwaInstalled = isStandaloneDisplay();
    void refreshTaskAttention().catch(() => undefined);
    const interval = window.setInterval(() => {
      void refreshTaskAttention().catch(() => undefined);
    }, 15000);
    const clickListener = (event: MouseEvent) => recordAction('click', event.target);
    const inputListener = (event: Event) => recordAction('input', event.target);
    const beforeInstallPromptListener = (event: Event) => {
      event.preventDefault();
      const prompt = event as BeforeInstallPromptEvent;
      pwaWindow().__homelabdPwaInstallPrompt = prompt;
      showDashboardInstall(prompt);
    };
    const installReadyListener = () => {
      showDashboardInstall(pwaWindow().__homelabdPwaInstallPrompt);
    };
    const appInstalledListener = () => {
      pwaInstallPrompt = undefined;
      pwaWindow().__homelabdPwaInstallPrompt = undefined;
      pwaInstallReady = false;
      pwaInstalled = true;
    };
    const controllerChangeListener = () => {
      if (pwaRefreshing) {
        window.location.reload();
      }
    };
    let animationFrame = 0;
    const scheduleNavUpdate = () => {
      if (animationFrame) {
        cancelAnimationFrame(animationFrame);
      }
      animationFrame = requestAnimationFrame(() => {
        animationFrame = 0;
        updateCompactNav();
      });
    };
    const resizeObserver =
      typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(scheduleNavUpdate);
    window.addEventListener('click', clickListener, { capture: true });
    window.addEventListener('change', inputListener, { capture: true });
    window.addEventListener('resize', scheduleNavUpdate);
    window.addEventListener('beforeinstallprompt', beforeInstallPromptListener);
    window.addEventListener('homelabd-pwa-install-ready', installReadyListener);
    window.addEventListener('appinstalled', appInstalledListener);
    for (const element of [navbarElement, brandElement, navMeasureElement, rightElement]) {
      if (element) {
        resizeObserver?.observe(element);
      }
    }
    void tick().then(scheduleNavUpdate);
    showDashboardInstall(pwaWindow().__homelabdPwaInstallPrompt);
    if ('serviceWorker' in navigator) {
      navigator.serviceWorker.addEventListener('controllerchange', controllerChangeListener);
      void navigator.serviceWorker.ready.then((registration) => {
        queueDashboardUpdate(registration.waiting);
        registration.addEventListener('updatefound', () => {
          const worker = registration.installing;
          worker?.addEventListener('statechange', () => {
            if (worker.state === 'installed') {
              queueDashboardUpdate(worker);
            }
          });
        });
        void registration.update().catch(() => {
          // Update checks are opportunistic; navigation and live API calls still work without them.
        });
      });
    }
    return () => {
      window.clearInterval(interval);
      window.removeEventListener('click', clickListener, { capture: true });
      window.removeEventListener('change', inputListener, { capture: true });
      window.removeEventListener('resize', scheduleNavUpdate);
      window.removeEventListener('beforeinstallprompt', beforeInstallPromptListener);
      window.removeEventListener('homelabd-pwa-install-ready', installReadyListener);
      window.removeEventListener('appinstalled', appInstalledListener);
      navigator.serviceWorker?.removeEventListener('controllerchange', controllerChangeListener);
      resizeObserver?.disconnect();
      if (animationFrame) {
        cancelAnimationFrame(animationFrame);
      }
    };
  });
</script>

<header class="navbar" bind:this={navbarElement}>
  <a class="brand" href="/chat" bind:this={brandElement} onclickcapture={closeMobileMenu}>
    <span>{subtitle}</span>
    <strong>{title}</strong>
  </a>

  <nav class="desktop-nav" class:compact={compactNav} aria-label="Primary">
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

  <nav class="nav-measure" bind:this={navMeasureElement} aria-hidden="true">
    {#each links as link}
      <a href={link.href} tabindex="-1">
        {link.label}
      </a>
    {/each}
  </nav>

  <div class="right" class:compact={compactNav} bind:this={rightElement}>
    {#if apiBase}
      <span class="api">{apiBase}</span>
    {/if}
    <div class="desktop-theme">
      <ThemeToggle />
    </div>
    {#if pwaUpdateReady}
      <button
        type="button"
        class="pwa-button update"
        aria-label="Update app"
        title="Reload to update app"
        disabled={pwaRefreshing}
        onclickcapture={applyDashboardUpdate}
      >
        {pwaRefreshing ? 'Updating' : 'Update'}
      </button>
    {:else if pwaInstallReady && !pwaInstalled}
      <button
        type="button"
        class="pwa-button"
        aria-label="Install app"
        title="Install dashboard app"
        disabled={pwaInstalling}
        onclickcapture={installDashboard}
      >
        {pwaInstalling ? 'Installing' : 'Install'}
      </button>
    {/if}
    <button
      type="button"
      class="help-button"
      aria-label="Submit help task"
      title="Submit help task"
      disabled={!helpReady || helpCapturing || helpSubmitting}
      onclickcapture={openHelpTaskDialog}
    >
      Help
    </button>
    <details class="mobile-menu" class:compact={compactNav} bind:this={mobileMenu} bind:open={mobileMenuOpen}>
      <!-- svelte-ignore a11y_no_redundant_roles -- Chromium exposes this styled summary consistently with an explicit role. -->
      <summary
        class="menu-button"
        role="button"
        aria-controls="primary-mobile-nav"
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
            onclickcapture={closeMobileMenu}
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

<dialog
  class="help-dialog"
  bind:this={helpDialog}
  aria-labelledby="help-task-title"
  onclose={() => {
    helpDialogOpen = false;
  }}
>
  <form
    class="help-dialog-body"
    onsubmit={(event) => {
      event.preventDefault();
      void submitHelpTask();
    }}
  >
    <header>
      <div>
        <p>Dashboard report</p>
        <h2 id="help-task-title">Submit help task</h2>
      </div>
      <button type="button" aria-label="Close help task dialog" onclickcapture={closeHelpDialog}>x</button>
    </header>

    <p class="help-status">{helpStatus}</p>

    <label for="help-task-detail">
      <span>More detail</span>
      <textarea
        id="help-task-detail"
        bind:value={helpDetails}
        rows="4"
        placeholder="What went wrong? What did you expect?"
        disabled={helpSubmitting}
      ></textarea>
    </label>

    <section class="help-attachments" aria-label="Captured attachments">
      {#each helpAttachments as attachment}
        <div>
          <strong>{attachment.name}</strong>
          <span>{attachment.content_type} / {formatAttachmentSize(attachment.size)}</span>
        </div>
      {/each}
    </section>

    {#if helpError}
      <p class="help-error" role="alert">{helpError}</p>
    {/if}

    <footer>
      <button type="button" onclickcapture={closeHelpDialog} disabled={helpSubmitting}>Cancel</button>
      <button type="submit" class="primary" disabled={helpSubmitting}>
        {helpSubmitting ? 'Submitting' : 'Submit help task'}
      </button>
    </footer>
  </form>
</dialog>

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
  :global(html[data-theme='dark'] .task-attachments),
  :global(html[data-theme='dark'] .task-attachment),
  :global(html[data-theme='dark'] .task-attachment pre),
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
  :global(html[data-theme='dark'] .attachment-chip),
  :global(html[data-theme='dark'] .attachment-chip.pending button),
  :global(html[data-theme='dark'] .composer-buttons button),
  :global(html[data-theme='dark'] .composer-buttons .attach-button),
  :global(html[data-theme='dark'] .prompt-actions button),
  :global(html[data-theme='dark'] .help-dialog),
  :global(html[data-theme='dark'] .help-attachments div),
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
  :global(html[data-theme='dark'] .task-attachments h3),
  :global(html[data-theme='dark'] .task-attachment strong),
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
  :global(html[data-theme='dark'] .task-attachment span),
  :global(html[data-theme='dark'] .task-attachment a),
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
  .nav-measure a,
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
  .nav-measure,
  .right {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .desktop-nav.compact {
    display: none;
  }

  .nav-measure {
    position: absolute;
    inset: auto auto 100% 0;
    width: max-content;
    visibility: hidden;
    pointer-events: none;
  }

  .right.compact {
    justify-content: flex-end;
  }

  .right.compact .api,
  .right.compact .desktop-theme {
    display: none;
  }

  .desktop-nav a,
  .nav-measure a,
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
  }

  .desktop-nav a,
  .nav-measure a {
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

  .mobile-menu.compact {
    display: block;
  }

  .mobile-menu summary {
    list-style: none;
  }

  .mobile-menu summary::-webkit-details-marker {
    display: none;
  }

  .pwa-button,
  .help-button,
  .menu-button {
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

  .pwa-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }

  .pwa-button.update {
    border-color: var(--accent, #2563eb);
    color: #ffffff;
    background: var(--accent, #2563eb);
  }

  .pwa-button:disabled {
    opacity: 0.72;
    cursor: wait;
  }

  .help-button,
  .menu-button {
    display: none;
  }

  .help-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 3.2rem;
  }

  .menu-button {
    display: none;
    align-items: center;
    justify-content: center;
  }

  .mobile-menu.compact .menu-button {
    display: inline-flex;
  }

  .mobile-menu.compact[open] .mobile-nav {
    display: grid;
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

  .help-dialog {
    width: min(92vw, 31rem);
    max-height: min(92vh, calc(100dvh - 1rem));
    margin: 0;
    padding: 0;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.85rem;
    color: var(--text, #172033);
    background: var(--surface, #ffffff);
    box-shadow: 0 24px 70px rgb(15 23 42 / 0.24);
  }

  .help-dialog[open] {
    position: fixed;
    top: 50%;
    left: 50%;
    z-index: 50;
    transform: translate(-50%, -50%);
  }

  .help-dialog::backdrop {
    background: rgb(15 23 42 / 0.46);
  }

  .help-dialog-body {
    display: grid;
    gap: 0.85rem;
    max-height: min(92vh, calc(100dvh - 1rem));
    overflow-y: auto;
    padding: 1rem;
  }

  .help-dialog header,
  .help-dialog footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
  }

  .help-dialog header p,
  .help-dialog header h2,
  .help-status,
  .help-error {
    margin: 0;
  }

  .help-dialog header p,
  .help-dialog label span {
    color: var(--muted, #64748b);
    font-size: 0.72rem;
    font-weight: 850;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .help-dialog header h2 {
    color: var(--text-strong, #0f172a);
    font-size: 1.05rem;
  }

  .help-dialog header button,
  .help-dialog footer button {
    min-height: 2.35rem;
    padding: 0 0.75rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 0.55rem;
    color: var(--text, #243047);
    background: var(--surface, #ffffff);
    font: inherit;
    font-size: 0.84rem;
    font-weight: 800;
    cursor: pointer;
  }

  .help-dialog footer .primary {
    border-color: var(--accent, #2563eb);
    color: #ffffff;
    background: var(--accent, #2563eb);
  }

  .help-dialog label {
    display: grid;
    gap: 0.4rem;
  }

  .help-dialog textarea {
    box-sizing: border-box;
    width: 100%;
    min-height: 6.5rem;
    padding: 0.75rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 0.65rem;
    color: var(--text-strong, #111827);
    background: var(--surface, #ffffff);
    font: inherit;
    line-height: 1.45;
    resize: vertical;
  }

  .help-status {
    color: var(--muted, #64748b);
    font-size: 0.86rem;
    line-height: 1.4;
  }

  .help-error {
    padding: 0.65rem;
    border: 1px solid #fecaca;
    border-radius: 0.55rem;
    color: var(--danger-text, #991b1b);
    background: var(--danger-bg, #fef2f2);
    font-size: 0.84rem;
  }

  .help-attachments {
    display: grid;
    gap: 0.4rem;
  }

  .help-attachments div {
    display: grid;
    gap: 0.1rem;
    padding: 0.55rem 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.55rem;
    background: var(--surface-muted, #f8fafc);
  }

  .help-attachments strong {
    overflow-wrap: anywhere;
    color: var(--text-strong, #0f172a);
    font-size: 0.84rem;
  }

  .help-attachments span {
    color: var(--muted, #64748b);
    font-size: 0.76rem;
  }

  @media (max-width: 1120px) {
    .navbar {
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 0.75rem;
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

    .help-button,
    .pwa-button,
    .menu-button {
      display: inline-flex;
    }

    .mobile-nav {
      display: none;
    }

    .mobile-menu[open] .mobile-nav {
      display: grid;
    }
  }

  @media (max-width: 760px) {
    .navbar {
      min-height: 3.75rem;
    }
  }
</style>
