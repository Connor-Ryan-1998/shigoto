import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { setPage } from '../../../../lib/test/app.svelte';
import { mockApi } from '../../../../lib/test/mock-api';
import Page from './+page.svelte';

const ticket = { id: 7, ref: 'R-0042', title: 'Kitchen tap', status: 'assigned', urgency: 'urgent', property_address: '12 Banksia St', created_at: '2026-11-19T01:00:00Z' };

beforeEach(() => setPage({ params: { org: 'acme' }, url: new URL('http://localhost/portal/acme/repairs') }));

describe('portal repairs list', () => {
	it('lists tickets and offers to report a repair', async () => {
		mockApi({ 'GET /api/v1/portal/acme/maintenance': { emergency_phone: '', leases: [{ id: 4, property_address: '12 Banksia St', active: true }], tickets: [ticket] } });
		render(Page);
		const link = await screen.findByRole('link', { name: /Kitchen tap/ });
		expect(link.getAttribute('href')).toBe('/portal/acme/repairs/7');
		expect(screen.getByText('Tradesperson assigned')).toBeTruthy();
		expect(screen.getByRole('link', { name: 'Report a repair' }).getAttribute('href')).toBe('/portal/acme/repairs/new');
	});

	it('explains repairs need a current lease', async () => {
		mockApi({ 'GET /api/v1/portal/acme/maintenance': { emergency_phone: '', leases: [{ id: 4, property_address: 'Old', active: false }], tickets: [] } });
		render(Page);
		expect(await screen.findByText('You can report repairs for a current lease.')).toBeTruthy();
		expect(screen.queryByRole('link', { name: 'Report a repair' })).toBeNull();
	});

	it('shows an empty state', async () => {
		mockApi({ 'GET /api/v1/portal/acme/maintenance': { emergency_phone: '', leases: [{ id: 4, property_address: 'x', active: true }], tickets: [] } });
		render(Page);
		expect(await screen.findByText('No repairs yet')).toBeTruthy();
	});
});
