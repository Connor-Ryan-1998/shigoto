import { test, expect } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { startStack, signIn, type Stack } from './stack';

// Quotes end to end: an owner creates a quote with a labour and a material
// line, marks it sent, downloads its PDF and accepts it, landing on the new
// fixed-price project with a task from the labour line and a Costs tab;
// the client then signs in by magic link, sees the quote on the hub and
// opens its PDF.

const ownerEmail = 'owner-quotes@example.com';
const password = 'e2e-password';
const mailpitBase = 'http://127.0.0.1:8025';
const runID = randomUUID().slice(0, 8);
const clientEmail = `quote-client-${runID}@example.com`;

let stack: Stack;
let base = '';

type MailpitMessage = { ID: string; To: { Address: string }[] };

async function waitForMail(to: string, pattern: RegExp): Promise<string> {
	const deadline = Date.now() + 15_000;
	while (Date.now() < deadline) {
		const list = (await (await fetch(`${mailpitBase}/api/v1/messages`)).json()) as { messages: MailpitMessage[] };
		for (const msg of list.messages.filter((m) => m.To.some((t) => t.Address === to))) {
			const full = (await (await fetch(`${mailpitBase}/api/v1/message/${msg.ID}`)).json()) as { Text: string };
			const match = full.Text.match(pattern);
			if (match) return match[1] ?? match[0];
		}
		await new Promise((r) => setTimeout(r, 300));
	}
	throw new Error(`no email matching ${pattern} arrived for ${to}`);
}

test.beforeAll(async () => {
	stack = await startStack({
		db: 'shigoto_e2e_quotes',
		owner: { email: ownerEmail, password },
		orgs: [{ slug: 'org-q', name: 'Quote Sparky' }],
		services: ['db', 'mailpit'],
		env: { SHIGOTO_SMTP_HOST: '127.0.0.1', SHIGOTO_SMTP_PORT: '1025', SHIGOTO_SMTP_FROM: 'noreply@shigoto.local', SHIGOTO_SMTP_TLS: 'none' }
	});
	base = stack.base;
});

test.afterAll(() => stack?.stop());

test('owner quotes, sends and accepts; the client sees the quote', async ({ page, browser }) => {
	const cookie = await stack.login(ownerEmail, password);
	const client = await stack.api<{ id: number }>(cookie, 'POST', '/api/v1/orgs/org-q/contacts', { kind: 'client', name: 'Priya Quote', email: clientEmail });

	page.on('dialog', (d) => d.accept());
	await signIn(page, base, ownerEmail, password, '/org-q');
	await page.goto(`${base}/org-q/quotes/new`);
	await expect(page.getByRole('heading', { name: 'New quote' })).toBeVisible();
	await page.getByLabel('Customer', { exact: true }).selectOption(String(client.id));
	await page.getByLabel('Title', { exact: true }).fill(`Switchboard ${runID}`);
	await page.getByLabel('GST code', { exact: true }).selectOption('FRE');
	await page.getByLabel('Description', { exact: true }).fill('Rough-in');
	await page.getByLabel('Hours', { exact: true }).fill('2.5');
	await page.getByLabel('Rate ($/h)', { exact: true }).fill('95');
	await page.getByRole('button', { name: 'Add material' }).click();
	await page.getByLabel('Description', { exact: true }).nth(1).fill('Cable');
	await page.getByLabel('Quantity', { exact: true }).fill('1');
	await page.getByLabel('Unit price ($)', { exact: true }).fill('180');
	await page.getByLabel('GST code', { exact: true }).nth(1).selectOption('FRE');
	await page.getByRole('button', { name: 'Save draft' }).click();

	await expect(page.getByRole('heading', { name: `Q-0001 · Switchboard ${runID}` })).toBeVisible();
	await page.getByRole('button', { name: 'Mark sent' }).click();
	await expect(page.getByRole('button', { name: 'Accept' })).toBeVisible();

	const pdfHref = await page.getByRole('link', { name: 'Download PDF' }).getAttribute('href');
	const pdf = await page.request.get(`${base}${pdfHref}`);
	expect(pdf.status()).toBe(200);
	expect(pdf.headers()['content-type']).toBe('application/pdf');
	expect((await pdf.body()).subarray(0, 5).toString()).toBe('%PDF-');

	await page.getByRole('button', { name: 'Accept' }).click();
	await expect(page.getByRole('heading', { name: `Switchboard ${runID}` })).toBeVisible();
	await expect(page.getByRole('cell', { name: 'Rough-in', exact: true })).toBeVisible();
	await page.getByRole('tab', { name: 'Costs' }).click();
	await expect(page.getByText('All amounts exclude GST.')).toBeVisible();
	await expect(page.locator('.stat', { hasText: 'Quoted' }).getByText('$417.50', { exact: true })).toBeVisible();

	// Client: magic link, hub quotes, quote page.
	await stack.api(cookie, 'POST', `/api/v1/orgs/org-q/contacts/${client.id}/portal-invite`);
	const magic = await waitForMail(clientEmail, /(https?:\/\/\S*\/auth\/magic\?token=\S+)/);
	const clientContext = await browser.newContext();
	const cp = await clientContext.newPage();
	await cp.goto(magic);
	await expect(cp).toHaveURL(`${base}/portal/org-q/hub`);
	await cp.getByRole('link', { name: /Q-0001/ }).click();
	await expect(cp.getByRole('heading', { name: `Q-0001 · Switchboard ${runID}` })).toBeVisible();
	await expect(cp.getByText('Accepted')).toBeVisible();
	const clientPdf = await cp.request.get(`${base}${await cp.getByRole('link', { name: 'Download PDF' }).getAttribute('href')}`);
	expect(clientPdf.headers()['content-type']).toBe('application/pdf');
	await clientContext.close();
});
