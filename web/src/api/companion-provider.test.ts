import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  CompanionProvider,
  CompanionUnauthorizedError,
  companionCapabilities,
  POLL_INTERVAL_MS,
  RECONNECT_BASE_MS,
  type WebSocketLike,
} from '@/api/companion-provider';
import { ProviderError, type ChangeEvent } from '@/api/provider';
import { clearToken, resetTokenCache, setToken } from '@/api/token';

const BASE = 'http://127.0.0.1:7317';

// ------------------------------------------------------------------ fixtures

function response(
  body: unknown,
  init: { status?: number; statusText?: string; headers?: Record<string, string> } = {},
): Response {
  const { status = 200, statusText = 'OK', headers = {} } = init;
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText,
    headers: { get: (name: string) => headers[name] ?? null },
    json: () => Promise.resolve(body),
  } as unknown as Response;
}

/** A body that is not JSON at all, like an HTML error page from a proxy. */
function unparseable(status: number, statusText: string): Response {
  return {
    ok: false,
    status,
    statusText,
    headers: { get: () => null },
    json: () => Promise.reject(new SyntaxError('Unexpected token <')),
  } as unknown as Response;
}

const restItem = {
  id: 'ACME-US-0042',
  type: 'story',
  project: 'ACME',
  title: 'Login with SSO',
  status: 'in_progress',
  priority: 'high',
  assignees: ['jose'],
  labels: ['auth'],
  parent: 'ACME-EP-0007',
  updated: '2026-09-03T07:41:11Z',
  links: [{ relation: 'blocked_by', target: 'ACME-T-0300' }],
  commentCount: 4,
  path: 'docs/.pmngr/stories/ACME-US-0042-login-with-sso.md',
  rev: 'sha256:6f1ca09',
};

function provider(
  fetchImpl: ReturnType<typeof vi.fn>,
  options: { webSocketFactory?: ((url: string) => WebSocketLike) | null } = {},
): CompanionProvider {
  return new CompanionProvider({
    baseUrl: BASE,
    fetchImpl: fetchImpl as unknown as typeof fetch,
    capabilities: companionCapabilities,
    webSocketFactory: options.webSocketFactory ?? null,
  });
}

function lastCall(fetchImpl: ReturnType<typeof vi.fn>): { url: string; init: RequestInit } {
  const call = fetchImpl.mock.calls.at(-1);
  return { url: String(call?.[0]), init: (call?.[1] ?? {}) as RequestInit };
}

function headerOf(init: RequestInit, name: string): string | undefined {
  return (init.headers as Record<string, string> | undefined)?.[name];
}

function bodyOf(init: RequestInit): unknown {
  return typeof init.body === 'string' ? JSON.parse(init.body) : null;
}

beforeEach(() => {
  resetTokenCache();
  globalThis.sessionStorage?.clear();
});

// --------------------------------------------------------------------- reads

