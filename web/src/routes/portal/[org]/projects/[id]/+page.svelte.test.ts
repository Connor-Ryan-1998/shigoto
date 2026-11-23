import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen, within } from '@testing-library/svelte';
import { setPage } from '../../../../../lib/test/app.svelte';
import { mockApi } from '../../../../../lib/test/mock-api';
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
		const inactive = screen.getByText('Old NDA').closest('li')!;
		expect(within(inactive).queryByRole('link', { name: /sign/i })).toBeNull();
		expect(within(inactive).getByText('Withdrawn')).toBeTruthy();
		expect(screen.getByRole('link', { name: /142/ }).getAttribute('href')).toBe('/portal/acme/invoices/9');
	});
});
