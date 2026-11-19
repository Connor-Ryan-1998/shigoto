<script lang="ts">
	import { mediaKind, type TicketFile } from './maintenance';

	// base is the ticket's files endpoint; each file is fetched inline
	// (HTTP Range capable, so phone video plays and seeks).
	let { base, files }: { base: string; files: TicketFile[] } = $props();
</script>

{#if files.length === 0}
	<p class="text-base-content/70">No photos or recordings.</p>
{:else}
	<ul class="grid grid-cols-2 gap-3 sm:grid-cols-3">
		{#each files as f (f.id)}
			{@const src = `${base}/${f.id}?inline=1`}
			<li class="flex flex-col gap-1">
				{#if mediaKind(f.mime, f.original_name) === 'image'}
					<a href={src} target="_blank" rel="noopener"><img {src} alt={f.original_name} class="aspect-square w-full rounded-box object-cover" loading="lazy" /></a>
				{:else if mediaKind(f.mime, f.original_name) === 'video'}
					<!-- svelte-ignore a11y_media_has_caption -->
					<video {src} controls preload="metadata" playsinline class="aspect-square w-full rounded-box bg-base-300" aria-label={f.original_name}></video>
				{:else if mediaKind(f.mime, f.original_name) === 'audio'}
					<audio {src} controls preload="metadata" class="w-full" aria-label={f.original_name}></audio>
				{/if}
				<a class="link truncate text-xs" href={`${base}/${f.id}`} download>Download<span class="sr-only"> {f.original_name}</span></a>
			</li>
		{/each}
	</ul>
{/if}
