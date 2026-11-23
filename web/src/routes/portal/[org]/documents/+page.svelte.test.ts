import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { setPage } from '../../../../lib/test/app.svelte';
import { mockApi } from '../../../../lib/test/mock-api';
import Page from './+page.svelte';

const file = { id: 1, mime: 'application/pdf', size_bytes: 1, original_name: 'x.pdf' };
const d = (over: object) => ({ id: 1, project_id: null, project_name: '', title: 'Doc', kind: 'signature', category: 'general', status: 'sent', sent_at: null, applies_to_all: false, file, signature: null, ...over });
const docs = [d({ id: 1, title: 'SOW' }), d({ id: 2, title: 'MSA', status: 'signed' }), d({ id: 3, title: 'Report', kind: 'shared' })];

describe('client documents', () => {
	beforeEach(() => mockApi({ 'GET /api/v1/portal/acme/documents': docs }));

	it('filters by To sign, Signed and Shared', async () => {
		setPage({ params: { org: 'acme' }, url: new URL('http://localhost/portal/acme/documents') });
		render(Page);
		await screen.findByText('SOW');
		expect(screen.getByText('MSA')).toBeTruthy();
		await fireEvent.click(screen.getByRole('tab', { name: 'Signed' }));
		expect(screen.queryByText('SOW')).toBeNull();
		expect(screen.getByText('MSA')).toBeTruthy();
		await fireEvent.click(screen.getByRole('tab', { name: 'Shared' }));
		expect(screen.getByText('Report')).toBeTruthy();
	});

	it('opens on the To sign filter from the hub banner', async () => {
		setPage({ params: { org: 'acme' }, url: new URL('http://localhost/portal/acme/documents?filter=to_sign') });
		render(Page);
		await screen.findByText('SOW');
		expect(screen.queryByText('MSA')).toBeNull();
	});
});
