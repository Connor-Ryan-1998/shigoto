import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { setPage } from '../../../../../lib/test/app.svelte';
import { apiError, mockApi } from '../../../../../lib/test/mock-api';
import Page from './+page.svelte';

beforeEach(() => setPage({ params: { org: 'acme', id: '7' }, url: new URL('http://localhost/portal/acme/quotes/7') }));

describe('portal quote', () => {
	it('shows the quote and links its PDF', async () => {
		mockApi({ 'GET /api/v1/portal/acme/quotes/7': {
			id: 7, number_label: 'Q-0007', title: 'Switchboard', project_id: 5, project_name: 'Kitchen', status: 'sent', display_status: 'expired',
			issue_date: '2026-11-01', valid_until: '2026-11-15', subtotal: 48000, gst_total: 4800, total: 52800, terms: '50% deposit',
			lines: [
				{ kind: 'labour', description: 'Rough-in', quantity_milli: 2500, unit_amount: 12000, gst_code: 'GST', amount: 30000 },
				{ kind: 'material', description: 'Switchboard unit', quantity_milli: 1000, unit_amount: 18000, gst_code: 'GST', amount: 18000 }
			]
		} });
		render(Page);
		expect(await screen.findByRole('heading', { name: 'Q-0007 · Switchboard' })).toBeTruthy();
		expect(screen.getByText('Expired')).toBeTruthy();
		expect(screen.getByText('Rough-in')).toBeTruthy();
		expect(screen.getByText('Switchboard unit')).toBeTruthy();
		expect(screen.getByText('50% deposit')).toBeTruthy();
		expect(screen.getByRole('link', { name: 'Download PDF' }).getAttribute('href')).toBe('/api/v1/portal/acme/quotes/7/pdf');
		expect(screen.queryByText(/attachment/i)).toBeNull();
	});

	it('says when the quote is not found', async () => {
		mockApi({ 'GET /api/v1/portal/acme/quotes/7': apiError(404, 'not found') });
		render(Page);
		expect(await screen.findByText('Quote not found.')).toBeTruthy();
	});
});
