// formState.ts is the pure submit-state rule for a plugin form node (the
// FormNode renderer's twin of model.ts): given the fields, the current values
// and whether the form has an action / is submitting, it picks the button's
// disabled flag, label and one-line reason. No react-native import so vitest's
// plain node transform can load it.

export type FormStateField = { name: string; type: 'text' | 'secret' | 'select' | 'toggle'; required: boolean };

export function formSubmitState(input: {
  fields: FormStateField[];
  values: Record<string, string | boolean>;
  hasAction: boolean;
  submitting: boolean;
  label: string;
}): { disabled: boolean; label: string; reason: string | null } {
  if (input.submitting) return { disabled: true, label: 'Submitting…', reason: null };
  // A required field blocks submit until it has a value; a toggle never blocks.
  const missing = input.fields.some((field) => field.required && field.type !== 'toggle' && !input.values[field.name]);
  if (missing) return { disabled: true, label: input.label, reason: 'Fill the required fields' };
  if (!input.hasAction) return { disabled: true, label: input.label, reason: 'This form has nothing to submit' };
  return { disabled: false, label: input.label, reason: null };
}
