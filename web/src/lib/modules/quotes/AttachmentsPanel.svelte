<script lang="ts">
	// AttachmentsPanel lists a quote's internal attachments (scope of work,
	// site photos, the customer's evidence). They are staff records: never
	// on the PDF and never in the portal.
	import { api, ApiError } from '../../api';
	import Field from '../../components/Field.svelte';
	import DateText from '../../components/DateText.svelte';
	import type { QuoteAttachment } from './types';

	let {
		org,
		quoteId,
		attachments,
		canWrite,
		onchange
	}: { org: string; quoteId: number; attachments: QuoteAttachment[]; canWrite: boolean; onchange: () => void } = $props();

	let busy = $state(false);
	let error = $state('');
	const base = $derived(`/api/v1/orgs/${org}/quotes/${quoteId}/attachments`);

	async function upload(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		if (!file || busy || !canWrite) return;
		busy = true;
		error = '';
		try {
			const form = new FormData();
			form.append('file', file);
			await api('POST', base, form);
			onchange();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not upload the file.';
		} finally {
			busy = false;
			input.value = '';
		}
	}

	async function remove(a: QuoteAttachment) {
		if (busy || !canWrite || !confirm(`Remove ${a.file.original_name}?`)) return;
		busy = true;
		error = '';
		try {
			await api('DELETE', `${base}/${a.id}`);
			onchange();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not remove the file.';
		} finally {
			busy = false;
		}
	}
</script>

<section aria-labelledby="quote-attachments-heading" class="rounded-box border border-base-300 bg-base-100 p-4">
	<h2 id="quote-attachments-heading" class="mt-0 mb-1 text-lg font-semibold">Attachments</h2>
	<p class="mb-3 text-sm text-base-content/70">Internal records only. Never shown to the customer.</p>
	{#if error}<p role="alert" class="alert alert-error alert-soft mb-3">{error}</p>{/if}
	{#if attachments.length === 0}
		<p class="text-base-content/70">No attachments.</p>
	{:else}
		<ul class="mb-3 grid gap-2">
			{#each attachments as a (a.id)}
				<li class="flex flex-wrap items-center gap-3">
					<a class="link" href={`${base}/${a.id}`} target="_blank" rel="noopener">{a.file.original_name}</a>
					<span class="text-sm text-base-content/70"><DateText iso={a.created_at.slice(0, 10)} /></span>
					{#if canWrite}
						<button type="button" class="btn btn-sm btn-outline" disabled={busy} aria-label={`Remove ${a.file.original_name}`} onclick={() => remove(a)}>Remove</button>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
	{#if canWrite}
		<Field label="Attach a file" id="quote-attachment" hint="PDF, JPEG or PNG, up to 10 MB.">
			<input class="file-input w-full max-w-md" id="quote-attachment" type="file" accept="application/pdf,image/jpeg,image/png" disabled={busy} onchange={upload} />
		</Field>
	{/if}
</section>
