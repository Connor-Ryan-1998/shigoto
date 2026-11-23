import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { apiError, apiMock, mockApi, pending } from '../../test/mock-api';
import DocumentUploadDialog from './DocumentUploadDialog.svelte';

const pdf = () => new File(['%PDF-1.4'], 'sow.pdf', { type: 'application/pdf' });
const url = '/api/v1/orgs/acme/contacts/9/documents?title=SOW&kind=signature&category=general&project_id=5';

async function fill() {
	await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'SOW' } });
	await fireEvent.change(screen.getByLabelText('Project'), { target: { value: '5' } });
	await fireEvent.change(screen.getByLabelText('PDF file'), { target: { files: [pdf()] } });
}

describe('DocumentUploadDialog', () => {
	it('uploads the PDF with its metadata in the query string', async () => {
		mockApi({ [`POST ${url}`]: { id: 3 } });
		const onsaved = vi.fn();
		render(DocumentUploadDialog, { open: true, org: 'acme', contactId: 9, projects: [{ id: 5, name: 'Site' }], onsaved });
		await fill();
		await fireEvent.click(screen.getByRole('button', { name: 'Upload' }));
		await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
		const body = apiMock.mock.calls[0][2] as FormData;
		expect((body.get('file') as File).name).toBe('sow.pdf');
	});

	it('blocks a second submit while uploading and keeps input on a 422', async () => {
		mockApi({ [`POST ${url}`]: pending() });
		render(DocumentUploadDialog, { open: true, org: 'acme', contactId: 9, projects: [{ id: 5, name: 'Site' }], onsaved: vi.fn() });
		await fill();
		await fireEvent.click(screen.getByRole('button', { name: 'Upload' }));
		await fireEvent.click(screen.getByRole('button', { name: 'Upload' }));
		expect(apiMock).toHaveBeenCalledTimes(1);
		expect((screen.getByLabelText('Title') as HTMLInputElement).disabled).toBe(true);
	});

	it('keeps typed values after a rejected upload', async () => {
		mockApi({ [`POST ${url}`]: apiError(422, 'project belongs to another client') });
		render(DocumentUploadDialog, { open: true, org: 'acme', contactId: 9, projects: [{ id: 5, name: 'Site' }], onsaved: vi.fn() });
		await fill();
		await fireEvent.click(screen.getByRole('button', { name: 'Upload' }));
		await screen.findByText('project belongs to another client');
		expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('SOW');
		expect((screen.getByLabelText('Title') as HTMLInputElement).disabled).toBe(false);
	});

	it('requires a file before uploading', async () => {
		render(DocumentUploadDialog, { open: true, org: 'acme', contactId: 9, projects: [], onsaved: vi.fn() });
		await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'SOW' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Upload' }));
		await screen.findByText('Choose a PDF file.');
		expect(apiMock).not.toHaveBeenCalled();
	});

	it('replaces a sent document with only a new file', async () => {
		mockApi({ ['POST /api/v1/orgs/acme/documents/7/replace']: { id: 8 } });
		const onsaved = vi.fn();
		render(DocumentUploadDialog, { open: true, org: 'acme', contactId: 9, projects: [], replace: { id: 7, title: 'NDA' }, onsaved });
		expect(screen.queryByLabelText('Title')).toBeNull();
		await fireEvent.change(screen.getByLabelText('PDF file'), { target: { files: [pdf()] } });
		await fireEvent.click(screen.getByRole('button', { name: 'Replace' }));
		await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
	});
});
