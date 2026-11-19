import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { setPage } from '../../../../../../../lib/test/app.svelte';
import { apiError, mockApi } from '../../../../../../../lib/test/mock-api';

import Page from './+page.svelte';

const receipt = {
	org_name: 'Ryan Properties',
	org_abn: '51 824 753 556',
	tenant_names: ['Tina Tenant', 'Co Tenant'],
	property_address: '5 Banksia St, Fitzroy',
	payment: { id: 11, amount: 62000, paid_on: '2026-11-21', method: 'bank_transfer', reference: 'REF-1' },
	invoice: { id: 9, number: 42, total: 62000, period: 'Rent 21–27 Nov 2026' },
	balance_after: 0,
	generated_at: '2026-11-21T10:00:00Z'
};

beforeEach(() =>
	setPage({ params: { org: 'acme', leaseId: '5', paymentId: '11' }, url: new URL('http://localhost/portal/acme/rent/5/receipts/11') })
);

describe('portal payment receipt', () => {
	it('shows the receipt details', async () => {
		mockApi({ 'GET /api/v1/portal/acme/leases/5/payments/11/receipt': receipt });
		render(Page);
		expect(await screen.findByText('Ryan Properties')).toBeTruthy();
		expect(screen.getByText('Tina Tenant, Co Tenant')).toBeTruthy();
		expect(screen.getByText('5 Banksia St, Fitzroy')).toBeTruthy();
		expect(screen.getByText('Bank transfer')).toBeTruthy();
		expect(screen.getByText('Rent 21–27 Nov 2026')).toBeTruthy();
		expect(screen.getByText('REF-1')).toBeTruthy();
		expect(screen.getByText('#42')).toBeTruthy();
	});

	it('prints', async () => {
		const print = vi.spyOn(window, 'print').mockImplementation(() => {});
		mockApi({ 'GET /api/v1/portal/acme/leases/5/payments/11/receipt': receipt });
		render(Page);
		await fireEvent.click(await screen.findByRole('button', { name: 'Print' }));
		expect(print).toHaveBeenCalled();
	});

	it('shows not found for a payment that is not yours', async () => {
		mockApi({ 'GET /api/v1/portal/acme/leases/5/payments/11/receipt': apiError(404, 'not found') });
		render(Page);
		expect(await screen.findByText("Couldn't load the receipt")).toBeTruthy();
	});
});
