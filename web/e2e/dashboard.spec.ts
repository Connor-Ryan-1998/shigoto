import { test, expect } from '@playwright/test';
import { signIn, startStack, type Stack } from './stack';

const email = 'dashboard-owner@example.com';
const password = 'dashboard-e2e-password';
const apiBase = '/api/v1/orgs/dashboard';
const today = new Date().toISOString().slice(0, 10);
const projectName = 'HTM Data Platform with a deliberately long project name for dashboard layout';
let stack: Stack;
let projectID: number;

test.beforeAll(async () => {
	stack = await startStack({
		db: 'shigoto_e2e_dashboard',
		owner: { email, password },
		orgs: [{ slug: 'dashboard', name: 'Dashboard tests' }],
		env: { SHIGOTO_TIMEZONE: 'UTC', SHIGOTO_MODULES: 'property,timesheets,reconciliation' }
	});
	const cookie = await stack.login(email, password);
	const api = <T>(method: string, path: string, body?: unknown) => stack.api<T>(cookie, method, `${apiBase}${path}`, body);
	const client = await api<{ id: number }>('POST', '/contacts', { kind: 'client', name: 'Rizzware with a deliberately long client name for narrow screens' });
	const project = await api<{ id: number }>('POST', '/projects', { name: projectName, client_contact_id: client.id, hourly_rate: 12000 });
	projectID = project.id;
	const task = await api<{ id: number }>('POST', `/projects/${project.id}/tasks`, { name: 'Development' });
	await api('POST', '/time-entries', { task_id: task.id, work_date: today, minutes: 90 });
	await api('POST', '/billing-runs', {
		client_contact_id: client.id, from: today, to: today, due_in_days: 14, split: 'combined',
		tasks: [{ task_id: task.id, minutes: 90, amount: 18000 }]
	});
	await api('POST', '/time-entries', { task_id: task.id, work_date: today, minutes: 30 });
	const inv = await api<{ id: number }>('POST', '/invoices', {
		contact_id: client.id, source: 'manual', due_in_days: 14,
		lines: [{ description: 'Consulting', quantity_milli: 1000, unit_amount: 15000, gst_code: 'N-T' }]
	});
	await api('POST', `/invoices/${inv.id}/issue`);
	const account = await api<{ id: number }>('POST', '/bank-accounts', {
		name: 'Dashboard activity', csv_mapping: { has_header: true, date_column: 0, date_format: '2006-01-02', amount_column: 1, description_column: 2 }
	});
	const form = new FormData();
	form.append('file', new Blob([`Date,Amount,Description\n${today},150.00,Incoming\n${today},-40.00,Outgoing\n`], { type: 'text/csv' }), 'activity.csv');
	const res = await fetch(`${stack.base}${apiBase}/bank-accounts/${account.id}/imports`, {
		method: 'POST', headers: { Cookie: cookie, 'X-Shigoto-CSRF': '1' }, body: form
	});
	expect(res.ok, await res.text()).toBeTruthy();
});

test.afterAll(() => stack?.stop());

test('dashboard charts resize in both themes and period selection stays local', async ({ page }, testInfo) => {
	const errors: string[] = [];
	page.on('pageerror', e => errors.push(e.message));
	let dashboardRequests = 0;
	page.on('request', r => { if (r.url().endsWith(`${apiBase}/dashboard`)) dashboardRequests++; });
	await signIn(page, stack.base, email, password, '/dashboard');
	const cash = page.getByRole('region', { name: 'Cash Flow' });
	await expect(cash.getByRole('group', { name: 'Cash flow totals' })).toContainText('$150.00');
	await expect(page.getByRole('progressbar', { name: /hours billed/ })).toHaveAttribute('aria-valuenow', '75');
	const bars = cash.locator('.lc-bars-bar');
	await expect(cash.locator('svg').first()).toBeVisible();
	await expect.poll(async () => (await bars.evaluateAll(nodes => nodes.some(n => n.getBoundingClientRect().height > 0)))).toBe(true);
	for (const colourScheme of ['light', 'dark'] as const) {
		await page.emulateMedia({ colorScheme: colourScheme, reducedMotion: 'reduce' });
		for (const width of [1280, 390]) {
			await page.setViewportSize({ width, height: 900 });
			await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
			await expect(cash.getByLabel('Cash flow period')).toBeVisible();
			await expect.poll(() => cash.locator('.lc-axis-tick-label').first().evaluate(node => getComputedStyle(node).stroke)).toBe('none');
			await expect(page.getByRole('link', { name: new RegExp(projectName) })).toBeVisible();
			await expect.poll(async () => {
				const bounds = await cash.locator('svg').first().boundingBox();
				return !!bounds && bounds.width > 100 && bounds.height > 100 && bounds.x >= 0 && bounds.x + bounds.width <= width;
			}).toBe(true);
			await page.screenshot({ path: testInfo.outputPath(`dashboard-${colourScheme}-${width}.png`), fullPage: true });
		}
	}
	const before = dashboardRequests;
	await cash.getByLabel('Cash flow period').selectOption('previous');
	await expect(cash.getByText('No imported bank activity for this period.')).toBeVisible();
	expect(dashboardRequests).toBe(before);
	await page.getByRole('link', { name: new RegExp(projectName) }).click();
	await expect(page).toHaveURL(`${stack.base}/dashboard/projects/${projectID}`);
	await page.goBack();
	await page.getByRole('link', { name: /current.*\$150.00/i }).click();
	await expect(page).toHaveURL(`${stack.base}/dashboard/invoices?status=open`);
	expect(errors).toEqual([]);
});
