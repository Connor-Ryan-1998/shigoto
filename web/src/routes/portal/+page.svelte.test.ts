import { describe, expect, it } from 'vitest';
import { render } from '@testing-library/svelte';
import { goto } from '../../lib/test/app.svelte';
import { mockApi } from '../../lib/test/mock-api';
import Page from './+page.svelte';

describe('portal org picker', () => {
	it('sends a single client org straight to the hub', async () => {
		mockApi({ 'GET /api/v1/portal/orgs': [{ slug: 'acme', name: 'Acme', role: 'client' }] });
		render(Page);
		await expect.poll(() => goto.mock.calls.at(-1)?.[0]).toBe('/portal/acme/hub');
	});

	it('keeps a single tenant org on invoices', async () => {
		mockApi({ 'GET /api/v1/portal/orgs': [{ slug: 'acme', name: 'Acme', role: 'tenant' }] });
		render(Page);
		await expect.poll(() => goto.mock.calls.at(-1)?.[0]).toBe('/portal/acme/invoices');
	});
});