describe('CompanionProvider reads', () => {
  it('lists items against GET /api/v1/items and maps the documented shape', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(
        response(
          { items: [restItem], total: 37, nextCursor: 'eyJvIjoyfQ' },
          { headers: { 'X-Total-Count': '37' } },
        ),
      );

    const page = await provider(fetchImpl).listItems({
      project: 'ACME',
      type: 'story',
      status: ['todo', 'in_progress'],
      sort: 'updated',
      order: 'desc',
      limit: 2,
    });

    const { url, init } = lastCall(fetchImpl);
    expect(url).toBe(
      `${BASE}/api/v1/items?project=ACME&type=story&status=todo&status=in_progress&sort=-updated&limit=2`,
    );
    expect(init.method).toBe('GET');
    expect(page.total).toBe(37);
    expect(page.nextCursor).toBe('eyJvIjoyfQ');
    expect(page.items[0]).toMatchObject({
      id: 'ACME-US-0042',
      type: 'story',
      title: 'Login with SSO',
      rev: 'sha256:6f1ca09',
      // REST calls the relation `relation`; the core model calls it `kind`.
      links: [{ kind: 'blocked_by', target: 'ACME-T-0300' }],
    });
    expect(page.items[0]).not.toHaveProperty('commentCount');
  });

  it('falls back to X-Total-Count when the body carries no total', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(response({ items: [restItem] }, { headers: { 'X-Total-Count': '12' } }));

    await expect(provider(fetchImpl).listItems({})).resolves.toMatchObject({ total: 12 });
  });

  it('reads one item, its children and its comments', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(response(restItem))
      .mockResolvedValueOnce(response({ items: [restItem], total: 1 }))
      .mockResolvedValueOnce(
        response({
          comments: [
            {
              item: 'ACME-US-0042',
              author: 'marta',
              body: 'Blocked on the sandbox.',
              created: '2026-09-03T10:40:12Z',
              path: 'docs/.pmngr/comments/ACME-US-0042/x.md',
              rev: 'sha256:c41a9f0',
            },
          ],
        }),
      );

    const client = provider(fetchImpl);
    await expect(client.getItem('ACME-US-0042')).resolves.toMatchObject({ id: 'ACME-US-0042' });
    expect(lastCall(fetchImpl).url).toBe(`${BASE}/api/v1/items/ACME-US-0042`);

    await expect(client.getChildren('ACME-EP-0007')).resolves.toHaveLength(1);
    expect(lastCall(fetchImpl).url).toBe(`${BASE}/api/v1/items?parent=ACME-EP-0007&limit=500`);

    const comments = await client.listComments('ACME-US-0042');
    expect(lastCall(fetchImpl).url).toBe(`${BASE}/api/v1/items/ACME-US-0042/comments`);
    expect(comments[0]).toMatchObject({ author: 'marta', item: 'ACME-US-0042' });
  });

  it('reads repositories, projects, the KB tree, a page and search results', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(
        response([
          {
            key: 'ACME',
            role: 'project',
            name: 'ACME API',
            path: '/home/jose/code/acme-api',
            docs: 'docs',
            git: { branch: 'main', ahead: 0, behind: 2 },
            lastIndexed: '2026-09-03T09:14:02Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        response({
          projects: [
            {
              key: 'ACME',
              name: 'ACME Platform',
              docs: 'docs',
              workflow: ['todo', 'in_progress', 'done'],
              counts: { epics: 12, stories: 58, tasks: 138, milestones: 6, comments: 402 },
            },
          ],
        }),
      )
      .mockResolvedValueOnce(
        response([
          { path: 'architecture', name: 'architecture', kind: 'dir', children: [] },
          {
            path: 'architecture/overview.md',
            name: 'overview.md',
            kind: 'page',
            title: 'Overview',
          },
        ]),
      )
      .mockResolvedValueOnce(
        response({
          path: 'architecture/overview.md',
          title: 'Architecture overview',
          frontmatter: { tags: ['architecture'] },
          raw: '# Architecture overview\n',
          links: { wiki: [{ target: 'Auth Overview', resolved: 'auth/overview.md' }] },
          backlinks: ['adr/0003-oidc.md'],
          rev: 'sha256:2a90f31',
        }),
      )
      .mockResolvedValueOnce(
        response({
          results: [
            { kind: 'item', id: 'ACME-T-0311', title: 'Wire OIDC', score: 8.42, path: 'a.md' },
            { kind: 'kb', path: 'architecture/auth.md', title: 'Authentication', score: 5.1 },
          ],
        }),
      );

    const client = provider(fetchImpl);

    const repos = await client.listRepos();
    expect(lastCall(fetchImpl).url).toBe(`${BASE}/api/v1/repos`);
    expect(repos[0]).toMatchObject({
      id: 'ACME',
      kind: 'project',
      location: '/home/jose/code/acme-api',
      branch: 'main',
      behind: 2,
      state: 'ready',
      projects: ['ACME'],
    });

    const projects = await client.listProjects();
    expect(projects[0]).toMatchObject({ key: 'ACME', name: 'ACME Platform', docsPath: 'docs' });
    expect(projects[0]?.statuses).toEqual([
      { id: 'todo', name: 'Todo', category: 'todo' },
      { id: 'in_progress', name: 'In Progress', category: 'in_progress' },
      { id: 'done', name: 'Done', category: 'done' },
    ]);
    expect(projects[0]?.itemCounts).toMatchObject({ story: 58, task: 138 });

    const tree = await client.listKbTree({ kind: 'project', projectKey: 'ACME' });
    expect(lastCall(fetchImpl).url).toBe(`${BASE}/api/v1/projects/ACME/kb/tree`);
    expect(tree).toHaveLength(2);

    const page = await client.getPage({ kind: 'project', projectKey: 'ACME' }, 'a/overview.md');
    expect(lastCall(fetchImpl).url).toBe(
      `${BASE}/api/v1/projects/ACME/kb/page?path=a%2Foverview.md&format=raw`,
    );
    expect(page).toMatchObject({
      title: 'Architecture overview',
      body: '# Architecture overview\n',
      outgoing: ['auth/overview.md'],
      backlinks: ['adr/0003-oidc.md'],
      frontMatter: { tags: ['architecture'] },
    });

    const hits = await client.search({ text: 'oidc', projectKey: 'ACME', limit: 20 });
    expect(lastCall(fetchImpl).url).toBe(
      `${BASE}/api/v1/search?q=oidc&scope=items%2Ckb&project=ACME&limit=20`,
    );
    expect(hits.map((hit) => hit.kind)).toEqual(['item', 'page']);
  });

  it('reads capabilities from GET /api/v1/capabilities', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      response({
        version: '0.4.0',
        schema: 'v1',
        features: { watcher: true, git: true, mcpHttp: false, search: 'bleve', write: true },
        limits: { maxBatchWrite: 200 },
      }),
    );

    const client = new CompanionProvider({
      baseUrl: BASE,
      fetchImpl: fetchImpl as unknown as typeof fetch,
      webSocketFactory: null,
    });
    await client.ready;

    expect(lastCall(fetchImpl).url).toBe(`${BASE}/api/v1/capabilities`);
    expect(client.capabilities).toEqual({
      write: true,
      git: true,
      ssh: true,
      watch: true,
      fullTextSearch: 'bleve',
      mcp: false,
      openInEditor: true,
      maxBatchWrite: 200,
    });
    expect(client.version).toBe('0.4.0');
  });

  it('sends the bearer token on every request', async () => {
    setToken('s7Q1e9Zk');
    const fetchImpl = vi.fn().mockResolvedValue(response({ items: [], total: 0 }));

    await provider(fetchImpl).listItems({});

    expect(headerOf(lastCall(fetchImpl).init, 'Authorization')).toBe('Bearer s7Q1e9Zk');
  });
});

