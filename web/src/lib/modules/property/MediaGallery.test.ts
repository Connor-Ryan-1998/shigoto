import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import MediaGallery from './MediaGallery.svelte';

describe('MediaGallery', () => {
	it('renders images, video and audio inline with ranged sources', () => {
		const { container } = render(MediaGallery, {
			base: '/api/v1/orgs/acme/maintenance/7/files',
			files: [
				{ id: 1, mime: 'image/jpeg', size_bytes: 10, original_name: 'leak.jpg' },
				{ id: 2, mime: 'video/mp4', size_bytes: 10, original_name: 'leak.mp4' },
				{ id: 3, mime: 'audio/webm', size_bytes: 10, original_name: 'voice.webm' }
			]
		});
		expect(screen.getByRole('img', { name: 'leak.jpg' }).getAttribute('src')).toBe('/api/v1/orgs/acme/maintenance/7/files/1?inline=1');
		const video = container.querySelector('video')!;
		expect(video.getAttribute('src')).toBe('/api/v1/orgs/acme/maintenance/7/files/2?inline=1');
		expect(video.hasAttribute('controls')).toBe(true);
		expect(video.getAttribute('preload')).toBe('metadata');
		expect(container.querySelector('audio')!.getAttribute('src')).toBe('/api/v1/orgs/acme/maintenance/7/files/3?inline=1');
		expect(screen.getAllByRole('link', { name: /Download/ })).toHaveLength(3);
	});

	it('plays a recorded WebM voice note as audio', () => {
		const { container } = render(MediaGallery, {
			base: '/f',
			files: [{ id: 4, mime: 'video/webm', size_bytes: 10, original_name: 'voice-note-2026.webm' }, { id: 5, mime: 'video/webm', size_bytes: 10, original_name: 'clip.webm' }]
		});
		expect(container.querySelectorAll('audio')).toHaveLength(1);
		expect(container.querySelectorAll('video')).toHaveLength(1);
	});

	it('says when there is no media', () => {
		render(MediaGallery, { base: '/x', files: [] });
		expect(screen.getByText('No photos or recordings.')).toBeTruthy();
	});
});
