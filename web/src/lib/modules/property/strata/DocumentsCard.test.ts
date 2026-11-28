import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/svelte';
import { apiError, apiMock, deferred, mockApi } from '../../../test/mock-api';
import { signIn } from '../../../test/session';
import DocumentsCard from './DocumentsCard.svelte';
import type { StrataDocument, StrataScheme } from '../types';

const scheme: StrataScheme = {
	id: 3, name: 'Harbour View CTS', cts_number: '', is_chairperson: true, is_secretary: false, is_treasurer: false, properties: []
};
const list = '/api/v1/orgs/acme/strata-schemes/3/documents';
const doc = (over: Partial<StrataDocument>): StrataDocument => ({
	id: 1, doc_type: 'minutes', title: 'AGM 2024', document_date: '2024-09-12', status: 'indexed', error: '',
	page_count: 12, extract_model: 'm', chunks_total: 4, chunks_embedded: 4, ...over
});

function mount(canWrite = true, s: StrataScheme = scheme) {
	return render(DocumentsCard, { props: { org: 'acme', scheme: s, canWrite } });
}
const rows = () => screen.getAllByRole('row').slice(1);

beforeEach(() => signIn());
afterEach(() => vi.useRealTimers());

describe('strata documents card', () => {
	it('lists documents with type, date, pages and status', async () => {
		mockApi({ [`GET ${list}`]: [
			doc({ id: 1 }),
			doc({ id: 2, title: 'Budget FY25', doc_type: 'budget', status: 'pending', page_count: 3 }),
			doc({ id: 3, title: 'Levy notice', doc_type: 'levy_notice', status: 'failed', error: 'The model provider is unavailable (HTTP 503).' })
		] });
		mount();
		expect(await screen.findByText('AGM 2024')).toBeTruthy();
		const [first, second, third] = rows();
		expect(within(first).getByText('Minutes')).toBeTruthy();
		expect(within(first).getByText('Indexed')).toBeTruthy();
		expect(within(first).getByText('12')).toBeTruthy();
		expect(within(second).getByText('Budget')).toBeTruthy();
		expect(within(second).getByText('Indexing…')).toBeTruthy();
		expect(within(third).getByText('Failed')).toBeTruthy();
		expect(within(third).getByText('The model provider is unavailable (HTTP 503).')).toBeTruthy();
		const link = within(first).getByRole('link', { name: 'AGM 2024' }) as HTMLAnchorElement;
		expect(link.getAttribute('href')).toBe(`${list}/1/file`);
		// Served inline: a full browser load, not a SvelteKit navigation.
		expect(link.hasAttribute('data-sveltekit-reload')).toBe(true);
	});

	it('shows an empty state', async () => {
		mockApi({ [`GET ${list}`]: [] });
		mount();
		expect(await screen.findByText(/No documents yet/)).toBeTruthy();
	});

	it('hides write actions without property_write', async () => {
		mockApi({ [`GET ${list}`]: [doc({ status: 'failed' })] });
		mount(false);
		await screen.findByText('AGM 2024');
		expect(screen.queryByRole('button', { name: 'Upload document' })).toBeNull();
		expect(screen.queryByRole('button', { name: /Reindex/ })).toBeNull();
		expect(screen.queryByRole('button', { name: /Delete/ })).toBeNull();
	});

	it('polls while a document is indexing and stops once indexed', async () => {
		vi.useFakeTimers();
		let calls = 0;
		mockApi({ [`GET ${list}`]: () => (++calls < 3 ? [doc({ status: calls === 1 ? 'pending' : 'extracting' })] : [doc({})]) });
		mount();
		await vi.waitFor(() => expect(calls).toBe(1));
		await vi.advanceTimersByTimeAsync(5000);
		expect(calls).toBe(2);
		await vi.advanceTimersByTimeAsync(5000);
		expect(calls).toBe(3);
		await vi.advanceTimersByTimeAsync(20000);
		expect(calls).toBe(3);
		expect(screen.getByText('Indexed')).toBeTruthy();
	});

	it('does not poll when everything is settled', async () => {
		vi.useFakeTimers();
		let calls = 0;
		mockApi({ [`GET ${list}`]: () => { calls++; return [doc({}), doc({ id: 2, status: 'failed' })]; } });
		mount();
		await vi.waitFor(() => expect(calls).toBe(1));
		await vi.advanceTimersByTimeAsync(30000);
		expect(calls).toBe(1);
	});

	it('confirms before deleting', async () => {
		mockApi({ [`GET ${list}`]: [doc({})], [`DELETE ${list}/1`]: null });
		const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true);
		mount();
		await fireEvent.click(await screen.findByRole('button', { name: 'Delete AGM 2024' }));
		expect(apiMock).not.toHaveBeenCalledWith('DELETE', `${list}/1`);
		await fireEvent.click(screen.getByRole('button', { name: 'Delete AGM 2024' }));
		await vi.waitFor(() => expect(apiMock).toHaveBeenCalledWith('DELETE', `${list}/1`));
		expect(confirm).toHaveBeenCalledTimes(2);
		await vi.waitFor(() => expect(apiMock.mock.calls.filter(([m, p]) => m === 'GET' && p === list)).toHaveLength(2));
	});

	it('reindexes a failed document', async () => {
		mockApi({ [`GET ${list}`]: [doc({ status: 'failed', error: 'x' })], [`POST ${list}/1/reindex`]: null });
		mount();
		await fireEvent.click(await screen.findByRole('button', { name: 'Reindex AGM 2024' }));
		await vi.waitFor(() => expect(apiMock).toHaveBeenCalledWith('POST', `${list}/1/reindex`));
	});

	it('reports a failed action without losing the list', async () => {
		mockApi({ [`GET ${list}`]: [doc({})], [`POST ${list}/1/reindex`]: apiError(500, 'internal error') });
		mount();
		await fireEvent.click(await screen.findByRole('button', { name: 'Reindex AGM 2024' }));
		expect(await screen.findByRole('alert')).toBeTruthy();
		expect(screen.getByText('AGM 2024')).toBeTruthy();
	});

	it('ignores a stale response after the scheme changes', async () => {
		const slow = deferred<StrataDocument[]>();
		const other = { ...scheme, id: 4 };
		mockApi({ [`GET ${list}`]: slow.promise, ['GET /api/v1/orgs/acme/strata-schemes/4/documents']: [doc({ id: 9, title: 'Other AGM' })] });
		const { rerender } = mount();
		await rerender({ org: 'acme', scheme: other, canWrite: true });
		expect(await screen.findByText('Other AGM')).toBeTruthy();
		slow.resolve([doc({ title: 'Stale AGM' })]);
		await new Promise((r) => setTimeout(r, 0));
		expect(screen.queryByText('Stale AGM')).toBeNull();
	});
});
