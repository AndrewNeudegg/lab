const dashboardURL = process.env.DASHBOARD_URL || 'http://127.0.0.1:5173/tasks';
const chromeBin = process.env.CHROME_BIN || 'chromium';
const port = Number(process.env.CHROME_REMOTE_DEBUGGING_PORT || 9333);
const userDataDir = process.env.CHROME_USER_DATA_DIR || `/tmp/homelab-tasks-uat-${port}`;

const sleep = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));

const assert = (condition, message, detail) => {
  if (!condition) {
    const suffix = detail === undefined ? '' : `\n${JSON.stringify(detail, null, 2)}`;
    throw new Error(`${message}${suffix}`);
  }
};

const createPage = async (url) => {
  const endpoint = `http://127.0.0.1:${port}`;
  let response = await fetch(`${endpoint}/json/new?${encodeURIComponent(url)}`, { method: 'PUT' });
  if (!response.ok) {
    response = await fetch(`${endpoint}/json/new?${encodeURIComponent(url)}`);
  }
  assert(response.ok, `failed to create Chromium page: ${response.status}`);
  return response.json();
};

const connect = (webSocketURL) => {
  let nextID = 1;
  const pending = new Map();
  const ws = new WebSocket(webSocketURL);

  ws.onmessage = (message) => {
    const payload = JSON.parse(message.data);
    if (!payload.id || !pending.has(payload.id)) {
      return;
    }
    const { resolve, reject } = pending.get(payload.id);
    pending.delete(payload.id);
    if (payload.error) {
      reject(new Error(JSON.stringify(payload.error)));
      return;
    }
    resolve(payload.result);
  };

  const opened = new Promise((resolve, reject) => {
    ws.onopen = resolve;
    ws.onerror = reject;
  });

  return {
    opened,
    call(method, params = {}) {
      const id = nextID++;
      ws.send(JSON.stringify({ id, method, params }));
      return new Promise((resolve, reject) => pending.set(id, { resolve, reject }));
    },
    close() {
      ws.close();
    }
  };
};

const evalJS = async (cdp, expression) => {
  const result = await cdp.call('Runtime.evaluate', {
    expression,
    awaitPromise: true,
    returnByValue: true
  });
  if (result.exceptionDetails) {
    throw new Error(JSON.stringify(result.exceptionDetails, null, 2));
  }
  return result.result.value;
};

const launchChrome = () =>
  Bun.spawn(
    [
      chromeBin,
      '--headless',
      '--no-sandbox',
      '--disable-gpu',
      `--remote-debugging-port=${port}`,
      `--user-data-dir=${userDataDir}`,
      'about:blank'
    ],
    {
      stdout: 'pipe',
      stderr: 'pipe'
    }
  );

