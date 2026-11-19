import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { apiMock, mockApi } from '../../test/mock-api';
import VisitDialog from './VisitDialog.svelte';

const ticket = { id: 7, supplier: { id: 4, name: 'Fitzroy Plumbing' } };

describe('VisitDialog', () => {
	it('books with org-local wall times and the ticket supplier', async () => {
		mockApi({ 'GET /api/v1/orgs/acme/contacts?kind=supplier': [{ id: 4, name: 'Fitzroy Plumbing' }], 'POST /api/v1/orgs/acme/maintenance/7/visits': { id: 1 } });
		const onsaved = vi.fn();
		render(VisitDialog, { open: true, org: 'acme', ticket, visit: null, onsaved });
		expect(((await screen.findByLabelText('Tradesperson')) as HTMLSelectElement).value).toBe('4');
		await fireEvent.input(screen.getByLabelText('Date'), { target: { value: '2026-11-24' } });
		await fireEvent.input(screen.getByLabelText('Arrive from'), { target: { value: '09:00' } });
		await fireEvent.input(screen.getByLabelText('Arrive by'), { target: { value: '11:00' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Book visit' }));
		await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
		expect(apiMock).toHaveBeenCalledWith('POST', '/api/v1/orgs/acme/maintenance/7/visits', { supplier_contact_id: 4, starts_at: '2026-11-24T09:00', ends_at: '2026-11-24T11:00' });
	});

	it('rejects an end before the start without calling the API', async () => {
		mockApi({ 'GET /api/v1/orgs/acme/contacts?kind=supplier': [{ id: 4, name: 'Fitzroy Plumbing' }] });
		render(VisitDialog, { open: true, org: 'acme', ticket, visit: null, onsaved: vi.fn() });
		await fireEvent.input(await screen.findByLabelText('Date'), { target: { value: '2026-11-24' } });
		await fireEvent.input(screen.getByLabelText('Arrive from'), { target: { value: '11:00' } });
		await fireEvent.input(screen.getByLabelText('Arrive by'), { target: { value: '09:00' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Book visit' }));
		expect(await screen.findByText('The arrival window must end after it starts.')).toBeTruthy();
		expect(apiMock.mock.calls.some(([m]) => m === 'POST')).toBe(false);
	});

	it('warns that rescheduling clears confirmation and entry', async () => {
		mockApi({});
		const visit = { id: 9, supplier: { id: 4, name: 'Fitzroy Plumbing' }, starts_at: '2026-11-24T09:00:00+10:00', ends_at: '2026-11-24T11:00:00+10:00' };
		render(VisitDialog, { open: true, org: 'acme', ticket, visit, onsaved: vi.fn() });
		expect(screen.getByText("Rescheduling clears the tenant's confirmation and entry permission.")).toBeTruthy();
		expect((screen.getByLabelText('Date') as HTMLInputElement).value).toBe('2026-11-24');
		expect(screen.queryByLabelText('Tradesperson')).toBeNull();
	});
});
