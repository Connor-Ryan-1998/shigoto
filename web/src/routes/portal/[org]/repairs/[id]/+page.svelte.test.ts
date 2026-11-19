import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { setPage } from '../../../../../lib/test/app.svelte';
import { apiMock, mockApi } from '../../../../../lib/test/mock-api';
import Page from './+page.svelte';

const ticket = (status = 'new') => ({
	id: 7, ref: 'R-0042', property_id: 1, property_address: '12 Banksia St', lease_id: 4, title: 'Kitchen tap', description: 'Leaking',
	category: 'plumbing', urgency: 'urgent', status, supplier: status === 'assigned' ? { id: 2, name: 'Fitzroy Plumbing' } : null,
	access_notes: '', raised_by_name: 'Tina', file_count: 0, cancel_reason: '', created_at: '2026-11-19T01:00:00Z',
	viewed_at: null, completed_at: null, cancelled_at: null
});
const detail = (status = 'new', extra: Record<string, unknown> = {}) => ({
	ticket: ticket(status), files: [], entry_preference: null, availability: [], open_visit: null, visits: [], ...extra,
	events: [
		{ id: 1, kind: 'created', body: '', data: {}, actor_name: 'Tina', tenant_visible: true, created_at: '2026-11-19T01:00:00Z' },
		{ id: 2, kind: 'comment', body: 'Plumber turns water off', data: {}, actor_name: 'Owner', tenant_visible: true, created_at: '2026-11-19T02:00:00Z' }
	]
});

beforeEach(() => setPage({ params: { org: 'acme', id: '7' }, url: new URL('http://localhost/portal/acme/repairs/7') }));

describe('portal repair detail', () => {
	it('shows status, timeline newest first and shared messages', async () => {
		mockApi({ 'GET /api/v1/portal/acme/maintenance/7': detail('assigned') });
		render(Page);
		expect(await screen.findByRole('heading', { name: 'Kitchen tap' })).toBeTruthy();
		expect(screen.getAllByText('Tradesperson assigned').length).toBeGreaterThan(0);
		expect(screen.getByText('Fitzroy Plumbing')).toBeTruthy();
		const items = screen.getAllByRole('listitem').map((li) => li.textContent ?? '');
		expect(items[0]).toContain('Plumber turns water off');
		expect(screen.queryByRole('button', { name: 'Cancel request' })).toBeNull();
	});

	it('sends a comment', async () => {
		mockApi({
			'GET /api/v1/portal/acme/maintenance/7': detail(),
			'POST /api/v1/portal/acme/maintenance/7/comments': { id: 3, kind: 'comment', body: 'Thanks' }
		});
		render(Page);
		await fireEvent.input(await screen.findByLabelText('Add a message'), { target: { value: 'Thanks' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Send' }));
		await screen.findByRole('button', { name: 'Send' });
		expect(apiMock).toHaveBeenCalledWith('POST', '/api/v1/portal/acme/maintenance/7/comments', { body: 'Thanks' });
	});

	it('lets the tenant cancel a new request', async () => {
		mockApi({ 'GET /api/v1/portal/acme/maintenance/7': detail() });
		render(Page);
		await fireEvent.click(await screen.findByRole('button', { name: 'Cancel request' }));
		expect(screen.getByRole('dialog', { name: 'Cancel request' })).toBeTruthy();
	});

	it('shows the visit card for an open visit', async () => {
		const open_visit = { id: 3, supplier: { id: 2, name: 'Fitzroy Plumbing' }, starts_at: '2099-11-24T09:00:00+10:00', ends_at: '2099-11-24T11:00:00+10:00', status: 'booked', tenant_confirmed_at: null, change_note: '', entry_state: 'unconfirmed', entry_granted_at: null, entry_withdrawn_at: null };
		mockApi({ 'GET /api/v1/portal/acme/maintenance/7': detail('scheduled', { open_visit, entry_preference: 'permitted' }) });
		render(Page);
		expect(await screen.findByText('Tue 24 Nov, 9am – 11am')).toBeTruthy();
		expect((screen.getByLabelText("OK to enter if I'm out") as HTMLInputElement).checked).toBe(true);
		await fireEvent.click(screen.getByRole('button', { name: 'Request change' }));
		expect(screen.getByRole('dialog', { name: 'Request a different time' })).toBeTruthy();
	});

	it('lets the tenant edit their times before a visit is confirmed', async () => {
		mockApi({
			'GET /api/v1/portal/acme/maintenance/7': detail('viewed', { availability: [{ date: '2099-01-05', slot: 'morning', start: null, end: null }] }),
			'PUT /api/v1/portal/acme/maintenance/7/availability': { availability: [] }
		});
		render(Page);
		await fireEvent.click(await screen.findByRole('button', { name: /^Remove/ }));
		await vi.waitFor(() => expect(apiMock).toHaveBeenCalledWith('PUT', '/api/v1/portal/acme/maintenance/7/availability', { windows: [] }));
	});
});
