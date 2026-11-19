import { test, expect } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { signIn, startStack, type Stack } from './stack';

// Repair tickets end to end: a tenant reports an urgent repair from the
// portal with a photo and a voice note, staff see it on the property's
// Maintenance tab (opening it marks it viewed), assign a tradesperson and
// leave an internal note and a shared message; the tenant sees the
// progress and the message but never the note, and the dashboard lists the
// open repair.

const ownerEmail = 'owner-maint@example.com';
const password = 'e2e-password';
const mailpitBase = 'http://127.0.0.1:8025';
const runID = randomUUID().slice(0, 8);
const tenantEmail = `maint-tenant-${runID}@example.com`;

// A minimal 1x1 PNG and a WebM (EBML) header, as a phone camera and
// MediaRecorder would produce.
const png = Buffer.from('89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c4890000000d49444154789c6360000002000154a24f5d0000000049454e44ae426082', 'hex');
const webm = Buffer.concat([Buffer.from([0x1a, 0x45, 0xdf, 0xa3]), Buffer.alloc(64)]);

let stack: Stack;
let base = '';

async function waitForMagicLink(to: string): Promise<string> {
	const deadline = Date.now() + 15_000;
	while (Date.now() < deadline) {
		const list = (await (await fetch(`${mailpitBase}/api/v1/messages`)).json()) as { messages: { ID: string; To: { Address: string }[] }[] };
		const msg = list.messages.find((m) => m.To.some((t) => t.Address === to));
		if (msg) {
			const full = (await (await fetch(`${mailpitBase}/api/v1/message/${msg.ID}`)).json()) as { Text: string };
			const match = full.Text.match(/(https?:\/\/\S*\/auth\/magic\?token=\S+)/);
			if (match) return match[1];
		}
		await new Promise((r) => setTimeout(r, 300));
	}
	throw new Error(`no magic-link email arrived for ${to}`);
}

test.beforeAll(async () => {
	stack = await startStack({
		db: 'shigoto_e2e_maintenance',
		owner: { email: ownerEmail, password },
		orgs: [{ slug: 'maint', name: 'Maint Pty Ltd' }],
		services: ['db', 'mailpit'],
		env: { SHIGOTO_SMTP_HOST: '127.0.0.1', SHIGOTO_SMTP_PORT: '1025', SHIGOTO_SMTP_FROM: 'noreply@shigoto.local', SHIGOTO_SMTP_TLS: 'none' }
	});
	base = stack.base;
});

test.afterAll(() => stack?.stop());

