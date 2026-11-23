import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { mockApi } from '../../../../lib/test/mock-api';
import { setPage } from '../../../../lib/test/app.svelte';
import { signIn } from '../../../../lib/test/session';
import { session } from '../../../../lib/session.svelte';
import Page from './+page.svelte';

const base = '/api/v1/orgs/acme';
const contacts = [
	{ id: 9, kind: 'client', name: 'Client One', email: 'c@x', phone: '', company_name: '', abn: '' },
	{ id: 10, kind: 'tenant', name: 'Tina', email: '', phone: '', company_name: '', abn: '' }
];

describe('client page', () => {
	it('shows the welcome note, checklist and documents for a client', async () => {
		mockApi({
			[`GET ${base}/contacts`]: contacts,
			[`GET ${base}/contacts/9/welcome`]: { notes: '<b>Slack</b> #acme', updated_at: null },
			[`GET ${base}/contacts/9/checklist`]: [],
			[`GET ${base}/contacts/9/documents`]: [],
			[`GET ${base}/projects?client_id=9`]: []
		});
		setPage({ params: { org: 'acme', id: '9' } });
		signIn();
		session.setModules(['timesheets']);
		render(Page);
		expect(await screen.findByRole('heading', { name: 'Client One' })).toBeTruthy();
		// Notes are plain text: markup shows literally.
		expect(await screen.findByText('<b>Slack</b> #acme')).toBeTruthy();
		expect(screen.getByRole('heading', { name: 'Client action checklist' })).toBeTruthy();
		expect(screen.getByRole('heading', { name: 'Documents' })).toBeTruthy();
	});

	it('explains that only clients have a portal page', async () => {
		mockApi({ [`GET ${base}/contacts`]: contacts });
		setPage({ params: { org: 'acme', id: '10' } });
		signIn();
		render(Page);
		expect(await screen.findByText('Not a client')).toBeTruthy();
	});
});
