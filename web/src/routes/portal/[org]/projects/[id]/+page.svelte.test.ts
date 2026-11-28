import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/svelte';
import { setPage } from '../../../../../lib/test/app.svelte';
import { mockApi } from '../../../../../lib/test/mock-api';
import { session } from '../../../../../lib/session.svelte';
import Page from './+page.svelte';

const file = { id: 1, mime: 'application/pdf', size_bytes: 1, original_name: 'x.pdf' };
const d = (over: object) => ({ id: 1, project_id: 5, project_name: 'Website rebuild', title: 'Doc', kind: 'signature', category: 'general', status: 'sent', sent_at: null, applies_to_all: false, file, signature: null, ...over });

beforeEach(() => setPage({ params: { org: 'acme', id: '5' }, url: new URL('http://localhost/portal/acme/projects/5') }));

describe('client project page', () => {
	it('shows summary, goals, stakeholders, grouped documents and invoices', async () => {
		mockApi({
			'GET /api/v1/portal/acme/projects/5': {
				id: 5, name: 'Website rebuild', status: 'pending', summary: 'Rebuild the site', goals: ['Bookings live'], stakeholders: ['Priya Nair — Sponsor'],
				start_date: '2026-12-01', end_date: null,
				documents: {
					to_sign: [d({ id: 2, title: 'SOW' })],
					signed: [d({ id: 3, title: 'MSA', project_id: null, status: 'signed', applies_to_all: true, signature: { typed_name: 'Priya', email: 'p@x', signed_at: '2026-11-03T00:00:00Z' } })],
					discovery: [d({ id: 4, title: 'Workshop notes', kind: 'shared', category: 'discovery' })],
					shared: [],
					inactive: [d({ id: 5, title: 'Old NDA', status: 'withdrawn' })]
				},
				invoices: [{ id: 9, number: 142, status: 'open', total: 50000, due_date: '2026-12-05' }]
			}
		});
		render(Page);
		expect(await screen.findByRole('heading', { name: 'Website rebuild' })).toBeTruthy();
		expect(screen.getByText('Rebuild the site')).toBeTruthy();
		expect(screen.getByText('Bookings live')).toBeTruthy();
		expect(screen.getByText('Priya Nair — Sponsor')).toBeTruthy();
		const toSign = screen.getByRole('heading', { name: /To sign/ }).closest('section')!;
		expect(within(toSign).getByRole('link', { name: 'Review and sign SOW' }).getAttribute('href')).toBe('/portal/acme/documents/2');
		const signed = screen.getByRole('heading', { name: 'Signed' }).closest('section')!;
		expect(within(signed).getByText('Applies to all projects')).toBeTruthy();
		const pdfs = screen.getAllByRole('link', { name: /^Download / });
		expect(pdfs.length).toBeGreaterThan(0);
		for (const pdf of pdfs) {
			expect(pdf.getAttribute('href')).toMatch(/^\/api\/v1\/portal\/acme\/documents\/\d+\/file$/);
			expect(pdf.hasAttribute('download')).toBe(true);
		}
		const inactive = screen.getByText('Old NDA').closest('li')!;
		expect(within(inactive).queryByRole('link', { name: /sign/i })).toBeNull();
		expect(within(inactive).getByText('Withdrawn')).toBeTruthy();
		expect(screen.getByRole('link', { name: /142/ }).getAttribute('href')).toBe('/portal/acme/invoices/9');
	});

	it('lists the project\'s quotes when the quotes module is on', async () => {
		session.setModules(['timesheets', 'quotes']);
		mockApi({
			'GET /api/v1/portal/acme/projects/5': {
				id: 5, name: 'Website rebuild', status: 'pending', summary: '', goals: [], stakeholders: [], start_date: null, end_date: null,
				documents: { to_sign: [], signed: [], discovery: [], shared: [], inactive: [] }, invoices: []
			},
			'GET /api/v1/portal/acme/quotes?project_id=5': { quotes: [{ id: 7, number_label: 'Q-0007', title: 'Switchboard', project_id: 5, project_name: 'Website rebuild', status: 'accepted', display_status: 'accepted', issue_date: '2026-11-27', valid_until: '2026-12-27', total: 52800 }] }
		});
		render(Page);
		const quotes = (await screen.findByRole('heading', { name: 'Quotes' })).closest('section')!;
		expect((await within(quotes).findByRole('link', { name: /Q-0007/ })).getAttribute('href')).toBe('/portal/acme/quotes/7');
	});
});
