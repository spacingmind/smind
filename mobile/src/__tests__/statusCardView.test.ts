import { describe, expect, it } from 'vitest';
import { actionVariants, statusDotTone } from '../screens/statusCardView';

describe('actionVariants', () => {
  it('0 actions -> no variants', () => {
    expect(actionVariants(0)).toEqual([]);
  });

  it('1 action -> outlined', () => {
    expect(actionVariants(1)).toEqual(['outlined']);
  });

  it('2 actions -> filled forward action, then text', () => {
    expect(actionVariants(2)).toEqual(['filled', 'text']);
  });

  it('3 actions -> filled, then text, text', () => {
    expect(actionVariants(3)).toEqual(['filled', 'text', 'text']);
  });
});

describe('statusDotTone', () => {
  it('maps info to running', () => {
    expect(statusDotTone('info')).toBe('running');
  });

  it('keeps warning and danger', () => {
    expect(statusDotTone('warning')).toBe('warning');
    expect(statusDotTone('danger')).toBe('danger');
  });
});
