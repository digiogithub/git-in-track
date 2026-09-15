/** The frontend tool registry's surface (story GIT-US-0064). */

export {
  assertNoHitlShadowing,
  createToolRunner,
  frontendTools,
  isFrontendTool,
  toolDeclarations,
  type FrontendToolRunner,
} from '@/features/agent/tools/registry';
export {
  applyBacklogFilterTool,
  backlogFilterArgs,
  MAX_SHOWN_ITEMS,
  parseShowItemsResult,
  showItemsTool,
  toItemSearchParams,
  type BacklogFilterArgs,
  type ItemCard,
  type ShowItemsResult,
} from '@/features/agent/tools/backlog';
export {
  BOARD_SLUG_PATTERN,
  focusBoardCardInDom,
  focusBoardCardTool,
  ITEM_ID_PATTERN,
  openItemTool,
  openKbPageTool,
  safeKbPath,
} from '@/features/agent/tools/navigation';
export {
  defineTool,
  validationResult,
  type FrontendTool,
  type JsonSchemaObject,
  type ToolContext,
  type ToolLookup,
  type ToolNavigate,
  type ToolResult,
  type ToolValidation,
} from '@/features/agent/tools/types';
