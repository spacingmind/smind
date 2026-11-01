// permissionPolicyLine.test.ts: the policy footprint on a permission card as data -- matched rule vs no rule,
// and the command shown in full, byte for byte, whether it came from policy.normalized or detail.command.

import { describe, expect, it } from 'vitest';
import { permissionCommand, permissionPolicyLine } from '../transcript/permissionPolicyLine';

describe('permissionPolicyLine', () => {
  it('permission card rule line', () => {
    expect(permissionPolicyLine({ policy: { ruleId: 'git-status', why: 'git commands are pre-vetted' } })).toEqual({
      title: 'Matched rule git-status',
      ruleId: 'git-status',
      why: 'git commands are pre-vetted',
      command: null,
    });
    expect(permissionPolicyLine({ policy: { ruleId: '', why: 'nothing matched this shape' } })).toEqual({
      title: 'No rule matched',
      ruleId: null,
      why: null,
      command: null,
    });
    expect(permissionPolicyLine({ command: 'rm -rf /tmp/x', truncated: true })).toBeNull();
    expect(permissionPolicyLine({ policy: { ruleId: 'floor', action: 'some-new-action', why: 'floor' } })).toEqual({
      title: 'Matched rule floor',
      ruleId: 'floor',
      why: 'floor',
      command: null,
    });
  });

  it('permission card full normalised command', () => {
    const normalized = `bash -lc ${'x'.repeat(3000)}\ncd /repo && git status\n\ntrailing newline\n`;
    const line = permissionPolicyLine({ policy: { ruleId: 'git-status', normalized } });
    expect(line?.command).toBe(`bash -lc ${'x'.repeat(3000)}\ncd /repo && git status\n\ntrailing newline\n`);
    expect(line?.command?.length).toBe(normalized.length);

    expect(permissionCommand({ policy: { normalized } })).toBe(normalized);
    expect(permissionCommand({ command: 'git status' })).toBe('git status');
    expect(permissionCommand({})).toBeNull();
  });
});
