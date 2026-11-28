import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import { setPage } from '../../../../../lib/test/app.svelte';
import { deferred, mockApi } from '../../../../../lib/test/mock-api';
import { signIn } from '../../../../../lib/test/session';
import Page from './+page.svelte';

const base = '/api/v1/orgs/acme/properties';
const scheme = (held: Partial<Record<'is_chairperson' | 'is_secretary' | 'is_treasurer', boolean>>, extra = {}) => ({
	id: 3, name: 'Harbour View CTS', cts_number: '12345', is_chairperson: false, is_secretary: false, is_treasurer: false,
	properties: [{ id: 1, name: 'Cottage' }], ...held, ...extra
});
const prop = (id: number, name: string, strata_scheme: unknown) => ({ id, name, strata_scheme });
const roleHeadings = () =>
	screen.queryAllByRole('heading', { level: 3 }).map((h) => h.textContent).filter((t) => t !== 'Documents');
const docs = (id = 3) => ({ [`GET /api/v1/orgs/acme/strata-schemes/${id}/documents`]: [] });

beforeEach(() => {
	setPage({ params: { org: 'acme', id: '1' } });
	signIn();
});

describe('strata tab', () => {
	it('renders a card per held position in order', async () => {
		mockApi({ [`GET ${base}/1`]: prop(1, 'Cottage', scheme({ is_treasurer: true, is_secretary: true })), ...docs() });
		render(Page);
		expect(await screen.findByRole('heading', { name: 'Harbour View CTS' })).toBeTruthy();
		expect(screen.getByText('CTS 12345')).toBeTruthy();
		expect(roleHeadings()).toEqual(['Secretary', 'Treasurer']);
		expect(screen.getAllByText('Duties for this role will appear here.')).toHaveLength(1);
		expect(screen.getByText('The assistant is not configured.')).toBeTruthy();
	});

	it('renders all three when every position is held', async () => {
		mockApi({ [`GET ${base}/1`]: prop(1, 'Cottage', scheme({ is_chairperson: true, is_secretary: true, is_treasurer: true })), ...docs() });
		render(Page);
		await screen.findByRole('heading', { name: 'Harbour View CTS' });
		expect(roleHeadings()).toEqual(['Chairperson', 'Secretary', 'Treasurer']);
	});

	it('lists other properties in the scheme', async () => {
		mockApi({ [`GET ${base}/1`]: prop(1, 'Cottage', scheme({ is_chairperson: true }, { cts_number: '', properties: [{ id: 1, name: 'Cottage' }, { id: 2, name: 'Unit 2' }] })), ...docs() });
		render(Page);
		expect(await screen.findByText('Shared with: Unit 2')).toBeTruthy();
		expect(screen.getByText('No CTS number')).toBeTruthy();
	});

	it('prompts for positions when none are held', async () => {
		mockApi({ [`GET ${base}/1`]: prop(1, 'Cottage', scheme({})) });
		render(Page);
		expect(await screen.findByText("You don't hold any committee positions in this scheme.")).toBeTruthy();
		expect(screen.getByRole('link', { name: 'Choose positions on Details' }).getAttribute('href')).toBe('/acme/properties/1');
		expect(roleHeadings()).toEqual([]);
	});

	it('shows an empty state for an unlinked property', async () => {
		mockApi({ [`GET ${base}/1`]: prop(1, 'Cottage', null) });
		render(Page);
		expect(await screen.findByText('This property is not part of a strata scheme.')).toBeTruthy();
		expect(screen.getByRole('link', { name: 'Set up strata on Details' }).getAttribute('href')).toBe('/acme/properties/1');
	});

	it('ignores a stale response after switching property', async () => {
		const slow = deferred();
		mockApi({ [`GET ${base}/1`]: slow.promise, [`GET ${base}/2`]: prop(2, 'Flat', scheme({ is_treasurer: true }, { name: 'Flat CTS' })), ...docs() });
		render(Page);
		setPage({ params: { org: 'acme', id: '2' } });
		expect(await screen.findByRole('heading', { name: 'Flat CTS' })).toBeTruthy();
		slow.resolve(prop(1, 'Cottage', scheme({ is_chairperson: true })));
		await waitFor(() => expect(roleHeadings()).toEqual(['Treasurer']));
		expect(screen.queryByRole('heading', { name: 'Harbour View CTS' })).toBeNull();
	});

	it('shows the documents card only for chairperson or treasurer', async () => {
		mockApi({ [`GET ${base}/1`]: prop(1, 'Cottage', scheme({ is_secretary: true })) });
		const { unmount } = render(Page);
		await screen.findByRole('heading', { name: 'Harbour View CTS' });
		expect(screen.queryByRole('heading', { name: 'Documents' })).toBeNull();
		unmount();
		mockApi({ [`GET ${base}/1`]: prop(1, 'Cottage', scheme({ is_treasurer: true })), ...docs() });
		render(Page);
		expect(await screen.findByRole('heading', { name: 'Documents' })).toBeTruthy();
		expect(await screen.findByText('No documents yet')).toBeTruthy();
	});

	it('hides document uploads without property_write', async () => {
		signIn({ role: 'bookkeeper', perms: ['property_read'] });
		mockApi({ [`GET ${base}/1`]: prop(1, 'Cottage', scheme({ is_chairperson: true })), ...docs() });
		render(Page);
		await screen.findByText('No documents yet');
		expect(screen.queryByRole('button', { name: 'Upload document' })).toBeNull();
	});

	it('denies without property_read', async () => {
		signIn({ role: 'staff', perms: [] });
		mockApi({});
		render(Page);
		expect(screen.getByRole('alert').textContent).toContain('You do not have permission to view properties.');
	});
});
