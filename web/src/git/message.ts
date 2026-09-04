/**
 * Commit message rendering for browser-only mode.
 *
 * The companion renders messages with Go's `text/template` inside
 * `internal/gitops`. The browser cannot run that code — git in the browser is
 * isomorphic-git, which shares nothing with the Go backend (ADR-006) — so the
 * one thing the two runtimes must agree on is the *format*, which is specified
 * in docs/06-git-sync.md §3.3 and implemented twice against the same cases.
 *
 * This module implements the documented subset: placeholder substitution and
 * the machine-readable trailers. It deliberately does not implement Go template
 * control flow (`{{if}}`, `{{range}}`, pipelines): a template that uses those is
 * a companion-only template, and `renderCommitMessage` says so rather than
 * rendering something different from what the companion would produce.
 */

/** What a write did, as `{{action}}` reports it. */
export type CommitAction = 'create' | 'update' | 'delete' | 'move' | 'comment';

/** The template context of docs/06 §3.3. */
export type CommitFields = {
  itemId?: string;
  title?: string;
  type?: string;
  status?: string;
  prevStatus?: string;
  projectKey?: string;
  board?: string;
  action?: CommitAction;
  /** Items covered by one commit; > 1 selects the bulk subject. */
  count?: number;
  user?: string;
  date?: string;
  /** `Tool:` trailer, for example `gintrack 0.4.1 (browser)`. */
  tool?: string;
  /** `Agent:` trailer, set when an agent made the change. */
  agent?: string;
};

/** A rendered message: the subject line and the trailer body. */
export type CommitMessage = { subject: string; body: string };

/** The shipped template (docs/06 §3.3). */
export const DEFAULT_COMMIT_TEMPLATE = 'pmngr: update {{.ItemID}} "{{.Title}}"';

/** Subject lines are truncated here; the full title survives in the body. */
export const SUBJECT_LIMIT = 72;

/** Both spellings of every placeholder, mapped onto one field. */
const PLACEHOLDERS: Record<string, keyof CommitFields> = {
  '.ItemID': 'itemId',
  id: 'itemId',
  '.Title': 'title',
  title: 'title',
  '.Type': 'type',
  type: 'type',
  '.Status': 'status',
  status: 'status',
  '.PrevStatus': 'prevStatus',
  prevStatus: 'prevStatus',
  '.ProjectKey': 'projectKey',
  project: 'projectKey',
  '.Board': 'board',
  board: 'board',
  '.Action': 'action',
  action: 'action',
  '.Count': 'count',
  count: 'count',
  '.User': 'user',
  user: 'user',
  '.Date': 'date',
  date: 'date',
};

/** A template that does not render the same way in both runtimes. */
export class CommitTemplateError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'CommitTemplateError';
  }
}

/**
 * Checks a template without rendering it, which is what the settings form does
 * before it saves. It throws `CommitTemplateError` with an actionable message.
 */
export function validateCommitTemplate(template: string): void {
  const source = template.trim() === '' ? DEFAULT_COMMIT_TEMPLATE : template;
  let depth = 0;
  for (let i = 0; i < source.length; i += 1) {
    if (source.startsWith('{{', i)) depth += 1;
    if (source.startsWith('}}', i)) depth -= 1;
    if (depth < 0) break;
  }
  if (depth !== 0) {
    throw new CommitTemplateError('the template has an unbalanced {{ … }}');
  }
  for (const match of source.matchAll(/\{\{\s*([^}]*?)\s*\}\}/g)) {
    const name = (match[1] ?? '').trim();
    if (!(name in PLACEHOLDERS)) {
      throw new CommitTemplateError(
        `unknown placeholder {{${name}}}: use ${Object.keys(PLACEHOLDERS)
          .filter((key) => !key.startsWith('.'))
          .join(', ')}`,
      );
    }
  }
}

/**
 * Renders a commit message. It mirrors `(*gitops.Template).Render`: the subject
 * is one line truncated to 72 characters, a truncated title is repeated in the
 * body, and the body carries the trailers in a fixed order.
 */
export function renderCommitMessage(template: string, fields: CommitFields): CommitMessage {
  const filled: CommitFields = {
    ...fields,
    action: fields.action ?? 'update',
    count: fields.count === undefined || fields.count < 1 ? 1 : fields.count,
    date: fields.date ?? new Date().toISOString().slice(0, 10),
  };

  let subject: string;
  if ((filled.count ?? 1) > 1 && (filled.itemId ?? '') === '') {
    // Several items in one commit: no id or title can name it (docs/06 §3.3).
    subject = `pmngr: ${filled.action ?? 'update'} ${String(filled.count)} items`;
  } else {
    validateCommitTemplate(template);
    const source = template.trim() === '' ? DEFAULT_COMMIT_TEMPLATE : template;
    subject = collapse(
      source.replace(/\{\{\s*([^}]*?)\s*\}\}/g, (_whole, name: string) => {
        const field = PLACEHOLDERS[name.trim()];
        const value = field === undefined ? undefined : filled[field];
        return value === undefined ? '' : String(value);
      }),
    );
  }
  if (subject === '') {
    throw new CommitTemplateError('the template rendered an empty subject');
  }

  const trailers = commitTrailers(filled);
  if (subject.length > SUBJECT_LIMIT) {
    trailers.unshift(`Title: ${filled.title ?? ''}`, '');
    subject = `${subject.slice(0, SUBJECT_LIMIT - 1).trimEnd()}…`;
  }
  return { subject, body: trailers.join('\n') };
}

/** The machine-readable trailers, in the fixed order of docs/06 §3.3. */
function commitTrailers(fields: CommitFields): string[] {
  const out: string[] = [];
  const add = (key: string, value: string | undefined) => {
    if (value !== undefined && value !== '') out.push(`${key}: ${value}`);
  };
  add('Item', fields.itemId);
  add('Type', fields.type);
  if (
    fields.prevStatus !== undefined &&
    fields.prevStatus !== '' &&
    fields.status !== undefined &&
    fields.status !== '' &&
    fields.prevStatus !== fields.status
  ) {
    add('Status', `${fields.prevStatus} -> ${fields.status}`);
  } else {
    add('Status', fields.status);
  }
  add('Project', fields.projectKey);
  add('Board', fields.board);
  if ((fields.count ?? 1) > 1) add('Items', String(fields.count));
  add('Tool', fields.tool);
  add('Agent', fields.agent);
  return out;
}

/** Folds a rendered subject into a single trimmed line. */
function collapse(value: string): string {
  return value.replace(/\s+/g, ' ').trim();
}
