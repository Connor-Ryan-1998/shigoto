import { test, expect } from '@playwright/test';
import { signIn, startStack, type Stack } from './stack';
import { pdf, startFakeLLM } from './fake-llm';

// The strata chairperson assistant end to end against a scripted model:
// the owner uploads AGM minutes to a scheme from the Strata tab, the
// background indexer reads them, and a question about the roof gets an
// answer whose citation opens the source PDF page.

const ownerEmail = 'owner-strata@example.com';
const password = 'e2e-password';

let stack: Stack;
let llm: Awaited<ReturnType<typeof startFakeLLM>>;

test.beforeAll(async () => {
	llm = await startFakeLLM();
	stack = await startStack({
		db: 'shigoto_e2e_strata',
		owner: { email: ownerEmail, password },
		orgs: [{ slug: 'strata', name: 'Strata Pty Ltd' }],
		env: {
			// The indexer runs with the scheduler.
			SHIGOTO_SCHEDULER: 'on',
			SHIGOTO_LLM_BASE_URL: llm.url,
			SHIGOTO_LLM_API_KEY: 'e2e-key',
			SHIGOTO_LLM_MODEL: 'fake-chat',
			SHIGOTO_EMBED_BASE_URL: llm.url,
			SHIGOTO_EMBED_API_KEY: 'e2e-key',
			SHIGOTO_EMBED_MODEL: 'fake-embed'
		}
	});
});

test.afterAll(async () => {
	await stack?.stop();
	await llm?.stop();
});

test('chairperson uploads minutes and asks about the roof', async ({ page }) => {
	const cookie = await stack.login(ownerEmail, password);
	const property = await stack.api<{ id: number }>(cookie, 'POST', '/api/v1/orgs/strata/properties', { name: 'Unit 4', address: '4/10 Harbour St' });
	const scheme = await stack.api<{ id: number }>(cookie, 'POST', '/api/v1/orgs/strata/strata-schemes', {
		name: 'Harbour View CTS', cts_number: '12345', is_chairperson: true, is_secretary: false, is_treasurer: false
	});
	await stack.api(cookie, 'PUT', `/api/v1/orgs/strata/properties/${property.id}/strata`, { strata_scheme_id: scheme.id });

	await signIn(page, stack.base, ownerEmail, password, '/strata');
	await page.goto(`${stack.base}/strata/properties/${property.id}/strata`);

	await page.getByRole('button', { name: 'Upload document' }).click();
	const dialog = page.getByRole('dialog', { name: 'Upload strata document' });
	await dialog.getByLabel('PDF document', { exact: true }).setInputFiles({
		name: 'agm-2024.pdf', mimeType: 'application/pdf', buffer: pdf(['Annual general meeting', 'Motion 5', 'Close'])
	});
	await dialog.getByLabel('Type', { exact: true }).selectOption('minutes');
	await dialog.getByLabel('Title', { exact: true }).fill('AGM 2024');
	await dialog.getByLabel('Document date', { exact: true }).fill('2024-09-12');
	await dialog.getByRole('button', { name: 'Upload document' }).click();
	await expect(dialog).toBeHidden();

	const row = page.getByRole('row', { name: /AGM 2024/ });
	await expect(row.getByText('Indexed')).toBeVisible({ timeout: 30_000 });
	await expect(row.getByText('3', { exact: true })).toBeVisible();

	const chair = page.getByRole('region', { name: 'Chairperson' });
	await chair.getByLabel('Question', { exact: true }).fill('What did the committee decide about the roof in 2024?');
	await chair.getByRole('button', { name: 'Ask' }).click();
	await expect(chair.getByText(/replace the roof membrane/)).toBeVisible({ timeout: 30_000 });
	await expect(chair.getByText(/No cited source supports this answer/)).toHaveCount(0);

	const cite = chair.getByRole('link', { name: /AGM 2024 — .*p\. 1/ });
	const href = await cite.getAttribute('href');
	expect(href).toMatch(/\/documents\/\d+\/file#page=1$/);
	const file = await page.request.get(`${stack.base}${href!.split('#')[0]}`);
	expect(file.status()).toBe(200);
	expect(file.headers()['content-type']).toBe('application/pdf');
});
