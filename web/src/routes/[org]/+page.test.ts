import { beforeEach, describe, expect, it } from 'vitest';
import { setPage } from '../../lib/test/app.svelte';
import { apiMock, apiError, mockApi, deferred } from '../../lib/test/mock-api';
import { signIn } from '../../lib/test/session';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { tick } from 'svelte';
import Page from './+page.svelte';

const emptyCore = { open_count: 0, open_outstanding: 0, overdue_count: 0, overdue_outstanding: 0, draft_count: 0, invoice_count: 0 };
const cashflow = { from: '2026-07-01', to: '2027-06-30', months: [] };
const reconciliation = { unmatched_count: 2, oldest_unmatched_on: null, unmatched_in: 0, unmatched_out: 0 };

beforeEach(() => {
	setPage({ params: { org: 'acme' } });
	signIn();
});

describe('[org] dashboard', () => {
	it('shows invoice onboarding without fetching hidden cash flow', async () => {
		mockApi({ 'GET /api/v1/orgs/acme/dashboard': { core: emptyCore, modules: {} } });
		render(Page);
		expect(await screen.findByText('No invoices yet — create one')).toBeTruthy();
		expect(screen.queryByRole('heading', { name: 'Total Receivables' })).toBeNull();
		expect(screen.queryByRole('heading', { name: 'Cash Flow' })).toBeNull();
		expect(apiMock).toHaveBeenCalledTimes(1);
	});

	it('shows module cards and cash flow even before the first invoice', async () => {
		mockApi({
			'GET /api/v1/orgs/acme/cash-flow?period=current': cashflow,
			'GET /api/v1/orgs/acme/dashboard': {
				core: emptyCore,
				modules: {
					reconciliation,
					property: {
						arrears_total: 0, unissued_total: 0, bonds_unlodged_overdue: 0, insurance_renewing_soon: 0,
						repairs: { open_count: 1, items: [{ ticket_id: 7, property_id: 3, property_address: '12 Banksia St', ref: 'R-0001', title: 'Burst pipe', urgency: 'urgent', status: 'new' }] }
					}
				}
			}
		});
		render(Page);
		expect(await screen.findByText('No invoices yet — create one')).toBeTruthy();
		expect(await screen.findByRole('heading', { name: 'Repairs' })).toBeTruthy();
		expect(await screen.findByRole('heading', { name: 'Cash Flow' })).toBeTruthy();
		expect(screen.getByRole('link', { name: /reconciliation/i }).getAttribute('href')).toBe('/acme/reconciliation');
	});

	it('shows zero receivables instead of onboarding for a settled org', async () => {
		mockApi({ 'GET /api/v1/orgs/acme/dashboard': { core: { ...emptyCore, invoice_count: 3 }, modules: {} } });
		render(Page);
		expect(await screen.findByRole('heading', { name: 'Total Receivables' })).toBeTruthy();
		expect(screen.queryByText('No invoices yet — create one')).toBeNull();
		expect(screen.getByRole('link', { name: /current/i }).getAttribute('href')).toBe('/acme/invoices?status=open');
	});

	it('keeps cards visible when cash flow fails and changing period never reloads the dashboard', async () => {
		mockApi({
			'GET /api/v1/orgs/acme/dashboard': { core: { ...emptyCore, invoice_count: 1 }, modules: { reconciliation } },
			'GET /api/v1/orgs/acme/cash-flow?period=current': apiError(500, 'Cash unavailable'),
			'GET /api/v1/orgs/acme/cash-flow?period=previous': cashflow
		});
		render(Page);
		await screen.findByRole('alert');
		expect(screen.getByRole('heading', { name: 'Total Receivables' })).toBeTruthy();
		expect(screen.getByRole('heading', { name: 'Reconciliation' })).toBeTruthy();
		await fireEvent.change(screen.getByLabelText('Cash flow period'), { target: { value: 'previous' } });
		await screen.findByRole('group', { name: 'Cash flow totals' });
		expect(apiMock.mock.calls.filter(c => c[1].endsWith('/dashboard'))).toHaveLength(1);
	});

	it('discards a late dashboard response from the previous org', async () => {
		const old = deferred();
		mockApi({
			'GET /api/v1/orgs/acme/dashboard': old.promise,
			'GET /api/v1/orgs/other/dashboard': { core: { ...emptyCore, invoice_count: 1 }, modules: {} }
		});
		render(Page);
		signIn({ org: 'other' });
		setPage({ params: { org: 'other' } });
		await screen.findByRole('heading', { name: 'Total Receivables' });
		old.resolve({ core: emptyCore, modules: { reconciliation } });
		await tick();
		expect(screen.queryByText('No invoices yet — create one')).toBeNull();
		expect(screen.queryByRole('heading', { name: 'Cash Flow' })).toBeNull();
	});
});
