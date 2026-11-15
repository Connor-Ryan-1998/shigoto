import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { goto, setPage } from '../test/app.svelte';
import { apiMock, deferred, mockApi } from '../test/mock-api';
import { ApiError } from '../api';
import { session } from '../session.svelte';
import CsvImport from './CsvImport.svelte';
import type { ImportField, ImportSummary } from './types';

const fields: ImportField[] = [
	{ key: 'number', label: 'Invoice number', required: true },
	{ key: 'total', label: 'Total', required: true },
	{ key: 'paid_date', label: 'Paid date', required: false }
];
const props = {
	fields,
	previewPath: '/api/v1/orgs/acme/invoices/import/preview',
	applyPath: '/api/v1/orgs/acme/invoices/import',
	backHref: '/acme/invoices',
	noun: 'invoices',
	summaryLabel: (s: ImportSummary) => `${s.new} new invoices`
};
const csv = 'Invoice number,Amount\nA-1,10\n';
const clean: ImportSummary = { new: 1, existing: 0, problems: [] };

async function pick(text = csv) {
	const input = await screen.findByLabelText('CSV file');
	await fireEvent.change(input, { target: { files: [new File([text], 'inv.csv', { type: 'text/csv' })] } });
	await screen.findByLabelText('Invoice number');
}

function lastBody(): FormData {
	const call = apiMock.mock.calls.at(-1)!;
	return call[2] as FormData;
}

beforeEach(() => {
	setPage({ params: { org: 'acme' } });
	session.setMe({ user: { id: 1, email: 'o@example.com', name: 'O' }, orgs: [{ slug: 'acme', name: 'Acme', role: 'owner', perms: ['import'] }] });
	session.setOrg('acme');
});

describe('CsvImport', () => {
	it('lists the file headers for mapping and guesses matches', async () => {
		render(CsvImport, props);
		await pick();
		const number = screen.getByLabelText('Invoice number') as HTMLSelectElement;
		expect(number.value).toBe('Invoice number');
		expect([...number.options].map((o) => o.text)).toEqual(['— not mapped —', 'Invoice number', 'Amount']);
		expect((screen.getByLabelText('Total') as HTMLSelectElement).value).toBe('');
	});

	it('previews with the file and mapping, then imports', async () => {
		mockApi({ [`POST ${props.previewPath}`]: clean, [`POST ${props.applyPath}`]: clean });
		render(CsvImport, props);
		await pick();
		await fireEvent.change(screen.getByLabelText('Total'), { target: { value: 'Amount' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
		await screen.findByText('1 new invoices');

		const body = lastBody();
		expect((body.get('file') as File).name).toBe('inv.csv');
		expect(JSON.parse(body.get('mapping') as string)).toEqual({
			columns: { number: 'Invoice number', total: 'Amount', paid_date: '' },
			date_format: '02/01/2006'
		});

		await fireEvent.click(screen.getByRole('button', { name: 'Import' }));
		await screen.findByRole('link', { name: 'Back to invoices' });
		expect(apiMock).toHaveBeenLastCalledWith('POST', props.applyPath, expect.any(FormData));
	});

	it('disables Import while the preview has problems', async () => {
		mockApi({ [`POST ${props.previewPath}`]: { new: 0, existing: 0, problems: [{ row: 2, field: 'total', message: 'invalid total' }] } });
		render(CsvImport, props);
		await pick();
		await fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
		await screen.findByText('invalid total');
		expect((screen.getByRole('button', { name: 'Import' }) as HTMLButtonElement).disabled).toBe(true);
	});

	it('needs a fresh preview after the mapping changes', async () => {
		mockApi({ [`POST ${props.previewPath}`]: clean });
		render(CsvImport, props);
		await pick();
		await fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
		await screen.findByText('1 new invoices');
		expect((screen.getByRole('button', { name: 'Import' }) as HTMLButtonElement).disabled).toBe(false);
		await fireEvent.change(screen.getByLabelText('Total'), { target: { value: 'Amount' } });
		expect((screen.getByRole('button', { name: 'Import' }) as HTMLButtonElement).disabled).toBe(true);
	});

	it('sends one import while busy', async () => {
		const apply = deferred<ImportSummary>();
		mockApi({ [`POST ${props.previewPath}`]: clean, [`POST ${props.applyPath}`]: apply.promise });
		render(CsvImport, props);
		await pick();
		await fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
		await screen.findByText('1 new invoices');
		const button = screen.getByRole('button', { name: 'Import' });
		await fireEvent.click(button);
		await fireEvent.click(button);
		expect(apiMock.mock.calls.filter((c) => c[1] === props.applyPath)).toHaveLength(1);
		apply.resolve(clean);
		await screen.findByRole('link', { name: 'Back to invoices' });
	});

	it('keeps the file and mapping after a failed import and shows its problems', async () => {
		const failure = new ApiError(422, 'import has problems; preview again', undefined, [{ row: 4, field: 'number', message: 'gone wrong' }]);
		mockApi({ [`POST ${props.previewPath}`]: clean, [`POST ${props.applyPath}`]: Promise.reject(failure) });
		render(CsvImport, props);
		await pick();
		await fireEvent.change(screen.getByLabelText('Total'), { target: { value: 'Amount' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
		await screen.findByText('1 new invoices');
		await fireEvent.click(screen.getByRole('button', { name: 'Import' }));
		await screen.findByText('import has problems; preview again');
		await screen.findByText('gone wrong');
		expect((screen.getByLabelText('Total') as HTMLSelectElement).value).toBe('Amount');
		expect((screen.getByRole('button', { name: 'Import' }) as HTMLButtonElement).disabled).toBe(true);
		await fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
		await waitFor(() => expect(lastBody().get('file')).toBeInstanceOf(File));
	});

	it('cancels back to the list', async () => {
		render(CsvImport, props);
		await fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
		expect(goto).toHaveBeenCalledWith('/acme/invoices');
	});
});
