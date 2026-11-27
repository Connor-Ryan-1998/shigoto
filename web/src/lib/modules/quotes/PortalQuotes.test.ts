import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { mockApi } from '../../test/mock-api';

import PortalQuotes from './PortalQuotes.svelte';

const item = (over: Record<string, unknown>) => ({
	id: 7, number_label: 'Q-0007', title: 'Switchboard', project_id: null, project_name: '', status: 'sent',
	display_status: 'sent', issue_date: '2026-11-27', valid_until: '2026-12-27', total: 52800, ...over
});

describe('PortalQuotes', () => {
	it('lists quotes, greying ones no longer open', async () => {
		mockApi({ 'GET /api/v1/portal/acme/quotes': { quotes: [item({}), item({ id: 6, number_label: 'Q-0006', title: 'Old', status: 'superseded', display_status: 'superseded' })] } });
		render(PortalQuotes, { org: 'acme' });
		const open = await screen.findByRole('link', { name: /Q-0007/ });
		expect(open.getAttribute('href')).toBe('/portal/acme/quotes/7');
		expect(open.textContent).toContain('$528.00');
		const old = screen.getByRole('link', { name: /Q-0006/ });
		expect(old.closest('li')!.className).toContain('opacity-60');
		expect(old.textContent).toContain('Superseded');
	});

	it('filters to one project', async () => {
		mockApi({ 'GET /api/v1/portal/acme/quotes?project_id=5': { quotes: [item({ project_id: 5 })] } });
		render(PortalQuotes, { org: 'acme', projectId: 5 });
		expect(await screen.findByRole('link', { name: /Q-0007/ })).toBeTruthy();
	});

	it('says when there are none', async () => {
		mockApi({ 'GET /api/v1/portal/acme/quotes': { quotes: [] } });
		render(PortalQuotes, { org: 'acme' });
		expect(await screen.findByText('No quotes yet.')).toBeTruthy();
	});
});
