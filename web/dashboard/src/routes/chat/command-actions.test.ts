import { describe, expect, test } from 'bun:test';
import { extractCommands, isSafeCommand } from './command-actions';

describe('chat command actions', () => {
  test('keeps long task suggestions actionable', () => {
    const goal = [
      'Improve task suggestion handling',
      'preserve repository scan context, reviewed plan details, validation expectations, and operator constraints',
      'keep the final risk note because it explains why truncation can drop vital task input',
      'include the complete model-generated investigation trail and handoff notes'
    ].join(' '.repeat(12));
    const command = `new ${goal}`;

    expect(command.length).toBeGreaterThan(260);
    expect(isSafeCommand(command)).toBe(true);
    expect(extractCommands(`Action: \`${command}\``)).toEqual([command.replace(/\s+/g, ' ')]);
  });

  test('still rejects placeholder and unsafe commands', () => {
    expect(isSafeCommand('new <goal>')).toBe(false);
    expect(isSafeCommand('rm -rf /')).toBe(false);
  });
});
