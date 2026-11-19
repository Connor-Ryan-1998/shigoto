<script lang="ts">
	import { onDestroy } from 'svelte';
	import Button from '../../components/Button.svelte';

	// Records a voice note with the browser's MediaRecorder; the browser
	// picks the format (WebM/Opus on Chrome/Android, MP4/AAC on iOS Safari).
	// Hidden where recording isn't supported.
	const MAX_SECONDS = 300;

	let { onrecorded, disabled = false }: { onrecorded: (clip: Blob, name: string) => void; disabled?: boolean } = $props();

	const supported = typeof MediaRecorder !== 'undefined' && typeof navigator !== 'undefined' && !!navigator.mediaDevices?.getUserMedia;
	let phase = $state<'idle' | 'recording' | 'review'>('idle');
	let error = $state('');
	let seconds = $state(0);
	let clip: Blob | null = null;
	let previewURL = $state('');
	let recorder: MediaRecorder | null = null;
	let stream: MediaStream | null = null;
	let timer: ReturnType<typeof setInterval> | undefined;

	function stopTracks() {
		stream?.getTracks().forEach((t) => t.stop());
		stream = null;
		clearInterval(timer);
	}

	async function start() {
		error = '';
		try {
			stream = await navigator.mediaDevices.getUserMedia({ audio: true });
		} catch {
			error = 'Allow microphone access in your browser to record a voice note.';
			return;
		}
		const chunks: Blob[] = [];
		recorder = new MediaRecorder(stream);
		recorder.ondataavailable = (e) => { if (e.data.size) chunks.push(e.data); };
		recorder.onstop = () => {
			stopTracks();
			clip = new Blob(chunks, { type: recorder?.mimeType || chunks[0]?.type || 'audio/webm' });
			previewURL = URL.createObjectURL(clip);
			phase = 'review';
		};
		seconds = 0;
		timer = setInterval(() => { seconds++; if (seconds >= MAX_SECONDS) stop(); }, 1000);
		recorder.start();
		phase = 'recording';
	}

	function stop() {
		if (recorder && recorder.state !== 'inactive') recorder.stop();
	}

	function discard() {
		if (previewURL) URL.revokeObjectURL(previewURL);
		previewURL = '';
		clip = null;
		phase = 'idle';
	}

	function use() {
		if (!clip) return;
		const ext = clip.type.includes('mp4') || clip.type.includes('aac') ? 'm4a' : clip.type.includes('ogg') ? 'ogg' : 'webm';
		onrecorded(clip, `voice-note-${new Date().toISOString().replace(/[:.]/g, '-')}.${ext}`);
		discard();
	}

	onDestroy(() => { stop(); stopTracks(); if (previewURL) URL.revokeObjectURL(previewURL); });
</script>

{#if supported}
	<div class="flex flex-col gap-2">
		{#if phase === 'idle'}
			<Button variant="secondary" {disabled} onclick={start}>Record voice note</Button>
		{:else if phase === 'recording'}
			<p aria-live="polite" class="text-sm"><span class="status status-error mr-1" aria-hidden="true"></span>Recording… {Math.floor(seconds / 60)}:{String(seconds % 60).padStart(2, '0')}</p>
			<Button variant="danger" onclick={stop}>Stop recording</Button>
		{:else}
			<audio src={previewURL} controls class="w-full" aria-label="Voice note preview"></audio>
			<div class="grid grid-cols-2 gap-2">
				<Button variant="secondary" onclick={discard}>Discard</Button>
				<Button onclick={use}>Use recording</Button>
			</div>
		{/if}
		{#if error}<p class="text-sm text-error" role="alert">{error}</p>{/if}
	</div>
{/if}
