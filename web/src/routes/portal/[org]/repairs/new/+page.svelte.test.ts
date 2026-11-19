import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { goto } from '$app/navigation';
import { setPage } from '../../../../../lib/test/app.svelte';
import { apiError, apiMock, mockApi, pending } from '../../../../../lib/test/mock-api';
import Page from './+page.svelte';

const list = (phone = '0400 123 456') => ({ emergency_phone: phone, leases: [{ id: 4, property_address: '12 Banksia St', active: true }], tickets: [] });

beforeEach(() => setPage({ params: { org: 'acme' }, url: new URL('http://localhost/portal/acme/repairs/new') }));

async function fillBasics() {
	await fireEvent.input(await screen.findByLabelText("What's wrong?"), { target: { value: 'Tap leaking' } });
	await fireEvent.input(screen.getByLabelText('Details'), { target: { value: 'Water under the sink' } });
}

describe('report a repair', () => {
	it('shows the call-now banner for urgent repairs', async () => {
		mockApi({ 'GET /api/v1/portal/acme/maintenance': list() });
		render(Page);
		await screen.findByLabelText("What's wrong?");
		expect(screen.queryByRole('alert')).toBeNull();
		await fireEvent.click(screen.getByLabelText('Urgent'));
		const banner = screen.getByRole('alert');
		expect(banner.textContent).toContain('Burst pipe, gas leak, no power or hot water, or anything unsafe?');
		expect(screen.getByRole('link', { name: '0400 123 456' }).getAttribute('href')).toBe('tel:0400123456');
	});

	it('falls back to phoning the property manager without a number', async () => {
		mockApi({ 'GET /api/v1/portal/acme/maintenance': list('') });
		render(Page);
		await fireEvent.click(await screen.findByLabelText('Urgent'));
		expect(screen.getByRole('alert').textContent).toContain('Phone your property manager now, then log it here.');
	});

	it('submits with uploaded media and opens the new ticket', async () => {
		mockApi({
			'GET /api/v1/portal/acme/maintenance': list(),
			'POST /api/v1/portal/acme/maintenance/uploads': { pending_id: 11, file: { id: 3 } },
			'POST /api/v1/portal/acme/maintenance': { id: 9 }
		});
		render(Page);
		await fillBasics();
		await fireEvent.change(screen.getByLabelText('Choose from library'), { target: { files: [new File(['x'], 'leak.jpg')] } });
		await screen.findByRole('button', { name: 'Remove leak.jpg' });
		await fireEvent.submit(screen.getByRole('button', { name: 'Submit repair' }).closest('form')!);
		await screen.findByRole('button', { name: 'Submit repair' });
		expect(apiMock).toHaveBeenCalledWith('POST', '/api/v1/portal/acme/maintenance', expect.objectContaining({
			lease_id: 4, title: 'Tap leaking', description: 'Water under the sink', category: 'plumbing', urgency: 'routine', pending_file_ids: [11]
		}));
		expect(goto).toHaveBeenCalledWith('/portal/acme/repairs/9');
	});

	it('blocks submit while an upload is in flight', async () => {
		mockApi({ 'GET /api/v1/portal/acme/maintenance': list(), 'POST /api/v1/portal/acme/maintenance/uploads': pending() });
		render(Page);
		await fillBasics();
		await fireEvent.change(screen.getByLabelText('Choose from library'), { target: { files: [new File(['x'], 'big.mov')] } });
		const submit = screen.getByRole('button', { name: /Submit repair|Waiting for uploads/ }) as HTMLButtonElement;
		expect(submit.disabled).toBe(true);
	});

	it('keeps input after a rejected submit', async () => {
		mockApi({ 'GET /api/v1/portal/acme/maintenance': list(), 'POST /api/v1/portal/acme/maintenance': apiError(422, 'invalid input: lease is not active') });
		render(Page);
		await fillBasics();
		await fireEvent.submit(screen.getByRole('button', { name: 'Submit repair' }).closest('form')!);
		expect(await screen.findByText('invalid input: lease is not active')).toBeTruthy();
		expect((screen.getByLabelText("What's wrong?") as HTMLInputElement).value).toBe('Tap leaking');
	});
});
