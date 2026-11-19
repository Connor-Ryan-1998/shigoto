import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { apiMock, mockApi } from '../../test/mock-api';
import VisitCard from './VisitCard.svelte';
import type { Visit } from './maintenance';

const future = '2099-11-24T09:00:00+10:00';
const visit = (over: Partial<Visit> = {}): Visit => ({
	id: 3, supplier: { id: 4, name: 'Fitzroy Plumbing' }, starts_at: future, ends_at: '2099-11-24T11:00:00+10:00', status: 'booked',
	tenant_confirmed_at: null, change_note: '', entry_state: 'unconfirmed', entry_granted_at: null, entry_withdrawn_at: null, ...over
});
const base = '/api/v1/portal/acme/maintenance/7';

describe('VisitCard', () => {
	it('shows the org-time window and confirms with entry prefilled from the preference', async () => {
		mockApi({ [`POST ${base}/visits/3/confirm`]: visit({ status: 'confirmed', entry_state: 'permitted' }) });
		const onchanged = vi.fn();
		render(VisitCard, { visit: visit(), preference: 'permitted', apiBase: base, onchanged });
		expect(screen.getByText('Tue 24 Nov, 9am – 11am')).toBeTruthy();
		expect(screen.getByText('Fitzroy Plumbing')).toBeTruthy();
		expect((screen.getByLabelText("OK to enter if I'm out") as HTMLInputElement).checked).toBe(true);
		await fireEvent.click(screen.getByRole('button', { name: 'Confirm time' }));
		await vi.waitFor(() => expect(onchanged).toHaveBeenCalled());
		expect(apiMock).toHaveBeenCalledWith('POST', `${base}/visits/3/confirm`, { entry_mode: 'permitted' });
	});

	it('confirms as home when chosen', async () => {
		mockApi({ [`POST ${base}/visits/3/confirm`]: visit({ status: 'confirmed', entry_state: 'tenant_home' }) });
		render(VisitCard, { visit: visit(), preference: null, apiBase: base, onchanged: vi.fn() });
		await fireEvent.click(screen.getByLabelText("I'll be home"));
		await fireEvent.click(screen.getByRole('button', { name: 'Confirm time' }));
		await vi.waitFor(() => expect(apiMock).toHaveBeenCalledWith('POST', `${base}/visits/3/confirm`, { entry_mode: 'tenant_home' }));
	});

	it('lets the tenant withdraw entry before the visit starts', async () => {
		mockApi({ [`POST ${base}/visits/3/withdraw-entry`]: visit({ status: 'confirmed', entry_state: 'withdrawn' }) });
		render(VisitCard, { visit: visit({ status: 'confirmed', entry_state: 'permitted' }), preference: null, apiBase: base, onchanged: vi.fn() });
		expect(screen.getByText("You've allowed entry if you're out")).toBeTruthy();
		await fireEvent.click(screen.getByRole('button', { name: 'Withdraw permission' }));
		await fireEvent.click(screen.getByRole('dialog').querySelector('button[type=submit]')!);
		await vi.waitFor(() => expect(apiMock).toHaveBeenCalledWith('POST', `${base}/visits/3/withdraw-entry`, undefined));
	});

	it('hides withdraw once the visit has started', () => {
		render(VisitCard, { visit: visit({ status: 'confirmed', entry_state: 'permitted', starts_at: '2020-01-01T09:00:00+10:00', ends_at: '2020-01-01T11:00:00+10:00' }), preference: null, apiBase: base, onchanged: vi.fn() });
		expect(screen.queryByRole('button', { name: 'Withdraw permission' })).toBeNull();
	});

	it('says a change is pending', () => {
		render(VisitCard, { visit: visit({ status: 'change_requested', change_note: 'Working' }), preference: null, apiBase: base, onchanged: vi.fn() });
		expect(screen.getByText('Change requested — waiting for your property manager')).toBeTruthy();
	});
});