// -------------------------------------------------------------------- writes

describe('CompanionProvider writes', () => {
  it('creates an item and hydrates the partial answer with one read', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(response({ id: 'ACME-T-0311', path: 'x.md', rev: 'sha256:11c35de' }))
      .mockResolvedValueOnce(response({ ...restItem, id: 'ACME-T-0311', type: 'task' }));

    const item = await provider(fetchImpl).createItem({
      project: 'ACME',
      type: 'task',
      title: 'Wire OIDC discovery endpoint',
    });

    const [create, read] = fetchImpl.mock.calls;
    expect(String(create?.[0])).toBe(`${BASE}/api/v1/items`);
    expect((create?.[1] as RequestInit).method).toBe('POST');
    expect(String(read?.[0])).toBe(`${BASE}/api/v1/items/ACME-T-0311`);
    expect(item.id).toBe('ACME-T-0311');
  });

  it('sends If-Match on update, move and delete', async () => {
    const full = { ...restItem, status: 'in_review', rev: 'sha256:7ab0d12' };
    const fetchImpl = vi.fn().mockResolvedValue(response(full));
    const client = provider(fetchImpl);

    await client.updateItem(
      'ACME-US-0042',
      { set: { status: 'in_review' }, unset: ['due'], body: '## Description\n' },
      'sha256:6f1ca09',
    );
    let call = lastCall(fetchImpl);
    expect(call.url).toBe(`${BASE}/api/v1/items/ACME-US-0042`);
    expect(call.init.method).toBe('PATCH');
    expect(headerOf(call.init, 'If-Match')).toBe('sha256:6f1ca09');
    expect(bodyOf(call.init)).toEqual({
      status: 'in_review',
      unset: ['due'],
      body: '## Description\n',
    });

    await client.moveItem('ACME-US-0042', 'in_review', 'sha256:6f1ca09');
    call = lastCall(fetchImpl);
    expect(call.url).toBe(`${BASE}/api/v1/items/ACME-US-0042/move`);
    expect(call.init.method).toBe('POST');
    expect(headerOf(call.init, 'If-Match')).toBe('sha256:6f1ca09');
    expect(bodyOf(call.init)).toEqual({ status: 'in_review' });

    await client.deleteItem('ACME-US-0042', 'sha256:6f1ca09');
    call = lastCall(fetchImpl);
    expect(call.init.method).toBe('DELETE');
    expect(headerOf(call.init, 'If-Match')).toBe('sha256:6f1ca09');
  });

  it('adds a comment and completes the answer from the request', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(
        response({ id: 'ACME-T-0311#x', path: 'c.md', created: '2026-09-03T10:40:12Z', rev: 'r1' }),
      );

    const comment = await provider(fetchImpl).addComment('ACME-T-0311', 'Blocked.', 'marta');

    expect(lastCall(fetchImpl).url).toBe(`${BASE}/api/v1/items/ACME-T-0311/comments`);
    expect(comment).toMatchObject({
      item: 'ACME-T-0311',
      author: 'marta',
      body: 'Blocked.',
      rev: 'r1',
    });
  });

  it('reports per-item failures from a batch instead of aborting it', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(response(restItem))
      .mockResolvedValueOnce(
        response(
          { code: 'stale_revision', detail: 'Item ACME-T-2 changed on disk.', status: 409 },
          { status: 409, statusText: 'Conflict' },
        ),
      );

    const result = await provider(fetchImpl).updateMany([
      { id: 'ACME-T-1', patch: { set: { status: 'done' } }, rev: 'r1' },
      { id: 'ACME-T-2', patch: { set: { status: 'done' } }, rev: 'r2' },
    ]);

    expect(result.applied).toBe(1);
    expect(result.failed).toEqual([
      { id: 'ACME-T-2', code: 'stale_revision', message: 'Item ACME-T-2 changed on disk.' },
    ]);
  });

  it('writes a knowledge base page with If-Match', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      response({
        path: 'a/overview.md',
        title: 'Overview',
        raw: '# Overview\n',
        rev: 'sha256:new',
      }),
    );

    const page = await provider(fetchImpl).writePage(
      { kind: 'project', projectKey: 'ACME' },
      'a/overview.md',
      '# Overview\n',
      'sha256:old',
    );

    const call = lastCall(fetchImpl);
    expect(call.url).toBe(`${BASE}/api/v1/projects/ACME/kb/page`);
    expect(call.init.method).toBe('PUT');
    expect(headerOf(call.init, 'If-Match')).toBe('sha256:old');
    expect(page.rev).toBe('sha256:new');
  });
});

