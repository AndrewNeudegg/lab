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
        rows: document.querySelectorAll('.task-row').length,
        panelCollapsed: document.querySelector('.command-panel')?.classList.contains('collapsed') ?? null,
        panelButton: document.querySelector('.command-header-actions button')?.innerText || '',
        queueCollapsed: document.querySelector('.task-pane')?.classList.contains('collapsed') ?? null,
        workflowState: document.querySelector('.state-machine')?.innerText || ''
      })`
    );
    assert(initial.filters.length === 3, 'task filters did not render', initial);
    assert(initial.filters.some((text) => text.includes('Needs action')), 'Needs action filter missing', initial);
    assert(initial.filters.some((text) => text.includes('Running')), 'Running filter missing', initial);
    assert(initial.filters.some((text) => text.includes('All')), 'All filter missing', initial);
    assert(initial.panelCollapsed === false, 'Act on this queue should start open', initial);
    if (initial.rows > 0) {
      assert(
        initial.workflowState.toLowerCase().includes('workflow state'),
        'workflow state machine guidance did not render',
        initial
      );
    }

    const afterRunning = await evalJS(
      cdp,
      `([...document.querySelectorAll('.triage button')].find((button) => button.innerText.includes('Running'))?.click(),
        new Promise((resolve) => setTimeout(() => resolve({
          active: document.querySelector('.triage button.active')?.innerText || '',
          rows: document.querySelectorAll('.task-row').length,
          selected: document.querySelector('.task-row.selected')?.innerText || '',
          queueCollapsed: document.querySelector('.task-pane')?.classList.contains('collapsed') ?? null
        }), 100)))`
    );
    assert(afterRunning.active.includes('Running'), 'Running filter did not become active', afterRunning);
    assert(afterRunning.queueCollapsed === false, 'desktop task queue collapsed during filter change', afterRunning);

    const afterAll = await evalJS(
      cdp,
      `([...document.querySelectorAll('.triage button')].find((button) => button.innerText.includes('All'))?.click(),
        new Promise((resolve) => setTimeout(() => resolve({
          active: document.querySelector('.triage button.active')?.innerText || '',
          rows: document.querySelectorAll('.task-row').length,
          selected: document.querySelector('.task-row.selected')?.innerText || '',
          queueCollapsed: document.querySelector('.task-pane')?.classList.contains('collapsed') ?? null
        }), 100)))`
    );
    assert(afterAll.active.includes('All'), 'All filter did not become active', afterAll);
    assert(afterAll.rows >= afterRunning.rows, 'All queue has fewer rows than Running queue', {
      afterRunning,
      afterAll
    });
    assert(afterAll.rows > 0, 'All queue rendered no task rows', afterAll);
    assert(afterAll.queueCollapsed === false, 'desktop task queue collapsed during all-filter selection', afterAll);

    const afterSelect = await evalJS(
      cdp,
      `(document.querySelector('.task-row')?.click(),
        new Promise((resolve) => setTimeout(() => resolve({
          queueCollapsed: document.querySelector('.task-pane')?.classList.contains('collapsed') ?? null,
          selected: document.querySelector('.task-row.selected')?.innerText || '',
          rows: document.querySelectorAll('.task-row').length
        }), 100)))`
    );
    assert(afterSelect.queueCollapsed === false, 'desktop task queue collapsed after selecting a task', afterSelect);
    assert(afterSelect.rows > 0, 'task rows disappeared after selecting a task', afterSelect);

    const collapse = await evalJS(
      cdp,
      `(document.querySelector('.command-header-actions button')?.click(),
        new Promise((resolve) => setTimeout(() => resolve({
          collapsed: document.querySelector('.command-panel')?.classList.contains('collapsed') ?? null,
          text: document.querySelector('.command-header-actions button')?.innerText || '',
          messagesVisible: document.querySelector('.messages') !== null
        }), 100)))`
    );
    assert(collapse.collapsed === true, 'Act on this queue did not collapse', collapse);
    assert(collapse.messagesVisible === false, 'collapsed command panel still rendered messages', collapse);

    const open = await evalJS(
      cdp,
      `(document.querySelector('.command-header-actions button')?.click(),
        new Promise((resolve) => setTimeout(() => resolve({
          collapsed: document.querySelector('.command-panel')?.classList.contains('collapsed') ?? null,
          text: document.querySelector('.command-header-actions button')?.innerText || '',
          messagesVisible: document.querySelector('.messages') !== null
        }), 100)))`
    );
    assert(open.collapsed === false, 'Act on this queue did not reopen', open);
    assert(open.messagesVisible === true, 'reopened command panel did not render messages', open);

    await cdp.call('Emulation.setDeviceMetricsOverride', {
      width: 390,
      height: 844,
      deviceScaleFactor: 2,
      mobile: true
    });
    await sleep(200);
    const mobile = await evalJS(
      cdp,
      `({
        bodyWidth: document.body.scrollWidth,
        viewport: window.innerWidth,
        rows: document.querySelectorAll('.task-row').length,
        queueToggle: document.querySelector('.queue-toggle')?.innerText || '',
        panelButton: document.querySelector('.command-header-actions button')?.innerText || ''
      })`
    );
    assert(mobile.rows > 0, 'mobile viewport rendered no task rows', mobile);
    assert(mobile.bodyWidth <= mobile.viewport + 2, 'mobile viewport has horizontal overflow', mobile);

    const typed = await evalJS(
      cdp,
      `(document.querySelector('#message').focus(),
        document.querySelector('#message').value = 'mobile typing should not echo below composer',
        document.querySelector('#message').dispatchEvent(new Event('input', { bubbles: true })),
        new Promise((resolve) => setTimeout(() => resolve({
          draftPreviewCount: document.querySelectorAll('.draft-preview').length,
          composerText: document.querySelector('#message')?.value || '',
          echoedBelowComposer: [...document.querySelectorAll('.composer ~ *, .draft-preview')]
            .some((element) => element.textContent.includes('mobile typing should not echo below composer'))
        }), 100)))`
    );
    assert(typed.composerText.includes('mobile typing should not echo'), 'mobile composer did not retain typed text', typed);
    assert(typed.draftPreviewCount === 0, 'mobile task command composer rendered a draft preview', typed);
    assert(typed.echoedBelowComposer === false, 'mobile typed text echoed below the composer', typed);

    const errors = await evalJS(cdp, `window.__uatErrors || []`);
    assert(errors.length === 0, 'browser console reported runtime errors', errors);

    console.log(
      JSON.stringify(
        {
          ok: true,
          dashboardURL,
          initial,
          afterRunning,
          afterAll,
          afterSelect,
          collapse,
          open,
          mobile,
          typed
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
