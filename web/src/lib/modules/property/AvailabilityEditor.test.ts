import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import AvailabilityEditor from './AvailabilityEditor.svelte';
import type { Window } from './maintenance';

function setup(windows: Window[] = []) {
	const state = { windows };
	const r = render(AvailabilityEditor, { props: { windows, today: '2026-11-19', onchange: (w: Window[]) => (state.windows = w) } });
	return { ...r, state };
}

describe('AvailabilityEditor', () => {
	it('adds a morning window, never before today', async () => {
		const { state } = setup();
		const date = screen.getByLabelText('Day') as HTMLInputElement;
		expect(date.min).toBe('2026-11-19');
		await fireEvent.input(date, { target: { value: '2026-11-24' } });
		await fireEvent.change(screen.getByLabelText('Time of day'), { target: { value: 'morning' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Add time' }));
		expect(state.windows).toEqual([{ date: '2026-11-24', slot: 'morning', start: null, end: null }]);
		expect(screen.getByText('Tue 24 Nov · Morning (8am–12pm)')).toBeTruthy();
	});

	it('needs a custom start before the end', async () => {
		const { state } = setup();
		await fireEvent.input(screen.getByLabelText('Day'), { target: { value: '2026-11-24' } });
		await fireEvent.change(screen.getByLabelText('Time of day'), { target: { value: 'custom' } });
		await fireEvent.input(screen.getByLabelText('From'), { target: { value: '17:00' } });
		await fireEvent.input(screen.getByLabelText('Until'), { target: { value: '15:00' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Add time' }));
		expect(screen.getByText('Choose a start time before the end time.')).toBeTruthy();
		expect(state.windows).toEqual([]);
	});

	it('removes a window and stops at ten', async () => {
		const ten = Array.from({ length: 10 }, (_, i) => ({ date: `2026-12-${String(i + 1).padStart(2, '0')}`, slot: 'afternoon' as const, start: null, end: null }));
		const { state } = setup(ten);
		expect((screen.getByRole('button', { name: 'Add time' }) as HTMLButtonElement).disabled).toBe(true);
		await fireEvent.click(screen.getAllByRole('button', { name: /^Remove/ })[0]);
		expect(state.windows).toHaveLength(9);
	});
});
