import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { tick } from 'svelte';
import { apiError, apiMock, deferred, mockApi } from '../../test/mock-api';
import { signIn } from '../../test/session';
import CashFlowCard from './CashFlowCard.svelte';

const currentPath = 'GET /api/v1/orgs/acme/cash-flow?period=current';
const previousPath = 'GET /api/v1/orgs/acme/cash-flow?period=previous';
const activity = {
	from: '2026-07-01', to: '2027-06-30',
	months: Array.from({ length: 12 }, (_, i) => ({
		month: `${i < 6 ? 2026 : 2027}-${String((i + 6) % 12 + 1).padStart(2, '0')}-01`,
		incoming: i === 0 ? 15000 : 0, outgoing: i === 0 ? 4000 : 0
	}))
};
const previous = { from: '2025-07-01', to: '2026-06-30', months: activity.months.map(m => ({ ...m, month: m.month.replace('2026', '2025').replace('2027', '2026'), incoming: 0, outgoing: 200 })) };

beforeEach(() => {
	signIn();
});

describe('Cash Flow card', () => {
	it('shows imported cash movement, totals and an accessible monthly table', async () => {
		mockApi({ [currentPath]: activity });
		render(CashFlowCard, { org: 'acme' });
		const totals = await screen.findByRole('group', { name: 'Cash flow totals' });
		expect(within(totals).getByText('$150.00')).toBeTruthy();
		expect(within(totals).getByText('$40.00')).toBeTruthy();
		expect(within(totals).getByText('$110.00')).toBeTruthy();
		expect(screen.getByRole('table', { name: /monthly cash flow/i }).querySelectorAll('tbody tr')).toHaveLength(12);
		expect(screen.getByText(/imported bank activity/i)).toBeTruthy();
	});

	it('changes only cash flow when selecting the previous year, including negative net', async () => {
		mockApi({ [currentPath]: activity, [previousPath]: previous });
		render(CashFlowCard, { org: 'acme' });
		await screen.findByRole('group', { name: 'Cash flow totals' });
		await fireEvent.change(screen.getByLabelText('Cash flow period'), { target: { value: 'previous' } });
		await waitFor(() => expect(screen.getByText('1 July 2025 – 30 June 2026')).toBeTruthy());
		expect(within(screen.getByRole('group', { name: 'Cash flow totals' })).getByText('-$24.00')).toBeTruthy();
		expect(apiMock.mock.calls.map(c => c[1])).toEqual([currentPath.slice(4), previousPath.slice(4)]);
	});

	it('renders an empty year as zero movement', async () => {
		mockApi({ [currentPath]: { ...activity, months: activity.months.map(m => ({ ...m, incoming: 0, outgoing: 0 })) } });
		render(CashFlowCard, { org: 'acme' });
		const totals = await screen.findByRole('group', { name: 'Cash flow totals' });
		expect(within(totals).getAllByText('$0.00')).toHaveLength(3);
		expect(screen.getByText('No imported bank activity for this period.')).toBeTruthy();
	});

	it('retries a failed selected period without breaking the card', async () => {
		mockApi({ [currentPath]: activity, [previousPath]: apiError(500, 'Unavailable') });
		render(CashFlowCard, { org: 'acme' });
		await screen.findByRole('group', { name: 'Cash flow totals' });
		await fireEvent.change(screen.getByLabelText('Cash flow period'), { target: { value: 'previous' } });
		expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Unavailable');
		mockApi({ [previousPath]: previous });
		await fireEvent.click(screen.getByRole('button', { name: 'Retry cash flow' }));
		await screen.findByRole('group', { name: 'Cash flow totals' });
		expect(screen.queryByRole('alert')).toBeNull();
	});

	it.each(['resolve', 'reject'] as const)('ignores an old period %s after selection changes', async (settle) => {
		const old = deferred();
		mockApi({ [currentPath]: old.promise, [previousPath]: previous });
		render(CashFlowCard, { org: 'acme' });
		await fireEvent.change(screen.getByLabelText('Cash flow period'), { target: { value: 'previous' } });
		await screen.findByRole('group', { name: 'Cash flow totals' });
		if (settle === 'resolve') old.resolve(activity); else old.reject(new Error('Old failure'));
		await tick();
		expect(screen.queryByText('$150.00')).toBeNull();
		expect(screen.queryByRole('alert')).toBeNull();
	});

	it('clears old-org data and ignores a late old-org response', async () => {
		const old = deferred();
		mockApi({ [currentPath]: old.promise, 'GET /api/v1/orgs/other/cash-flow?period=current': previous });
		const view = render(CashFlowCard, { org: 'acme' });
		signIn({ org: 'other' });
		await view.rerender({ org: 'other' });
		await screen.findByRole('group', { name: 'Cash flow totals' });
		old.resolve(activity);
		await tick();
		expect(screen.queryByText('$150.00')).toBeNull();
	});

	it('clears resolved data on org change and permission loss', async () => {
		mockApi({ [currentPath]: activity, 'GET /api/v1/orgs/other/cash-flow?period=current': deferred().promise });
		const view = render(CashFlowCard, { org: 'acme' });
		await screen.findByRole('group', { name: 'Cash flow totals' });
		signIn({ org: 'other' });
		await view.rerender({ org: 'other' });
		expect(screen.queryByText('$150.00')).toBeNull();
		signIn({ org: 'other', role: 'client', perms: [] });
		await tick();
		expect(screen.queryByRole('group', { name: 'Cash flow totals' })).toBeNull();
	});

	it('ignores a request failure after unmount', async () => {
		const old = deferred();
		mockApi({ [currentPath]: old.promise });
		const view = render(CashFlowCard, { org: 'acme' });
		await tick();
		view.unmount();
		old.reject(new Error('Late'));
		await tick();
		expect(screen.queryByRole('alert')).toBeNull();
	});
});