const run = async () => {
  const chrome = launchChrome();
  await sleep(1800);

  try {
    const page = await createPage('about:blank');
    const cdp = connect(page.webSocketDebuggerUrl);
    await cdp.opened;
    await cdp.call('Runtime.enable');
    await cdp.call('Page.enable');
    await cdp.call('Emulation.setDeviceMetricsOverride', {
      width: 1440,
      height: 1000,
      deviceScaleFactor: 1,
      mobile: false
    });
    await cdp.call('Page.navigate', { url: dashboardURL });
    await sleep(4000);

    await evalJS(
      cdp,
      `window.__uatErrors = [];
       window.addEventListener('error', (event) => window.__uatErrors.push(event.message));
       window.addEventListener('unhandledrejection', (event) => window.__uatErrors.push(String(event.reason)));
       true`
    );

    const initial = await evalJS(
      cdp,
      `({
        filters: [...document.querySelectorAll('.triage button')].map((button) => button.innerText),
        commandPanelCount: document.querySelectorAll('.command-panel').length,
        composerCount: document.querySelectorAll('.composer, #message').length,
        taskActionText: document.querySelector('[aria-label="Task actions"]')?.innerText || '',
        syncText: document.querySelector('.task-header button')?.innerText || '',
        createSummary: document.querySelector('.target-create summary')?.innerText || '',
        mobilePanelNavCount: document.querySelectorAll('[aria-label="Task panels"]').length
      })`
    );
    assert(initial.filters.length === 3, 'task filters did not render', initial);
    assert(initial.filters.some((text) => text.includes('Needs action')), 'Needs action filter missing', initial);
    assert(initial.filters.some((text) => text.includes('Running')), 'Running filter missing', initial);
    assert(initial.filters.some((text) => text.includes('All')), 'All filter missing', initial);
    assert(initial.commandPanelCount === 0, 'old chat command panel still rendered', initial);
    assert(initial.composerCount === 0, 'chat composer still rendered on tasks page', initial);
    assert(initial.syncText.includes('Sync'), 'manual Sync button missing', initial);
    assert(initial.createSummary.includes('New task'), 'new task details control missing', initial);
    assert(initial.mobilePanelNavCount === 0, 'ambiguous mobile Queue/Task tabs still rendered', initial);

    const afterAll = await evalJS(
      cdp,
      `([...document.querySelectorAll('.triage button')].find((button) => button.innerText.includes('All'))?.click(),
        new Promise((resolve) => {
          const started = Date.now();
          const sample = () => {
            const rows = document.querySelectorAll('.task-row').length;
            const active = document.querySelector('.triage button.active')?.innerText || '';
            if ((rows > 0 && active.includes('All')) || Date.now() - started > 2500) {
              resolve({
                active,
                rows,
                selected: document.querySelector('.task-row.selected')?.innerText || '',
                actionText: document.querySelector('[aria-label="Task actions"]')?.innerText || '',
                workflowState: document.querySelector('.state-machine')?.innerText || ''
              });
              return;
            }
            setTimeout(sample, 100);
          };
          sample();
        }))`
    );
    assert(afterAll.active.includes('All'), 'All filter did not become active', afterAll);
    assert(afterAll.rows > 0, 'All queue rendered no task rows', afterAll);
    assert(afterAll.selected, 'All queue did not leave a selected task', afterAll);
    assert(afterAll.actionText.length > 0, 'direct task action panel did not render', afterAll);
    assert(
      afterAll.workflowState.toLowerCase().includes('workflow state'),
      'workflow state did not render',
      afterAll
    );

    const manualSync = await evalJS(
      cdp,
      `(() => {
        const countResources = () => {
          const counts = { tasks: 0, approvals: 0, events: 0, agents: 0 };
          for (const entry of performance.getEntriesByType('resource')) {
            let path = '';
            try {
              path = new URL(entry.name, location.href).pathname;
            } catch {
              continue;
            }
            if (path === '/api/tasks' || path === '/tasks') counts.tasks += 1;
            if (path === '/api/approvals' || path === '/approvals') counts.approvals += 1;
            if (path === '/api/events' || path === '/events') counts.events += 1;
            if (path === '/api/agents' || path === '/agents') counts.agents += 1;
          }
          return counts;
        };
        return new Promise((resolve) => {
          const button = document.querySelector('.task-header button');
          const before = countResources();
          const syncedBefore = document.querySelector('.task-header span')?.innerText || '';
          button?.click();
          const started = Date.now();
          const sample = () => {
            const after = countResources();
            const completed =
              Boolean(button) &&
              !button.disabled &&
              after.tasks > before.tasks &&
              after.approvals > before.approvals &&
              after.events > before.events &&
              after.agents > before.agents;
            if (completed || Date.now() - started > 8000) {
              resolve({
                completed,
                before,
                after,
                syncedBefore,
                syncedAfter: document.querySelector('.task-header span')?.innerText || '',
                rows: document.querySelectorAll('.task-row').length,
                selected: document.querySelector('.task-row.selected')?.innerText || ''
              });
              return;
            }
            setTimeout(sample, 100);
          };
          setTimeout(sample, 100);
        });
      })()`
    );
    assert(manualSync.completed === true, 'manual Sync did not reload all task pane data sources', manualSync);
    assert(manualSync.rows > 0, 'manual Sync left the task queue empty', manualSync);
    assert(manualSync.selected, 'manual Sync did not leave a selected visible task', manualSync);
    assert(
      /synced\s+\d{1,2}:\d{2}:\d{2}/i.test(manualSync.syncedAfter),
      'manual Sync freshness timestamp did not include seconds',
      manualSync
    );

    const afterRunning = await evalJS(
      cdp,
      `([...document.querySelectorAll('.triage button')].find((button) => button.innerText.includes('Running'))?.click(),
        new Promise((resolve) => {
          const started = Date.now();
          const sample = () => {
            const active = document.querySelector('.triage button.active')?.innerText || '';
            if (active.includes('Running') || Date.now() - started > 2500) {
              resolve({
                active,
                rows: document.querySelectorAll('.task-row').length
              });
              return;
            }
            setTimeout(sample, 100);
          };
          sample();
        }))`
    );
    assert(afterRunning.active.includes('Running'), 'Running filter did not become active', afterRunning);

    const afterSelect = await evalJS(
      cdp,
      `([...document.querySelectorAll('.triage button')].find((button) => button.innerText.includes('All'))?.click(),
        new Promise((resolve) => {
          const started = Date.now();
          const waitForAll = () => {
            const active = document.querySelector('.triage button.active')?.innerText || '';
            const rows = [...document.querySelectorAll('.task-row')];
            if ((active.includes('All') && rows.length > 0) || Date.now() - started > 2500) {
              rows[Math.min(1, rows.length - 1)]?.click();
              setTimeout(() => resolve({
                rows: document.querySelectorAll('.task-row').length,
                selected: document.querySelector('.task-row.selected')?.innerText || '',
                actionButtons: [...document.querySelectorAll('[aria-label="Task actions"] button')]
                  .map((button) => ({ text: button.innerText, disabled: button.disabled })),
                retrySettings: document.querySelector('[aria-label="Retry settings"]')?.innerText || '',
                reopenReason: document.querySelector('[aria-label="Reopen reason"]')?.innerText || '',
                workerTrace: document.querySelector('[aria-label="Worker runs"]')?.innerText || '',
                hasComposer: document.querySelector('#message, .composer') !== null
              }), 250);
              return;
            }
            setTimeout(waitForAll, 100);
          };
          waitForAll();
        }))`
    );
    assert(afterSelect.rows > 0, 'task rows disappeared after selecting a task', afterSelect);
    assert(afterSelect.selected, 'task click did not select a row', afterSelect);
    assert(afterSelect.actionButtons.length > 0, 'no direct action buttons rendered for selected task', afterSelect);
    assert(afterSelect.hasComposer === false, 'task detail rendered a chat composer after selection', afterSelect);
    assert(
      afterSelect.workerTrace.toLowerCase().includes('worker trace'),
      'worker trace panel did not render',
      afterSelect
    );

    const createForm = await evalJS(
      cdp,
      `(document.querySelector('.target-create summary')?.click(),
        new Promise((resolve) => setTimeout(() => {
          const goal = document.querySelector('#new-task-goal');
          goal.value = 'Inspect button driven task page flow';
          goal.dispatchEvent(new Event('input', { bubbles: true }));
          setTimeout(() => resolve({
            open: document.querySelector('.target-create')?.open ?? false,
            disabled: document.querySelector('.target-create button[type="submit"]')?.disabled ?? null,
            label: document.querySelector('.target-create button[type="submit"]')?.innerText || ''
          }), 100);
        }, 100)))`
    );
    assert(createForm.open === true, 'new task details did not open', createForm);
    assert(createForm.disabled === false, 'new task button did not enable after entering a goal', createForm);

    const diffInitial = await evalJS(
      cdp,
      `new Promise((resolve) => {
        const started = Date.now();
        const sample = () => {
          const panel = document.querySelector('[aria-label="Task diff"]');
          const text = panel?.innerText || '';
          const loading = text.includes('Loading task diff');
          if ((panel && !loading) || Date.now() - started > 3500) {
            resolve({
              text,
              controls: [...document.querySelectorAll('[aria-label="Diff controls"] button')].map((button) => ({
                text: button.innerText,
                active: button.classList.contains('active'),
                disabled: button.disabled
              })),
              fileButtons: [...document.querySelectorAll('[aria-label="Changed files"] button')].map((button) => ({
                text: button.innerText,
                selected: button.classList.contains('selected'),
                label: button.querySelector('span')?.innerText || '',
                labelHeight: button.querySelector('span')?.getBoundingClientRect().height || 0
              })),
              splitRows: document.querySelectorAll('[aria-label="Split diff"] .split-row').length,
              unifiedRows: document.querySelectorAll('[aria-label="Unified diff"] .diff-row').length
            });
            return;
          }
          setTimeout(sample, 100);
        };
        sample();
      })`
    );
    assert(
      diffInitial.text.toLowerCase().includes('changes vs main'),
      'task diff panel did not render',
      diffInitial
    );
    assert(
      diffInitial.controls.some((control) => control.text.includes('Split') && control.active),
      'Split diff view was not active by default',
      diffInitial
    );
    assert(
      diffInitial.controls.some((control) => control.text.includes('Unified')),
      'Unified diff control did not render',
      diffInitial
    );
    if (diffInitial.fileButtons.length > 0) {
      assert(
        diffInitial.fileButtons.some((button) => button.selected),
        'changed file list rendered without a selected file',
        diffInitial
      );
      assert(
        diffInitial.fileButtons.every((button) => button.label.trim().length > 0 && button.labelHeight >= 10),
        'changed file list labels were empty or visually collapsed',
        diffInitial
      );
      assert(diffInitial.splitRows > 0, 'split diff rows did not render for selected file', diffInitial);
    }

    const diffUnified = await evalJS(
      cdp,
      `([...document.querySelectorAll('[aria-label="Diff controls"] button')]
          .find((button) => button.innerText.includes('Unified'))?.click(),
        new Promise((resolve) => setTimeout(() => resolve({
          controls: [...document.querySelectorAll('[aria-label="Diff controls"] button')].map((button) => ({
            text: button.innerText,
            active: button.classList.contains('active')
          })),
          unifiedRows: document.querySelectorAll('[aria-label="Unified diff"] .diff-row').length
        }), 150)))`
    );
    assert(
      diffUnified.controls.some((control) => control.text.includes('Unified') && control.active),
      'Unified diff control did not become active',
      diffUnified
    );
    if (diffInitial.fileButtons.length > 0) {
      assert(diffUnified.unifiedRows > 0, 'unified diff rows did not render after toggle', diffUnified);
    }

    const diffSplit = await evalJS(
      cdp,
      `([...document.querySelectorAll('[aria-label="Diff controls"] button')]
          .find((button) => button.innerText.includes('Split'))?.click(),
        new Promise((resolve) => setTimeout(() => resolve({
          controls: [...document.querySelectorAll('[aria-label="Diff controls"] button')].map((button) => ({
            text: button.innerText,
            active: button.classList.contains('active')
          }))
        }), 150)))`
    );
    assert(
      diffSplit.controls.some((control) => control.text.includes('Split') && control.active),
      'Split diff control did not become active again',
      diffSplit
    );

    const diffWrap = await evalJS(
      cdp,
      `(() => {
        const scroll = document.querySelector('.diff-scroll[data-mode="split"]');
        const diff = document.querySelector('[aria-label="Split diff"]');
        const code = document.querySelector('[aria-label="Split diff"] .split-row:not(.full) code:not(.blank)');
        if (!scroll || !diff || !code) {
          return { available: false };
        }
        const original = code.innerHTML;
        code.textContent = '+' + 'const_wrapped_diff_line_'.repeat(18);
        return new Promise((resolve) => requestAnimationFrame(() => {
          const style = getComputedStyle(code);
          const diffStyle = getComputedStyle(diff);
          const codeHeight = code.getBoundingClientRect().height;
          const lineHeight = Number.parseFloat(style.lineHeight);
          const result = {
            available: true,
            whiteSpace: style.whiteSpace,
            overflowWrap: style.overflowWrap,
            diffMinWidth: diffStyle.minWidth,
            codeHeight,
            lineHeight,
            scrollWidth: Math.round(scroll.scrollWidth),
            clientWidth: Math.round(scroll.clientWidth)
          };
          code.innerHTML = original;
          resolve(result);
        }));
      })()`
    );
    if (diffInitial.fileButtons.length > 0) {
      assert(diffWrap.available === true, 'split diff code cell was unavailable for wrap check', diffWrap);
      assert(diffWrap.whiteSpace === 'pre-wrap', 'split diff code cells do not preserve and wrap whitespace', diffWrap);
      assert(diffWrap.overflowWrap === 'anywhere', 'split diff code cells do not break long tokens', diffWrap);
      assert(diffWrap.diffMinWidth === '0px', 'split diff keeps a fixed minimum width', diffWrap);
      assert(diffWrap.codeHeight > diffWrap.lineHeight * 1.8, 'long split diff line did not wrap to multiple visual lines', diffWrap);
      assert(diffWrap.scrollWidth <= diffWrap.clientWidth + 2, 'split diff still creates horizontal overflow', diffWrap);
    }

    const diffDark = await evalJS(
      cdp,
      `(localStorage.setItem('homelabd.dashboard.theme', 'dark'),
        document.documentElement.dataset.theme = 'dark',
        document.documentElement.style.colorScheme = 'dark',
        new Promise((resolve) => requestAnimationFrame(() => {
          const panel = document.querySelector('[aria-label="Task diff"]');
          const selected = document.querySelector('[aria-label="Changed files"] button.selected');
          const label = selected?.querySelector('span');
          const styleFor = (element) => element ? {
            color: getComputedStyle(element).color,
            background: getComputedStyle(element).backgroundColor,
            border: getComputedStyle(element).borderColor,
            height: element.getBoundingClientRect().height,
            text: element.textContent || ''
          } : null;
          resolve({
            theme: document.documentElement.dataset.theme,
            panel: styleFor(panel),
            selected: styleFor(selected),
            label: styleFor(label)
          });
        })))`
    );
    if (diffInitial.fileButtons.length > 0) {
      assert(diffDark.theme === 'dark', 'dark theme did not apply to the document', diffDark);
      assert(diffDark.label?.text.trim(), 'dark diff file label text was empty', diffDark);
      assert(diffDark.label?.height >= 10, 'dark diff file label was visually collapsed', diffDark);
      assert(
        diffDark.panel?.background !== 'rgb(255, 255, 255)' &&
          diffDark.selected?.background !== 'rgb(239, 246, 255)',
        'diff panel kept light-mode backgrounds in dark mode',
        diffDark
      );
    }

    await cdp.call('Emulation.setDeviceMetricsOverride', {
      width: 390,
      height: 844,
      deviceScaleFactor: 2,
      mobile: true
    });
    await sleep(250);

    const mobileStart = await evalJS(
      cdp,
      `new Promise((resolve) => requestAnimationFrame(() => resolve({
        bodyWidth: document.body.scrollWidth,
        viewport: window.innerWidth,
        panelNavCount: document.querySelectorAll('[aria-label="Task panels"], .mobile-tabs').length,
        queueDisplay: getComputedStyle(document.querySelector('.task-pane')).display,
        detailDisplay: getComputedStyle(document.querySelector('.workbench')).display,
        commandPanelCount: document.querySelectorAll('.command-panel, .composer, #message').length
      })))`
    );
    assert(mobileStart.bodyWidth <= mobileStart.viewport + 2, 'mobile viewport has horizontal overflow', mobileStart);
    assert(mobileStart.panelNavCount === 0, 'mobile still renders ambiguous Queue/Task tabs', mobileStart);
    assert(mobileStart.commandPanelCount === 0, 'mobile tasks page still renders chat command controls', mobileStart);

    const mobileQueue = await evalJS(
      cdp,
      `(document.querySelector('.back-to-queue')?.click(),
        new Promise((resolve) => setTimeout(() => {
          const firstRow = document.querySelector('.task-row');
          const navbar = document.querySelector('.navbar');
          resolve({
          rows: document.querySelectorAll('.task-row').length,
          queueDisplay: getComputedStyle(document.querySelector('.task-pane')).display,
          detailDisplay: getComputedStyle(document.querySelector('.workbench')).display,
          firstRowTop: firstRow?.getBoundingClientRect().top ?? null,
          navbarBottom: navbar?.getBoundingClientRect().bottom ?? null,
          bodyWidth: document.body.scrollWidth,
          viewport: window.innerWidth
        });
        }, 150)))`
    );
    assert(mobileQueue.rows > 0, 'mobile Queue tab rendered no task rows', mobileQueue);
    assert(mobileQueue.queueDisplay !== 'none', 'mobile Queue tab did not show queue', mobileQueue);
    assert(mobileQueue.detailDisplay === 'none', 'mobile Queue tab did not hide detail pane', mobileQueue);
    assert(
      mobileQueue.firstRowTop === null ||
        mobileQueue.navbarBottom === null ||
        mobileQueue.firstRowTop >= mobileQueue.navbarBottom,
      'mobile queue rows are overlapped by the navbar',
      mobileQueue
    );
    assert(mobileQueue.bodyWidth <= mobileQueue.viewport + 2, 'mobile Queue tab has horizontal overflow', mobileQueue);

    const mobileSelect = await evalJS(
      cdp,
      `(document.querySelector('.task-row')?.click(),
        new Promise((resolve) => setTimeout(() => resolve({
          selected: document.querySelector('.task-row.selected')?.innerText || '',
          queueDisplay: getComputedStyle(document.querySelector('.task-pane')).display,
          detailDisplay: getComputedStyle(document.querySelector('.workbench')).display,
          backButton: document.querySelector('.back-to-queue')?.innerText || '',
          taskActions: document.querySelector('[aria-label="Task actions"]')?.innerText || '',
          workerOpen: document.querySelector('[aria-label="Worker runs"]')?.open ?? null,
          scrollY: window.scrollY,
          bodyWidth: document.body.scrollWidth,
          viewport: window.innerWidth
        }), 200)))`
    );
    assert(mobileSelect.selected, 'mobile task tap did not select a queue row', mobileSelect);
    assert(mobileSelect.queueDisplay === 'none', 'mobile Task tab did not hide queue', mobileSelect);
    assert(mobileSelect.detailDisplay !== 'none', 'mobile Task tab did not show selected detail', mobileSelect);
    assert(mobileSelect.backButton.includes('Back to queue'), 'mobile detail did not expose a clear back-to-queue control', mobileSelect);
    assert(mobileSelect.taskActions.length > 0, 'mobile selected task did not show action buttons', mobileSelect);
    assert(mobileSelect.workerOpen === false, 'mobile worker trace should start collapsed', mobileSelect);
    assert(mobileSelect.scrollY <= 2, 'mobile detail did not start at the top after selecting a task', mobileSelect);
    assert(mobileSelect.bodyWidth <= mobileSelect.viewport + 2, 'mobile selected detail has horizontal overflow', mobileSelect);

    const mobileBack = await evalJS(
      cdp,
      `(document.querySelector('.back-to-queue')?.click(),
        new Promise((resolve) => setTimeout(() => resolve({
          rows: document.querySelectorAll('.task-row').length,
          queueDisplay: getComputedStyle(document.querySelector('.task-pane')).display,
          detailDisplay: getComputedStyle(document.querySelector('.workbench')).display
        }), 150)))`
    );
    assert(mobileBack.detailDisplay === 'none', 'mobile Back to queue did not hide detail', mobileBack);
    assert(mobileBack.rows > 0, 'mobile queue rows disappeared after returning from detail', mobileBack);
    assert(mobileBack.queueDisplay !== 'none', 'mobile return did not show queue', mobileBack);

    const mobileScroll = await evalJS(
      cdp,
      `(window.scrollTo(0, document.body.scrollHeight),
        new Promise((resolve) => setTimeout(() => resolve({
        scrollY: window.scrollY,
        navbarTop: document.querySelector('.navbar')?.getBoundingClientRect().top ?? null
      }), 100)))`
    );
    assert(mobileScroll.scrollY > 0, 'mobile viewport did not scroll for sticky navbar check', mobileScroll);
    assert(
      Math.abs(mobileScroll.navbarTop) <= 1,
      'mobile navbar did not remain sticky at viewport top',
      mobileScroll
    );

    const errors = await evalJS(cdp, `window.__uatErrors || []`);
    assert(errors.length === 0, 'browser console reported runtime errors', errors);

    console.log(
      JSON.stringify(
        {
          ok: true,
          dashboardURL,
          initial,
          afterAll,
          manualSync,
          afterRunning,
          afterSelect,
          createForm,
          diffInitial,
          diffUnified,
          diffSplit,
          diffWrap,
          diffDark,
          mobileStart,
          mobileQueue,
          mobileSelect,
          mobileBack,
          mobileScroll
        },
        null,
        2
      )
    );
    cdp.close();
  } finally {
    chrome.kill();
    await chrome.exited.catch(() => undefined);
  }
};

run().catch((error) => {
  console.error(error.stack || error.message || error);
  process.exit(1);
});
