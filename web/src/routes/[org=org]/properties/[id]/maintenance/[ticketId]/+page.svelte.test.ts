import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { setPage } from '../../../../../../lib/test/app.svelte';
import { apiMock, mockApi } from '../../../../../../lib/test/mock-api';
import { session } from '../../../../../../lib/session.svelte';
import Page from './+page.svelte';

const ticket = {
	id: 7, ref: 'R-0042', property_id: 1, property_address: '12 Banksia St', lease_id: 4, title: 'Kitchen tap leaking',
	description: 'Water under sink', category: 'plumbing', urgency: 'urgent', status: 'new', supplier: null,
	access_notes: 'Lockbox 4821', raised_by_name: 'Tina', file_count: 1, cancel_reason: '', created_at: '2026-11-19T01:00:00Z',
	viewed_at: null, completed_at: null, cancelled_at: null
};
const detail = {
	ticket,
	tenants: [{ id: 3, name: 'Tina Tenant', email: 'tina@example.com', phone: '0400 000 000' }],
	files: [{ id: 9, mime: 'image/png', size_bytes: 5, original_name: 'leak.png' }],
	entry_preference: 'permitted',
	availability: [{ date: '2026-11-24', slot: 'morning', start: null, end: null }],
	open_visit: null,
	visits: [],
	events: [
		{ id: 1, kind: 'created', body: '', data: {}, actor_name: 'Tina', tenant_visible: true, created_at: '2026-11-19T01:00:00Z' },
		{ id: 2, kind: 'note', body: 'Called plumber', data: {}, actor_name: 'Owner', tenant_visible: false, created_at: '2026-11-19T02:00:00Z' }
	]
};

function signIn(perms: string[]) {
	session.setMe({ user: { id: 1, email: 'o@example.com', name: 'Owner' }, orgs: [{ slug: 'acme', name: 'Acme', role: 'owner', perms }] });
	session.setOrg('acme');
}

beforeEach(() => setPage({ params: { org: 'acme', id: '1', ticketId: '7' }, url: new URL('http://localhost/acme/properties/1/maintenance/7') }));

describe('staff ticket page', () => {
	it('marks a new ticket viewed once for writers and shows the ticket', async () => {
		signIn(['property_read', 'property_write']);
		mockApi({
			'GET /api/v1/orgs/acme/maintenance/7': detail,
			'POST /api/v1/orgs/acme/maintenance/7/viewed': { ...ticket, status: 'viewed' }
		});
		render(Page);
		expect(await screen.findByRole('heading', { name: /Kitchen tap leaking/ })).toBeTruthy();
		await screen.findByText('Viewed');
		expect(apiMock.mock.calls.filter(([m, p]) => m === 'POST' && p.endsWith('/viewed'))).toHaveLength(1);
		expect(screen.getByText('Tina Tenant')).toBeTruthy();
		expect(screen.getByText('tina@example.com')).toBeTruthy();
		expect(screen.getByText('Lockbox 4821')).toBeTruthy();
		expect(screen.getByRole('img', { name: 'leak.png' })).toBeTruthy();
		expect(screen.getByText('Internal')).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Assign tradesperson' })).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Mark complete' })).toBeTruthy();
	});

	it('does not mark viewed or offer changes for readers', async () => {
		signIn(['property_read']);
		mockApi({ 'GET /api/v1/orgs/acme/maintenance/7': detail });
		render(Page);
		await screen.findByRole('heading', { name: /Kitchen tap leaking/ });
		expect(apiMock.mock.calls.some(([m]) => m === 'POST')).toBe(false);
		expect(screen.queryByRole('button', { name: 'Assign tradesperson' })).toBeNull();
		expect(screen.queryByRole('button', { name: 'Add note' })).toBeNull();
	});

	it('hides close actions on a completed ticket', async () => {
		signIn(['property_read', 'property_write']);
		mockApi({ 'GET /api/v1/orgs/acme/maintenance/7': { ...detail, ticket: { ...ticket, status: 'completed' } } });
		render(Page);
		await screen.findByRole('heading', { name: /Kitchen tap leaking/ });
		expect(screen.queryByRole('button', { name: 'Mark complete' })).toBeNull();
		expect(screen.getByRole('button', { name: 'Add note' })).toBeTruthy();
	});

	it('offers to book a visit once a tradesperson is assigned, with the tenant times and preference', async () => {
		signIn(['property_read', 'property_write']);
		mockApi({ 'GET /api/v1/orgs/acme/maintenance/7': { ...detail, ticket: { ...ticket, status: 'assigned', supplier: { id: 4, name: 'Fitzroy Plumbing' } } } });
		render(Page);
		expect(await screen.findByRole('button', { name: 'Book visit' })).toBeTruthy();
		expect(screen.getByText('Tue 24 Nov · Morning (8am–12pm)')).toBeTruthy();
		expect(screen.getByText('Tenant preference: OK to enter if out')).toBeTruthy();
	});

	it('shows the open visit with its entry state and visit actions', async () => {
		signIn(['property_read', 'property_write']);
		const visit = { id: 3, supplier: { id: 4, name: 'Fitzroy Plumbing' }, starts_at: '2026-11-24T09:00:00+10:00', ends_at: '2026-11-24T11:00:00+10:00', status: 'confirmed', tenant_confirmed_at: '2026-11-22T00:00:00Z', change_note: '', entry_state: 'permitted', entry_granted_at: '2026-11-22T00:00:00Z', entry_withdrawn_at: null };
		mockApi({ 'GET /api/v1/orgs/acme/maintenance/7': { ...detail, ticket: { ...ticket, status: 'scheduled', supplier: visit.supplier }, open_visit: visit, visits: [visit] } });
		render(Page);
		expect(await screen.findByText('Tue 24 Nov, 9am – 11am')).toBeTruthy();
		expect(screen.getByText('Entry permitted')).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Reschedule' })).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Cancel visit' })).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Mark visit done' })).toBeTruthy();
	});
});
