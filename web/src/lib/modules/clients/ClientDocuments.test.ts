import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/svelte';
import { apiError, apiMock, mockApi } from '../../test/mock-api';
import ClientDocuments from './ClientDocuments.svelte';
import type { ClientDocument } from './types';

const file = { id: 1, mime: 'application/pdf', size_bytes: 10, original_name: 'a.pdf' };
function doc(over: Partial<ClientDocument>): ClientDocument {
	return {
		id: 1, contact_id: 9, project_id: null, title: 'MSA', kind: 'signature', category: 'general', status: 'draft',
		sent_at: null, created_at: '2026-11-20T00:00:00Z', supersedes_id: null, file, signature: null, ...over
	};
}
const base = '/api/v1/orgs/acme';

describe('ClientDocuments', () => {
	it('shows only actions valid for each status, and none on a signed row', async () => {
		mockApi({
			[`GET ${base}/contacts/9/documents`]: [
				doc({ id: 1, title: 'Draft SOW', status: 'draft' }),
				doc({ id: 2, title: 'Sent NDA', status: 'sent' }),
				doc({ id: 3, title: 'Signed MSA', status: 'signed', signature: { typed_name: 'Priya Nair', email: 'p@x', signed_at: '2026-11-21T00:00:00Z' } })
			]
		});
		render(ClientDocuments, { org: 'acme', contactId: 9, projects: [], canWrite: true });
		const draft = (await screen.findByText('Draft SOW')).closest('tr')!;
		expect(within(draft).getByRole('button', { name: 'Send Draft SOW' })).toBeTruthy();
		expect(within(draft).getByRole('button', { name: 'Delete Draft SOW' })).toBeTruthy();
		const sent = screen.getByText('Sent NDA').closest('tr')!;
		expect(within(sent).getByRole('button', { name: 'Withdraw Sent NDA' })).toBeTruthy();
		expect(within(sent).getByRole('button', { name: 'Replace Sent NDA' })).toBeTruthy();
		const signed = screen.getByText('Signed MSA').closest('tr')!;
		expect(within(signed).queryByRole('button', { name: /Withdraw|Replace|Delete|Send/ })).toBeNull();
		expect(within(signed).getByText(/Priya Nair/)).toBeTruthy();
		expect(within(signed).getByRole('link', { name: 'Certificate for Signed MSA' }).getAttribute('href')).toBe(`${base}/documents/3/certificate`);
	});

	it('explains a send refused for a client without a portal login', async () => {
		mockApi({
			[`GET ${base}/contacts/9/documents`]: [doc({ id: 1, title: 'MSA', status: 'draft' })],
			[`POST ${base}/documents/1/send`]: apiError(422, 'Invite this client to the portal first')
		});
		render(ClientDocuments, { org: 'acme', contactId: 9, projects: [], canWrite: true });
		await fireEvent.click(await screen.findByRole('button', { name: 'Send MSA' }));
		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('Invite this client to the portal first');
		expect(within(alert).getByRole('link', { name: 'Invite to portal' }).getAttribute('href')).toBe('/acme/settings/members');
	});

	it('filters to a project and hides write actions without permission', async () => {
		mockApi({ [`GET ${base}/contacts/9/documents?project=5`]: [doc({ id: 1, title: 'SOW', project_id: 5 })] });
		render(ClientDocuments, { org: 'acme', contactId: 9, projectId: 5, projects: [{ id: 5, name: 'Site' }], canWrite: false });
		await screen.findByText('SOW');
		expect(screen.queryByRole('button', { name: /Send|Upload document/ })).toBeNull();
		expect(apiMock).toHaveBeenCalledWith('GET', `${base}/contacts/9/documents?project=5`);
	});
});
