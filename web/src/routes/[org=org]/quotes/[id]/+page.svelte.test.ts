import { beforeEach, describe, expect, it, vi } from 'vitest';
import { goto, setPage } from '../../../../lib/test/app.svelte';
import { apiMock } from '../../../../lib/test/mock-api';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { session } from '../../../../lib/session.svelte';

import Page from './+page.svelte';

let detail: Record<string, unknown>;

function base(over: Record<string, unknown> = {}) {
	return {
		id: 7, contact_id: 9, contact_name: 'Priya', project_id: null, project_name: '', number: 7, revision: 1,
		number_label: 'Q-0007', parent_id: null, title: 'Switchboard', terms: '50% deposit', status: 'draft', display_status: 'draft',
		issue_date: '2026-11-27', valid_until: '2026-12-27', subtotal: 48000, gst_total: 4800, total: 52800,
		sent_at: null, decided_at: null,
		lines: [
			{ id: 1, kind: 'labour', description: 'Rough-in', quantity_milli: 2500, unit_amount: 12000, gst_code: 'GST', gst_amount: 3000, amount: 30000 },
			{ id: 2, kind: 'material', description: 'Switchboard', quantity_milli: 1000, unit_amount: 18000, gst_code: 'GST', gst_amount: 1800, amount: 18000 }
		],
		attachments: [{ id: 5, created_at: '2026-11-27T00:00:00Z', file: { id: 11, mime: 'image/png', size_bytes: 10, original_name: 'site.png' } }],
		revisions: [{ id: 7, number_label: 'Q-0007', status: 'draft' }],
		...over
	};
}

function signIn(role: string, perms: string[]) {
	session.setMe({ user: { id: 1, email: 'u@example.com', name: 'U' }, orgs: [{ slug: 'acme', name: 'Acme', role, perms }] });
	session.setOrg('acme');
}

const OWNER = ['quotes_read', 'quotes_write', 'timesheets_admin', 'timesheets_read'];

beforeEach(() => {
	setPage({ params: { org: 'acme', id: '7' }, url: new URL('http://x/acme/quotes/7') });
	signIn('owner', OWNER);
	detail = base();
	apiMock.mockImplementation((method: string, path: string) => {
		if (method === 'GET' && path.endsWith('/quotes/7')) return Promise.resolve(detail);
		if (path.endsWith('/accept')) return Promise.resolve({ quote: { ...detail, status: 'accepted' }, project: { id: 3 } });
		if (path.endsWith('/revise')) return Promise.resolve({ id: 12 });
		if (path.endsWith('/send')) return Promise.resolve({ ...detail, status: 'sent', display_status: 'sent' });
		return Promise.resolve(undefined);
	});
	goto.mockReset();
});

describe('quote detail', () => {
	it('shows lines grouped by kind and totals', async () => {
		render(Page);
		expect(await screen.findByRole('heading', { name: 'Labour' })).toBeTruthy();
		expect(screen.getByRole('heading', { name: 'Materials' })).toBeTruthy();
		expect(screen.getByText('Rough-in')).toBeTruthy();
		expect(screen.getAllByText('$528.00').length).toBeGreaterThan(0);
		expect(screen.getByText('50% deposit')).toBeTruthy();
	});

	it('offers draft actions', async () => {
		render(Page);
		await screen.findByText('Rough-in');
		for (const name of ['Edit', 'Mark sent', 'Delete']) expect(screen.getByRole('button', { name })).toBeTruthy();
		expect(screen.getByRole('link', { name: 'Download PDF' }).getAttribute('href')).toBe('/api/v1/orgs/acme/quotes/7/pdf');
		expect(screen.queryByRole('button', { name: 'Accept' })).toBeNull();
	});

	it('marks a draft sent', async () => {
		render(Page);
		await fireEvent.click(await screen.findByRole('button', { name: 'Mark sent' }));
		await vi.waitFor(() => expect(apiMock).toHaveBeenCalledWith('POST', '/api/v1/orgs/acme/quotes/7/send'));
	});

	it('accepts an expired quote once and opens the project', async () => {
		detail = base({ status: 'sent', display_status: 'expired' });
		render(Page);
		expect(await screen.findByText('Expired')).toBeTruthy();
		const accept = screen.getByRole('button', { name: 'Accept' });
		await fireEvent.click(accept);
		await fireEvent.click(accept);
		await vi.waitFor(() => expect(goto).toHaveBeenCalledWith('/acme/projects/3'));
		expect(apiMock.mock.calls.filter(([, p]) => String(p).endsWith('/accept'))).toHaveLength(1);
	});

	it('revises a sent quote into an editable draft', async () => {
		detail = base({ status: 'sent', display_status: 'sent' });
		render(Page);
		await fireEvent.click(await screen.findByRole('button', { name: 'Revise' }));
		await vi.waitFor(() => expect(goto).toHaveBeenCalledWith('/acme/quotes/12/edit'));
	});

	it('hides Accept from staff who cannot create projects', async () => {
		signIn('staff', ['quotes_read', 'quotes_write', 'timesheets_read']);
		detail = base({ status: 'sent', display_status: 'sent' });
		render(Page);
		expect(await screen.findByRole('button', { name: 'Decline' })).toBeTruthy();
		expect(screen.queryByRole('button', { name: 'Accept' })).toBeNull();
	});

	it('links an accepted quote to its project', async () => {
		detail = base({ status: 'accepted', display_status: 'accepted', project_id: 3, project_name: 'Kitchen' });
		render(Page);
		expect((await screen.findByRole('link', { name: 'Open project' })).getAttribute('href')).toBe('/acme/projects/3');
		expect(screen.queryByRole('button', { name: 'Revise' })).toBeNull();
	});

	it('gives readers no write actions', async () => {
		signIn('bookkeeper', ['quotes_read']);
		detail = base({ status: 'sent', display_status: 'sent' });
		render(Page);
		await screen.findByText('Rough-in');
		for (const name of ['Accept', 'Decline', 'Revise', 'Upload attachment']) expect(screen.queryByRole('button', { name })).toBeNull();
		expect(screen.getByRole('link', { name: 'Download PDF' })).toBeTruthy();
		expect(screen.getByRole('link', { name: 'site.png' })).toBeTruthy();
	});

	it('uploads and removes attachments', async () => {
		render(Page);
		await screen.findByText('site.png');
		const file = new File(['%PDF'], 'sow.pdf', { type: 'application/pdf' });
		await fireEvent.change(screen.getByLabelText('Attach a file'), { target: { files: [file] } });
		await vi.waitFor(() => expect(apiMock).toHaveBeenCalledWith('POST', '/api/v1/orgs/acme/quotes/7/attachments', expect.any(FormData)));
		await fireEvent.click(screen.getByRole('button', { name: 'Remove site.png' }));
		await vi.waitFor(() => expect(apiMock).toHaveBeenCalledWith('DELETE', '/api/v1/orgs/acme/quotes/7/attachments/5'));
	});
});
