import { test, expect } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { startStack, signIn, type Stack } from './stack';

// The client portal end to end: an owner writes welcome notes, adds a
// checklist item and uploads a signature PDF and a discovery PDF for a
// client, sends the signature document; the client signs in by magic link,
// lands on the hub, signs with a typed name and consent and downloads the
// signing certificate; the owner then sees the signed, immutable row.

const ownerEmail = 'owner-client-portal@example.com';
const password = 'e2e-password';
const mailpitBase = 'http://127.0.0.1:8025';
const runID = randomUUID().slice(0, 8);
const clientEmail = `client-${runID}@example.com`;
const otherEmail = `other-client-${runID}@example.com`;
const pdf = (label: string) => Buffer.from(`%PDF-1.4\n% ${label} ${runID}\n%%EOF\n`);

let stack: Stack;
let base = '';

type MailpitMessage = { ID: string; To: { Address: string }[]; Subject: string };

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
		db: 'shigoto_e2e_client_portal',
		owner: { email: ownerEmail, password },
		orgs: [{ slug: 'org-a', name: 'Alpha Studio' }],
		services: ['db', 'mailpit'],
		env: { SHIGOTO_SMTP_HOST: '127.0.0.1', SHIGOTO_SMTP_PORT: '1025', SHIGOTO_SMTP_FROM: 'noreply@shigoto.local', SHIGOTO_SMTP_TLS: 'none' }
	});
	base = stack.base;
	await fetch(`${mailpitBase}/api/v1/messages`, { method: 'DELETE' });
});

test.afterAll(() => stack?.stop());

