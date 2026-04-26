export type DiffLineKind = 'context' | 'add' | 'delete' | 'meta';
export type DiffSplitRowKind = 'context' | 'add' | 'delete' | 'change' | 'hunk' | 'meta';

export interface ParsedDiffLine {
  kind: DiffLineKind;
  content: string;
  oldNumber?: number;
  newNumber?: number;
}

export interface ParsedDiffHunk {
  header: string;
  oldStart: number;
  oldLines: number;
  newStart: number;
  newLines: number;
  lines: ParsedDiffLine[];
}

export interface ParsedDiffFile {
  path: string;
  oldPath?: string;
  status: string;
  additions: number;
  deletions: number;
  binary: boolean;
  headerLines: string[];
  hunks: ParsedDiffHunk[];
}

export interface DiffSplitRow {
  kind: DiffSplitRowKind;
  label?: string;
  left?: ParsedDiffLine;
  right?: ParsedDiffLine;
}

export interface InlineSegment {
  text: string;
  changed: boolean;
}

const hunkPattern = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/;

const cleanDiffPath = (path = '') => {
  const cleaned = path.trim().replace(/^"|"$/g, '').replace(/^[ab]\//, '');
  return cleaned === '/dev/null' ? '' : cleaned;
};

const pathFromGitHeader = (line: string) => {
  const match = line.match(/^diff --git (?:a\/(.+?)|(.+?)) (?:b\/(.+)|(.+))$/);
  if (!match) {
    return { oldPath: '', path: '' };
  }
  return {
    oldPath: cleanDiffPath(match[1] || match[2] || ''),
    path: cleanDiffPath(match[3] || match[4] || '')
  };
};

const ensurePath = (file: ParsedDiffFile) => {
  if (!file.path) {
    file.path = file.oldPath || 'unknown';
  }
  if (!file.status) {
    file.status = 'modified';
  }
};

export const parseUnifiedDiff = (rawDiff = ''): ParsedDiffFile[] => {
  const lines = rawDiff.replace(/\r\n/g, '\n').split('\n');
  if (lines[lines.length - 1] === '') {
    lines.pop();
  }

  const files: ParsedDiffFile[] = [];
  let current: ParsedDiffFile | undefined;
  let currentHunk: ParsedDiffHunk | undefined;
  let oldLine = 0;
  let newLine = 0;

  const finishFile = () => {
    if (!current) {
      return;
    }
    ensurePath(current);
    files.push(current);
  };

  for (const line of lines) {
    if (line.startsWith('diff --git ')) {
      finishFile();
      const paths = pathFromGitHeader(line);
      current = {
        path: paths.path,
        oldPath: paths.oldPath || undefined,
        status: 'modified',
        additions: 0,
        deletions: 0,
        binary: false,
        headerLines: [line],
        hunks: []
      };
      currentHunk = undefined;
      continue;
    }

    if (!current) {
      continue;
    }

    const hunk = line.match(hunkPattern);
    if (hunk) {
      oldLine = Number(hunk[1]);
      newLine = Number(hunk[3]);
      currentHunk = {
        header: line,
        oldStart: oldLine,
        oldLines: Number(hunk[2] || '1'),
        newStart: newLine,
        newLines: Number(hunk[4] || '1'),
        lines: []
      };
      current.hunks.push(currentHunk);
      continue;
    }

    if (!currentHunk) {
      current.headerLines.push(line);
      if (line.startsWith('new file mode')) {
        current.status = 'added';
      } else if (line.startsWith('deleted file mode')) {
        current.status = 'deleted';
      } else if (line.startsWith('rename from ')) {
        current.status = 'renamed';
        current.oldPath = line.replace('rename from ', '').trim();
      } else if (line.startsWith('rename to ')) {
        current.status = 'renamed';
        current.path = line.replace('rename to ', '').trim();
      } else if (line.startsWith('Binary files ') || line.startsWith('GIT binary patch')) {
        current.binary = true;
      } else if (line.startsWith('--- ')) {
        const oldPath = cleanDiffPath(line.replace('--- ', ''));
        if (!oldPath) {
          current.status = 'added';
        } else {
          current.oldPath = oldPath;
        }
      } else if (line.startsWith('+++ ')) {
        const newPath = cleanDiffPath(line.replace('+++ ', ''));
        if (!newPath) {
          current.status = 'deleted';
        } else {
          current.path = newPath;
        }
      }
      continue;
    }

    if (line.startsWith('\\')) {
      currentHunk.lines.push({ kind: 'meta', content: line });
      continue;
    }

    const marker = line[0] || ' ';
    const content = line.length > 0 ? line.slice(1) : '';
    if (marker === '+') {
      current.additions += 1;
      currentHunk.lines.push({ kind: 'add', content, newNumber: newLine });
      newLine += 1;
    } else if (marker === '-') {
      current.deletions += 1;
      currentHunk.lines.push({ kind: 'delete', content, oldNumber: oldLine });
      oldLine += 1;
    } else {
      currentHunk.lines.push({
        kind: 'context',
        content,
        oldNumber: oldLine,
        newNumber: newLine
      });
      oldLine += 1;
      newLine += 1;
    }
  }

  finishFile();
  return files;
};

export const buildSplitRows = (file?: ParsedDiffFile): DiffSplitRow[] => {
  if (!file) {
    return [];
  }
  const rows: DiffSplitRow[] = [];
  for (const hunk of file.hunks) {
    rows.push({ kind: 'hunk', label: hunk.header });
    let deletions: ParsedDiffLine[] = [];
    let additions: ParsedDiffLine[] = [];

    const flushChanges = () => {
      const total = Math.max(deletions.length, additions.length);
      for (let i = 0; i < total; i += 1) {
        const left = deletions[i];
        const right = additions[i];
        rows.push({
          kind: left && right ? 'change' : left ? 'delete' : 'add',
          left,
          right
        });
      }
      deletions = [];
      additions = [];
    };

    for (const line of hunk.lines) {
      if (line.kind === 'delete') {
        deletions.push(line);
        continue;
      }
      if (line.kind === 'add') {
        additions.push(line);
        continue;
      }
      flushChanges();
      if (line.kind === 'meta') {
        rows.push({ kind: 'meta', label: line.content });
      } else {
        rows.push({ kind: 'context', left: line, right: line });
      }
    }
    flushChanges();
  }
  return rows;
};

const prefixLength = (left: string, right: string) => {
  const limit = Math.min(left.length, right.length);
  let index = 0;
  while (index < limit && left[index] === right[index]) {
    index += 1;
  }
  return index;
};

const suffixLength = (left: string, right: string, prefix: number) => {
  const limit = Math.min(left.length, right.length) - prefix;
  let index = 0;
  while (
    index < limit &&
    left[left.length - 1 - index] === right[right.length - 1 - index]
  ) {
    index += 1;
  }
  return index;
};

const segmentsFor = (value: string, prefix: number, suffix: number): InlineSegment[] => {
  const middleEnd = suffix === 0 ? value.length : value.length - suffix;
  return [
    { text: value.slice(0, prefix), changed: false },
    { text: value.slice(prefix, middleEnd), changed: true },
    { text: value.slice(middleEnd), changed: false }
  ].filter((segment) => segment.text.length > 0);
};

export const inlineChangeSegments = (
  left = '',
  right = ''
): { left: InlineSegment[]; right: InlineSegment[] } => {
  if (left === right) {
    return {
      left: [{ text: left, changed: false }],
      right: [{ text: right, changed: false }]
    };
  }
  const prefix = prefixLength(left, right);
  const suffix = suffixLength(left, right, prefix);
  return {
    left: segmentsFor(left, prefix, suffix),
    right: segmentsFor(right, prefix, suffix)
  };
};

