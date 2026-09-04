import { useParams } from '@tanstack/react-router';
import {
  createColumnHelper,
  createSortedRowModel,
  rowSelectionFeature,
  rowSortingFeature,
  tableFeatures,
  useTable,
  type RowSelectionState,
  type SortingState,
} from '@tanstack/react-table';
import { ArrowDown, ArrowUp, ArrowUpDown, Plus } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';

import type { Item, ProjectSummary } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { ToastProvider } from '@/components/ui/toast';
import { TooltipProvider } from '@/components/ui/tooltip';
import {
  ItemLink,
  LabelChip,
  PriorityBadge,
  StatusBadge,
  TypeBadge,
} from '@/features/backlog/Badges';
import { BulkMoveBar } from '@/features/backlog/BulkMoveBar';
import { FeatureLink } from '@/features/backlog/FeatureLink';
import { FilterBar } from '@/features/backlog/FilterBar';
import { formatDate } from '@/features/backlog/item-meta';
import { useBacklogEvents, useItems, useProject } from '@/features/backlog/queries';
import { QuickViews } from '@/features/backlog/QuickViews';
import { isEmptySearch, toItemFilter, type SortField } from '@/features/backlog/search';
import { useItemSearch, useSetItemSearch } from '@/features/backlog/use-search';

const features = tableFeatures({
  rowSelectionFeature,
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
});

const columnHelper = createColumnHelper<typeof features, Item>();

const EMPTY_ITEMS: Item[] = [];

/** Columns the provider can sort; every other header is inert. */
const sortableColumns = new Set<SortField>(['id', 'title', 'priority', 'updated']);

function isSortField(id: string): id is SortField {
  return sortableColumns.has(id as SortField);
}

function SortableHeader({
  label,
  columnId,
  sorting,
  onSort,
}: {
  label: string;
  columnId: string;
  sorting: SortingState;
  onSort: (columnId: string) => void;
}) {
  if (!isSortField(columnId)) return <>{label}</>;
  const active = sorting[0]?.id === columnId ? sorting[0] : undefined;
  const Icon = active ? (active.desc ? ArrowDown : ArrowUp) : ArrowUpDown;
  return (
    <button
      type="button"
      className="inline-flex items-center gap-1 rounded px-1 py-0.5 hover:text-foreground"
      onClick={() => {
        onSort(columnId);
      }}
    >
      {label}
      <Icon aria-hidden="true" className="h-3 w-3" />
      <span className="sr-only">
        {active ? `sorted ${active.desc ? 'descending' : 'ascending'}` : 'not sorted'}
      </span>
    </button>
  );
}

