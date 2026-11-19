import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import VoiceRecorder from './VoiceRecorder.svelte';

class FakeRecorder {
	static instances: FakeRecorder[] = [];
	mimeType = 'audio/mp4';
	state = 'inactive';
	ondataavailable: ((e: { data: Blob }) => void) | null = null;
	onstop: (() => void) | null = null;
	constructor(public stream: unknown) { FakeRecorder.instances.push(this); }
	start() { this.state = 'recording'; }
	stop() {
		this.state = 'inactive';
		this.ondataavailable?.({ data: new Blob(['voice'], { type: 'audio/mp4' }) });
		this.onstop?.();
	}
}

afterEach(() => vi.unstubAllGlobals());

describe('VoiceRecorder', () => {
	it('is hidden where the browser cannot record', () => {
		vi.stubGlobal('MediaRecorder', undefined);
		render(VoiceRecorder, { onrecorded: vi.fn() });
		expect(screen.queryByRole('button', { name: /Record voice note/ })).toBeNull();
	});

	it('records, previews and hands over a named clip', async () => {
		vi.stubGlobal('MediaRecorder', FakeRecorder);
		const stop = vi.fn();
		vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: vi.fn().mockResolvedValue({ getTracks: () => [{ stop }] }) } });
		vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: () => 'blob:x', revokeObjectURL: () => {} }));
		const onrecorded = vi.fn();
		render(VoiceRecorder, { onrecorded });
		await fireEvent.click(screen.getByRole('button', { name: 'Record voice note' }));
		await fireEvent.click(await screen.findByRole('button', { name: 'Stop recording' }));
		expect(await screen.findByRole('button', { name: 'Use recording' })).toBeTruthy();
		expect(stop).toHaveBeenCalled();
		await fireEvent.click(screen.getByRole('button', { name: 'Use recording' }));
		expect(onrecorded).toHaveBeenCalledWith(expect.any(Blob), expect.stringMatching(/^voice-note-.*\.m4a$/));
	});

	it('explains when the microphone is blocked', async () => {
		vi.stubGlobal('MediaRecorder', FakeRecorder);
		vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: vi.fn().mockRejectedValue(new Error('denied')) } });
		render(VoiceRecorder, { onrecorded: vi.fn() });
		await fireEvent.click(screen.getByRole('button', { name: 'Record voice note' }));
		expect(await screen.findByText(/microphone/i)).toBeTruthy();
	});
});
