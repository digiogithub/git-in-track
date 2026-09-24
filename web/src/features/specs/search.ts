/**
 * Specs page filter state lives in the URL, like the backlog filter
 * (`features/backlog/search.ts`): `?status=todo,in_progress&coverage=failing`
 * is a shareable view. Lists are comma-separated and every field degrades to
 * "no filter" on a hand-edited value instead of breaking the route.
 */

import type { SearchSchemaInput } from '@tanstack/react-router';
import { z } from 'zod';

import type { CoverageRow } from '@/api/provider';

/** The computed coverage states of doc 03 §21.6, in the order the UI lists them. */
export const coverageStates = ['untested', 'passing', 'failing', 'suspect'] as const;

export type CoverageState = CoverageRow['status'];

function toList(value: unknown): string[] | undefined {
  if (value === undefined || value === null || value === '') return undefined;
  const raw: unknown[] = Array.isArray(value)
    ? value
    : typeof value === 'string'
      ? value.split(',')
      : [];
  const items = raw
    .filter((entry): entry is string => typeof entry === 'string')
    .map((entry) => entry.trim())
    .filter(Boolean);
  return items.length > 0 ? [...new Set(items)] : undefined;
}

export const specSearchSchema = z.object({
  /** Requirement workflow statuses to keep. */
  status: z.preprocess(toList, z.array(z.string()).optional()).catch(undefined),
  /** Coverage states to keep; ignored while coverage is unavailable. */
  coverage: z.preprocess(toList, z.array(z.enum(coverageStates)).optional()).catch(undefined),
});

export type SpecSearch = z.infer<typeof specSearchSchema>;

/** What `navigate({ search })` accepts: lists as comma-separated strings. */
export type SpecSearchInput = {
  status?: string | undefined;
  coverage?: string | undefined;
};

/** Parses raw search params. Never throws. */
export function parseSpecSearch(input: unknown): SpecSearch {
  const result = specSearchSchema.safeParse(input ?? {});
  if (!result.success) return {};
  const { status, coverage } = result.data;
  return {
    ...(status ? { status } : {}),
    ...(coverage ? { coverage } : {}),
  };
}

/** Route `validateSearch` for `/p/$project/specs`. */
export function validateSpecSearch(input: SpecSearchInput & SearchSchemaInput): SpecSearch {
  return parseSpecSearch(input);
}

/** Serialises a list back into the URL form; an empty list clears the param. */
export function listParam(values: readonly string[]): string | undefined {
  return values.length > 0 ? values.join(',') : undefined;
}
