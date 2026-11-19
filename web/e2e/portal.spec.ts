import { test, expect } from '@playwright/test';
import { createHash, randomUUID } from 'node:crypto';
import { startStack, type Stack } from './stack';

// The portal end-to-end: an owner invites a tenant contact to the portal,
// the tenant signs in via a magic link fetched from the compose mailpit
// instance (not a real inbox), and sees their own invoice.

const ownerEmail = 'owner-portal@example.com';
const password = 'e2e-password';
const mailpitBase = 'http://127.0.0.1:8025';

// Unique per run: the magic-request rate limit is 5/email/hour
// (global-constraints.md), so a rerun against the same mailpit/db must never
// reuse an email from a previous run.
const runID = randomUUID().slice(0, 8);
const tenantEmail = `tenant-${runID}@example.com`;
const otherTenantEmail = `other-tenant-${runID}@example.com`;

let stack: Stack;
let base = '';

type MailpitMessage = { ID: string; To: { Address: string }[] };
type MailpitListResponse = { messages: MailpitMessage[] };

// waitForMagicLink polls mailpit (magic mail is sent from a detached
// goroutine - Ruling 61 - so it never lands instantly) for the newest
// message to `to`, and extracts the /auth/magic?token=... link from its
// plain-text body.
async function waitForMagicLink(to: string): Promise<string> {
	const deadline = Date.now() + 15_000;
	while (Date.now() < deadline) {
		const list = (await (await fetch(`${mailpitBase}/api/v1/messages`)).json()) as MailpitListResponse;
		const msg = list.messages.find((m) => m.To.some((t) => t.Address === to));
		if (msg) {
			const full = (await (await fetch(`${mailpitBase}/api/v1/message/${msg.ID}`)).json()) as {
				Text: string;
			};
			const match = full.Text.match(/(https?:\/\/\S*\/auth\/magic\?token=\S+)/);
			if (match) return match[1];
		}
		await new Promise((r) => setTimeout(r, 300));
	}
	throw new Error(`no magic-link email arrived for ${to} within the deadline`);
}

test.beforeAll(async () => {
	stack = await startStack({
		db: 'shigoto_e2e_portal',
		owner: { email: ownerEmail, password },
		orgs: [{ slug: 'org-a', name: 'Alpha Pty Ltd' }, { slug: 'org-b', name: 'Beta Pty Ltd' }],
		services: ['db', 'mailpit'],
		env: { SHIGOTO_SMTP_HOST: '127.0.0.1', SHIGOTO_SMTP_PORT: '1025', SHIGOTO_SMTP_FROM: 'noreply@shigoto.local', SHIGOTO_SMTP_TLS: 'none' }
	});
	base = stack.base;
	// mailpit has no per-recipient delete; deleting everything up front
	// keeps a rerun's message search from ever matching a stale message
	// left over by a previous run against the same compose mailpit.
	await fetch(`${mailpitBase}/api/v1/messages`, { method: 'DELETE' });
});

test.afterAll(() => stack?.stop());

// seedTenantInvoice creates a tenant contact with the given email in org,
// issues one invoice to it, and returns the invoice id and its reference.
async function seedTenantInvoice(
	cookie: string,
	org: string,
	email: string
): Promise<{ invoiceID: number; reference: string }> {
	const contact = await stack.api<{ id: number }>(cookie, 'POST', `/api/v1/orgs/${org}/contacts`, {
		kind: 'tenant',
		name: `Tenant ${email}`,
		email
	});
	const inv = await stack.api<{ id: number }>(cookie, 'POST', `/api/v1/orgs/${org}/invoices`, {
		contact_id: contact.id,
		source: 'manual',
		due_in_days: 14,
		lines: [{ description: 'Rent', quantity_milli: 1000, unit_amount: 50000, gst_code: 'N-T' }]
	});
	await stack.api(cookie, 'POST', `/api/v1/orgs/${org}/invoices/${inv.id}/issue`);
	const detail = await stack.api<{ payment_reference: string }>(
		cookie,
		'GET',
		`/api/v1/orgs/${org}/invoices/${inv.id}`
	);
	return { invoiceID: inv.id, reference: detail.payment_reference };
}

