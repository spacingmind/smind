// AssistantMessage: unboxed, full-width markdown (the ZCode look: no bubble
// around the assistant's text).

import { memo } from 'react';
import { Markdown } from './Markdown';

export const AssistantMessage = memo(function AssistantMessage({ text }: { text: string }) {
  return <Markdown text={text} />;
});
