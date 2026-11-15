import { test, expect, type APIRequestContext } from '@playwright/test';
import { signIn, startStack, type Stack } from './stack';

// The CSV import path through the real stack: upload, map, server preview,
// import, for invoices then the time billed on them. Row rules are owned
// by Go tests; this proves the pieces join up.

const email = 'import-owner@example.com';
const password = 'import-e2e-password';
const apiBase = '/api/v1/orgs/imports';
let stack: Stack;
let base = '';

test.use({ timezoneId: 'UTC' });

async function api<T>(request: APIRequestContext, path: string): Promise<T> {
	const res = await request.fetch(`${base}${apiBase}${path}`);
	expect(res.ok(), `GET ${path}: ${await res.text()}`).toBeTruthy();
	return res.json();
}

test.beforeAll(async () => {
	stack = await startStack({
		db: 'shigoto_e2e_imports',
		owner: { email, password },
		orgs: [{ slug: 'imports', name: 'Import tests' }],
		env: {
			SHIGOTO_TIMEZONE: 'UTC', SHIGOTO_MODULES: 'timesheets', SHIGOTO_SMTP_HOST: '',
			SHIGOTO_OIDC_ISSUER: '', SHIGOTO_OIDC_CLIENT_ID: '', SHIGOTO_OIDC_CLIENT_SECRET: ''
		},
		seed: (psql) => psql(`
			UPDATE orgs SET gst_registered = true;
			INSERT INTO contacts (org_id, kind, name) SELECT id, 'client', 'Acme Pty Ltd' FROM orgs;
			INSERT INTO projects (org_id, client_contact_id, name, hourly_rate, status)
				SELECT o.id, c.id, 'Website', 15000, 'completed' FROM orgs o JOIN contacts c ON c.org_id = o.id;
			INSERT INTO project_tasks (org_id, project_id, name) SELECT org_id, id, 'Build' FROM projects;`)
	});
	base = stack.base;
});

test.afterAll(() => stack?.stop());

test.beforeEach(({ page }) => signIn(page, base, email, password, '/imports'));

const invoiceCSV = 'Invoice number,Customer,Issue date,Due date,Amount ex GST,GST,Total,Outstanding,Paid date\n' +
	'ACC-SINV-1,Acme Pty Ltd,2025-03-01,2025-03-15,1000.00,100.00,1100.00,0,2025-03-10\n';
const timeCSV = 'Date,Hours,Project,Task,Person (email),Invoice number,Note\n' +
	`2025-03-03,7.5,Website,Build,${email},ACC-SINV-1,Design\n`;

test('imports invoices, then time billed on them', async ({ page }) => {
	await page.goto(`${base}/imports/invoices`);
	await page.getByRole('button', { name: 'Import', exact: true }).click();
	await expect(page).toHaveURL(`${base}/imports/invoices/import`);
	await page.getByLabel('CSV file').setInputFiles({ name: 'invoices.csv', mimeType: 'text/csv', buffer: Buffer.from(invoiceCSV) });
	await page.getByLabel('Date format').selectOption('2006-01-02');
	await page.getByRole('button', { name: 'Preview', exact: true }).click();
	await expect(page.getByText('1 new invoice totalling')).toBeVisible();
	await page.getByRole('button', { name: 'Import', exact: true }).click();
	await expect(page.getByRole('status').filter({ hasText: 'Imported 1 new invoice' })).toBeVisible();

	const list = await api<{ invoices: { id: number; status: string; total: number }[] }>(page.request, '/invoices');
	expect(list.invoices).toHaveLength(1);
	expect(list.invoices[0]).toMatchObject({ status: 'paid', total: 110000 });
	await page.goto(`${base}/imports/invoices/${list.invoices[0].id}`);
	await expect(page.getByText('Imported as ACC-SINV-1')).toBeVisible();

	await page.goto(`${base}/imports/time`);
	await page.getByRole('button', { name: 'Import', exact: true }).click();
	await page.getByLabel('CSV file').setInputFiles({ name: 'time.csv', mimeType: 'text/csv', buffer: Buffer.from(timeCSV) });
	await page.getByLabel('Date format').selectOption('2006-01-02');
	await page.getByRole('button', { name: 'Preview', exact: true }).click();
	await expect(page.getByText('1 new entry totalling 7h 30m')).toBeVisible();
	await page.getByRole('button', { name: 'Import', exact: true }).click();
	await expect(page.getByRole('status').filter({ hasText: 'Imported 1 new entry' })).toBeVisible();

	const unbilled = await api<{ entries: unknown[] }>(page.request, '/time-entries?unbilled=true');
	expect(unbilled.entries).toHaveLength(0);
	const all = await api<{ entries: { minutes: number; invoice_line_id: number | null }[] }>(page.request, '/time-entries');
	expect(all.entries).toHaveLength(1);
	expect(all.entries[0].minutes).toBe(450);
	expect(all.entries[0].invoice_line_id).not.toBeNull();
});
