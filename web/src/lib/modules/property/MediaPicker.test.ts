import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { apiError, mockApi, pending } from '../../test/mock-api';
import MediaPicker from './MediaPicker.svelte';

const file = (name: string) => new File(['x'], name, { type: 'image/jpeg' });

describe('MediaPicker', () => {
	it('offers camera and library inputs', () => {
		render(MediaPicker, { uploadUrl: '/u', onchange: vi.fn() });
		const camera = screen.getByLabelText('Take photo or video') as HTMLInputElement;
		expect(camera.getAttribute('accept')).toBe('image/*,video/*');
		expect(camera.getAttribute('capture')).toBe('environment');
		const library = screen.getByLabelText('Choose from library') as HTMLInputElement;
		expect(library.multiple).toBe(true);
		expect(library.getAttribute('accept')).toBe('image/*,video/*,audio/*');
	});

	it('uploads each file, reports busy while uploading and removes a finished one', async () => {
		const onchange = vi.fn();
		let resolve!: (v: unknown) => void;
		mockApi({
			'POST /u': () => new Promise((r) => (resolve = r)),
			'DELETE /u/11': undefined
		});
		render(MediaPicker, { uploadUrl: '/u', deleteUrl: (id: number) => `/u/${id}`, onchange });
		await fireEvent.change(screen.getByLabelText('Choose from library'), { target: { files: [file('leak.jpg')] } });
		expect(await screen.findByText('leak.jpg')).toBeTruthy();
		expect(onchange).toHaveBeenLastCalledWith({ busy: true, pendingIds: [] });
		resolve({ pending_id: 11, file: { id: 3 } });
		await vi.waitFor(() => expect(onchange).toHaveBeenLastCalledWith({ busy: false, pendingIds: [11] }));
		await fireEvent.click(screen.getByRole('button', { name: 'Remove leak.jpg' }));
		await vi.waitFor(() => expect(onchange).toHaveBeenLastCalledWith({ busy: false, pendingIds: [] }));
	});

	it('shows a failed upload with retry and remove, without blocking', async () => {
		const onchange = vi.fn();
		let fail = true;
		mockApi({ 'POST /u': () => (fail ? apiError(413, 'file too large') : { pending_id: 12, file: { id: 4 } }) });
		render(MediaPicker, { uploadUrl: '/u', onchange });
		await fireEvent.change(screen.getByLabelText('Choose from library'), { target: { files: [file('huge.mov')] } });
		expect(await screen.findByText('file too large')).toBeTruthy();
		expect(onchange).toHaveBeenLastCalledWith({ busy: false, pendingIds: [] });
		fail = false;
		await fireEvent.click(screen.getByRole('button', { name: 'Retry huge.mov' }));
		await vi.waitFor(() => expect(onchange).toHaveBeenLastCalledWith({ busy: false, pendingIds: [12] }));
	});

	it('stays busy while an upload is in flight', async () => {
		const onchange = vi.fn();
		mockApi({ 'POST /u': pending() });
		render(MediaPicker, { uploadUrl: '/u', onchange });
		await fireEvent.change(screen.getByLabelText('Choose from library'), { target: { files: [file('a.jpg')] } });
		expect(onchange).toHaveBeenLastCalledWith({ busy: true, pendingIds: [] });
	});
});
