import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/svelte';
import { setPage } from '../../../../lib/test/app.svelte';
import { apiError, mockApi, deferred } from '../../../../lib/test/mock-api';
import { currentFY } from '../../../../lib/modules/property/fy';
import { todayLocal } from '../../../../lib/format';

import Page from './+page.svelte';

const lease = (id: number, over: Record<string, unknown> = {}) => ({
	id,
	property_name: `${id} Banksia St`,
	property_address: `${id} Banksia St, Fitzroy`,
	start_date: '2026-01-05',
	end_date: null,
	active: true,
	rent_amount: 62000,
	frequency: 'weekly',
	arrears: 0,
	invoiced_not_due: 0,
	...over
});

function entries(n: number) {
	return Array.from({ length: n }, (_, i) =>
		i % 2 === 0
			? { date: '2026-11-05', kind: 'payment', description: 'Payment · Bank transfer', debit: 0, credit: 62000, balance: 0, payment_id: 100 + i }
			: { date: '2026-11-05', kind: 'charge', description: `Rent ${i}`, debit: 62000, credit: 0, balance: 62000 }
	);
}

const account = (over: Record<string, unknown> = {}) => ({
	lease: { id: 5, property_address: '5 Banksia St, Fitzroy', start_date: '2026-01-05', end_date: null, frequency: 'weekly', rent_amount: 62000 },
	summary: { paid_to: '2026-11-27', owing_now: 0, next_due: { date: '2026-11-28', amount: 62000 }, balance: 0 },
	entries: entries(2),
	...over
});

beforeEach(() => setPage({ params: { org: 'acme' }, url: new URL('http://localhost/portal/acme/rent') }));

