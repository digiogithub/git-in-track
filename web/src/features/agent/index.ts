/**
 * The agent feature's public surface (epic GIT-EP-0018).
 *
 * Everything outside `web/src/features/agent/` imports from here: the router,
 * the shell, and the later waves that add the permission dialogs, the shared-
 * state panel and the frontend tools. Reaching into a module directly is how
 * the store ends up with three owners.
 *
 * The transport is not re-exported. Every agent call goes through
 * `DataProvider` (`web/src/api/provider.ts`), and a feature that could reach
 * the client would be a feature that could bypass the seam.
 */

export { AgentPage, type AgentPageProps } from '@/features/agent/ui/AgentPage';
export { AgentUnavailable, EmptyState } from '@/features/agent/ui/EmptyState';
export {
  DefaultInterrupt,
  type AgentInterruptRenderProps,
  type AgentInterruptRenderer,
} from '@/features/agent/ui/InterruptSlot';
export { Composer, type ComposerProps } from '@/features/agent/ui/Composer';
export { MessageList, type MessageListProps } from '@/features/agent/ui/MessageList';
export { MessageBubble, type MessageBubbleProps } from '@/features/agent/ui/MessageBubble';
export { ReasoningBlock, type ReasoningBlockProps } from '@/features/agent/ui/ReasoningBlock';
export {
  ToolCallCard,
  RESULT_CLAMP,
  type ToolCallCardProps,
} from '@/features/agent/ui/ToolCallCard';
export { ThreadList, type ThreadListProps } from '@/features/agent/ui/ThreadList';
export { isVisible, mergeThreadRows, type ThreadRow } from '@/features/agent/ui/model';
export {
  THREAD_META_KEY,
  UNTITLED_THREAD,
  deriveThreadTitle,
  forgetThreadMeta,
  readThreadMeta,
  rememberThreadMeta,
  type AgentThreadMeta,
} from '@/features/agent/ui/threadMeta';

export { createAgentStore, useAgentStore, type AgentState } from '@/features/agent/store';
export type {
  AgentError,
  AgentErrorCode,
  AgentInterrupt,
  AgentMessage,
  AgentRunStatus,
  AgentToolCall,
  AgentToolCallStatus,
} from '@/features/agent/types';
