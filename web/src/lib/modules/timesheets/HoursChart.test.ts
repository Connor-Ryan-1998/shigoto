import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import HoursChart from './HoursChart.svelte';
import type { Entry } from './types';

const entry = (work_date: string, minutes: number, extra: Partial<Entry> = {}) =>
	({ id: Math.random(), work_date, minutes, billing_method: 'tm', ...extra }) as Entry;

describe('HoursChart', () => {
	it('buckets this week by day and billing status', () => {
		// 2026-11-08 is a Sunday: week is Mon 2 - Sun 8 Nov.
		render(HoursChart, {
			today: '2026-11-08',
			entries: [
				entry('2026-11-02', 60, { invoice_line_id: 1 }),
				entry('2026-11-02', 30),
				entry('2026-11-08', 45, { billing_method: 'fixed' }),
				entry('2026-11-01', 600) // previous week: ignored
			]
		});
		const items = screen.getAllByRole('listitem').map((li) => li.textContent);
		expect(items).toHaveLength(7);
		expect(items[0]).toContain('01:00 billed, 00:30 unbilled, 00:00 fixed fee');
		expect(items[6]).toContain('00:00 billed, 00:00 unbilled, 00:45 fixed fee');
		expect(items.slice(1, 6).every((t) => t?.includes('00:00 billed, 00:00 unbilled, 00:00 fixed fee'))).toBe(true);
	});

	it('is not a <figure>, which DaisyUI cards lay out as a centred row', () => {
		const { container } = render(HoursChart, { today: '2026-11-08', entries: [] });
		expect(container.querySelector('figure')).toBeNull();
		expect(screen.getByRole('figure', { name: /hours/i })).toBeTruthy();
	});

	it('steps between weeks and cannot go past the current week', async () => {
		render(HoursChart, { today: '2026-11-21', entries: [entry('2026-11-10', 90)] });
		expect(screen.getByText('No time logged this week.')).toBeTruthy();
		expect((screen.getByRole('button', { name: /next week/i }) as HTMLButtonElement).disabled).toBe(true);

		await fireEvent.click(screen.getByRole('button', { name: /previous week/i }));
		expect(screen.queryByText('No time logged this week.')).toBeNull();
		expect(screen.getAllByRole('listitem')[1].textContent).toContain('00:00 billed, 01:30 unbilled');

		await fireEvent.click(screen.getByRole('button', { name: /current week/i }));
		expect(screen.getByText('No time logged this week.')).toBeTruthy();
	});
});
