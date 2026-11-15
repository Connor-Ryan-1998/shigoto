import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { setPage } from '../../../../lib/test/app.svelte';
import { signIn } from '../../../../lib/test/session';
import Page from './+page.svelte';

describe('time import page', () => {
	beforeEach(() => setPage({ params: { org: 'acme' }, url: 'http://localhost/acme/time/import' }));

	it('renders the import workflow for an owner', async () => {
		signIn({ perms: ['timesheets_read', 'import'] });
		render(Page);
		expect(await screen.findByRole('heading', { name: 'Import time' })).toBeTruthy();
		expect(screen.getByLabelText('CSV file')).toBeTruthy();
	});

	it('explains when the user may not import', async () => {
		signIn({ perms: ['timesheets_read'] });
		render(Page);
		expect(await screen.findByText('Only the organisation owner can import.')).toBeTruthy();
		expect(screen.queryByLabelText('CSV file')).toBeNull();
	});
});
