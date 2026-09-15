/**
 * The frontend-tool contract (task GIT-T-0082).
 *
 * A frontend tool is a *UI action* the agent may ask for: open this item,
 * narrow that filter, show me these cards. It is never a data path — anything
 * that reads or writes backlog content goes through the gintrack MCP server,
 * where the rev protocol and the write gate apply. Two consequences are built
 * into the types below:
 *
 * - **Arguments are model output.** Every tool carries a zod schema next to
 *   its JSON schema, and the runner validates before it executes. A bad
 *   argument object is a *result* sent back to the agent, never a thrown
 *   error and never a half-performed navigation.
 * - **Nothing here reaches for a router.** Navigation arrives as
 *   {@link ToolContext.navigate}, handed in by the page. A registry that
 *   imported the app's router would be a registry that could not be tested
 *   and could not be reused.
 */

import type { z } from 'zod';

import type { Item } from '@/api/provider';

/** A JSON-schema object, as `AguiTool.parameters` carries it. */
export type JsonSchemaObject = {
  type: 'object';
  properties: Record<string, unknown>;
  required?: string[];
  additionalProperties: false;
};

/** Whatever an executor hands back to the agent; serialised as JSON. */
export type ToolResult = Record<string, unknown>;

/**
 * The subset of the router's `navigate` a tool needs. Declaring it structurally
 * keeps `@tanstack/react-router` out of this directory and lets a test pass a
 * plain spy.
 */
export type ToolNavigate = (options: {
  to: string;
  params?: Record<string, string>;
  search?: Record<string, string | undefined>;
}) => unknown;

/**
 * How a tool finds out whether what the model named actually exists. Both are
 * optional: without them a tool validates the *shape* of an id or a path and
 * navigates, which is the honest degradation when nothing can resolve.
 */
export type ToolLookup = {
  /** Resolves one item id, or `null` when the workspace has no such item. */
  item?: (id: string) => Promise<Item | null>;
  /** True when the KB has a page at this vault-relative path. */
  kbPage?: (path: string) => Promise<boolean>;
};

/** Everything an executor is allowed to touch. */
export type ToolContext = {
  navigate: ToolNavigate;
  /** Active project key; a tool that needs a route param fails without one. */
  project: string | null;
  lookup?: ToolLookup;
  /**
   * Brings a board card into view once the board has rendered. Injected by the
   * page (and by tests); the default walks the DOM.
   */
  focusCard?: (itemId: string) => Promise<boolean>;
};

/** What validation produced: a value to execute with, or a result to send back. */
export type ToolValidation = { ok: true; value: unknown } | { ok: false; result: ToolResult };

/** One registered tool, with its argument type erased so a registry can hold many. */
export type FrontendTool = {
  name: string;
  description: string;
  parameters: JsonSchemaObject;
  validate: (args: unknown) => ToolValidation;
  execute: (args: unknown, context: ToolContext) => Promise<ToolResult>;
};

/** Turns a zod failure into the structured result the agent can correct itself from. */
export function validationResult(error: z.ZodError): ToolResult {
  const issue = error.issues[0];
  const field = issue === undefined ? '' : issue.path.map(String).join('.');
  return {
    error: 'invalid_arguments',
    ...(field === '' ? {} : { field }),
    message: issue?.message ?? 'The arguments did not match the tool schema.',
  };
}

/**
 * Declares a tool, keeping the executor typed against its own schema while the
 * registry sees a uniform shape.
 */
export function defineTool<S extends z.ZodType>(spec: {
  name: string;
  description: string;
  parameters: JsonSchemaObject;
  schema: S;
  execute: (args: z.output<S>, context: ToolContext) => Promise<ToolResult>;
}): FrontendTool {
  return {
    name: spec.name,
    description: spec.description,
    parameters: spec.parameters,
    validate(args: unknown): ToolValidation {
      const parsed = spec.schema.safeParse(args ?? {});
      return parsed.success
        ? { ok: true, value: parsed.data }
        : { ok: false, result: validationResult(parsed.error) };
    },
    execute(args: unknown, context: ToolContext): Promise<ToolResult> {
      return spec.execute(args as z.output<S>, context);
    },
  };
}
