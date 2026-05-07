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
        syncButtonCount: document.querySelectorAll('.task-header button').length,
        syncStatusText: document.querySelector('.sync-status')?.innerText || '',
        syncStatusTone: document.querySelector('.sync-status')?.dataset.syncStatus || '',
        createSummary: document.querySelector('.target-create summary')?.innerText || '',
        mobilePanelNavCount: document.querySelectorAll('[aria-label="Task panels"]').length,
        approvalPopoutCount: document.querySelectorAll('[aria-label="Pending approvals"], .approval-list').length
      })`
    );
    assert(initial.filters.length === 3, 'task filters did not render', initial);
    assert(initial.filters.some((text) => text.includes('Attention')), 'Attention filter missing', initial);
    assert(initial.filters.some((text) => text.includes('Running')), 'Running filter missing', initial);
    assert(initial.filters.some((text) => text.includes('All')), 'All filter missing', initial);
    assert(initial.commandPanelCount === 0, 'old chat command panel still rendered', initial);
    assert(initial.composerCount === 0, 'chat composer still rendered on tasks page', initial);
    assert(initial.syncButtonCount === 0, 'manual Sync button still rendered', initial);
    assert(initial.syncStatusText.length > 0, 'automatic sync status indicator missing', initial);
    assert(
      ['connected', 'temporary-error', 'sustained-error'].includes(initial.syncStatusTone),
      'automatic sync status tone missing',
      initial
    );
    assert(initial.createSummary.includes('New task'), 'new task details control missing', initial);
    assert(initial.mobilePanelNavCount === 0, 'ambiguous mobile Queue/Task tabs still rendered', initial);
    assert(initial.approvalPopoutCount === 0, 'pending approvals queue popout still rendered', initial);

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
                path: location.pathname + location.search,
                selected: document.querySelector('.task-row.selected')?.innerText || '',
                actionText: document.querySelector('[aria-label="Task actions"]')?.innerText || '',
                emptyRecord: document.querySelector('.empty-record')?.innerText || '',
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
    assert(afterAll.path === '/tasks', 'All queue changed the overview URL before task selection', afterAll);
    assert(!afterAll.selected, 'All queue auto-selected a task before task click', afterAll);
    assert(afterAll.emptyRecord.includes('Select a task'), 'overview did not show the empty task record', afterAll);

    const automaticSync = await evalJS(
      cdp,
      `(() => {
        const countResources = () => {
	          const counts = { tasks: 0, approvals: 0, events: 0, agents: 0, workspaces: 0 };
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
	            if (path === '/api/workspaces' || path === '/workspaces') counts.workspaces += 1;
          }
          return counts;
        };
        return new Promise((resolve) => {
          const before = countResources();
          const syncedBefore = document.querySelector('.task-header span')?.innerText || '';
          const started = Date.now();
          const sample = () => {
            const after = countResources();
            const completed =
              after.tasks > before.tasks &&
	              after.approvals > before.approvals &&
	              after.events > before.events &&
	              after.agents > before.agents &&
	              after.workspaces > before.workspaces;
            if (completed || Date.now() - started > 11500) {
              const status = document.querySelector('.sync-status');
              resolve({
                completed,
                before,
                after,
                syncButtonCount: document.querySelectorAll('.task-header button').length,
                syncStatusText: status?.innerText || '',
                syncStatusTone: status?.dataset.syncStatus || '',
                syncedBefore,
                syncedAfter: document.querySelector('.task-header span')?.innerText || '',
                path: location.pathname + location.search,
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
    assert(automaticSync.completed === true, 'automatic sync did not reload all task pane data sources', automaticSync);
    assert(automaticSync.syncButtonCount === 0, 'automatic sync still exposed a manual button', automaticSync);
    assert(automaticSync.syncStatusText.includes('Connected'), 'automatic sync did not show connected status text', automaticSync);
    assert(automaticSync.syncStatusTone === 'connected', 'automatic sync did not show connected tone', automaticSync);
    assert(automaticSync.rows > 0, 'automatic sync left the task queue empty', automaticSync);
    assert(automaticSync.path === '/tasks', 'automatic sync changed the overview URL before task selection', automaticSync);
    assert(!automaticSync.selected, 'automatic sync auto-selected a visible task before task click', automaticSync);
    assert(
      /updated\s+\d{1,2}:\d{2}:\d{2}/i.test(automaticSync.syncedAfter),
      'automatic sync freshness timestamp did not include seconds',
      automaticSync
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

    const runningAfterAutoSync = await evalJS(
      cdp,
      `(() => {
        const countTaskResources = () => {
          let count = 0;
          for (const entry of performance.getEntriesByType('resource')) {
            try {
              const path = new URL(entry.name, location.href).pathname;
              if (path === '/api/tasks' || path === '/tasks') count += 1;
            } catch {
              continue;
            }
          }
          return count;
        };
        return new Promise((resolve) => {
          const before = countTaskResources();
          const started = Date.now();
          const sample = () => {
            const after = countTaskResources();
            const active = document.querySelector('.triage button.active')?.innerText || '';
            const elapsed = Date.now() - started;
            if ((after > before && elapsed >= 8300) || elapsed > 11500) {
              resolve({
                active,
                before,
                after,
                elapsed,
                rows: document.querySelectorAll('.task-row').length,
                selected: document.querySelector('.task-row.selected')?.innerText || ''
              });
              return;
            }
            setTimeout(sample, 200);
          };
          sample();
        });
      })()`
    );
    assert(
      runningAfterAutoSync.after > runningAfterAutoSync.before,
      'background task sync did not run while waiting on Running filter',
      runningAfterAutoSync
    );
    assert(
      runningAfterAutoSync.active.includes('Running'),
      'Running filter changed after background task sync',
      runningAfterAutoSync
    );

    const historyBack = await evalJS(
      cdp,
      `([...document.querySelectorAll('.triage button')].find((button) => button.innerText.includes('All'))?.click(),
        new Promise((resolve) => {
          const started = Date.now();
          const waitForAll = () => {
            const active = document.querySelector('.triage button.active')?.innerText || '';
            const rows = [...document.querySelectorAll('.task-row')];
            if ((active.includes('All') && rows.length > 0) || Date.now() - started > 2500) {
              const before = location.pathname + location.search;
              rows[0]?.click();
              setTimeout(() => {
                const selectedPath = location.pathname + location.search;
                history.back();
                setTimeout(() => resolve({
                  before,
                  selectedPath,
                  after: location.pathname + location.search,
                  selected: document.querySelector('.task-row.selected')?.innerText || '',
                  emptyRecord: document.querySelector('.empty-record')?.innerText || '',
                  detailVisible: getComputedStyle(document.querySelector('.workbench')).display !== 'none'
                }), 350);
              }, 250);
              return;
            }
            setTimeout(waitForAll, 100);
          };
          waitForAll();
        }))`
    );
    assert(historyBack.before === '/tasks', 'task selection history did not start from the overview URL', historyBack);
    assert(
      historyBack.selectedPath.startsWith('/tasks?task='),
      'task row click did not navigate to a task-specific URL',
      historyBack
    );
    assert(historyBack.after === '/tasks', 'browser Back from a selected task did not return to overview URL', historyBack);
    assert(!historyBack.selected, 'browser Back left a task selected on the overview route', historyBack);
    assert(historyBack.emptyRecord.includes('Select a task'), 'browser Back did not restore the overview empty record', historyBack);

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
                workflowState: document.querySelector('.state-machine')?.innerText || '',
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
      afterSelect.workflowState.toLowerCase().includes('workflow state'),
      'workflow state did not render after selecting a task',
      afterSelect
    );
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
      width: 980,
      height: 900,
      deviceScaleFactor: 1,
      mobile: false
    });
    await sleep(250);

    const mediumDiff = await evalJS(
      cdp,
      `new Promise((resolve) => requestAnimationFrame(() => {
        const fileList = document.querySelector('[aria-label="Changed files"]');
        const splitDiff = document.querySelector('[aria-label="Split diff"]');
        const scroll = document.querySelector('.diff-scroll[data-mode="split"]');
        const firstRow = document.querySelector('.task-row');
        resolve({
          available: Boolean(fileList && splitDiff && scroll),
          fileListDisplay: fileList ? getComputedStyle(fileList).display : '',
          fileListBorderRight: fileList ? getComputedStyle(fileList).borderRightWidth : '',
          splitMinWidth: splitDiff ? getComputedStyle(splitDiff).minWidth : '',
          scrollWidth: scroll ? Math.round(scroll.scrollWidth) : 0,
          clientWidth: scroll ? Math.round(scroll.clientWidth) : 0,
          rowHeight: firstRow ? firstRow.getBoundingClientRect().height : 0,
          rowScrollHeight: firstRow ? firstRow.scrollHeight : 0
        });
      }))`
    );
    if (diffInitial.fileButtons.length > 0) {
      assert(mediumDiff.available === true, 'medium-width diff was unavailable', mediumDiff);
      assert(mediumDiff.fileListDisplay === 'flex', 'medium-width diff file list did not move above the diff', mediumDiff);
      assert(mediumDiff.fileListBorderRight === '0px', 'medium-width diff file list still consumes a side column', mediumDiff);
      assert(mediumDiff.splitMinWidth !== '0px', 'medium-width split diff still allows code columns to collapse', mediumDiff);
      assert(
        mediumDiff.scrollWidth > mediumDiff.clientWidth,
        'medium-width split diff did not keep a readable scrolled width',
        mediumDiff
      );
      assert(mediumDiff.rowHeight + 1 >= mediumDiff.rowScrollHeight, 'task queue row content is vertically clipped', mediumDiff);
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
          const taskHeading = document.querySelector('.task-header h1');
          const syncStatus = document.querySelector('.sync-status');
          resolve({
          rows: document.querySelectorAll('.task-row').length,
          queueDisplay: getComputedStyle(document.querySelector('.task-pane')).display,
          detailDisplay: getComputedStyle(document.querySelector('.workbench')).display,
          taskHeadingTop: taskHeading?.getBoundingClientRect().top ?? null,
          syncTop: syncStatus?.getBoundingClientRect().top ?? null,
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
    assert(
      mobileQueue.taskHeadingTop === null ||
        mobileQueue.navbarBottom === null ||
        mobileQueue.taskHeadingTop >= mobileQueue.navbarBottom,
      'mobile task queue heading is overlapped by the navbar',
      mobileQueue
    );
    assert(
      mobileQueue.syncTop === null ||
        mobileQueue.navbarBottom === null ||
        mobileQueue.syncTop >= mobileQueue.navbarBottom,
      'mobile sync status is overlapped by the navbar',
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
      `(() => {
        const taskList = document.querySelector('.task-list');
        const taskPane = document.querySelector('.task-pane');
        if (taskList) {
          taskList.scrollTop = taskList.scrollHeight;
        }
        window.scrollTo(0, document.body.scrollHeight);
        return new Promise((resolve) => setTimeout(() => resolve({
          windowScrollY: window.scrollY,
          navbarTop: document.querySelector('.navbar')?.getBoundingClientRect().top ?? null,
          taskListScrollTop: Math.round(taskList?.scrollTop || 0),
          taskListScrollable: Math.round((taskList?.scrollHeight || 0) - (taskList?.clientHeight || 0)),
          taskListOverflowY: taskList ? getComputedStyle(taskList).overflowY : '',
          taskPaneOverflowY: taskPane ? getComputedStyle(taskPane).overflowY : '',
          bodyWidth: document.body.scrollWidth,
          viewport: window.innerWidth
        }), 100));
      })()`
    );
    assert(mobileScroll.windowScrollY <= 2, 'mobile page scrolled instead of task list', mobileScroll);
    assert(
      Math.abs(mobileScroll.navbarTop) <= 1,
      'mobile navbar did not remain sticky at viewport top',
      mobileScroll
    );
    assert(mobileScroll.taskListOverflowY === 'auto', 'mobile task list does not own vertical scrolling', mobileScroll);
    assert(mobileScroll.taskPaneOverflowY === 'hidden', 'mobile task pane allows page-level scrolling', mobileScroll);
    if (mobileScroll.taskListScrollable > 2) {
      assert(mobileScroll.taskListScrollTop > 0, 'mobile task list did not scroll independently', mobileScroll);
    }
    assert(mobileScroll.bodyWidth <= mobileScroll.viewport + 2, 'mobile scroll check has horizontal overflow', mobileScroll);

    const mobileEmptyQueue = await evalJS(
      cdp,
      `(() => {
        const search = document.querySelector('#task-search');
        if (search) {
          search.value = 'zz_no_matching_mobile_tasks_scroll_regression';
          search.dispatchEvent(new Event('input', { bubbles: true }));
        }
        return new Promise((resolve) => setTimeout(() => {
          const taskList = document.querySelector('.task-list');
          const taskPane = document.querySelector('.task-pane');
          const footer = document.querySelector('.task-pane footer');
          window.scrollTo(0, document.body.scrollHeight);
          resolve({
            rows: document.querySelectorAll('.task-row').length,
            emptyText: document.querySelector('.task-list .empty')?.innerText || '',
            scrollY: window.scrollY,
            taskListHeight: Math.round(taskList?.getBoundingClientRect().height || 0),
            taskListOverflowY: taskList ? getComputedStyle(taskList).overflowY : '',
            taskPaneOverflowY: taskPane ? getComputedStyle(taskPane).overflowY : '',
            footerBottom: footer?.getBoundingClientRect().bottom ?? null,
            pageScrollHeight: document.scrollingElement?.scrollHeight ?? document.documentElement.scrollHeight,
            pageClientHeight: document.scrollingElement?.clientHeight ?? document.documentElement.clientHeight,
            bodyWidth: document.body.scrollWidth,
            viewport: window.innerWidth
          });
        }, 200));
      })()`
    );
    assert(mobileEmptyQueue.rows === 0, 'mobile empty queue still rendered task rows', mobileEmptyQueue);
    assert(
      mobileEmptyQueue.emptyText.includes('No tasks match'),
      'mobile empty queue message did not render',
      mobileEmptyQueue
    );
    assert(mobileEmptyQueue.scrollY <= 2, 'mobile empty queue page scrolled below the footer', mobileEmptyQueue);
    assert(mobileEmptyQueue.taskListHeight > 0, 'mobile empty queue list container collapsed', mobileEmptyQueue);
    assert(
      mobileEmptyQueue.taskListOverflowY === 'auto',
      'mobile empty queue task list lost internal scrolling',
      mobileEmptyQueue
    );
    assert(
      mobileEmptyQueue.taskPaneOverflowY === 'hidden',
      'mobile empty queue allowed page-level pane scrolling',
      mobileEmptyQueue
    );
    assert(
      mobileEmptyQueue.pageScrollHeight <= mobileEmptyQueue.pageClientHeight + 1,
      'mobile empty queue document has a vertical scroll range',
      mobileEmptyQueue
    );
    assert(
      mobileEmptyQueue.footerBottom === null ||
        mobileEmptyQueue.footerBottom <= mobileEmptyQueue.pageClientHeight + 1,
      'mobile empty queue footer fell below the layout viewport',
      mobileEmptyQueue
    );
    assert(
      mobileEmptyQueue.bodyWidth <= mobileEmptyQueue.viewport + 2,
      'mobile empty queue has horizontal overflow',
      mobileEmptyQueue
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
          automaticSync,
          afterRunning,
          runningAfterAutoSync,
          historyBack,
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
          mobileScroll,
          mobileEmptyQueue
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
