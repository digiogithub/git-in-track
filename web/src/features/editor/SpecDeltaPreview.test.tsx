import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { FakeProvider } from '@/api/fake-provider';
import type { DeltaPreviewOperation, SpecDeltaPreviewInput } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { SpecDeltaPreview } from '@/features/editor/SpecDeltaPreview';

const body =
  '## Description\n\nTighten.\n\n## Spec Delta\n\n' +
  '### ADDED ACME-SP-0003 — Reject long input\n\n...\n';

/** What the core answers for `body`: every operation, one of them dangling. */
const operations: DeltaPreviewOperation[] = [
  {
    op: 'ADDED',
    spec: 'ACME-SP-0003',
    specTitle: 'Checkout addresses',
    target: 'ACME-SP-0003',
    title: 'Reject long input',
    line: 7,
    proposed: 'The checkout SHALL refuse an address longer than 200 characters.',
  },
  {
    op: 'MODIFIED',
    spec: 'ACME-SP-0003',
    target: 'ACME-SP-0003.R1',
    title: 'Trim pasted input',
    line: 11,
    proposed: 'The checkout SHALL trim pasted addresses on both ends.',
    current: {
      ref: 'ACME-SP-0003.R1',
      title: 'Trim input',
      text: 'The checkout SHALL trim pasted addresses.',
    },
  },
  {
    op: 'REMOVED',
    spec: 'ACME-SP-0003',
    target: 'ACME-SP-0003.R2',
    title: 'Reject empty input',
    line: 15,
    reason: 'the form catches it',
    current: {
      ref: 'ACME-SP-0003.R2',
      title: 'Reject empty input',
      text: 'The checkout SHALL refuse an empty address.',
    },
  },
  {
    op: 'MODIFIED',
    spec: 'ACME-SP-0003',
    target: 'ACME-SP-0003.R9',
    title: 'Nowhere',
    line: 19,
    proposed: 'The checkout SHALL vanish.',
    dangling: 'MODIFIED names unknown requirement ACME-SP-0003.R9',
  },
];

function renderPreview(provider: FakeProvider, text: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <DataProviderProvider provider={provider}>
        <SpecDeltaPreview project="ACME" id="ACME-US-0042" body={text} />
      </DataProviderProvider>
    </QueryClientProvider>,
  );
}

describe('SpecDeltaPreview', () => {
  it('shows every operation against the current spec', async () => {
    const preview = vi.fn((_input: SpecDeltaPreviewInput) => operations);
    renderPreview(new FakeProvider({ specLive: { preview } }), body);

    const section = await screen.findByRole('region', { name: 'Spec Delta preview' });
    expect(preview).toHaveBeenCalledWith({ id: 'ACME-US-0042', body });

    const added = within(section).getByRole('listitem', { name: 'ADDED ACME-SP-0003' });
    expect(added).toHaveTextContent('in Checkout addresses');
    expect(added).toHaveTextContent('Added');
    expect(added).toHaveTextContent('SHALL refuse an address longer than 200 characters');

    const modified = within(section).getByRole('listitem', { name: 'MODIFIED ACME-SP-0003.R1' });
    expect(within(modified).getByText('Current — Trim input')).toBeInTheDocument();
    expect(modified).toHaveTextContent('The checkout SHALL trim pasted addresses.');
    expect(within(modified).getByText('Proposed')).toBeInTheDocument();
    expect(modified).toHaveTextContent('SHALL trim pasted addresses on both ends.');

    const removed = within(section).getByRole('listitem', { name: 'REMOVED ACME-SP-0003.R2' });
    expect(removed).toHaveTextContent('Removed — Reject empty input');
    expect(removed).toHaveTextContent('Reason: the form catches it');

    const dangling = within(section).getByRole('listitem', { name: 'MODIFIED ACME-SP-0003.R9' });
    expect(within(dangling).getByRole('status')).toHaveTextContent(
      'W-DELTA-DANGLINGMODIFIED names unknown requirement ACME-SP-0003.R9',
    );
    expect(within(dangling).queryByText(/^Current/)).not.toBeInTheDocument();
  });

  it('asks the core nothing for a body without a Spec Delta', async () => {
    const preview = vi.fn(() => operations);
    const { container } = renderPreview(
      new FakeProvider({ specLive: { preview } }),
      '## Notes\n\nNo delta here.\n',
    );
    await new Promise((resolve) => setTimeout(resolve, 450));
    expect(preview).not.toHaveBeenCalled();
    expect(container).toBeEmptyDOMElement();
  });

  it('says so when the core cannot preview', async () => {
    const provider = new FakeProvider();
    vi.spyOn(provider, 'previewSpecDelta').mockRejectedValue(
      new ProviderError('unavailable', 'the companion predates spec.delta.preview'),
    );
    renderPreview(provider, body);
    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent('The preview is not available');
    });
  });
});