// -------------------------------------------------------------------- errors

describe('CompanionProvider error mapping', () => {
  const cases: {
    name: string;
    status: number;
    statusText?: string;
    body: unknown;
    code: string;
    message?: string;
  }[] = [
    {
      name: 'a stale revision (409)',
      status: 409,
      body: { code: 'stale_revision', detail: 'Item was modified on disk.', currentRev: 'r2' },
      code: 'stale_revision',
      message: 'Item was modified on disk.',
    },
    {
      name: 'a failed precondition (412)',
      status: 412,
      statusText: 'Precondition Failed',
      body: null,
      code: 'stale_revision',
    },
    {
      name: 'a missing item (404)',
      status: 404,
      body: { code: 'not_found', detail: 'No item ACME-T-9999.' },
      code: 'not_found',
    },
    {
      name: 'a read-only companion (403)',
      status: 403,
      body: { code: 'read_only', detail: 'The companion runs read-only.' },
      code: 'read_only',
    },
    {
      name: 'a forbidden write (403)',
      status: 403,
      body: { code: 'forbidden', detail: 'Not allowed.' },
      code: 'permission_denied',
    },
    {
      name: 'a git conflict',
      status: 409,
      body: { code: 'git_conflict', detail: 'Rebase produced conflicts.' },
      code: 'git_conflict',
    },
    {
      name: 'a git authentication failure',
      status: 401,
      body: { code: 'git_auth_failed', detail: 'SSH key rejected.' },
      code: 'git_auth_failed',
    },
    {
      name: 'a repository that is not cloned',
      status: 409,
      body: { code: 'repo_not_cloned', detail: 'Clone it first.' },
      code: 'repo_not_cloned',
    },
    {
      name: 'an unavailable index',
      status: 503,
      body: { code: 'index_unavailable', detail: 'Indexing.' },
      code: 'internal',
    },
  ];

  for (const testCase of cases) {
    it(`maps ${testCase.name}`, async () => {
      const fetchImpl = vi.fn().mockResolvedValue(
        response(testCase.body, {
          status: testCase.status,
          statusText: testCase.statusText ?? 'Error',
        }),
      );

      const error = await provider(fetchImpl)
        .updateItem('ACME-T-1', { set: {} }, 'r1')
        .catch((reason: unknown) => reason);

      expect(error).toBeInstanceOf(ProviderError);
      expect((error as ProviderError).code).toBe(testCase.code);
      if (testCase.message !== undefined) {
        expect((error as ProviderError).message).toBe(testCase.message);
      }
    });
  }

  it('surfaces per-field details of a validation problem', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      response(
        {
          type: 'https://git-in-track.dev/problems/validation-failed',
          title: 'Validation failed',
          status: 422,
          detail: '2 fields are invalid.',
          code: 'validation_failed',
          errors: [
            { field: 'status', code: 'unknown_status', message: '"in-progress" is not allowed' },
            { field: 'parent', code: 'wrong_parent_type', message: 'Task parent must be a story' },
          ],
        },
        { status: 422, statusText: 'Unprocessable Entity' },
      ),
    );

    const error = (await provider(fetchImpl)
      .createItem({ project: 'ACME', type: 'task', title: 'x' })
      .catch((reason: unknown) => reason)) as ProviderError;

    expect(error.code).toBe('validation_failed');
    expect(error.message).toContain('2 fields are invalid.');
    expect(error.message).toContain('status: "in-progress" is not allowed');
    expect(error.message).toContain('parent: Task parent must be a story');
  });

  it('clears the stored token and raises a typed error on 401', async () => {
    setToken('stale-token');
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(
        response(
          { code: 'unauthorized', detail: 'Bearer token missing or invalid.', status: 401 },
          { status: 401, statusText: 'Unauthorized' },
        ),
      );

    const error = await provider(fetchImpl)
      .getItem('ACME-T-1')
      .catch((reason: unknown) => reason);

    expect(error).toBeInstanceOf(CompanionUnauthorizedError);
    expect((error as CompanionUnauthorizedError).code).toBe('permission_denied');
    expect((error as CompanionUnauthorizedError).unauthorized).toBe(true);
    expect(globalThis.sessionStorage.getItem('gintrack:companion-token')).toBeNull();
  });

  it('falls back to the status text when the body is not a problem document', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(unparseable(500, 'Internal Server Error'));

    const error = (await provider(fetchImpl)
      .getItem('ACME-T-1')
      .catch((reason: unknown) => reason)) as ProviderError;

    expect(error.code).toBe('internal');
    expect(error.message).toBe('500 Internal Server Error');
  });

  it('turns a network failure into an internal error naming the companion', async () => {
    const fetchImpl = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));

    const error = (await provider(fetchImpl)
      .getItem('ACME-T-1')
      .catch((reason: unknown) => reason)) as ProviderError;

    expect(error.code).toBe('internal');
    expect(error.message).toContain(BASE);
  });
});

