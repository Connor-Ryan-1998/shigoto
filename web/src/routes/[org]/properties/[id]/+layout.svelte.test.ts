import { beforeEach, expect, it } from 'vitest';
import { createRawSnippet } from 'svelte';
import { render, screen, waitFor } from '@testing-library/svelte';
import { setPage } from '../../../../lib/test/app.svelte';
import { mockApi } from '../../../../lib/test/mock-api';
import { signIn } from '../../../../lib/test/session';
import { propertyHeader } from '../../../../lib/modules/property/header.svelte';
import Layout from './+layout.svelte';

const children = createRawSnippet(() => ({ render: () => '<p>Tab body</p>' }));
const scheme = { id: 3, name: 'Harbour View CTS', cts_number: '', is_chairperson: true, is_secretary: false, is_treasurer: false, properties: [] };
const tabs = () => [...screen.getByRole('navigation', { name: 'Property sections' }).querySelectorAll('a')].map((a) => a.textContent);

beforeEach(() => {
	signIn();
	propertyHeader.key = ''; propertyHeader.name = ''; propertyHeader.strata = false;
	mockApi({ 'GET /api/v1/orgs/acme/properties/1': { id: 1, name: 'Cottage', strata_scheme: null } });
});

it('titles the page with the property and marks the active section', async () => {
	setPage({ params: { org: 'acme', id: '1' }, url: '/acme/properties/1/bills' });
	render(Layout, { children });
	expect(await screen.findByRole('heading', { name: 'Cottage' })).toBeTruthy();
	const nav = screen.getByRole('navigation', { name: 'Property sections' });
	const links = [...nav.querySelectorAll('a')].map((a) => [a.textContent, a.getAttribute('href'), a.getAttribute('aria-current')]);
	expect(links).toEqual([
		['Details', '/acme/properties/1', null],
		['Bills', '/acme/properties/1/bills', 'page'],
		['Depreciation', '/acme/properties/1/depreciation', null],
		['Notes', '/acme/properties/1/notes', null],
		['Maintenance', '/acme/properties/1/maintenance', null]
	]);
	expect(screen.getByRole('link', { name: 'Back to properties' }).getAttribute('href')).toBe('/acme/properties');
	expect(screen.getByText('Tab body')).toBeTruthy();
});

it('shows the Strata tab only for a linked property', async () => {
	mockApi({ 'GET /api/v1/orgs/acme/properties/1': { id: 1, name: 'Cottage', strata_scheme: scheme } });
	setPage({ params: { org: 'acme', id: '1' }, url: '/acme/properties/1/strata' });
	render(Layout, { children });
	await waitFor(() => expect(tabs()).toEqual(['Details', 'Strata', 'Bills', 'Depreciation', 'Notes', 'Maintenance']));
	expect(screen.getByRole('link', { name: 'Strata' }).getAttribute('href')).toBe('/acme/properties/1/strata');
	expect(screen.getByRole('link', { name: 'Strata' }).getAttribute('aria-current')).toBe('page');
});

it('linking shows the Strata tab', async () => {
	setPage({ params: { org: 'acme', id: '1' }, url: '/acme/properties/1' });
	render(Layout, { children });
	await screen.findByRole('heading', { name: 'Cottage' });
	expect(tabs()).not.toContain('Strata');
	propertyHeader.strata = true;
	await waitFor(() => expect(tabs()).toContain('Strata'));
});
