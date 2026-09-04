import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it } from 'vitest';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { FakeProvider, sampleProject } from '@/api/fake-provider';
import type { ProjectSummary } from '@/api/provider';
import { FrontMatterForm } from '@/components/editor/FrontMatterForm';
import type { FrontMatterValues } from '@/features/editor/front-matter';
import { emptyValues } from '@/features/editor/front-matter';
import { readProjectSchema } from '@/features/editor/project-schema';

const withWorkflow = {
  ...sampleProject,
  workflow: {
    initial: 'backlog',
    transitions: {
      todo: ['in_progress', 'backlog'],
    },
  },
  custom_fields: [{ key: 'risk', type: 'enum', values: ['low', 'high'], applies_to: ['story'] }],
} as unknown as ProjectSummary;

function Harness({ initial, project }: { initial: FrontMatterValues; project: ProjectSummary }) {
  const [values, setValues] = useState(initial);
  return (
    <FrontMatterForm
      type="story"
      values={values}
      onChange={setValues}
      schema={readProjectSchema(project)}
      projectKey="ACME"
    />
  );
}

function renderForm(initial: FrontMatterValues, project = withWorkflow) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={new FakeProvider()}>
        <Harness initial={initial} project={project} />
      </DataProviderProvider>
    </QueryClientProvider>,
  );
}

describe('FrontMatterForm', () => {
  it('offers only the transitions the workflow allows', () => {
    renderForm({ ...emptyValues('todo'), title: 'A story' });

    const options = [...screen.getByLabelText<HTMLSelectElement>('Status').options].map(
      (option) => option.value,
    );
    expect(options).toEqual(['backlog', 'todo', 'in_progress']);
  });

  it('offers every status when the workflow declares no transitions', () => {
    renderForm({ ...emptyValues('todo'), title: 'A story' }, sampleProject);

    const options = [...screen.getByLabelText<HTMLSelectElement>('Status').options].map(
      (option) => option.value,
    );
    expect(options).toContain('done');
  });

  it('searches the index from the parent combobox and stores the id', async () => {
    const user = userEvent.setup();
    renderForm({ ...emptyValues('todo'), title: 'A story' });

    const parent = screen.getByLabelText('Parent');
    await user.click(parent);
    await user.type(parent, 'Single');

    await screen.findByRole('option', { name: /ACME-EP-0001/ }, { timeout: 5000 });
    // The list re-renders while the debounced query settles, so re-query the
    // option on every attempt instead of holding on to a detached node.
    await waitFor(
      () => {
        const option = screen.getByRole('option', { name: /ACME-EP-0001/ });
        fireEvent.click(within(option).getByRole('button'));
        expect(screen.getByLabelText('Parent')).toHaveValue('ACME-EP-0001');
      },
      { timeout: 5000 },
    );
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('renders declared custom fields for the item type', () => {
    renderForm({ ...emptyValues('todo'), title: 'A story' });
    expect(screen.getByLabelText('risk')).toBeInTheDocument();
  });

  it('adds and removes typed links', async () => {
    const user = userEvent.setup();
    renderForm({ ...emptyValues('todo'), title: 'A story' });

    await user.click(screen.getByRole('button', { name: 'Add link' }));
    const target = screen.getByLabelText('Link 1 target');
    await user.type(target, 'ACME-T-0107');
    expect(screen.getByLabelText('Link 1 kind')).toHaveValue('relates_to');

    await user.click(screen.getByRole('button', { name: 'Remove link 1' }));
    expect(screen.queryByLabelText('Link 1 target')).not.toBeInTheDocument();
  });
});