// -------------------------------------------------------------------- events

/** Minimal `WebSocket` stand-in driven by the test. */
class FakeSocket implements WebSocketLike {
  static instances: FakeSocket[] = [];

  readonly sent: string[] = [];
  closed = false;
  onopen: ((event: unknown) => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: ((event: unknown) => void) | null = null;
  onerror: ((event: unknown) => void) | null = null;

  constructor(readonly url: string) {
    FakeSocket.instances.push(this);
  }

  send(data: string): void {
    this.sent.push(data);
  }

  close(): void {
    this.closed = true;
  }

  open(): void {
    this.onopen?.({});
  }

  emit(frame: unknown): void {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }

  drop(): void {
    this.onclose?.({});
  }

  frames(): unknown[] {
    return this.sent.map((entry) => JSON.parse(entry) as unknown);
  }
}

function eventProvider(options: { random?: () => number } = {}): CompanionProvider {
  FakeSocket.instances = [];
  return new CompanionProvider({
    baseUrl: BASE,
    fetchImpl: vi.fn() as unknown as typeof fetch,
    capabilities: companionCapabilities,
    webSocketFactory: (url) => new FakeSocket(url),
    random: options.random ?? (() => 0),
  });
}

describe('CompanionProvider event stream', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('opens the socket on the first subscriber and sends a subscribe frame', () => {
    const client = eventProvider();
    const stop = client.subscribe(() => undefined);

    const socket = FakeSocket.instances[0];
    expect(socket?.url).toBe(`${BASE.replace('http', 'ws')}/api/v1/events`);
    expect(client.connectionState).toBe('connecting');

    socket?.open();
    expect(client.connectionState).toBe('open');
    expect(socket?.frames()[0]).toMatchObject({ op: 'subscribe' });

    stop();
    expect(socket?.closed).toBe(true);
    expect(client.connectionState).toBe('idle');
  });

  it('appends the token to the socket URL, since headers are not an option', () => {
    setToken('s7Q1e9Zk');
    const client = eventProvider();
    client.subscribe(() => undefined);

    expect(FakeSocket.instances[0]?.url).toBe(
      `${BASE.replace('http', 'ws')}/api/v1/events?token=s7Q1e9Zk`,
    );
    client.dispose();
    clearToken();
  });

  it('translates server frames into change events', () => {
    const client = eventProvider();
    const events: ChangeEvent[] = [];
    client.subscribe((event) => events.push(event));
    const socket = FakeSocket.instances[0];
    socket?.open();

    socket?.emit({
      type: 'item.changed',
      seq: 4821,
      data: { repo: 'ACME', id: 'ACME-T-0311', op: 'updated' },
    });
    socket?.emit({
      type: 'index.updated',
      seq: 4822,
      data: {
        repo: 'ACME',
        durationMs: 18,
        counts: { epics: 12, stories: 58, tasks: 139, milestones: 6, comments: 4 },
      },
    });
    socket?.emit({
      type: 'file.changed',
      seq: 4823,
      data: { repo: 'ACME', path: 'docs/architecture.md', op: 'write', isKb: true },
    });
    socket?.emit({
      type: 'file.changed',
      seq: 4824,
      data: { repo: 'ACME', path: 'docs/.pmngr/tasks/x.md', op: 'write', isPmngr: true },
    });
    socket?.emit({ type: 'sync.progress', seq: 4825, data: { repo: 'ACME', phase: 'push' } });
    socket?.emit({ type: 'conflict.detected', seq: 4826, data: { repo: 'TEAM' } });

    expect(events).toEqual([
      { kind: 'items', repoId: 'ACME', ids: ['ACME-T-0311'] },
      {
        kind: 'index',
        repoId: 'ACME',
        stats: {
          projects: 1,
          items: 215,
          pages: 0,
          comments: 4,
          durationMs: 18,
          fingerprint: '',
          diagnostics: [],
        },
      },
      { kind: 'kb', repoId: 'ACME', paths: ['docs/architecture.md'] },
      { kind: 'repo', repoId: 'ACME' },
      { kind: 'repo', repoId: 'TEAM' },
    ]);

    client.dispose();
  });

  it('asks for a full refresh when the server reports a resume gap', () => {
    const client = eventProvider();
    const events: ChangeEvent[] = [];
    client.subscribe((event) => events.push(event));
    FakeSocket.instances[0]?.open();

    FakeSocket.instances[0]?.emit({ type: 'resume.gap', seq: 5000 });

    expect(events).toEqual([
      { kind: 'repo', repoId: '' },
      { kind: 'kb', repoId: '', paths: [] },
    ]);
    client.dispose();
  });

  it('reconnects with exponential backoff and resumes from the last seq', () => {
    vi.useFakeTimers();
    const client = eventProvider({ random: () => 0 });
    client.subscribe(() => undefined);

    const first = FakeSocket.instances[0];
    first?.open();
    first?.emit({ type: 'item.changed', seq: 4821, data: { repo: 'ACME', id: 'ACME-T-1' } });
    first?.drop();

    // Attempt 1: ceiling 500 ms, jitter floor 250 ms with `random() === 0`.
    expect(client.connectionState).toBe('reconnecting');
    vi.advanceTimersByTime(RECONNECT_BASE_MS / 2 - 1);
    expect(FakeSocket.instances).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(FakeSocket.instances).toHaveLength(2);

    const second = FakeSocket.instances[1];
    second?.open();
    expect(second?.frames()).toEqual([
      { op: 'subscribe', topics: expect.any(Array) as unknown },
      { op: 'resume', seq: 4821 },
    ]);

    // A successful open resets the schedule: the next gap waits 250 ms again.
    second?.drop();
    vi.advanceTimersByTime(RECONNECT_BASE_MS / 2);
    expect(FakeSocket.instances).toHaveLength(3);

    client.dispose();
  });

  it('degrades to interval polling when the socket cannot be opened', () => {
    vi.useFakeTimers();
    const client = eventProvider();
    const events: ChangeEvent[] = [];
    client.subscribe((event) => events.push(event));

    // Three failed opens in a row, with the backoff in between.
    FakeSocket.instances[0]?.drop();
    vi.advanceTimersByTime(RECONNECT_BASE_MS / 2);
    FakeSocket.instances[1]?.drop();
    vi.advanceTimersByTime(RECONNECT_BASE_MS);
    FakeSocket.instances[2]?.drop();

    expect(client.connectionState).toBe('polling');
    expect(events).toHaveLength(0);

    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    expect(events).toEqual([
      { kind: 'repo', repoId: '' },
      { kind: 'kb', repoId: '', paths: [] },
    ]);

    // Polling keeps trying to upgrade back to the socket.
    const revived = FakeSocket.instances.at(-1);
    revived?.open();
    expect(client.connectionState).toBe('open');

    client.dispose();
  });

  it('polls straight away when the runtime has no WebSocket at all', () => {
    vi.useFakeTimers();
    const client = new CompanionProvider({
      baseUrl: BASE,
      fetchImpl: vi.fn() as unknown as typeof fetch,
      capabilities: companionCapabilities,
      webSocketFactory: null,
    });
    const events: ChangeEvent[] = [];
    client.subscribe((event) => events.push(event));

    expect(client.connectionState).toBe('polling');
    vi.advanceTimersByTime(POLL_INTERVAL_MS);
    expect(events).toHaveLength(2);

    client.dispose();
    expect(client.connectionState).toBe('closed');
  });

  it('notifies connection state listeners', () => {
    const client = eventProvider();
    const states: string[] = [];
    client.onConnectionStateChange((state) => states.push(state));
    client.subscribe(() => undefined);
    FakeSocket.instances[0]?.open();
    client.dispose();

    expect(states).toEqual(['idle', 'connecting', 'open', 'closed']);
  });
});