function ItemTableView() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';
  const search = useItemSearch();
  const setSearch = useSetItemSearch();
  const provider = useProvider();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});

  useBacklogEvents(projectKey);

  const projectQuery = useProject(projectKey);
  const project: ProjectSummary | undefined = projectQuery.data;

  const filter = useMemo(
    () => toItemFilter(search, { project: projectKey, projectSummary: project }),
    [search, projectKey, project],
  );
  const itemsQuery = useItems(filter);

  const milestonesQuery = useItems(
    useMemo(
      () => ({
        project: projectKey,
        type: ['milestone' as const],
        limit: 100,
        sort: 'id' as const,
      }),
      [projectKey],
    ),
  );
  const milestones = useMemo(
    () => milestonesQuery.data?.pages.flatMap((page) => page.items) ?? EMPTY_ITEMS,
    [milestonesQuery.data],
  );

  const items = useMemo(
    () => itemsQuery.data?.pages.flatMap((page) => page.items) ?? EMPTY_ITEMS,
    [itemsQuery.data],
  );
  const total = itemsQuery.data?.pages[0]?.total ?? 0;

  const sorting = useMemo<SortingState>(() => {
    const field = search.sort ?? 'updated';
    return [{ id: field, desc: (search.order ?? 'desc') === 'desc' }];
  }, [search.sort, search.order]);

  const applySort = (columnId: string) => {
    if (!isSortField(columnId)) return;
    const current = sorting[0];
    const desc = current?.id === columnId ? !current.desc : columnId === 'updated';
    setSearch({ sort: columnId, order: desc ? 'desc' : 'asc' });
  };

  const columns = useMemo(
    () =>
      columnHelper.columns([
        columnHelper.display({
          id: 'select',
          header: ({ table }) => (
            <Checkbox
              aria-label="Select all loaded items"
              checked={table.getIsAllRowsSelected()}
              indeterminate={table.getIsSomeRowsSelected()}
              onChange={table.getToggleAllRowsSelectedHandler()}
            />
          ),
          cell: ({ row }) => (
            <Checkbox
              aria-label={`Select ${row.original.id}`}
              checked={row.getIsSelected()}
              onChange={row.getToggleSelectedHandler()}
            />
          ),
        }),
        columnHelper.accessor('id', {
          id: 'id',
          header: 'ID',
          cell: ({ row }) => (
            <ItemLink
              project={projectKey}
              id={row.original.id}
              rowLink
              className="whitespace-nowrap"
            >
              {row.original.id}
            </ItemLink>
          ),
        }),
        columnHelper.accessor('type', {
          id: 'type',
          header: 'Type',
          enableSorting: false,
          cell: ({ row }) => <TypeBadge type={row.original.type} />,
        }),
        columnHelper.accessor('title', {
          id: 'title',
          header: 'Title',
          cell: ({ row }) => (
            <ItemLink
              project={projectKey}
              id={row.original.id}
              className="font-sans font-medium text-foreground"
            >
              {row.original.title}
            </ItemLink>
          ),
        }),
        columnHelper.accessor('status', {
          id: 'status',
          header: 'Status',
          enableSorting: false,
          cell: ({ row }) => <StatusBadge status={row.original.status} project={project} />,
        }),
        columnHelper.accessor('priority', {
          id: 'priority',
          header: 'Priority',
          cell: ({ row }) => <PriorityBadge priority={row.original.priority} />,
        }),
        columnHelper.accessor((row) => (row.assignees ?? []).join(', '), {
          id: 'assignees',
          header: 'Assignees',
          enableSorting: false,
          cell: ({ row }) =>
            (row.original.assignees ?? []).length > 0 ? (
              (row.original.assignees ?? []).join(', ')
            ) : (
              <span className="text-muted-foreground">—</span>
            ),
        }),
        columnHelper.accessor((row) => (row.labels ?? []).join(', '), {
          id: 'labels',
          header: 'Labels',
          enableSorting: false,
          cell: ({ row }) => (
            <span className="flex flex-wrap gap-1">
              {(row.original.labels ?? []).map((label) => (
                <LabelChip
                  key={label}
                  label={label}
                  color={project?.labels.find((entry) => entry.name === label)?.color}
                />
              ))}
            </span>
          ),
        }),
        columnHelper.accessor('estimate', {
          id: 'estimate',
          header: 'Estimate',
          enableSorting: false,
          cell: ({ row }) =>
            row.original.estimate ?? <span className="text-muted-foreground">—</span>,
        }),
        columnHelper.accessor('updated', {
          id: 'updated',
          header: 'Updated',
          cell: ({ row }) => (
            <span className="whitespace-nowrap text-muted-foreground">
              {formatDate(row.original.updated)}
            </span>
          ),
        }),
      ]),
    [projectKey, project],
  );

  const table = useTable({
    features,
    columns,
    data: items,
    getRowId: (row: Item) => row.id,
    manualSorting: true,
    state: { rowSelection, sorting },
    onRowSelectionChange: setRowSelection,
  });

  const selectedItems = useMemo(
    () => items.filter((item) => rowSelection[item.id] === true),
    [items, rowSelection],
  );

  // Keyboard navigation: j/k walk the rows, Enter follows the focused link.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'j' && event.key !== 'k') return;
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      const tag = target?.tagName ?? '';
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target?.isContentEditable) {
        return;
      }
      const node = containerRef.current;
      if (!node) return;
      const links = Array.from(node.querySelectorAll<HTMLAnchorElement>('a[data-row-link="true"]'));
      if (links.length === 0) return;
      const index = links.findIndex((link) => link === document.activeElement);
      const next =
        index === -1
          ? links[0]
          : links[
              event.key === 'j' ? Math.min(index + 1, links.length - 1) : Math.max(index - 1, 0)
            ];
      next?.focus();
      event.preventDefault();
    };

    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
    };
  }, []);

  const filtered = !isEmptySearch(search);

  return (
    <div className="space-y-4" ref={containerRef}>
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">Items</h1>
          <p className="text-sm text-muted-foreground">
            Epics, stories, tasks and milestones of <strong>{projectKey}</strong>
            {itemsQuery.isSuccess ? ` — ${items.length} of ${total}` : null}
          </p>
        </div>
        <FeatureLink
          to="/p/$project/items/new"
          params={{ project: projectKey }}
          search={{ type: 'story' }}
          className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          <Plus aria-hidden="true" className="h-4 w-4" />
          New item
        </FeatureLink>
      </header>

      <QuickViews search={search} />
      <FilterBar search={search} project={project} milestones={milestones} />

      <BulkMoveBar
        projectKey={projectKey}
        project={project}
        selected={selectedItems}
        canWrite={provider.capabilities.write}
        onDone={() => {
          setRowSelection({});
        }}
      />

      {itemsQuery.isPending ? (
        <p className="py-8 text-center text-sm text-muted-foreground">Loading items…</p>
      ) : null}

      {itemsQuery.isError ? (
        <Card>
          <CardHeader>
            <CardTitle>The index could not be read</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm text-muted-foreground">
            <p>{itemsQuery.error.message}</p>
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                void itemsQuery.refetch();
              }}
            >
              Try again
            </Button>
          </CardContent>
        </Card>
      ) : null}

      {itemsQuery.isSuccess && items.length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>{filtered ? 'No items match these filters' : 'No items yet'}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm text-muted-foreground">
            {filtered ? (
              <>
                <p>Widen the search, or clear the filters to see the whole backlog.</p>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    setSearch({
                      q: undefined,
                      type: undefined,
                      status: undefined,
                      category: undefined,
                      priority: undefined,
                      label: undefined,
                      assignee: undefined,
                      milestone: undefined,
                      parent: undefined,
                      view: undefined,
                    });
                  }}
                >
                  Clear filters
                </Button>
              </>
            ) : (
              <p>
                An item is a Markdown file with front matter under{' '}
                <code className="font-mono">docs/.pmngr/</code>. Create the first one with{' '}
                <strong>New item</strong>, or add a file by hand and re-index.
              </p>
            )}
          </CardContent>
        </Card>
      ) : null}

      {items.length > 0 ? (
        <>
          <Table>
            <TableHeader>
              {table.getHeaderGroups().map((group) => (
                <TableRow key={group.id}>
                  {group.headers.map((header) => (
                    <TableHead
                      key={header.id}
                      aria-sort={
                        sorting[0]?.id === header.column.id
                          ? sorting[0]?.desc
                            ? 'descending'
                            : 'ascending'
                          : undefined
                      }
                    >
                      {header.isPlaceholder ? null : typeof header.column.columnDef.header ===
                        'string' ? (
                        <SortableHeader
                          label={header.column.columnDef.header}
                          columnId={header.column.id}
                          sorting={sorting}
                          onSort={applySort}
                        />
                      ) : (
                        <table.FlexRender header={header} />
                      )}
                    </TableHead>
                  ))}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {table.getRowModel().rows.map((row) => (
                <TableRow key={row.id} data-selected={row.getIsSelected()}>
                  {row.getAllCells().map((cell) => (
                    <TableCell key={cell.id}>
                      <table.FlexRender cell={cell} />
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>

          <div className="flex items-center justify-between gap-3">
            <p className="text-xs text-muted-foreground">
              Showing {items.length} of {total}. Press <kbd className="font-mono">j</kbd> /{' '}
              <kbd className="font-mono">k</kbd> to walk the rows.
            </p>
            {itemsQuery.hasNextPage ? (
              <Button
                variant="outline"
                size="sm"
                disabled={itemsQuery.isFetchingNextPage}
                onClick={() => {
                  void itemsQuery.fetchNextPage();
                }}
              >
                {itemsQuery.isFetchingNextPage ? 'Loading…' : 'Load more'}
              </Button>
            ) : null}
          </div>
        </>
      ) : null}
    </div>
  );
}

/** Item list route (`/p/$project/items`). */
export function ItemTable() {
  return (
    <TooltipProvider delayDuration={200}>
      <ToastProvider>
        <ItemTableView />
      </ToastProvider>
    </TooltipProvider>
  );
}
