import type { DataProvider, ItemType } from '@/api/provider';
import type { EditorLintSource } from '@/components/editor/lint';

/**
 * The item types whose body the core lints live (GIT-US-0132): a spec block
 * by block, a story or a task through its `## Spec Delta`.
 */
export function lintsSpecText(type: ItemType): boolean {
  return type === 'spec' || type === 'story' || type === 'task';
}

/**
 * The editor's lint source for one item: every pause in typing sends the body
 * to the core (`spec.lint`, the WASM module or the companion) and underlines
 * what it finds at the severity the project's `specs.lint` gives each rule. A
 * rule at `off` never runs, so it never reaches the editor. No grammar rule is
 * evaluated here: the rules live in `internal/core` alone.
 *
 * A lint that fails — a companion that predates the method, a worker still
 * loading — underlines nothing rather than breaking the editor; the save-time
 * validation still reports the same findings.
 */
export function specLintSource(
  provider: Pick<DataProvider, 'lintSpecText'>,
  project: string,
  item: { id?: string; type: ItemType },
): EditorLintSource {
  return async (body) => {
    let findings;
    try {
      findings = await provider.lintSpecText(project, {
        ...(item.id ? { id: item.id } : {}),
        type: item.type,
        body,
      });
    } catch {
      return [];
    }
    return findings.map((f) => ({
      line: f.line,
      severity: f.severity,
      message: f.message,
      source: f.code,
    }));
  };
}

/**
 * A cheap test that spares the core a call for the bodies, the vast majority,
 * with no `## Spec Delta` heading at all. The core still decides what the
 * section holds.
 */
export function mayHaveSpecDelta(body: string): boolean {
  return /^##[ \t]+spec delta[ \t]*$/im.test(body);
}
