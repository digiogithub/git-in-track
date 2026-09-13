/**
 * The preview table of the import dialog (story GIT-US-0059, task GIT-T-0104).
 *
 * The whole point of a preview is that it is the same call the run makes, minus
 * the writing: the options that produced these rows are the options the run is
 * sent, so what is on screen is what will happen. The two facts a person checks
 * here are the action — is this issue going to be *created*, or is it going to
 * *update* something that already exists — and the type it mapped onto.
 *
 * Warnings are visible and never blocking. A mapper warning says "this value
 * was not understood and here is what I used instead", which is information for
 * the person deciding, not an error: an import with warnings is usually exactly
 * the import they wanted, and refusing to run it would only teach them to stop
 * reading them.
 *
 * Every string on this table came from a third-party tracker, so every string
 * is rendered as text. There is no Markdown path here and no `dangerouslySet…`.
 */

import { TriangleAlert } from 'lucide-react';

import type { YouTrackImportPlanItem, YouTrackImportWarning } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';

export type ImportPreviewTableProps = {
  issues: YouTrackImportPlanItem[];
  /** Findings about the import as a whole rather than about one issue. */
  warnings?: YouTrackImportWarning[];
};

export function ImportPreviewTable({ issues, warnings = [] }: ImportPreviewTableProps) {
  if (issues.length === 0) {
    return <p className="empty-state">This selection would import nothing.</p>;
  }

  const created = issues.filter((row) => row.action === 'create').length;
  const updated = issues.length - created;

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground" data-testid="import-preview-summary">
        {String(created)} to create, {String(updated)} to update.
      </p>

      {warnings.length > 0 ? <WarningList warnings={warnings} label="About this import" /> : null}

      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Issue</TableHead>
              <TableHead>Action</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Parent</TableHead>
              <TableHead>Warnings</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {issues.map((row) => (
              <TableRow key={row.youtrackId}>
                <TableCell>
                  <span className="font-mono text-xs text-muted-foreground">{row.youtrackId}</span>
                  <span className="block truncate">{row.title}</span>
                </TableCell>
                <TableCell>
                  {row.action === 'create' ? (
                    <Badge variant="success">Create</Badge>
                  ) : (
                    <Badge variant="info">Update {row.targetId ?? ''}</Badge>
                  )}
                </TableCell>
                <TableCell>{row.mappedType}</TableCell>
                <TableCell className="font-mono text-xs">{row.parent ?? '—'}</TableCell>
                <TableCell>
                  {(row.warnings ?? []).length === 0 ? (
                    <span className="text-muted-foreground">—</span>
                  ) : (
                    <WarningList
                      warnings={row.warnings ?? []}
                      label={`${row.youtrackId} warnings`}
                    />
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

/**
 * Warnings, as a list of sentences. `reason` is a complete English sentence the
 * mapper wrote; `field` and `value` are the tracker's own words and are shown
 * beside it rather than interpolated into it.
 */
function WarningList({ warnings, label }: { warnings: YouTrackImportWarning[]; label: string }) {
  return (
    <ul aria-label={label} className="space-y-1">
      {warnings.map((warning, index) => (
        <li
          key={`${warning.field}-${String(index)}`}
          className="flex items-start gap-1.5 text-xs text-warning"
        >
          <TriangleAlert aria-hidden="true" className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>
            <span className="font-medium">{warning.field}</span>
            {warning.value === undefined || warning.value === '' ? null : (
              <span className="font-mono"> {warning.value}</span>
            )}
            {warning.reason === '' ? null : <span> — {warning.reason}</span>}
          </span>
        </li>
      ))}
    </ul>
  );
}