// -------------------------------------------------------------- git settings

describe('CompanionProvider git surface (story GIT-US-0020)', () => {
  const settingsBody = {
    commitOnSave: true,
    commitDebounceMs: 2000,
    messageTemplate: 'pmngr: update {{.ItemID}} "{{.Title}}"',
    backend: 'auto',
    resolvedBackend: 'system',
    gitVersion: '2.45.2',
    signCommits: false,
    pending: 3,
    persisted: true,
  };

  it('reads the settings from GET /git/settings', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(response(settingsBody));
    const settings = await provider(fetchImpl).getGitSettings();

    expect(lastCall(fetchImpl).url).toBe(`${BASE}/api/v1/git/settings`);
    expect(settings).toMatchObject({
      commitOnSave: true,
      commitDebounceMs: 2000,
      resolvedBackend: 'system',
      gitVersion: '2.45.2',
      pending: 3,
      supported: true,
    });
  });

  it('patches the settings and sends only what changed', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(response({ ...settingsBody, commitOnSave: false }));
    const settings = await provider(fetchImpl).updateGitSettings({ commitOnSave: false });

    const { url, init } = lastCall(fetchImpl);
    expect(url).toBe(`${BASE}/api/v1/git/settings`);
    expect(init.method).toBe('PATCH');
    expect(bodyOf(init)).toEqual({ commitOnSave: false });
    expect(settings.commitOnSave).toBe(false);
  });

  it('turns a refused template into a typed provider error', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(
        response(
          { code: 'invalid_request', detail: 'messageTemplate: the template does not parse' },
          { status: 400 },
        ),
      );

    await expect(
      provider(fetchImpl).updateGitSettings({ messageTemplate: '{{nope}}' }),
    ).rejects.toBeInstanceOf(ProviderError);
  });

  it('reads the per-repository status', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      response({
        repos: [{ repo: 'acme', path: '/code/acme', git: true, backend: 'system' }],
        settings: settingsBody,
      }),
    );
    const repos = await provider(fetchImpl).getGitStatus('acme');

    expect(lastCall(fetchImpl).url).toBe(`${BASE}/api/v1/git/status?repo=acme`);
    expect(repos).toHaveLength(1);
    expect(repos[0]).toMatchObject({ repo: 'acme', git: true, backend: 'system' });
  });

  it('flushes the batched edits with an empty commit request', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(response({ commits: [{ repo: 'acme', sha: 'abc123', empty: false }] }));
    const commits = await provider(fetchImpl).commitNow();

    const { url, init } = lastCall(fetchImpl);
    expect(url).toBe(`${BASE}/api/v1/git/commit`);
    expect(init.method).toBe('POST');
    expect(bodyOf(init)).toEqual({});
    expect(commits[0]).toMatchObject({ repo: 'acme', sha: 'abc123' });
  });
});