test('tenant reports a repair with media and follows staff progress', async ({ page, browser }) => {
	const cookie = await stack.login(ownerEmail, password);
	await stack.api(cookie, 'PUT', '/api/v1/orgs/maint/settings', { name: 'Maint Pty Ltd', invoice_lead_days: 7, emergency_phone: '0400 123 456' });
	const tenant = await stack.api<{ id: number }>(cookie, 'POST', '/api/v1/orgs/maint/contacts', { kind: 'tenant', name: 'Tina Tenant', email: tenantEmail });
	await stack.api(cookie, 'POST', '/api/v1/orgs/maint/contacts', { kind: 'supplier', name: 'Fitzroy Plumbing' });
	const property = await stack.api<{ id: number }>(cookie, 'POST', '/api/v1/orgs/maint/properties', { name: '12 Banksia St', address: '12 Banksia St, Fitzroy' });
	await stack.api(cookie, 'POST', '/api/v1/orgs/maint/leases', {
		property_id: property.id,
		tenants: [{ contact_id: tenant.id, primary: true }],
		start_date: '2026-01-05',
		frequency: 'weekly',
		rent_amount: 62000
	});
	await stack.api(cookie, 'POST', `/api/v1/orgs/maint/contacts/${tenant.id}/portal-invite`);

	// Tenant reports an urgent repair on a phone-sized screen.
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto(await waitForMagicLink(tenantEmail));
	await expect(page).toHaveURL(new RegExp(`${base}/portal/maint/`));
	await page.getByRole('link', { name: 'Repairs', exact: true }).click();
	await page.getByRole('link', { name: 'Report a repair' }).click();
	await page.getByLabel("What's wrong?").fill('Kitchen tap leaking');
	await page.getByLabel('Details').fill('Water pooling under the sink.');
	await page.getByLabel('Urgent').check();
	await expect(page.getByRole('alert')).toContainText('Call 0400 123 456 now');
	await page.getByLabel('Choose from library').setInputFiles([
		{ name: 'leak.png', mimeType: 'image/png', buffer: png },
		{ name: 'voice.webm', mimeType: 'audio/webm', buffer: webm }
	]);
	await expect(page.getByText('Uploaded')).toHaveCount(2);
	await page.getByLabel('Day', { exact: true }).fill('2099-01-05');
	await page.getByRole('button', { name: 'Add time' }).click();
	await page.getByLabel("OK to enter if I'm out").check();
	await page.getByLabel('Access notes (optional)').fill('Lockbox 4821');
	await page.getByRole('button', { name: 'Submit repair' }).click();
	await expect(page.getByRole('heading', { name: 'Kitchen tap leaking' })).toBeVisible();
	await expect(page.getByText('Request received')).toBeVisible();
	const ticketURL = page.url();
	await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

	// Staff open the Maintenance tab and the ticket.
	const staffContext = await browser.newContext();
	const staff = await staffContext.newPage();
	await signIn(staff, base, ownerEmail, password, '/maint');
	await expect(staff.getByRole('heading', { name: 'Repairs' })).toBeVisible();
	await staff.goto(`${base}/maint/properties/${property.id}/maintenance`);
	await staff.getByRole('link', { name: 'Kitchen tap leaking' }).click();
	await expect(staff.getByRole('img', { name: 'leak.png' })).toBeVisible();
	await expect(staff.locator('audio')).toHaveCount(1);
	await expect(staff.getByText('Lockbox 4821')).toBeVisible();
	await expect(staff.getByText('Viewed', { exact: true })).toBeVisible();

	await staff.getByRole('button', { name: 'Assign tradesperson' }).click();
	await staff.getByRole('dialog').getByRole('button', { name: 'Assign' }).click();
	await expect(staff.getByText('Tradesperson assigned', { exact: true }).first()).toBeVisible();

	await staff.getByRole('button', { name: 'Add note' }).click();
	await staff.getByLabel('Note', { exact: true }).fill('SENTINEL internal: plumber owes us');
	await staff.getByRole('button', { name: 'Save note' }).click();
	await staff.getByRole('button', { name: 'Message tenant' }).click();
	await staff.getByLabel('Message', { exact: true }).fill('Plumber will call ahead.');
	await staff.getByRole('button', { name: 'Send message' }).click();
	await expect(staff.getByText('Plumber will call ahead.')).toBeVisible();

	// The tenant's preference is shown, but is not permission.
	await expect(staff.getByText('Tenant preference: OK to enter if out')).toBeVisible();
	await expect(staff.getByText('Mon 5 Jan · Morning (8am–12pm)')).toBeVisible();
	await staff.getByRole('button', { name: 'Book visit' }).click();
	const dialog = staff.getByRole('dialog', { name: 'Book visit' });
	await dialog.getByLabel('Date', { exact: true }).fill('2099-01-05');
	await dialog.getByLabel('Arrive from').fill('09:00');
	await dialog.getByLabel('Arrive by').fill('11:00');
	await dialog.getByRole('button', { name: 'Book visit' }).click();
	await expect(staff.getByText('Mon 5 Jan, 9am – 11am')).toBeVisible();
	await expect(staff.getByText('Entry: not yet confirmed')).toBeVisible();
	const ticketPath = new URL(staff.url()).pathname;

	// The tenant sees progress and the shared message, never the note.
	await page.goto(ticketURL);
	await expect(page.getByText('Viewed by your property manager')).toBeVisible();
	await expect(page.getByText('Tradesperson assigned: Fitzroy Plumbing')).toBeVisible();
	await expect(page.getByText('Plumber will call ahead.')).toBeVisible();
	await expect(page.getByText(/SENTINEL/)).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Cancel request' })).toHaveCount(0);

	// Tenant confirms the visit, allowing entry.
	await expect(page.getByText('Mon 5 Jan, 9am – 11am')).toBeVisible();
	await expect(page.getByLabel("OK to enter if I'm out").last()).toBeChecked();
	await page.getByRole('button', { name: 'Confirm time' }).click();
	await expect(page.getByText("You've allowed entry if you're out")).toBeVisible();

	await staff.goto(`${base}${ticketPath}`);
	await expect(staff.getByText('Entry permitted')).toBeVisible();
	await staff.goto(`${base}/maint/properties/${property.id}/maintenance`);
	await expect(staff.getByText('Entry permitted')).toBeVisible();

	// Rescheduling clears consent: the tenant must confirm again.
	await staff.goto(`${base}${ticketPath}`);
	await staff.getByRole('button', { name: 'Reschedule' }).click();
	const re = staff.getByRole('dialog', { name: 'Reschedule visit' });
	await re.getByLabel('Arrive from').fill('13:00');
	await re.getByLabel('Arrive by').fill('15:00');
	await re.getByRole('button', { name: 'Reschedule' }).click();
	await expect(staff.getByText('Entry: not yet confirmed')).toBeVisible();

	await page.reload();
	await expect(page.getByText('Mon 5 Jan, 1pm – 3pm')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Confirm time' })).toBeVisible();
	await page.getByRole('button', { name: 'Request change' }).click();
	await page.getByLabel('What would suit you better?').fill('Mornings only please');
	await page.getByRole('button', { name: 'Send request' }).click();
	await expect(page.getByText('Change requested — waiting for your property manager')).toBeVisible();

	await staff.reload();
	await expect(staff.getByText('“Mornings only please”')).toBeVisible();
	await staffContext.close();
});