test('tenant signs in by magic link and sees only their own invoice', async ({ page }) => {
	const cookie = await stack.login(ownerEmail, password);

	const { invoiceID, reference } = await seedTenantInvoice(cookie, 'org-a', tenantEmail);
	// A second tenant, in a different org, whose invoice must stay
	// inaccessible to the first tenant even by guessing its id.
	const other = await seedTenantInvoice(cookie, 'org-b', otherTenantEmail);

	// Invite the first tenant to the portal via the owner's own contact
	// list, same as the Members page's "Invite to portal" action does.
	const contacts = await stack.api<{ id: number; kind: string; email?: string }[]>(
		cookie,
		'GET',
		'/api/v1/orgs/org-a/contacts'
	);
	const tenantContact = contacts.find((c) => c.kind === 'tenant' && c.email === tenantEmail);
	if (!tenantContact) throw new Error('seeded tenant contact not found');
	await stack.api(cookie, 'POST', `/api/v1/orgs/org-a/contacts/${tenantContact.id}/portal-invite`);

	const magicLink = await waitForMagicLink(tenantEmail);

	await page.goto(magicLink);
	await expect(page).toHaveURL(`${base}/portal/org-a/invoices`);
	await expect(page.getByRole('link', { name: 'Skip to content', exact: true })).toHaveAttribute('href', '#main-content');
	await expect(page.getByRole('link', { name: 'Invoices', exact: true })).toHaveAttribute('aria-current', 'page');
	await expect(page.getByRole('navigation', { name: 'Primary', exact: true })).toHaveCount(0);
	await page.setViewportSize({ width: 375, height: 812 });
	await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

	// The issued invoice, with its payment reference, is visible.
	await expect(page.getByText(reference)).toBeVisible();
	await page.getByRole('link', { name: /^#\d+$/ }).click();
	await expect(page).toHaveURL(`${base}/portal/org-a/invoices/${invoiceID}`);
	await expect(page.getByText(reference, { exact: true })).toBeVisible();

	// The second tenant's invoice id, typed directly into this tenant's own
	// org path, gives "not found" (global-constraints.md "Portal
	// isolation") - a cross-org id, regardless of who it actually belongs
	// to.
	await page.goto(`${base}/portal/org-a/invoices/${other.invoiceID}`);
	await expect(page.getByText('not found')).toBeVisible();

	// Reusing the already-consumed magic link shows the fixed expired
	// message (single-use tokens, global-constraints.md "Tokens").
	await page.goto(magicLink);
	await expect(page.getByText('This link has expired — request a new one')).toBeVisible();

	const session = (await page.context().cookies()).find((c) => c.name === 'shigoto_session');
	expect(session).toBeDefined();
	const hash = createHash('sha256').update(session!.value).digest('hex');
	stack.psql(`UPDATE sessions SET expires_at=now()-interval '1 minute' WHERE token_hash=decode('${hash}','hex')`);
	await page.goto(`${base}/portal/org-a/invoices`);
	await expect(page).toHaveURL(new RegExp(`${base}/login(?:\\?|$)`));
	await expect(page.getByRole('dialog', { name: 'Session expired' })).not.toBeVisible();
});

test('tenants see the shared rent ledger, receipts and statements', async ({ page, browser }) => {
	const cookie = await stack.login(ownerEmail, password);
	const primaryEmail = `rent-primary-${runID}@example.com`;
	const coEmail = `rent-co-${runID}@example.com`;
	const strangerEmail = `rent-stranger-${runID}@example.com`;
	const contact = (email: string) =>
		stack.api<{ id: number }>(cookie, 'POST', '/api/v1/orgs/org-a/contacts', { kind: 'tenant', name: `Tenant ${email}`, email });
	const [primary, co, stranger] = [await contact(primaryEmail), await contact(coEmail), await contact(strangerEmail)];
	// Rent invoices need two payment options before they can be issued.
	await stack.api(cookie, 'PUT', '/api/v1/orgs/org-a/settings', {
		name: 'Alpha Pty Ltd', bank_bsb: '062-000', bank_account: '12345678', payid: '0412345678',
		invoice_lead_days: 7, auto_send_rent: false
	});

	const property = await stack.api<{ id: number }>(cookie, 'POST', '/api/v1/orgs/org-a/properties', {
		name: `Rent ${runID}`,
		address: `12 Banksia St ${runID}`
	});
	const today = new Date().toISOString().slice(0, 10);
	const lease = await stack.api<{ id: number }>(cookie, 'POST', '/api/v1/orgs/org-a/leases', {
		property_id: property.id,
		tenants: [
			{ contact_id: primary.id, primary: true },
			{ contact_id: co.id, primary: false }
		],
		start_date: today,
		frequency: 'weekly',
		rent_amount: 62000
	});
	await stack.api(cookie, 'POST', '/api/v1/orgs/org-a/rent-run');
	const ledger = await stack.api<{ periods: { invoice_id?: number }[] }>(cookie, 'GET', `/api/v1/orgs/org-a/leases/${lease.id}/ledger`);
	const first = ledger.periods[0].invoice_id!;
	await stack.api(cookie, 'POST', `/api/v1/orgs/org-a/invoices/${first}/issue`);
	await stack.api(cookie, 'POST', `/api/v1/orgs/org-a/invoices/${first}/payments`, { amount: 62000, paid_on: today, method: 'bank_transfer' });

	for (const c of [primary, co, stranger]) {
		await stack.api(cookie, 'POST', `/api/v1/orgs/org-a/contacts/${c.id}/portal-invite`);
	}

	// Primary tenant: Rent tab, ledger, receipt and statement downloads.
	await page.goto(await waitForMagicLink(primaryEmail));
	await page.getByRole('link', { name: 'Rent', exact: true }).click();
	await expect(page).toHaveURL(`${base}/portal/org-a/rent`);
	await expect(page.getByText('Paid to')).toBeVisible();
	await expect(page.getByText('Payment · Bank transfer')).toBeVisible();

	const pdf = page.waitForEvent('download');
	await page.getByRole('link', { name: 'Download PDF' }).click();
	const pdfFile = await pdf;
	expect(pdfFile.suggestedFilename()).toMatch(/^rent-statement-.*\.pdf$/);
	const { readFileSync } = await import('node:fs');
	expect(readFileSync((await pdfFile.path())!).subarray(0, 5).toString()).toBe('%PDF-');

	const csv = page.waitForEvent('download');
	await page.getByRole('link', { name: 'Download CSV' }).click();
	expect((await csv).suggestedFilename()).toMatch(/\.csv$/);

	await page.getByRole('link', { name: /^Receipt/ }).click();
	await expect(page.getByRole('heading', { name: 'Payment receipt' }).first()).toBeVisible();
	await expect(page.getByText('Bank transfer')).toBeVisible();

	// Co-tenant sees the same payment.
	const coContext = await browser.newContext();
	const coPage = await coContext.newPage();
	await coPage.goto(await waitForMagicLink(coEmail));
	await expect(coPage).toHaveURL(new RegExp(`${base}/portal/org-a/`));
	await coPage.goto(`${base}/portal/org-a/rent`);
	await expect(coPage.getByText('Payment · Bank transfer')).toBeVisible();
	await coContext.close();

	// A tenant not on the lease cannot read its receipt.
	const strangerContext = await browser.newContext();
	const strangerPage = await strangerContext.newPage();
	await strangerPage.goto(await waitForMagicLink(strangerEmail));
	await expect(strangerPage).toHaveURL(new RegExp(`${base}/portal/org-a/`));
	await strangerPage.goto(`${base}/portal/org-a/rent`);
	await expect(strangerPage.getByText('No leases yet')).toBeVisible();
	const receiptPath = new URL(page.url()).pathname;
	await strangerPage.goto(`${base}${receiptPath}`);
	await expect(strangerPage.getByText("Couldn't load the receipt")).toBeVisible();
	await strangerContext.close();
});
