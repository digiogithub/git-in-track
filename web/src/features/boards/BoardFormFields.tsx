import { useId } from 'react';

import type { ItemType, Priority, StatusCategory } from '@/api/provider';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import {
  boardCategories,
  boardItemTypes,
  boardPriorities,
  columnId,
  defaultColumns,
  type BoardForm,
  type ColumnForm,
} from '@/features/boards/board-form';

/**
 * The fields a board is created and edited with (docs/05-web-app.md §9).
 *
 * The wording is deliberate. A board holds no items: what a user changes here
 * is the query behind the cards — which projects are in scope, which types,
 * labels, assignees and priorities pass the filters, and which statuses each
 * column claims. That is what "put my epics, stories and tasks on this board"
 * actually means, so the form says it rather than leaving it to be discovered.
 */
export function BoardFormFields({
  form,
  onChange,
  projects,
  lockKind = false,
}: {
  form: BoardForm;
  onChange: (next: BoardForm) => void;
  /** The project keys `team.yaml` declares. */
  projects: string[];
  /** A board's kind is part of its file and never changes after creation. */
  lockKind?: boolean;
}) {
  const uid = useId();
  const id = (name: string) => `${uid}-${name}`;
  const set = (patch: Partial<BoardForm>) => onChange({ ...form, ...patch });

  const toggle = <T extends string>(list: T[], value: T): T[] =>
    list.includes(value) ? list.filter((entry) => entry !== value) : [...list, value];

  const setColumn = (index: number, patch: Partial<ColumnForm>) =>
    set({ columns: form.columns.map((c, i) => (i === index ? { ...c, ...patch } : c)) });

  return (
    <div className="space-y-5 text-sm">
      <section className="space-y-2">
        <div className="text-xs">
          <label htmlFor={id('title')} className="mb-1 block text-muted-foreground">
            Board name
          </label>
          <Input
            id={id('title')}
            required
            value={form.title}
            onChange={(event) => set({ title: event.target.value })}
          />
        </div>
        <div className="text-xs">
          <label htmlFor={id('description')} className="mb-1 block text-muted-foreground">
            Description
          </label>
          <Textarea
            id={id('description')}
            rows={2}
            value={form.description}
            onChange={(event) => set({ description: event.target.value })}
          />
        </div>
        <div className="text-xs">
          <label htmlFor={id('kind')} className="mb-1 block text-muted-foreground">
            Kind
          </label>
          <Select
            id={id('kind')}
            className="h-8 w-64 text-xs"
            disabled={lockKind}
            value={form.kind}
            onChange={(event) => {
              const kind = event.target.value as BoardForm['kind'];
              const columns = defaultColumns(kind);
              set({
                kind,
                columns,
                backlogColumn: kind === 'scrum' ? (columns[0]?.id ?? '') : '',
              });
            }}
          >
            <option value="kanban">Kanban — a continuous flow</option>
            <option value="scrum">Scrum — one sprint at a time</option>
          </Select>
        </div>
        {lockKind ? (
          <p className="text-xs text-muted-foreground">
            A board&apos;s kind is fixed once it exists; create another board to change it.
          </p>
        ) : null}
      </section>

      <section aria-label="Project scope" className="space-y-2">
        <h3 className="text-sm font-medium">What this board shows</h3>
        <p className="text-xs text-muted-foreground">
          A board holds no items of its own: every card is an epic, story, task or milestone that
          matches the scope and the filters below, read live from its own project repository. Widen
          the scope or relax a filter to put more work on the board.
        </p>
        <fieldset className="space-y-1">
          <legend className="mb-1 text-xs text-muted-foreground">Projects in scope</legend>
          <div className="flex items-center gap-2 text-xs">
            <Checkbox
              id={id('all-projects')}
              checked={form.projects.length === 0}
              onChange={() => set({ projects: form.projects.length === 0 ? [...projects] : [] })}
            />
            <label htmlFor={id('all-projects')}>Every project the team declares</label>
          </div>
          {projects.map((key) => (
            <div key={key} className="flex items-center gap-2 text-xs">
              <Checkbox
                id={id(`project-${key}`)}
                checked={form.projects.length === 0 || form.projects.includes(key)}
                disabled={form.projects.length === 0}
                onChange={() => set({ projects: toggle(form.projects, key) })}
              />
              <label htmlFor={id(`project-${key}`)} className="font-mono">
                {key}
              </label>
            </div>
          ))}
          {projects.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              The team repository declares no project yet.
            </p>
          ) : null}
        </fieldset>
      </section>

      <section aria-label="Filters" className="space-y-2">
        <h3 className="text-sm font-medium">Filters</h3>
        <fieldset className="flex flex-wrap gap-3">
          <legend className="mb-1 text-xs text-muted-foreground">
            Item types (none checked shows every type)
          </legend>
          {boardItemTypes.map((type) => (
            <div key={type} className="flex items-center gap-1 text-xs">
              <Checkbox
                id={id(`type-${type}`)}
                checked={form.types.includes(type)}
                onChange={() => set({ types: toggle<ItemType>(form.types, type) })}
              />
              <label htmlFor={id(`type-${type}`)}>{type}</label>
            </div>
          ))}
        </fieldset>
        <fieldset className="flex flex-wrap gap-3">
          <legend className="mb-1 text-xs text-muted-foreground">
            Priorities (none checked shows every priority)
          </legend>
          {boardPriorities.map((priority) => (
            <div key={priority} className="flex items-center gap-1 text-xs">
              <Checkbox
                id={id(`priority-${priority}`)}
                checked={form.priorities.includes(priority)}
                onChange={() => set({ priorities: toggle<Priority>(form.priorities, priority) })}
              />
              <label htmlFor={id(`priority-${priority}`)}>{priority}</label>
            </div>
          ))}
        </fieldset>
        <div className="grid gap-2 sm:grid-cols-2">
          <div className="text-xs">
            <label htmlFor={id('labels-any')} className="mb-1 block text-muted-foreground">
              With any of these labels
            </label>
            <Input
              id={id('labels-any')}
              placeholder="frontend, security"
              value={form.labelsAny}
              onChange={(event) => set({ labelsAny: event.target.value })}
            />
          </div>
          <div className="text-xs">
            <label htmlFor={id('labels-none')} className="mb-1 block text-muted-foreground">
              Without these labels
            </label>
            <Input
              id={id('labels-none')}
              placeholder="tech-debt"
              value={form.labelsNone}
              onChange={(event) => set({ labelsNone: event.target.value })}
            />
          </div>
          <div className="text-xs">
            <label htmlFor={id('assignees')} className="mb-1 block text-muted-foreground">
              Assignees
            </label>
            <Input
              id={id('assignees')}
              placeholder="jose, marta"
              value={form.assignees}
              onChange={(event) => set({ assignees: event.target.value })}
            />
          </div>
          <div className="text-xs">
            <label htmlFor={id('query')} className="mb-1 block text-muted-foreground">
              Text in the title
            </label>
            <Input
              id={id('query')}
              value={form.query}
              onChange={(event) => set({ query: event.target.value })}
            />
          </div>
        </div>
        <div className="flex items-center gap-2 text-xs">
          <Checkbox
            id={id('include-closed')}
            checked={form.includeClosed}
            onChange={() => set({ includeClosed: !form.includeClosed })}
          />
          <label htmlFor={id('include-closed')}>
            Show finished items outside the done column
          </label>
        </div>
      </section>

      <section aria-label="Columns" className="space-y-2">
        <h3 className="text-sm font-medium">Columns</h3>
        <p className="text-xs text-muted-foreground">
          A column claims the statuses it maps. Status categories work for every project, whatever
          each workflow calls its statuses; explicit status ids are for a column that has to be
          precise about one workflow.
        </p>
        <ul className="space-y-3">
          {form.columns.map((column, index) => (
            <li key={column.id} className="space-y-2 rounded border border-input p-2">
              <div className="flex flex-wrap items-end gap-2">
                <div className="text-xs">
                  <label
                    htmlFor={id(`column-${column.id}-name`)}
                    className="mb-1 block text-muted-foreground"
                  >
                    Name
                  </label>
                  <Input
                    id={id(`column-${column.id}-name`)}
                    className="w-44"
                    value={column.name}
                    onChange={(event) => setColumn(index, { name: event.target.value })}
                  />
                </div>
                <div className="text-xs">
                  <label
                    htmlFor={id(`column-${column.id}-wip`)}
                    className="mb-1 block text-muted-foreground"
                  >
                    WIP limit
                  </label>
                  <Input
                    id={id(`column-${column.id}-wip`)}
                    className="w-24"
                    inputMode="numeric"
                    placeholder="none"
                    value={column.wip}
                    onChange={(event) => setColumn(index, { wip: event.target.value })}
                  />
                </div>
                <div className="text-xs">
                  <label
                    htmlFor={id(`column-${column.id}-mapping`)}
                    className="mb-1 block text-muted-foreground"
                  >
                    Maps
                  </label>
                  <Select
                    id={id(`column-${column.id}-mapping`)}
                    className="h-8 w-40 text-xs"
                    value={column.mapping}
                    onChange={(event) =>
                      setColumn(index, { mapping: event.target.value as ColumnForm['mapping'] })
                    }
                  >
                    <option value="categories">Status categories</option>
                    <option value="statuses">Explicit statuses</option>
                  </Select>
                </div>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={form.columns.length <= 1}
                  onClick={() => set({ columns: form.columns.filter((_, i) => i !== index) })}
                >
                  Remove {column.name}
                </Button>
              </div>
              {column.mapping === 'categories' ? (
                <fieldset className="flex flex-wrap gap-3">
                  <legend className="sr-only">Categories of {column.name}</legend>
                  {boardCategories.map((category) => (
                    <div key={category} className="flex items-center gap-1 text-xs">
                      <Checkbox
                        id={id(`column-${column.id}-${category}`)}
                        checked={column.categories.includes(category)}
                        onChange={() =>
                          setColumn(index, {
                            categories: toggle<StatusCategory>(column.categories, category),
                          })
                        }
                      />
                      <label htmlFor={id(`column-${column.id}-${category}`)}>{category}</label>
                    </div>
                  ))}
                </fieldset>
              ) : (
                <div className="text-xs">
                  <label
                    htmlFor={id(`column-${column.id}-statuses`)}
                    className="mb-1 block text-muted-foreground"
                  >
                    Statuses of {column.name}, for every project
                  </label>
                  <Input
                    id={id(`column-${column.id}-statuses`)}
                    placeholder="backlog, todo"
                    value={column.statuses}
                    onChange={(event) => setColumn(index, { statuses: event.target.value })}
                  />
                </div>
              )}
            </li>
          ))}
        </ul>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={() => {
            const name = `Column ${form.columns.length + 1}`;
            set({
              columns: [
                ...form.columns,
                {
                  id: uniqueColumnId(form.columns, columnId(name)),
                  name,
                  mapping: 'categories',
                  categories: [],
                  statuses: '',
                  wip: '',
                },
              ],
            });
          }}
        >
          Add a column
        </Button>
        {form.kind === 'scrum' ? (
          <div className="text-xs">
            <label htmlFor={id('backlog-column')} className="mb-1 block text-muted-foreground">
              Backlog column — where sprint candidates appear
            </label>
            <Select
              id={id('backlog-column')}
              className="h-8 w-56 text-xs"
              value={form.backlogColumn}
              onChange={(event) => set({ backlogColumn: event.target.value })}
            >
              {form.columns.map((column) => (
                <option key={column.id} value={column.id}>
                  {column.name}
                </option>
              ))}
            </Select>
          </div>
        ) : null}
      </section>
    </div>
  );
}

/** Keeps a new column's id unique inside the board (E-BOARD-COLUMNS). */
function uniqueColumnId(columns: ColumnForm[], candidate: string): string {
  const taken = new Set(columns.map((column) => column.id));
  if (!taken.has(candidate)) return candidate;
  for (let n = 2; ; n += 1) {
    const next = `${candidate}_${n}`.slice(0, 32);
    if (!taken.has(next)) return next;
  }
}
