import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { apiError, apiMock, mockApi } from '../../test/mock-api';
import ChecklistPanel from './ChecklistPanel.svelte';

const item = { id: 4, position: 1, action: 'Grant access', owner: 'Sam', due_date: '2026-11-25', done_at: null };
const base = '/api/v1/orgs/acme/contacts/9/checklist';

describe('ChecklistPanel', () => {
	it('marks an item done once staff confirm it', async () => {
		mockApi({ [`GET ${base}`]: [item], [`POST ${base}/4/done`]: { ...item, done_at: '2026-11-23T00:00:00Z' } });
		render(ChecklistPanel, { org: 'acme', contactId: 9, canWrite: true });
		const box = (await screen.findByLabelText('Confirmed: Grant access')) as HTMLInputElement;
		await fireEvent.click(box);
		expect(apiMock).toHaveBeenCalledWith('POST', `${base}/4/done`, { done: true });
		await screen.findByText(/Confirmed \d/);
		expect(box.checked).toBe(true);
	});

	it('reverts the tick and shows the error when saving fails', async () => {
		mockApi({ [`GET ${base}`]: [item], [`POST ${base}/4/done`]: apiError(500, 'internal error') });
		render(ChecklistPanel, { org: 'acme', contactId: 9, canWrite: true });
		const box = (await screen.findByLabelText('Confirmed: Grant access')) as HTMLInputElement;
		await fireEvent.click(box);
		await screen.findByRole('alert');
		expect(box.checked).toBe(false);
	});

	it('is read-only without contacts_write', async () => {
		mockApi({ [`GET ${base}`]: [item] });
		render(ChecklistPanel, { org: 'acme', contactId: 9, canWrite: false });
		const box = (await screen.findByLabelText('Confirmed: Grant access')) as HTMLInputElement;
		expect(box.disabled).toBe(true);
		expect(screen.queryByRole('button', { name: /Add item/ })).toBeNull();
	});
});
