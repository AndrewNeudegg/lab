const dashboardURL = process.env.DASHBOARD_URL || 'http://127.0.0.1:5173/chat';
const chromeBin = process.env.CHROME_BIN || 'chromium';
const port = Number(process.env.CHROME_REMOTE_DEBUGGING_PORT || 9334);
const userDataDir = process.env.CHROME_USER_DATA_DIR || `/tmp/homelab-chat-markdown-uat-${port}`;
const seededTranscript = JSON.stringify([
  {
    id: 'long-code-regression',
    role: 'assistant',
    content: `long-code-regression\n\n\`\`\`ts\nconst token = "${'x'.repeat(320)}";\n\`\`\``,
    source: 'program',
    time: 'Now'
  }
]);

const sleep = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));

const assert = (condition, message, detail) => {
  if (!condition) {
    const suffix = detail === undefined ? '' : `\n${JSON.stringify(detail, null, 2)}`;
    throw new Error(`${message}${suffix}`);
  }
};

const createPage = async (url, chrome) => {
  const endpoint = `http://127.0.0.1:${port}`;
  let response;
  for (let attempt = 0; attempt < 20; attempt += 1) {
    try {
      response = await fetch(`${endpoint}/json/new?${encodeURIComponent(url)}`, { method: 'PUT' });
      break;
    } catch {
      await sleep(250);
    }
  }
  const exitCode = await Promise.race([chrome.exited, sleep(0).then(() => undefined)]);
  const stderr =
    exitCode === undefined ? '' : (await new Response(chrome.stderr).text()).slice(-1200);
  assert(response, `Chromium remote debugging endpoint did not start on port ${port}`, {
    chromePID: chrome.pid,
    exitCode,
    stderr
  });
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
      '--disable-crash-reporter',
      '--disable-crashpad',
      '--disable-breakpad',
      `--remote-debugging-port=${port}`,
      `--user-data-dir=${userDataDir}`,
      'about:blank'
    ],
    {
      stdout: 'pipe',
      stderr: 'pipe'
    }
  );

const viewportResult = (width, height, mobile) => `
  (async () => {
    const message = [...document.querySelectorAll('.message')]
      .find((element) => element.textContent.includes('long-code-regression'));
    const pre = message?.querySelector('pre');
    const code = pre?.querySelector('code');
    if (pre) {
      pre.scrollLeft = pre.scrollWidth;
    }
    await new Promise((resolve) => requestAnimationFrame(resolve));
    const messageRect = message?.getBoundingClientRect();
    const preRect = pre?.getBoundingClientRect();
    return {
      width: ${width},
      height: ${height},
      mobile: ${mobile},
      viewport: window.innerWidth,
      bodyWidth: document.body.scrollWidth,
      messageFound: Boolean(message),
      preFound: Boolean(pre),
      codeTextLength: code?.textContent.length ?? 0,
      messageWidth: messageRect?.width ?? 0,
      preWidth: preRect?.width ?? 0,
      preScrollWidth: pre?.scrollWidth ?? 0,
      preClientWidth: pre?.clientWidth ?? 0,
      preScrollLeft: pre?.scrollLeft ?? 0,
      preInsideMessage:
        Boolean(messageRect && preRect) &&
        preRect.left >= messageRect.left - 1 &&
        preRect.right <= messageRect.right + 1
    };
  })()
`;

const assertCodeBlockFits = (result) => {
  assert(result.messageFound, 'seeded chat message did not render', result);
  assert(result.preFound, 'fenced code block did not render', result);
  assert(result.codeTextLength > 200, 'seeded code line was unexpectedly short', result);
  assert(result.preScrollWidth > result.preClientWidth, 'long code block did not overflow its own scroll area', result);
  assert(result.preScrollLeft > 0, 'long code block could not be scrolled horizontally', result);
  assert(result.preInsideMessage, 'code block escaped the message bubble', result);
  assert(result.bodyWidth <= result.viewport + 2, 'page gained horizontal overflow', result);
};

const run = async () => {
  const chrome = launchChrome();
  await sleep(1800);

  try {
    const page = await createPage('about:blank', chrome);
    const cdp = connect(page.webSocketDebuggerUrl);
    await cdp.opened;
    await cdp.call('Runtime.enable');
    await cdp.call('Page.enable');
    await cdp.call('Page.addScriptToEvaluateOnNewDocument', {
      source: `
        localStorage.setItem('homelabd.dashboard.chatTranscript.v4', ${JSON.stringify(seededTranscript)});
        localStorage.removeItem('homelabd.dashboard.chatDraft.v1');
      `
    });

    await cdp.call('Emulation.setDeviceMetricsOverride', {
      width: 960,
      height: 720,
      deviceScaleFactor: 1,
      mobile: false
    });
    await cdp.call('Page.navigate', { url: dashboardURL });
    await sleep(1500);
    const desktop = await evalJS(cdp, viewportResult(960, 720, false));
    assertCodeBlockFits(desktop);

    await cdp.call('Emulation.setDeviceMetricsOverride', {
      width: 390,
      height: 844,
      deviceScaleFactor: 2,
      mobile: true
    });
    await sleep(200);
    const mobile = await evalJS(cdp, viewportResult(390, 844, true));
    assertCodeBlockFits(mobile);

    console.log(JSON.stringify({ ok: true, dashboardURL, desktop, mobile }, null, 2));
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
