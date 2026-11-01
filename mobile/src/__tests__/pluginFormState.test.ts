// pluginFormState.test.ts drives the pure form submit-state helper (the FormNode's
// disabled/label/reason twin of the model): one rule per test so the button's
// behaviour is pinned before the component renders it.

import { describe, expect, it } from 'vitest';
import { formSubmitState } from '../plugins/view/formState';

const fields = [
  { name: 'title', type: 'text' as const, required: true },
  { name: 'token', type: 'secret' as const, required: false },
  { name: 'severity', type: 'select' as const, required: false },
  { name: 'notify', type: 'toggle' as const, required: true },
];

describe('formSubmitState', () => {
  it('empty required field disables with a reason', () => {
    expect(formSubmitState({ fields, values: {}, hasAction: true, submitting: false, label: 'File report' })).toEqual({
      disabled: true,
      label: 'File report',
      reason: 'Fill the required fields',
    });
    expect(formSubmitState({ fields, values: { title: '' }, hasAction: true, submitting: false, label: 'File report' }).disabled).toBe(true);
  });

  it('filled form is enabled', () => {
    expect(formSubmitState({ fields, values: { title: 'Suite is slow' }, hasAction: true, submitting: false, label: 'File report' })).toEqual({
      disabled: false,
      label: 'File report',
      reason: null,
    });
  });

  it('toggle never blocks', () => {
    // `notify` is required but a toggle: only the empty text field blocks.
    expect(
      formSubmitState({ fields: [{ name: 'notify', type: 'toggle', required: true }], values: {}, hasAction: true, submitting: false, label: 'Go' }).disabled,
    ).toBe(false);
    expect(
      formSubmitState({ fields, values: { title: 'x', notify: false }, hasAction: true, submitting: false, label: 'File report' }).disabled,
    ).toBe(false);
  });

  it('submitting disables and relabels', () => {
    expect(formSubmitState({ fields, values: { title: 'x' }, hasAction: true, submitting: true, label: 'File report' })).toEqual({
      disabled: true,
      label: 'Submitting…',
      reason: null,
    });
    // Submitting wins even when a required field is empty or there is no action.
    expect(formSubmitState({ fields, values: {}, hasAction: true, submitting: true, label: 'File report' }).label).toBe('Submitting…');
    expect(formSubmitState({ fields, values: {}, hasAction: false, submitting: true, label: 'File report' }).label).toBe('Submitting…');
  });

  it('no action disables with a reason', () => {
    expect(formSubmitState({ fields, values: { title: 'x' }, hasAction: false, submitting: false, label: 'File report' })).toEqual({
      disabled: true,
      label: 'File report',
      reason: 'This form has nothing to submit',
    });
  });
});
