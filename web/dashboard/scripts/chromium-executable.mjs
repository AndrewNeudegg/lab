import { accessSync, constants } from 'node:fs';
import { delimiter, join } from 'node:path';

const chromiumCommands = ['chromium', 'chromium-browser', 'google-chrome', 'google-chrome-stable'];

const isExecutable = (path) => {
  try {
    accessSync(path, constants.X_OK);
    return true;
  } catch {
    return false;
  }
};

export const findChromiumOnPath = ({
  pathValue = process.env.PATH || '',
  commands = chromiumCommands,
  canExecute = isExecutable
} = {}) => {
  for (const dir of pathValue.split(delimiter)) {
    if (!dir) {
      continue;
    }
    for (const command of commands) {
      const candidate = join(dir, command);
      if (canExecute(candidate)) {
        return candidate;
      }
    }
  }
  return undefined;
};

export const chromiumExecutablePath = (env = process.env, options = {}) => {
  if (env.PLAYWRIGHT_CHROMIUM_EXECUTABLE) {
    return env.PLAYWRIGHT_CHROMIUM_EXECUTABLE;
  }
  if (env.HOMELAB_PLAYWRIGHT_USE_SYSTEM_CHROME === '0') {
    return undefined;
  }
  if (env.CHROME_BIN) {
    return env.CHROME_BIN;
  }
  return findChromiumOnPath({ pathValue: env.PATH || '', ...options });
};
