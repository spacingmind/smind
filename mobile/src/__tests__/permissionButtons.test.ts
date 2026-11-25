import { describe, expect, it } from 'vitest';
import { permissionButtonVariants } from '../transcript/permissionButtons';

describe('permissionButtonVariants', () => {
  it('first allow is filled, other allows outlined, rejects text', () => {
    expect(permissionButtonVariants([{ kind: 'allow_once' }, { kind: 'allow_always' }, { kind: 'reject_once' }])).toEqual([
      'filled',
      'outlined',
      'text',
    ]);
  });
  it('a reject listed first does not take the primary slot', () => {
    expect(permissionButtonVariants([{ kind: 'reject_once' }, { kind: 'allow_once' }])).toEqual(['text', 'filled']);
  });
  it('unknown kinds are outlined; empty is empty', () => {
    expect(permissionButtonVariants([{ kind: 'weird' }, { kind: '' }])).toEqual(['outlined', 'outlined']);
    expect(permissionButtonVariants([])).toEqual([]);
  });
  it('a browser card has no filled primary: allows are outlined, rejects are text', () => {
    expect(
      permissionButtonVariants([{ kind: 'allow_once' }, { kind: 'allow_once' }, { kind: 'reject_once' }], 'browser'),
    ).toEqual(['outlined', 'outlined', 'text']);
  });
  it('another detail kind keeps the first allow filled', () => {
    expect(
      permissionButtonVariants([{ kind: 'allow_once' }, { kind: 'allow_once' }, { kind: 'reject_once' }], 'execute'),
    ).toEqual(['filled', 'outlined', 'text']);
  });
  it('no detail kind keeps the first allow filled', () => {
    expect(
      permissionButtonVariants([{ kind: 'allow_once' }, { kind: 'allow_once' }, { kind: 'reject_once' }]),
    ).toEqual(['filled', 'outlined', 'text']);
  });
});
