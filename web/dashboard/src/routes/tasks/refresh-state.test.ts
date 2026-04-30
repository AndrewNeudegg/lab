import { describe, expect, test } from 'bun:test';
import { createCoalescedAsync } from './refresh-state';

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
