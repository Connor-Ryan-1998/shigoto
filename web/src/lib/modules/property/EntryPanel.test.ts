import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import EntryPanel from './EntryPanel.svelte';
import type { EntryState, Visit } from './maintenance';

const visit = (entry_state: string): Visit => ({
	id: 1, supplier: { id: 2, name: 'Fitzroy Plumbing' }, starts_at: '2026-11-24T09:00:00+10:00', ends_at: '2026-11-24T11:00:00+10:00',
	status: entry_state === 'unconfirmed' ? 'booked' : 'confirmed', tenant_confirmed_at: null, change_note: '', entry_state: entry_state as EntryState,
	entry_granted_at: entry_state === 'permitted' ? '2026-11-22T11:47:00Z' : null, entry_withdrawn_at: null
});

describe('EntryPanel', () => {
	it.each([
		['permitted', 'Entry permitted'],
		['tenant_home', 'Tenant will be home'],
		['withdrawn', 'Entry withdrawn'],
		['unconfirmed', 'Entry: not yet confirmed']
	])('labels %s in words', (state, label) => {
		render(EntryPanel, { visit: visit(state), preference: null, accessNotes: '' });
		expect(screen.getByText(label)).toBeTruthy();
	});

	it('puts access notes up front when entry is permitted', () => {
		render(EntryPanel, { visit: visit('permitted'), preference: null, accessNotes: 'Lockbox 4821' });
		expect(screen.getByText('Lockbox 4821').closest('[data-entry="permitted"]')).toBeTruthy();
	});

	it('shows the tenant preference only while unconfirmed', () => {
		const { unmount } = render(EntryPanel, { visit: visit('unconfirmed'), preference: 'permitted', accessNotes: '' });
		expect(screen.getByText('Tenant preference: OK to enter if out')).toBeTruthy();
		unmount();
		render(EntryPanel, { visit: visit('tenant_home'), preference: 'permitted', accessNotes: '' });
		expect(screen.queryByText(/Tenant preference/)).toBeNull();
	});

	it('shows the preference with no visit booked', () => {
		render(EntryPanel, { visit: null, preference: 'tenant_home', accessNotes: '' });
		expect(screen.getByText("Tenant preference: I'll be home")).toBeTruthy();
	});
});
