import { beforeEach, describe, expect, it, vi } from 'vitest';
import { beforeNavigate, goto, setPage } from '../../../../lib/test/app.svelte';
import { apiMock } from '../../../../lib/test/mock-api';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { session } from '../../../../lib/session.svelte';

import Page from './+page.svelte';

const projects = [
	{ id: 3, name: 'Kitchen', client_contact_id: 9, status: 'in_progress' },
	{ id: 4, name: 'Not hers', client_contact_id: 10, status: 'in_progress' },
	{ id: 5, name: 'Old job', client_contact_id: 9, status: 'completed' }
];

beforeEach(() => {
	setPage({ params: { org: 'acme' }, url: new URL('http://x/acme/quotes/new') });
	session.setMe({ user: { id: 1, email: 'o@example.com', name: 'Owner' }, orgs: [{ slug: 'acme', name: 'Acme', role: 'owner',
		perms: ['quotes_read', 'quotes_write', 'invoices_read', 'timesheets_read'] }] });
	session.setOrg('acme');
	session.setModules(['timesheets', 'quotes']);
	apiMock.mockImplementation((method: string, path: string) => {
		if (method === 'POST') return Promise.resolve({ id: 42 });
		if (path.includes('/contacts')) return Promise.resolve([{ id: 9, name: 'Priya', kind: 'client' }, { id: 10, name: 'Sam', kind: 'client' }]);
		if (path.endsWith('/projects')) return Promise.resolve(projects);
		if (path.endsWith('/settings')) return Promise.resolve({ gst_registered: true, quote_terms: '50% deposit' });
		return Promise.reject(new Error(`unrouted ${method} ${path}`));
	});
});

describe('new quote', () => {
	it('prefills terms and offers only the customer\'s open projects', async () => {
		render(Page);
		expect(((await screen.findByLabelText('Terms')) as HTMLTextAreaElement).value).toBe('50% deposit');
		const project = screen.getByLabelText('Project') as HTMLSelectElement;
		expect([...project.options].map((o) => o.textContent)).toEqual(['No project', 'Kitchen']);
	});

	it('saves a draft and opens it', async () => {
		render(Page);
		await screen.findByLabelText('Terms');
		await fireEvent.change(screen.getByLabelText('Project'), { target: { value: '3' } });
		await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Switchboard upgrade' } });
		await fireEvent.input(screen.getByLabelText('Description'), { target: { value: 'Rough-in' } });
		await fireEvent.input(screen.getByLabelText('Hours'), { target: { value: '2.5' } });
		await fireEvent.input(screen.getByLabelText('Rate ($/h)'), { target: { value: '95' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Save draft' }));
		await vi.waitFor(() => expect(goto).toHaveBeenCalledWith('/acme/quotes/42'));
		expect(apiMock).toHaveBeenCalledWith('POST', '/api/v1/orgs/acme/quotes', expect.objectContaining({
			contact_id: 9, project_id: 3, title: 'Switchboard upgrade', terms: '50% deposit',
			lines: [{ kind: 'labour', description: 'Rough-in', quantity_milli: 2500, unit_amount: 9500, gst_code: 'GST' }]
		}));
	});

	it('refuses an incomplete line without calling the API', async () => {
		render(Page);
		await screen.findByLabelText('Terms');
		await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Job' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Save draft' }));
		expect(await screen.findByText('Complete every line before saving.')).toBeTruthy();
		expect(apiMock.mock.calls.some(([m]) => m === 'POST')).toBe(false);
	});

	it('defaults line GST to GST-free when the org is not registered', async () => {
		apiMock.mockImplementation((method: string, path: string) => {
			if (path.includes('/contacts')) return Promise.resolve([{ id: 9, name: 'Priya', kind: 'client' }]);
			if (path.endsWith('/projects')) return Promise.resolve([]);
			if (path.endsWith('/settings')) return Promise.resolve({ gst_registered: false, quote_terms: '' });
			return Promise.resolve({});
		});
		render(Page);
		await screen.findByLabelText('Terms');
		expect((screen.getByLabelText('GST code') as HTMLSelectElement).value).toBe('FRE');
	});

	it('denies users without quotes_write', async () => {
		session.setMe({ user: { id: 1, email: 'b@example.com', name: 'Keeper' }, orgs: [{ slug: 'acme', name: 'Acme', role: 'bookkeeper', perms: ['quotes_read'] }] });
		render(Page);
		expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'You do not have permission to create quotes.');
		expect(apiMock).not.toHaveBeenCalled();
	});

	it('is not blocked by its own unsaved-changes guard when opening the saved quote', async () => {
		let cancelled = false;
		goto.mockImplementation((to: string) => {
			const guard = beforeNavigate.mock.calls.at(-1)![0] as (n: { to: { url: URL }; cancel: () => void }) => void;
			guard({ to: { url: new URL(`http://x${to}`) }, cancel: () => { cancelled = true; } });
			return Promise.resolve();
		});
		render(Page);
		await screen.findByLabelText('Terms');
		await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Job' } });
		await fireEvent.input(screen.getByLabelText('Description'), { target: { value: 'Work' } });
		await fireEvent.input(screen.getByLabelText('Hours'), { target: { value: '1' } });
		await fireEvent.input(screen.getByLabelText('Rate ($/h)'), { target: { value: '95' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Save draft' }));
		await vi.waitFor(() => expect(goto).toHaveBeenCalledWith('/acme/quotes/42'));
		expect(cancelled).toBe(false);
	});
});