test('owner sends a document and the client signs it in the portal', async ({ page, browser }) => {
	const cookie = await stack.login(ownerEmail, password);
	const client = await stack.api<{ id: number }>(cookie, 'POST', '/api/v1/orgs/org-a/contacts', { kind: 'client', name: 'Client One', email: clientEmail });
	const other = await stack.api<{ id: number }>(cookie, 'POST', '/api/v1/orgs/org-a/contacts', { kind: 'client', name: 'Client Two', email: otherEmail });
	const project = await stack.api<{ id: number }>(cookie, 'POST', '/api/v1/orgs/org-a/projects', {
		name: `Website ${runID}`, client_contact_id: client.id, hourly_rate: 12000, status: 'pending', summary: 'Rebuild the site'
	});
	await stack.api(cookie, 'POST', `/api/v1/orgs/org-a/contacts/${client.id}/portal-invite`);
	await stack.api(cookie, 'POST', `/api/v1/orgs/org-a/contacts/${other.id}/portal-invite`);

	// Staff: welcome notes, checklist and documents on the client page.
	await signIn(page, base, ownerEmail, password, '/org-a');
	await page.goto(`${base}/org-a/contacts/${client.id}`);
	await expect(page.getByRole('heading', { name: 'Client One' })).toBeVisible();
	await page.getByRole('button', { name: 'Edit notes' }).click();
	await page.getByRole('textbox', { name: 'Welcome notes' }).fill('Slack #acme for day-to-day, 4 business hours');
	await page.getByRole('button', { name: 'Save notes' }).click();
	await expect(page.getByText('Slack #acme for day-to-day, 4 business hours')).toBeVisible();

	await page.getByRole('button', { name: '+ Add item' }).click();
	await page.getByLabel('Action', { exact: true }).fill('Grant analytics access');
	await page.getByRole('button', { name: 'Add item', exact: true }).click();
	await expect(page.getByRole('cell', { name: 'Grant analytics access', exact: true })).toBeVisible();

	await page.getByRole('button', { name: 'Upload document' }).click();
	await page.getByLabel('Title', { exact: true }).fill('Statement of Work');
	await page.getByLabel('Project', { exact: true }).selectOption(String(project.id));
	await page.getByLabel('PDF file', { exact: true }).setInputFiles({ name: 'sow.pdf', mimeType: 'application/pdf', buffer: pdf('sow') });
	await page.getByRole('button', { name: 'Upload', exact: true }).click();
	await page.getByRole('button', { name: 'Send Statement of Work' }).click();
	await expect(page.getByText('Awaiting signature')).toBeVisible();

	await page.getByRole('button', { name: 'Upload document' }).click();
	await page.getByLabel('Title', { exact: true }).fill('Discovery notes');
	await page.getByLabel('Type', { exact: true }).selectOption('shared');
	await page.getByLabel('Category', { exact: true }).selectOption('discovery');
	await page.getByLabel('Project', { exact: true }).selectOption(String(project.id));
	await page.getByLabel('PDF file', { exact: true }).setInputFiles({ name: 'discovery.pdf', mimeType: 'application/pdf', buffer: pdf('discovery') });
	await page.getByRole('button', { name: 'Upload', exact: true }).click();
	await page.getByRole('button', { name: 'Send Discovery notes' }).click();
	await expect(page.getByText('Shared', { exact: true })).toBeVisible();

	// Client: magic link, hub, project, sign.
	const signLink = await waitForMail(clientEmail, /(https?:\/\/\S*\/portal\/org-a\/documents\/\d+)/);
	const magic = await waitForMail(clientEmail, /(https?:\/\/\S*\/auth\/magic\?token=\S+)/);
	const clientContext = await browser.newContext();
	const cp = await clientContext.newPage();
	await cp.goto(magic);
	await expect(cp).toHaveURL(`${base}/portal/org-a/hub`);
	await expect(cp.getByRole('link', { name: /1 document needs your signature/ })).toBeVisible();
	await expect(cp.getByText('Slack #acme for day-to-day, 4 business hours')).toBeVisible();
	await expect(cp.getByRole('cell', { name: 'Grant analytics access', exact: true })).toBeVisible();
	await cp.getByRole('link', { name: new RegExp(`Website ${runID}`) }).click();
	await expect(cp.getByText('Rebuild the site')).toBeVisible();
	await expect(cp.getByRole('heading', { name: 'Discovery' })).toBeVisible();
	await cp.getByRole('link', { name: 'Review and sign Statement of Work' }).click();
	await expect(cp).toHaveURL(signLink.replace(/^https?:\/\/[^/]+/, base));

	const popup = cp.waitForEvent('popup');
	await cp.getByRole('button', { name: 'Open Statement of Work (PDF)' }).click();
	await (await popup).close();
	await cp.getByLabel('Full legal name', { exact: true }).fill('Priya Nair');
	await cp.getByLabel(/I agree this is my electronic signature and I am authorised to sign for Client One\./).check();
	await cp.getByRole('button', { name: 'Sign document' }).click();
	await expect(cp.getByText(/Signed by Priya Nair/)).toBeVisible();
	const cert = await cp.request.get(`${base}/api/v1/portal/org-a/documents/${signLink.split('/').pop()}/certificate`);
	expect(cert.status()).toBe(200);
	expect(cert.headers()['content-type']).toBe('application/pdf');
	expect((await cert.body()).subarray(0, 5).toString()).toBe('%PDF-');

	// Staff were emailed, and the signed row is immutable.
	await waitForMail(ownerEmail, /Client One signed Statement of Work/);
	await page.reload();
	await expect(page.getByText(/Signed by Priya Nair/)).toBeVisible();
	await expect(page.getByRole('button', { name: 'Withdraw Statement of Work' })).toHaveCount(0);

	// Another client can't open the first client's document.
	const otherMagic = await waitForMail(otherEmail, /(https?:\/\/\S*\/auth\/magic\?token=\S+)/);
	const otherContext = await browser.newContext();
	const op = await otherContext.newPage();
	await op.goto(otherMagic);
	await expect(op).toHaveURL(`${base}/portal/org-a/hub`);
	await op.goto(signLink.replace(/^https?:\/\/[^/]+/, base));
	await expect(op.getByText('Document not found.')).toBeVisible();
	await clientContext.close();
	await otherContext.close();
});