describe('portal rent page', () => {
	it('summarises the lease and lists entries newest first with receipts on payments only', async () => {
		mockApi({ 'GET /api/v1/portal/acme/leases': [lease(5)], 'GET /api/v1/portal/acme/leases/5/account': account() });
		render(Page);

		expect(await screen.findByText('Paid to')).toBeTruthy();
		expect(screen.getByText('Owing now')).toBeTruthy();
		expect(screen.getByText(/Next due/)).toBeTruthy();
		const rows = within(screen.getByRole('table')).getAllByRole('row').slice(1);
		expect(rows).toHaveLength(2);
		expect(within(rows[0]).getByRole('link', { name: /Receipt/ }).getAttribute('href')).toBe('/portal/acme/rent/5/receipts/100');
		expect(within(rows[1]).queryByRole('link', { name: /Receipt/ })).toBeNull();
	});

	it('shows a negative balance as in credit', async () => {
		mockApi({
			'GET /api/v1/portal/acme/leases': [lease(5)],
			'GET /api/v1/portal/acme/leases/5/account': account({ summary: { paid_to: null, owing_now: 0, next_due: null, balance: -10000 } })
		});
		render(Page);
		expect(await screen.findByText(/in credit/i)).toBeTruthy();
	});

	it('shows 20 entries and reveals earlier ones on request', async () => {
		mockApi({ 'GET /api/v1/portal/acme/leases': [lease(5)], 'GET /api/v1/portal/acme/leases/5/account': account({ entries: entries(25) }) });
		render(Page);
		await screen.findByText('Paid to');
		expect(within(screen.getByRole('table')).getAllByRole('row')).toHaveLength(21);
		await fireEvent.click(screen.getByRole('button', { name: 'Show earlier' }));
		expect(within(screen.getByRole('table')).getAllByRole('row')).toHaveLength(26);
		expect(screen.queryByRole('button', { name: 'Show earlier' })).toBeNull();
	});

	it('defaults the statement to the current financial year', async () => {
		mockApi({ 'GET /api/v1/portal/acme/leases': [lease(5)], 'GET /api/v1/portal/acme/leases/5/account': account() });
		render(Page);
		await screen.findByText('Paid to');
		const fy = currentFY(todayLocal());
		const pdf = screen.getByRole('link', { name: /PDF/ }).getAttribute('href');
		expect(pdf).toBe(`/api/v1/portal/acme/leases/5/statement.pdf?from=${fy - 1}-07-01&to=${fy}-06-30`);
		expect(screen.getByRole('link', { name: /CSV/ }).getAttribute('href')).toContain('statement.csv?from=');
	});

	it('uses a custom range and blocks a backwards one', async () => {
		mockApi({ 'GET /api/v1/portal/acme/leases': [lease(5)], 'GET /api/v1/portal/acme/leases/5/account': account() });
		render(Page);
		await screen.findByText('Paid to');
		await fireEvent.change(screen.getByLabelText('Statement period'), { target: { value: 'custom' } });
		await fireEvent.input(screen.getByLabelText('From'), { target: { value: '2026-03-01' } });
		await fireEvent.input(screen.getByLabelText('To'), { target: { value: '2026-02-01' } });
		expect(await screen.findByText('From must be on or before To.')).toBeTruthy();
		expect(screen.queryByRole('link', { name: /PDF/ })).toBeNull();
		await fireEvent.input(screen.getByLabelText('To'), { target: { value: '2026-04-30' } });
		expect(screen.getByRole('link', { name: /PDF/ }).getAttribute('href')).toBe('/api/v1/portal/acme/leases/5/statement.pdf?from=2026-03-01&to=2026-04-30');
	});

	it('offers a lease selector only with several leases, current first', async () => {
		mockApi({
			'GET /api/v1/portal/acme/leases': [lease(3, { active: false, start_date: '2024-01-01' }), lease(5)],
			'GET /api/v1/portal/acme/leases/5/account': account(),
			'GET /api/v1/portal/acme/leases/3/account': account({ lease: { ...account().lease, id: 3, property_address: '3 Banksia St, Fitzroy' } })
		});
		render(Page);
		const select = (await screen.findByLabelText('Lease')) as HTMLSelectElement;
		expect(select.value).toBe('5');
		await fireEvent.change(select, { target: { value: '3' } });
		expect(await screen.findByText('3 Banksia St, Fitzroy')).toBeTruthy();
	});

	it('hides the lease selector for one lease', async () => {
		mockApi({ 'GET /api/v1/portal/acme/leases': [lease(5)], 'GET /api/v1/portal/acme/leases/5/account': account() });
		render(Page);
		await screen.findByText('Paid to');
		expect(screen.queryByLabelText('Lease')).toBeNull();
	});

	it('shows an empty state without leases', async () => {
		mockApi({ 'GET /api/v1/portal/acme/leases': [] });
		render(Page);
		expect(await screen.findByText('No leases yet')).toBeTruthy();
	});

	it('retries after a failed load', async () => {
		let fail = true;
		mockApi({
			'GET /api/v1/portal/acme/leases': () => (fail ? apiError(500, 'boom') : [lease(5)]),
			'GET /api/v1/portal/acme/leases/5/account': account()
		});
		render(Page);
		expect(await screen.findByText('boom')).toBeTruthy();
		fail = false;
		await fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
		expect(await screen.findByText('Paid to')).toBeTruthy();
	});

	it('ignores a slower response for a lease no longer selected', async () => {
		const slow = deferred();
		mockApi({
			'GET /api/v1/portal/acme/leases': [lease(5), lease(3, { active: false })],
			'GET /api/v1/portal/acme/leases/5/account': slow.promise,
			'GET /api/v1/portal/acme/leases/3/account': account({ lease: { ...account().lease, id: 3, property_address: '3 Banksia St, Fitzroy' } })
		});
		render(Page);
		const select = (await screen.findByLabelText('Lease')) as HTMLSelectElement;
		await fireEvent.change(select, { target: { value: '3' } });
		expect(await screen.findByRole('heading', { name: '3 Banksia St, Fitzroy' })).toBeTruthy();
		slow.resolve(account());
		await new Promise((r) => setTimeout(r, 0));
		expect(screen.getByRole('heading', { name: '3 Banksia St, Fitzroy' })).toBeTruthy();
		expect(screen.queryByRole('heading', { name: '5 Banksia St, Fitzroy' })).toBeNull();
	});
});
