import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { setPage } from '../../../../../lib/test/app.svelte';
import { apiError, apiMock, mockApi } from '../../../../../lib/test/mock-api';
import Page from './+page.svelte';

const file = { id: 1, mime: 'application/pdf', size_bytes: 1, original_name: 'sow.pdf' };
const consent = 'I agree this is my electronic signature and I am authorised to sign for Client One.';
const sent = { id: 2, project_id: 5, project_name: 'Website rebuild', title: 'SOW', kind: 'signature', category: 'general', status: 'sent', sent_at: null, applies_to_all: false, file, signature: null, consent_text: consent };
const signed = { ...sent, status: 'signed', consent_text: undefined, signature: { typed_name: 'Priya Nair', email: 'p@x', signed_at: '2026-11-23T04:30:00Z' } };
const url = '/api/v1/portal/acme/documents/2';

beforeEach(() => setPage({ params: { org: 'acme', id: '2' }, url: new URL('http://localhost/portal/acme/documents/2') }));

async function ready() {
	await screen.findByRole('heading', { name: 'SOW' });
	await fireEvent.click(screen.getByRole('button', { name: 'Open SOW (PDF)' }));
}

describe('sign document page', () => {
	it('enables Sign only after opening the PDF, typing a name and ticking consent', async () => {
		mockApi({ [`GET ${url}`]: sent, [`POST ${url}/sign`]: signed });
		render(Page);
		await screen.findByRole('heading', { name: 'SOW' });
		const sign = screen.getByRole('button', { name: 'Sign document' }) as HTMLButtonElement;
		expect(sign.disabled).toBe(true);
		expect(screen.getByLabelText(consent)).toBeTruthy();
		await fireEvent.click(screen.getByRole('button', { name: 'Open SOW (PDF)' }));
		await fireEvent.input(screen.getByLabelText('Full legal name'), { target: { value: 'Priya Nair' } });
		expect(sign.disabled).toBe(true);
		await fireEvent.click(screen.getByLabelText(consent));
		expect(sign.disabled).toBe(false);
		expect(window.open).toHaveBeenCalledWith(`${url}/file`, '_blank', 'noopener');
		await fireEvent.click(sign);
		expect(apiMock).toHaveBeenCalledWith('POST', `${url}/sign`, { typed_name: 'Priya Nair', consent: true });
		expect(await screen.findByText(/Signed by Priya Nair/)).toBeTruthy();
		expect(screen.getByRole('link', { name: 'Download signing certificate' }).getAttribute('href')).toBe(`${url}/certificate`);
	});

	it('keeps the typed name when the server rejects the signature', async () => {
		mockApi({ [`GET ${url}`]: sent, [`POST ${url}/sign`]: apiError(422, 'type your full name') });
		render(Page);
		await ready();
		await fireEvent.input(screen.getByLabelText('Full legal name'), { target: { value: 'P' } });
		await fireEvent.click(screen.getByLabelText(consent));
		await fireEvent.click(screen.getByRole('button', { name: 'Sign document' }));
		await screen.findByText('type your full name');
		expect((screen.getByLabelText('Full legal name') as HTMLInputElement).value).toBe('P');
	});

	it('shows the signed state when it was already signed elsewhere', async () => {
		let calls = 0;
		mockApi({ [`GET ${url}`]: () => (calls++ === 0 ? sent : signed), [`POST ${url}/sign`]: apiError(409, 'already signed') });
		render(Page);
		await ready();
		await fireEvent.input(screen.getByLabelText('Full legal name'), { target: { value: 'Priya Nair' } });
		await fireEvent.click(screen.getByLabelText(consent));
		await fireEvent.click(screen.getByRole('button', { name: 'Sign document' }));
		expect(await screen.findByText(/Signed by Priya Nair/)).toBeTruthy();
	});

	it('keeps the form and explains a 409 that leaves the document still to sign', async () => {
		mockApi({ [`GET ${url}`]: sent, [`POST ${url}/sign`]: apiError(409, 'document file has changed') });
		render(Page);
		await ready();
		await fireEvent.input(screen.getByLabelText('Full legal name'), { target: { value: 'Priya Nair' } });
		await fireEvent.click(screen.getByLabelText(consent));
		await fireEvent.click(screen.getByRole('button', { name: 'Sign document' }));
		expect((await screen.findByRole('alert')).textContent).toContain('document file has changed');
		expect((screen.getByLabelText('Full legal name') as HTMLInputElement).value).toBe('Priya Nair');
	});

	it('shows a withdrawn document without a sign form', async () => {
		mockApi({ [`GET ${url}`]: { ...sent, status: 'withdrawn', consent_text: undefined } });
		render(Page);
		await screen.findByRole('heading', { name: 'SOW' });
		expect(screen.getByText(/withdrawn/i)).toBeTruthy();
		expect(screen.queryByRole('button', { name: 'Sign document' })).toBeNull();
	});
});
