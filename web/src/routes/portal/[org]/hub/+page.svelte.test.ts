import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/svelte';
import { setPage } from '../../../../lib/test/app.svelte';
import { mockApi } from '../../../../lib/test/mock-api';
import Page from './+page.svelte';

const hub = (over = {}) => ({
	welcome: { notes: '<b>Slack</b> #acme\nEmail for contracts' },
	checklist: [
		{ id: 1, position: 1, action: 'Grant analytics access', owner: 'Sam', due_date: '2026-11-25', done_at: null },
		{ id: 2, position: 2, action: 'Send brand PDF', owner: 'Priya', due_date: null, done_at: '2026-11-22T00:00:00Z' }
	],
	projects: [
		{ id: 5, name: 'Website rebuild', status: 'pending', start_date: '2026-12-01', end_date: null, to_sign: 1 },
		{ id: 6, name: 'Old thing', status: 'cancelled', start_date: null, end_date: null, to_sign: 0 }
	],
	documents: { to_sign: 2, signed: 1, shared: 0 },
	...over
});
const docs = [{ id: 3, project_id: null, project_name: '', title: 'MSA', kind: 'signature', category: 'general', status: 'signed', sent_at: null, applies_to_all: false, file: { id: 1, mime: 'application/pdf', size_bytes: 1, original_name: 'msa.pdf' }, signature: null }];

beforeEach(() => setPage({ params: { org: 'acme' }, url: new URL('http://localhost/portal/acme/hub') }));

describe('client hub', () => {
	it('shows the signature banner, welcome notes as text, checklist and projects', async () => {
		mockApi({ 'GET /api/v1/portal/acme/hub': hub(), 'GET /api/v1/portal/acme/documents': docs });
		render(Page);
		const banner = await screen.findByRole('link', { name: /2 documents need your signature/ });
		expect(banner.getAttribute('href')).toBe('/portal/acme/documents?filter=to_sign');
		expect(screen.getByText(/<b>Slack<\/b> #acme/)).toBeTruthy();
		const checklist = screen.getByRole('heading', { name: 'Client action checklist' }).closest('section')!;
		expect(within(checklist).getByText('Grant analytics access')).toBeTruthy();
		const project = screen.getByRole('link', { name: /Website rebuild/ });
		expect(project.getAttribute('href')).toBe('/portal/acme/projects/5');
		expect(project.textContent).toContain('1 to sign');
		expect(screen.queryByText('Old thing')).toBeNull();
		await fireEvent.click(screen.getByRole('button', { name: 'Show cancelled (1)' }));
		expect(screen.getByText('Old thing')).toBeTruthy();
		expect(screen.getByRole('link', { name: /MSA/ })).toBeTruthy();
	});

	it('hides the banner when nothing needs signing', async () => {
		mockApi({ 'GET /api/v1/portal/acme/hub': hub({ documents: { to_sign: 0, signed: 0, shared: 0 }, projects: [] }), 'GET /api/v1/portal/acme/documents': [] });
		render(Page);
		await screen.findByRole('heading', { name: 'Welcome' });
		expect(screen.queryByText(/need your signature/)).toBeNull();
		expect(screen.queryByRole('heading', { name: 'Projects' })).toBeNull();
	});
});
