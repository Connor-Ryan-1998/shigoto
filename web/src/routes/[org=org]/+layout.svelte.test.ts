import { beforeEach, describe, expect, it, vi } from 'vitest';
import { page, setPage } from '../../lib/test/app.svelte';
import { flushSync, createRawSnippet } from 'svelte';
import { render, screen, within } from '@testing-library/svelte';

import Layout from './+layout.svelte';
import { session } from '../../lib/session.svelte';

// A module item without an icon (as a future module might declare) must
// still render as a plain labelled link.
vi.mock('../../lib/modules/property/manifest', () => ({
	property: {
		name: 'property',
		label: 'Property',
		nav: [{ href: 'properties', label: 'Properties', perm: 'property_read', group: 'property' }]
	}
}));

const noChildren = createRawSnippet(() => ({ render: () => '<p></p>' }));

function signIn() {
	session.setMe({
		user: { id: 1, email: 'owner@example.com', name: 'Owner' },
		orgs: [{ slug: 'acme', name: 'Acme', role: 'owner', perms: ['invoices_read', 'reports_read', 'property_read'] }]
	});
	session.setModules(['property']);
}

beforeEach(() => setPage({ params: { org: 'acme' }, url: new URL('http://localhost/acme') }));

describe('[org] layout', () => {
	// Pages load their data once on mount, so switching org must remount
	// them - otherwise the previous org's figures stay on screen.
	it('remounts the page when the org changes', () => {
		let mounts = 0;
		const children = createRawSnippet(() => ({
			render: () => `<p data-testid="child">mount ${++mounts}</p>`
		}));

		const { getByTestId } = render(Layout, { children });
		expect(getByTestId('child').textContent).toBe('mount 1');

		page.params.org = 'globex';
		page.url = new URL('http://localhost/globex');
		flushSync();

		expect(getByTestId('child').textContent).toBe('mount 2');
	});

	it('marks the current page and keeps nav icons decorative', () => {
		setPage({ params: { org: 'acme' }, url: new URL('http://localhost/acme/invoices') });
		signIn();
		render(Layout, { children: noChildren });
		const nav = screen.getByRole('navigation', { name: 'Primary' });
		const invoices = within(nav).getByRole('link', { name: 'Invoices' });
		expect(invoices.getAttribute('aria-current')).toBe('page');
		expect(invoices.querySelector('svg')?.getAttribute('aria-hidden')).toBe('true');
		expect(within(nav).getByRole('link', { name: 'Dashboard' }).hasAttribute('aria-current')).toBe(false);
	});

	it('renders a nav item without an icon', () => {
		signIn();
		render(Layout, { children: noChildren });
		const nav = screen.getByRole('navigation', { name: 'Primary' });
		const link = within(nav).getByRole('link', { name: 'Properties' });
		expect(link.querySelector('svg')).toBeNull();
	});

	it('shows group labels as text, not disclosures', () => {
		signIn();
		const { container } = render(Layout, { children: noChildren });
		const nav = screen.getByRole('navigation', { name: 'Primary' });
		expect(container.querySelector('nav[aria-label="Primary"] details')).toBeNull();
		expect(within(nav).getByText('Finance')).toBeTruthy();
	});

	it('offers Sign out, a labelled theme toggle and a labelled menu button', () => {
		signIn();
		render(Layout, { children: noChildren });
		expect(screen.getByRole('button', { name: 'Sign out' })).toBeTruthy();
		expect(screen.queryByRole('button', { name: 'Logout' })).toBeNull();
		expect(screen.getByRole('button', { name: /^Theme: Auto/ })).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Open navigation' })).toBeTruthy();
		expect(screen.getByLabelText('Organisation').tagName).toBe('SELECT');
	});
});
