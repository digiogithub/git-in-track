import { describe, expect, it } from 'vitest';

import {
  CommitTemplateError,
  DEFAULT_COMMIT_TEMPLATE,
  SUBJECT_LIMIT,
  renderCommitMessage,
  validateCommitTemplate,
} from '@/git/message';

/**
 * These cases mirror `internal/gitops/message_test.go` one for one: the two
 * runtimes render commit messages independently (ADR-006) and the only thing
 * that keeps them in step is the format documented in docs/06 §3.3.
 */
describe('renderCommitMessage', () => {
  const fields = {
    itemId: 'ACME-US-0042',
    title: 'Login with SSO',
    type: 'story',
    status: 'in_progress',
    prevStatus: 'todo',
    projectKey: 'ACME',
    board: 'team-alpha',
    action: 'move',
    user: 'jose',
    date: '2026-09-04',
    tool: 'gintrack 0.4.1 (companion)',
  } as const;

  it.each([
    ['the shipped default', '', `pmngr: update ACME-US-0042 "Login with SSO"`],
    ['the four placeholders of the story', '{{action}} {{id}}: {{title}}', 'move ACME-US-0042: Login with SSO'],
    ['the type placeholder', '{{action}} {{type}} {{id}}', 'move story ACME-US-0042'],
    [
      'the conventional-commits variant',
      'docs({{.ProjectKey}}): update {{.ItemID}} — {{.Title}}',
      'docs(ACME): update ACME-US-0042 — Login with SSO',
    ],
    ['the field form', '{{.Action}} {{.ItemID}} to {{.Status}}', 'move ACME-US-0042 to in_progress'],
  ])('renders %s', (_name, template, want) => {
    expect(renderCommitMessage(template, fields).subject).toBe(want);
  });

  it('folds a multi-line title into one subject line', () => {
    const { subject } = renderCommitMessage('{{action}} {{id}}: {{title}}', {
      itemId: 'ACME-T-0002',
      title: 'one\ntwo',
      action: 'create',
    });
    expect(subject).toBe('create ACME-T-0002: one two');
    expect(subject).not.toContain('\n');
  });

  it('uses the built-in subject for a batch of several items', () => {
    expect(renderCommitMessage('', { action: 'update', count: 12 }).subject).toBe(
      'pmngr: update 12 items',
    );
  });

  it('truncates a long subject and keeps the full title in the body', () => {
    const long = 'very long title '.repeat(12);
    const message = renderCommitMessage(DEFAULT_COMMIT_TEMPLATE, {
      itemId: 'ACME-US-0042',
      title: long,
    });
    expect(message.subject).toHaveLength(SUBJECT_LIMIT);
    expect(message.body).toContain(long);
  });

  it('writes the machine-readable trailers of docs/06 §3.3', () => {
    const { body } = renderCommitMessage('', fields);
    expect(body).toContain('Item: ACME-US-0042');
    expect(body).toContain('Type: story');
    expect(body).toContain('Status: todo -> in_progress');
    expect(body).toContain('Board: team-alpha');
    expect(body).toContain('Tool: gintrack 0.4.1 (companion)');
  });

  it('reports an unchanged status once, without an arrow', () => {
    const { body } = renderCommitMessage('', {
      itemId: 'ACME-US-0042',
      status: 'todo',
      prevStatus: 'todo',
    });
    expect(body).toContain('Status: todo');
    expect(body).not.toContain('->');
  });
});

describe('validateCommitTemplate', () => {
  it.each([
    ['an empty template falls back to the default', ''],
    ['the short form', '{{action}} {{id}}: {{title}}'],
    ['the field form', 'pmngr: update {{.ItemID}} "{{.Title}}"'],
  ])('accepts %s', (_name, template) => {
    expect(() => {
      validateCommitTemplate(template);
    }).not.toThrow();
  });

  it.each([
    ['an unbalanced brace', '{{action'],
    ['an unknown placeholder', '{{nosuchplaceholder}}'],
  ])('refuses %s', (_name, template) => {
    expect(() => {
      validateCommitTemplate(template);
    }).toThrow(CommitTemplateError);
  });

  it('names the placeholders a user may use', () => {
    expect(() => {
      validateCommitTemplate('{{nope}}');
    }).toThrow(/action/);
  });
});
