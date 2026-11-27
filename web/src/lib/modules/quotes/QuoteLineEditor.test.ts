import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';

import QuoteLineEditor, { parseRows, rowsFromLines, type LineRow } from './QuoteLineEditor.svelte';

function rows(...r: Partial<LineRow>[]): LineRow[] {
	return r.map((x, i) => ({ id: 10_000 + i, kind: 'labour', description: '', quantity: '', unitAmount: '', gstCode: 'GST', ...x }));
}

describe('QuoteLineEditor', () => {
	it('labels a labour line in hours and an hourly rate', () => {
		render(QuoteLineEditor, { rows: rows({}) });
		expect(screen.getByLabelText('Hours')).toBeTruthy();
		expect(screen.getByLabelText('Rate ($/h)')).toBeTruthy();
	});

	it('relabels a row when it becomes a material line', async () => {
		const onchange = vi.fn();
		render(QuoteLineEditor, { rows: rows({}), onchange });
		await fireEvent.change(screen.getByLabelText('Line type'), { target: { value: 'material' } });
		expect(screen.getByLabelText('Quantity')).toBeTruthy();
		expect(screen.getByLabelText('Unit price ($)')).toBeTruthy();
		expect(onchange).toHaveBeenCalled();
	});

	it('adds labour and material lines', async () => {
		render(QuoteLineEditor, { rows: rows({}) });
		await fireEvent.click(screen.getByRole('button', { name: 'Add material' }));
		await fireEvent.click(screen.getByRole('button', { name: 'Add labour' }));
		expect(screen.getAllByLabelText('Hours')).toHaveLength(2);
		expect(screen.getAllByLabelText('Quantity')).toHaveLength(1);
	});

	it('removes a line', async () => {
		render(QuoteLineEditor, { rows: rows({ description: 'A' }, { description: 'B' }) });
		await fireEvent.click(screen.getByRole('button', { name: 'Remove line 1' }));
		expect((screen.getByLabelText('Description') as HTMLInputElement).value).toBe('B');
	});

	it('flags an invalid amount', () => {
		render(QuoteLineEditor, { rows: rows({ quantity: 'abc', unitAmount: '1.234' }) });
		expect(screen.getByText('Enter a valid quantity, up to 3 decimal places.')).toBeTruthy();
		expect(screen.getByText('Enter a valid amount, up to 2 decimal places.')).toBeTruthy();
	});
});

describe('parseRows', () => {
	it('converts rows to request lines', () => {
		expect(parseRows(rows(
			{ kind: 'labour', description: ' Rough-in ', quantity: '2.5', unitAmount: '95', gstCode: 'GST' },
			{ kind: 'material', description: 'Cable', quantity: '1', unitAmount: '180.50', gstCode: 'FRE' }
		))).toEqual([
			{ kind: 'labour', description: 'Rough-in', quantity_milli: 2500, unit_amount: 9500, gst_code: 'GST' },
			{ kind: 'material', description: 'Cable', quantity_milli: 1000, unit_amount: 18050, gst_code: 'FRE' }
		]);
	});

	it('rejects an empty list or an incomplete row', () => {
		expect(parseRows([])).toBeNull();
		expect(parseRows(rows({ description: '', quantity: '1', unitAmount: '1' }))).toBeNull();
		expect(parseRows(rows({ description: 'x', quantity: '0', unitAmount: '1' }))).toBeNull();
	});

	it('round-trips stored lines', () => {
		const back = rowsFromLines([{ kind: 'labour', description: 'Fit-off', quantity_milli: 4000, unit_amount: 9500, gst_code: 'GST' }]);
		expect(back[0]).toMatchObject({ kind: 'labour', description: 'Fit-off', quantity: '4', unitAmount: '95.00', gstCode: 'GST' });
		expect(parseRows(back)).toEqual([{ kind: 'labour', description: 'Fit-off', quantity_milli: 4000, unit_amount: 9500, gst_code: 'GST' }]);
	});
});
