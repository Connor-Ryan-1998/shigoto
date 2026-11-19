import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { apiMock, mockApi } from '../../test/mock-api';
import RequestChangeDialog from './RequestChangeDialog.svelte';

describe('RequestChangeDialog', () => {
	it('sends a note without windows', async () => {
		mockApi({ 'POST /b/visits/3/request-change': {} });
		const onsaved = vi.fn();
		render(RequestChangeDialog, { open: true, apiBase: '/b', visitId: 3, today: '2026-11-19', onsaved });
		await fireEvent.input(screen.getByLabelText('What would suit you better?'), { target: { value: 'Any afternoon' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Send request' }));
		await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
		expect(apiMock).toHaveBeenCalledWith('POST', '/b/visits/3/request-change', { note: 'Any afternoon', windows: undefined });
	});
});