describe('CompanionProvider — sync (GIT-US-0021)', () => {
  const syncSettingsBody = {
    pullStrategy: 'rebase',
    pushOnSync: true,
    maxPushRetries: 3,
    supported: true,
  };

  it('reads the per-repository sync status', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      response({
        repos: [
          {
            repo: 'acme',
            path: '/code/acme',
            git: true,
            pending: 0,
            status: { branch: 'main', ahead: 1, behind: 2, state: 'diverged' },
          },
        ],
        settings: syncSettingsBody,
      }),
    );

    const repos = await provider(fetchImpl).getSyncStatus('acme');

    expect(lastCall(fetchImpl).url).toBe(`${BASE}/api/v1/sync/status?repo=acme`);
    expect(repos[0]).toMatchObject({ repo: 'acme', git: true });
    expect(repos[0]?.status).toMatchObject({ ahead: 1, behind: 2, state: 'diverged' });
  });

  it('runs a dry run against one repository', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      response({
        operationId: 'sync-1',
        dryRun: true,
        results: [{ repo: 'acme', phase: 'done', pulled: 0, pushed: 0 }],
      }),
    );

    const results = await provider(fetchImpl).sync('acme', { dryRun: true });

    const { url, init } = lastCall(fetchImpl);
    expect(url).toBe(`${BASE}/api/v1/sync/run`);
    expect(init.method).toBe('POST');
    expect(bodyOf(init)).toEqual({ repos: ['acme'], dryRun: true });
    expect(results[0]).toMatchObject({ repo: 'acme', phase: 'done' });
  });

  it('aborts a half-finished integration', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(response({ repo: 'acme', git: true, pending: 0 }));

    const status = await provider(fetchImpl).abortSync('acme');

    const { url, init } = lastCall(fetchImpl);
    expect(url).toBe(`${BASE}/api/v1/sync/abort`);
    expect(bodyOf(init)).toEqual({ repo: 'acme' });
    expect(status).toMatchObject({ repo: 'acme' });
  });

  it('changes the strategy through the sync settings', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(response({ ...syncSettingsBody, pullStrategy: 'merge' }));

    const settings = await provider(fetchImpl).updateSyncSettings({ pullStrategy: 'merge' });

    const { url, init } = lastCall(fetchImpl);
    expect(url).toBe(`${BASE}/api/v1/sync/settings`);
    expect(init.method).toBe('PATCH');
    expect(settings.pullStrategy).toBe('merge');
  });
});
