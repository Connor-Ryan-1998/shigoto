import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { apiError, apiMock, deferred, mockApi } from '../../../test/mock-api';
import { signIn } from '../../../test/session';
import { session } from '../../../session.svelte';
import AskPanel from './AskPanel.svelte';

const ask = '/api/v1/orgs/acme/strata-schemes/3/ask';
const answer = (over = {}) => ({
	answer: 'The committee resolved to replace the roof membrane [1].',
	citations: [{ document_id: 7, title: 'AGM 2024', document_date: '2024-09-12', page_no: 2 }],
	grounded: true,
	...over
});

function mount(role: 'chairperson' | 'treasurer' = 'chairperson') {
	render(AskPanel, { props: { org: 'acme', schemeId: 3, role } });
}
const box = () => screen.getByLabelText('Question', { exact: true }) as HTMLTextAreaElement;
const askButton = () => screen.getByRole('button', { name: 'Ask' }) as HTMLButtonElement;

beforeEach(() => {
	signIn();
	session.setStrataAssistant(true);
});

describe('strata ask panel', () => {
	it('asks the role assistant and renders a cited answer', async () => {
		mockApi({ [`POST ${ask}`]: answer() });
		mount();
		expect(box().placeholder).toContain('roof in 2024');
		await fireEvent.input(box(), { target: { value: '  What did the committee decide about the roof?  ' } });
		await fireEvent.click(askButton());
		expect(await screen.findByText(/replace the roof membrane/)).toBeTruthy();
		expect(apiMock).toHaveBeenCalledWith('POST', ask, { role: 'chairperson', question: 'What did the committee decide about the roof?' },
			expect.objectContaining({ signal: expect.any(AbortSignal) }));
		const cite = screen.getByRole('link', { name: /AGM 2024/ }) as HTMLAnchorElement;
		expect(cite.getAttribute('href')).toBe('/api/v1/orgs/acme/strata-schemes/3/documents/7/file#page=2');
		expect(cite.textContent).toContain('p. 2');
		expect(screen.queryByText(/No cited source supports this answer/)).toBeNull();
	});

	it('uses the treasurer placeholder and role', async () => {
		mockApi({ [`POST ${ask}`]: answer({ citations: [], grounded: false }) });
		mount('treasurer');
		expect(box().placeholder).toContain('sinking fund');
		await fireEvent.input(box(), { target: { value: 'Levies?' } });
		await fireEvent.click(askButton());
		expect(await screen.findByText(/No cited source supports this answer — check the documents\./)).toBeTruthy();
		expect(apiMock.mock.calls[0][2]).toEqual({ role: 'treasurer', question: 'Levies?' });
	});

	it('cannot ask an empty question', async () => {
		mockApi({});
		mount();
		expect(askButton().disabled).toBe(true);
		await fireEvent.input(box(), { target: { value: '   ' } });
		expect(askButton().disabled).toBe(true);
	});

	it('sends one request while busy and can cancel', async () => {
		const slow = deferred();
		mockApi({ [`POST ${ask}`]: slow.promise });
		mount();
		await fireEvent.input(box(), { target: { value: 'roof?' } });
		await fireEvent.click(askButton());
		await fireEvent.submit(box().closest('form')!);
		expect(apiMock).toHaveBeenCalledTimes(1);
		const signal = apiMock.mock.calls[0][3].signal as AbortSignal;
		await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
		expect(signal.aborted).toBe(true);
		expect(askButton().disabled).toBe(false);
		expect(box().value).toBe('roof?');
		// A late response after cancelling is ignored.
		slow.resolve(answer());
		await new Promise((r) => setTimeout(r, 0));
		expect(screen.queryByText(/replace the roof membrane/)).toBeNull();
	});

	it('replaces the previous answer', async () => {
		let n = 0;
		mockApi({ [`POST ${ask}`]: () => answer({ answer: `Answer ${++n}`, citations: [], grounded: false }) });
		mount();
		await fireEvent.input(box(), { target: { value: 'one' } });
		await fireEvent.click(askButton());
		expect(await screen.findByText('Answer 1')).toBeTruthy();
		await fireEvent.input(box(), { target: { value: 'two' } });
		await fireEvent.click(askButton());
		expect(await screen.findByText('Answer 2')).toBeTruthy();
		expect(screen.queryByText('Answer 1')).toBeNull();
	});

	it('shows the server message on failure and keeps the question', async () => {
		mockApi({ [`POST ${ask}`]: apiError(429, 'a question is already running') });
		mount();
		await fireEvent.input(box(), { target: { value: 'roof?' } });
		await fireEvent.click(askButton());
		expect((await screen.findByRole('alert')).textContent).toContain('a question is already running');
		expect(box().value).toBe('roof?');
	});

	it('renders answer markdown safely', async () => {
		mockApi({ [`POST ${ask}`]: answer({ answer: '**Carried** <img src=x onerror="alert(1)">', citations: [], grounded: false }) });
		mount();
		await fireEvent.input(box(), { target: { value: 'roof?' } });
		await fireEvent.click(askButton());
		const strong = await screen.findByText('Carried');
		expect(strong.tagName).toBe('STRONG');
		expect(document.body.innerHTML).not.toContain('onerror');
	});

	it('explains when the assistant is not configured', () => {
		session.setStrataAssistant(false);
		mockApi({});
		mount();
		expect(screen.getByText('The assistant is not configured.')).toBeTruthy();
		expect(screen.queryByLabelText('Question')).toBeNull();
	});
});
