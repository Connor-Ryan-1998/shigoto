import { beforeEach, expect, it } from 'vitest';
import { setPage } from '../../test/app.svelte';
import { render, screen } from '@testing-library/svelte';
import DashboardCard from './DashboardCard.svelte';

const empty = { arrears_total: 0, unissued_total: 0, bonds_unlodged_overdue: 0, insurance_renewing_soon: 0, repairs: { open_count: 0, items: [] } };
beforeEach(() => setPage({ params: { org: 'acme' } }));

it('shows how many insurance policies renew soon', () => {
	render(DashboardCard, { data: { ...empty, insurance_renewing_soon: 2 } });
	const term = screen.getByText('Insurance renewing within 30 days');
	expect(term.nextElementSibling?.textContent).toBe('2');
});

it('lists open repairs, most pressing first, linking to each ticket', () => {
	render(DashboardCard, {
		data: {
			...empty,
			repairs: {
				open_count: 6,
				items: [{ ticket_id: 7, property_id: 3, property_address: '12 Banksia St', ref: 'R-0042', title: 'Burst pipe', urgency: 'urgent', status: 'new' }]
			}
		}
	});
	expect(screen.getByRole('heading', { name: 'Repairs' })).toBeTruthy();
	expect(screen.getByText('6 open')).toBeTruthy();
	expect(screen.getByRole('link', { name: /Burst pipe/ }).getAttribute('href')).toBe('/acme/properties/3/maintenance/7');
	expect(screen.getByText('Urgent')).toBeTruthy();
});

it('hides the repairs card with nothing open', () => {
	render(DashboardCard, { data: empty });
	expect(screen.queryByRole('heading', { name: 'Repairs' })).toBeNull();
});
