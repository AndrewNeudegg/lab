import { describe, expect, test } from 'bun:test';
import { createCoalescedAsync, createMutationRevision } from './refresh-state';

describe('task refresh coalescing', () => {
  test('shares a slow in-flight refresh instead of starting another one', async () => {
    const coalesced = createCoalescedAsync<number>();
    let calls = 0;
    let release: (value: number) => void = () => {};

    const first = coalesced(async () => {
      calls += 1;
      return new Promise<number>((resolve) => {
        release = resolve;
      });
    });
    const second = coalesced(async () => {
      calls += 1;
      return 2;
    });

    expect(second).toBe(first);
    expect(calls).toBe(1);

    release(1);
    expect(await second).toBe(1);

    const third = await coalesced(async () => {
      calls += 1;
      return 3;
    });

    expect(third).toBe(3);
    expect(calls).toBe(2);
  });
});

describe('mutation revision guard', () => {
  test('marks refresh snapshots stale after a mutation', () => {
    const revision = createMutationRevision();
    const snapshot = revision.current();

    expect(revision.matches(snapshot)).toBe(true);
    revision.bump();
    expect(revision.matches(snapshot)).toBe(false);
    expect(revision.matches(revision.current())).toBe(true);
  });
});
