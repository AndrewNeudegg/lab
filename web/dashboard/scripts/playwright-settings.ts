export const DEFAULT_WEB_SERVER_TIMEOUT_MS = 120_000;
export const DEFAULT_TEST_TIMEOUT_MS = 120_000;
export const DEFAULT_EXPECT_TIMEOUT_MS = 20_000;

export function worktreePort(cwd: string) {
  let hash = 0;
  for (const char of cwd) {
    hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  }
  return 31_000 + (hash % 8_000);
}

export function playwrightWebServerTimeout(env: Record<string, string | undefined>) {
  return positiveEnvMilliseconds(
    env.PLAYWRIGHT_WEB_SERVER_TIMEOUT ?? env.PLAYWRIGHT_WEB_SERVER_TIMEOUT_MS,
    DEFAULT_WEB_SERVER_TIMEOUT_MS
  );
}

export function playwrightTestTimeout(env: Record<string, string | undefined>) {
  return positiveEnvMilliseconds(
    env.PLAYWRIGHT_TEST_TIMEOUT ?? env.PLAYWRIGHT_TEST_TIMEOUT_MS,
    DEFAULT_TEST_TIMEOUT_MS
  );
}

export function playwrightExpectTimeout(env: Record<string, string | undefined>) {
  return positiveEnvMilliseconds(
    env.PLAYWRIGHT_EXPECT_TIMEOUT ?? env.PLAYWRIGHT_EXPECT_TIMEOUT_MS,
    DEFAULT_EXPECT_TIMEOUT_MS
  );
}

function positiveEnvMilliseconds(value: string | undefined, fallback: number) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}
