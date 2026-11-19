<script lang="ts">
	import { api, uploadWithProgress, ApiError } from '../../api';
	import VoiceRecorder from './VoiceRecorder.svelte';

	// Phone-first media capture: camera, library and voice note. Each file
	// uploads as soon as it's added, with progress, Retry and Remove.
	// onchange reports whether anything is still uploading (callers block
	// submit while busy) and the server ids of finished uploads.
	type Item = { key: number; blob: Blob; name: string; status: 'uploading' | 'done' | 'failed'; progress: number; error: string; pendingId?: number; ctl?: AbortController };

	let {
		uploadUrl,
		deleteUrl,
		onchange,
		onuploaded,
		disabled = false,
		max = 20
	}: {
		uploadUrl: string;
		deleteUrl?: (pendingId: number) => string;
		onchange?: (state: { busy: boolean; pendingIds: number[] }) => void;
		onuploaded?: () => void;
		disabled?: boolean;
		max?: number;
	} = $props();

	let items = $state<Item[]>([]);
	let nextKey = 0;
	let limitMessage = $state('');

	function report() {
		onchange?.({
			busy: items.some((i) => i.status === 'uploading'),
			pendingIds: items.filter((i) => i.status === 'done' && i.pendingId !== undefined).map((i) => i.pendingId!)
		});
	}

	async function upload(item: Item) {
		item.status = 'uploading'; item.progress = 0; item.error = '';
		item.ctl = new AbortController();
		report();
		try {
			const out = await uploadWithProgress<{ pending_id?: number }>(uploadUrl, item.blob, item.name, (f) => { item.progress = f; }, item.ctl.signal);
			item.pendingId = out?.pending_id;
			item.status = 'done';
			onuploaded?.();
		} catch (e) {
			if (e instanceof DOMException && e.name === 'AbortError') return;
			item.status = 'failed';
			item.error = e instanceof ApiError ? e.message : 'Upload failed.';
		}
		report();
	}

	function add(blobs: { blob: Blob; name: string }[]) {
		limitMessage = '';
		const room = max - items.length;
		if (blobs.length > room) limitMessage = `You can add up to ${max} photos or recordings.`;
		for (const b of blobs.slice(0, Math.max(room, 0))) {
			items.push({ key: nextKey++, blob: b.blob, name: b.name, status: 'uploading', progress: 0, error: '' });
			upload(items[items.length - 1]);
		}
	}

	function picked(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		add([...(input.files ?? [])].map((f) => ({ blob: f, name: f.name })));
		input.value = '';
	}

	async function remove(item: Item) {
		item.ctl?.abort();
		items = items.filter((i) => i.key !== item.key);
		report();
		if (item.pendingId !== undefined && deleteUrl) {
			// Best effort: an orphaned pending upload is pruned after 24 hours.
			api('DELETE', deleteUrl(item.pendingId)).catch(() => {});
		}
	}
</script>

<div class="flex flex-col gap-2">
	<div class="grid grid-cols-2 gap-2">
		<label class={['btn', disabled && 'btn-disabled']}>
			Take photo or video
			<input class="sr-only" type="file" accept="image/*,video/*" capture="environment" {disabled} onchange={picked} aria-label="Take photo or video" />
		</label>
		<label class={['btn', disabled && 'btn-disabled']}>
			Choose from library
			<input class="sr-only" type="file" accept="image/*,video/*,audio/*" multiple {disabled} onchange={picked} aria-label="Choose from library" />
		</label>
	</div>
	<VoiceRecorder {disabled} onrecorded={(blob, name) => add([{ blob, name }])} />
	{#if limitMessage}<p class="text-sm text-warning" role="status">{limitMessage}</p>{/if}
	{#if items.length}
		<ul class="flex flex-col gap-2">
			{#each items as item (item.key)}
				<li class="flex flex-wrap items-center gap-2 rounded-box bg-base-200 p-2 text-sm">
					<span class="min-w-0 flex-1 truncate">{item.name}</span>
					{#if item.status === 'uploading'}
						<progress class="progress progress-primary w-24" value={Math.round(item.progress * 100)} max="100" aria-label={`Uploading ${item.name}`}></progress>
					{:else if item.status === 'failed'}
						<span class="text-error">{item.error}</span>
						<button type="button" class="btn btn-xs" onclick={() => upload(item)} aria-label={`Retry ${item.name}`}>Retry</button>
					{:else}
						<span class="text-success">Uploaded</span>
					{/if}
					{#if deleteUrl || item.status !== 'done'}
						<button type="button" class="btn btn-ghost btn-xs" onclick={() => remove(item)} aria-label={`Remove ${item.name}`}>Remove</button>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</div>
