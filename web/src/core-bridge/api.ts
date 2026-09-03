/**
 * Contract between the web app and the Go core running inside the WASM worker.
 *
 * Design (docs/05-web-app.md §6, docs/02-architecture.md):
 * - The core cannot call asynchronous browser APIs. The main thread therefore
 *   pushes file contents INTO the worker (`vault.load`, `vault.apply`) and the
 *   core keeps them in its in-memory FS. Every mutating call returns the list
 *   of files the core wrote or removed (`WriteSet`); the main thread persists
 *   them through the File System Access API and acknowledges nothing back —
 *   the in-memory copy is already up to date.
 * - All item shapes mirror the JSON tags of `internal/core/model.go`.
 * - Every method name below is a `CoreRequest.method`; params/results are the
 *   types listed in `CoreApi`.
 */

export type ItemType = 'epic' | 'story' | 'task' | 'milestone' | 'comment';
export type Priority = 'critical' | 'high' | 'medium' | 'low';
export type LinkKind = 'blocks' | 'blocked_by' | 'relates_to' | 'duplicates';
export type Severity = 'error' | 'warning' | 'info';

export type Link = { kind: LinkKind; target: string; note?: string };

export type Item = {
  id: string;
  type: ItemType;
  title: string;
  status?: string;
  priority?: Priority;
  parent?: string;
  epic?: string;
  milestone?: string;
  sprint?: string;
  assignees?: string[];
  author?: string;
  owner?: string;
  labels?: string[];
  estimate?: number;
  effort?: number;
  spent?: number;
  created?: string;
  updated?: string;
  started?: string;
  closed?: string;
  start?: string;
  due?: string;
  links?: Link[];
  attachments?: string[];
  custom?: Record<string, unknown>;
  deleted?: boolean;
  extra?: Record<string, unknown>;
  /** Markdown body after the front matter. Omitted by list calls unless `fields` asks for it. */
  body: string;
  /** Vault-relative path, forward slashes. */
  path: string;
  /** Content hash used for optimistic concurrency (`sha256:` + 16 hex). */
  rev: string;
};

export type Comment = {
  item: string;
  author: string;
  created?: string;
  updated?: string;
  inReplyTo?: string;
  kind?: string;
  body: string;
  path: string;
  rev: string;
};

export type Diagnostic = {
  code: string;
  severity: Severity;
  message: string;
  path?: string;
  field?: string;
};

export type VaultFile = { path: string; text: string };

export type FileEvent = {
  op: 'create' | 'write' | 'remove' | 'rename';
  path: string;
  /** Present for `create`/`write`; the new content. */
  text?: string;
  /** Present for `rename`; the previous path. */
  from?: string;
};

/** Files the core changed during a mutating call; the host must persist them. */
export type WriteSet = {
  written: VaultFile[];
  removed: string[];
};

export type ProjectSummary = {
  key: string;
  name: string;
  /** Vault-relative path of the folder that contains `.pmngr/`. */
  docsPath: string;
  statuses: { id: string; name: string; category: string; terminal?: boolean; wip?: number }[];
  labels: { name: string; color?: string; description?: string }[];
  priorities: Priority[];
  itemCounts: Record<ItemType, number>;
};

export type ItemFilter = {
  project?: string;
  type?: ItemType | ItemType[];
  status?: string | string[];
  category?: string | string[];
  assignee?: string;
  label?: string | string[];
  parent?: string;
  milestone?: string;
  updatedSince?: string;
  text?: string;
  includeDeleted?: boolean;
  sort?: 'updated' | 'created' | 'priority' | 'id' | 'title';
  order?: 'asc' | 'desc';
  limit?: number;
  cursor?: string;
  /** Front-matter fields to include; omit for all front matter without body. */
  fields?: string[];
};

export type ItemPage = { items: Item[]; nextCursor?: string; total: number };

export type ItemDraft = {
  project: string;
  type: Exclude<ItemType, 'comment'>;
  title: string;
  status?: string;
  priority?: Priority;
  parent?: string;
  milestone?: string;
  assignees?: string[];
  author?: string;
  labels?: string[];
  estimate?: number;
  due?: string;
  links?: Link[];
  custom?: Record<string, unknown>;
  body?: string;
};

export type ItemPatch = {
  set?: Partial<Omit<Item, 'id' | 'type' | 'path' | 'rev' | 'body'>>;
  unset?: string[];
  body?: string;
};

export type KbNode = {
  path: string;
  name: string;
  kind: 'dir' | 'page' | 'asset';
  title?: string;
  children?: KbNode[];
};

export type KbPage = {
  path: string;
  title: string;
  frontMatter: Record<string, unknown>;
  body: string;
  rev: string;
  outgoing: string[];
  backlinks: string[];
};

export type SearchHit = {
  kind: 'item' | 'page';
  id?: string;
  path: string;
  title: string;
  snippet: string;
  score: number;
};

export type IndexStats = {
  projects: number;
  items: number;
  pages: number;
  comments: number;
  durationMs: number;
  fingerprint: string;
  diagnostics: Diagnostic[];
};

export type SnapshotBlob = { fingerprint: string; json: string };

/** Method map: request method name → { params, result }. */
export type CoreApi = {
  ping: { params: undefined; result: { pong: true; wasm: boolean } };
  version: { params: undefined; result: { protocol: number; core: string | null } };

  /** Replace the in-memory vault with these files (full load). */
  'vault.load': { params: { files: VaultFile[]; rootLabel?: string }; result: IndexStats };
  /** Apply incremental file events (from a rescan diff or a watcher). */
  'vault.apply': { params: { events: FileEvent[] }; result: IndexStats };
  'vault.stats': { params: undefined; result: IndexStats };
  /** Serialised index for the IndexedDB cache; `snapshot.load` hydrates without files. */
  'snapshot.export': { params: undefined; result: SnapshotBlob };
  'snapshot.load': { params: SnapshotBlob; result: IndexStats };

  'project.list': { params: undefined; result: ProjectSummary[] };

  'item.list': { params: ItemFilter; result: ItemPage };
  'item.get': { params: { id: string }; result: Item };
  'item.children': { params: { id: string }; result: Item[] };
  'item.create': { params: ItemDraft; result: { item: Item; writes: WriteSet } };
  'item.update': {
    params: { id: string; patch: ItemPatch; rev: string };
    result: { item: Item; writes: WriteSet };
  };
  'item.move': {
    params: { id: string; status: string; rev: string };
    result: { item: Item; writes: WriteSet };
  };
  'item.delete': { params: { id: string; rev: string; hard?: boolean }; result: { writes: WriteSet } };
  'item.validate': { params: { id?: string; text?: string; path?: string }; result: Diagnostic[] };
  'item.parse': { params: { path: string; text: string }; result: Item };
  'item.serialize': { params: { item: Item }; result: { text: string } };

  'comment.list': { params: { id: string }; result: Comment[] };
  'comment.add': {
    params: { id: string; author: string; body: string; inReplyTo?: string };
    result: { comment: Comment; writes: WriteSet };
  };

  'kb.tree': { params: { project?: string }; result: KbNode[] };
  'kb.page': { params: { path: string }; result: KbPage };
  'kb.write': {
    params: { path: string; text: string; rev?: string };
    result: { page: KbPage; writes: WriteSet };
  };

  search: { params: { q: string; limit?: number; project?: string }; result: SearchHit[] };
};

export type CoreMethodName = keyof CoreApi;
export type CoreParams<M extends CoreMethodName> = CoreApi[M]['params'];
export type CoreResult<M extends CoreMethodName> = CoreApi[M]['result'];
