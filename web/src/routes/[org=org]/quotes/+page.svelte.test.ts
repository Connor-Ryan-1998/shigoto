import { beforeEach, describe, expect, it } from 'vitest';
import { setPage } from '../../../lib/test/app.svelte';
import { apiMock } from '../../../lib/test/mock-api';
import { render, screen, within } from '@testing-library/svelte';
import { session } from '../../../lib/session.svelte';

import Page from './+page.svelte';

const quote = (over: Record<string, unknown>) => ({
	id: 7, contact_id: 9, contact_name: 'Priya', project_id: null, project_name: '', number: 7, revision: 1,
	number_label: 'Q-0007', title: 'Switchboard', status: 'sent', display_status: 'sent', issue_date: '2026-11-27',
	valid_until: '2026-12-27', subtotal: 10000, gst_total: 1000, total: 11000, ...over
});

function signIn(perms: string[]) {
	session.setMe({ user: { id: 1, email: 'o@example.com', name: 'O' }, orgs: [{ slug: 'acme', name: 'Acme', role: 'owner', perms }] });
	session.setOrg('acme');
}

beforeEach(() => {
	setPage({ params: { org: 'acme' }, url: new URL('http://x/acme/quotes') });
	signIn(['quotes_read', 'quotes_write']);
	apiMock.mockImplementation(() => Promise.resolve({
		quotes: [quote({}), quote({ id: 8, number_label: 'Q-0008', display_status: 'expired', project_name: 'Kitchen', project_id: 3 })],
		total: 2
	}));
});

describe('quotes list', () => {
	it('lists quotes with status, customer and project', async () => {
		render(Page);
		const link = await screen.findByRole('link', { name: 'Q-0008' });
		expect(link.getAttribute('href')).toBe('/acme/quotes/8');
		const row = link.closest('tr')!;
		expect(within(row).getByText('Expired')).toBeTruthy();
		expect(within(row).getByText('Kitchen')).toBeTruthy();
		expect(apiMock).toHaveBeenCalledWith('GET', '/api/v1/orgs/acme/quotes?limit=50&offset=0');
	});

	it('filters by the status tab', async () => {
		setPage({ params: { org: 'acme' }, url: new URL('http://x/acme/quotes?status=accepted') });
		render(Page);
		await screen.findByRole('link', { name: 'Q-0007' });
		expect(apiMock).toHaveBeenCalledWith('GET', '/api/v1/orgs/acme/quotes?status=accepted&limit=50&offset=0');
	});

	it('offers New quote only with quotes_write', async () => {
		render(Page);
		expect(await screen.findByRole('button', { name: 'New quote' })).toBeTruthy();
	});

	it('hides New quote from readers', async () => {
		signIn(['quotes_read']);
		render(Page);
		await screen.findByRole('link', { name: 'Q-0007' });
		expect(screen.queryByRole('button', { name: 'New quote' })).toBeNull();
	});
});
