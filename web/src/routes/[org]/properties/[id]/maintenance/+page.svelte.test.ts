import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/svelte';
import { setPage } from '../../../../../lib/test/app.svelte';
import { mockApi } from '../../../../../lib/test/mock-api';
import { session } from '../../../../../lib/session.svelte';
import Page from './+page.svelte';

const ticket = (over: Record<string, unknown> = {}) => ({
	id: 7, ref: 'R-0042', property_id: 1, property_address: '12 Banksia St', lease_id: 4, title: 'Kitchen tap leaking',
	description: 'Water under sink', category: 'plumbing', urgency: 'urgent', status: 'new', supplier: null,
	access_notes: '', raised_by_name: 'Tina', file_count: 2, cancel_reason: '', created_at: '2026-11-19T01:00:00Z',
	viewed_at: null, completed_at: null, cancelled_at: null, ...over
});

function signIn(perms: string[]) {
	session.setMe({ user: { id: 1, email: 'o@example.com', name: 'Owner' }, orgs: [{ slug: 'acme', name: 'Acme', role: 'owner', perms }] });
	session.setOrg('acme');
}

beforeEach(() => {
	setPage({ params: { org: 'acme', id: '1' }, url: new URL('http://localhost/acme/properties/1/maintenance') });
	signIn(['property_read', 'property_write']);
});

describe('property maintenance tab', () => {
	it('lists open tickets with ref, urgency and status', async () => {
		mockApi({ 'GET /api/v1/orgs/acme/properties/1/maintenance?status=open': [ticket()] });
		render(Page);
		const link = await screen.findByRole('link', { name: /Kitchen tap leaking/ });
		expect(link.getAttribute('href')).toBe('/acme/properties/1/maintenance/7');
		const row = link.closest('tr')!;
		expect(within(row).getByText('R-0042')).toBeTruthy();
		expect(within(row).getByText('Urgent')).toBeTruthy();
		expect(within(row).getByText('New')).toBeTruthy();
	});

	it('switches to all tickets through the URL', async () => {
		setPage({ params: { org: 'acme', id: '1' }, url: new URL('http://localhost/acme/properties/1/maintenance?status=all') });
		mockApi({ 'GET /api/v1/orgs/acme/properties/1/maintenance?status=all': [ticket({ status: 'completed' })] });
		render(Page);
		expect(await screen.findByText('Completed')).toBeTruthy();
		expect(screen.getByRole('link', { name: 'Open' }).getAttribute('href')).toBe('/acme/properties/1/maintenance');
	});

	it('shows an empty state', async () => {
		mockApi({ 'GET /api/v1/orgs/acme/properties/1/maintenance?status=open': [] });
		render(Page);
		expect(await screen.findByText('No open repairs')).toBeTruthy();
	});

	it('offers New repair only to writers', async () => {
		mockApi({ 'GET /api/v1/orgs/acme/properties/1/maintenance?status=open': [] });
		render(Page);
		await screen.findByText('No open repairs');
		await fireEvent.click(screen.getByRole('button', { name: 'New repair' }));
		expect(screen.getByRole('dialog', { name: 'New repair' })).toBeTruthy();
	});

	it('hides New repair from readers', async () => {
		signIn(['property_read']);
		mockApi({ 'GET /api/v1/orgs/acme/properties/1/maintenance?status=open': [] });
		render(Page);
		await screen.findByText('No open repairs');
		expect(screen.queryByRole('button', { name: 'New repair' })).toBeNull();
	});
});
