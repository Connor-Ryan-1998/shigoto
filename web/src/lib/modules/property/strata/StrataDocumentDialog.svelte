<script lang="ts">
	// StrataDocumentDialog uploads one StrataMax PDF to a strata scheme with
	// its type, title and date. The metadata travels in the query string;
	// the server streams the file straight to storage.
	import { untrack } from 'svelte';
	import { api, ApiError } from '../../../api';
	import Dialog from '../../../components/Dialog.svelte';
	import Field from '../../../components/Field.svelte';
	import Button from '../../../components/Button.svelte';
	import { STRATA_DOC_TYPES, type StrataDocType, type StrataDocument } from '../types';

	let {
		open = $bindable(false),
		org,
		schemeId,
		onsaved
	}: {
		open?: boolean;
		org: string;
		schemeId: number;
		onsaved: (doc: StrataDocument) => void;
	} = $props();

	const MAX_BYTES = 25 * 1024 * 1024;

	let file = $state<File | null>(null);
	let docType = $state<StrataDocType>('minutes');
	let title = $state('');
	let documentDate = $state('');
	let error = $state('');
	let busy = $state(false);
	let fileInput = $state<HTMLInputElement | null>(null);
	let seq = 0;
	$effect(() => { org; schemeId; open; return () => { seq++; }; });

	$effect(() => {
		if (open) untrack(seed);
	});
	function seed() {
		error = ''; busy = false; file = null; docType = 'minutes'; title = ''; documentDate = '';
		if (fileInput) fileInput.value = '';
	}

	const dirty = $derived(file !== null || docType !== 'minutes' || title !== '' || documentDate !== '');

	function choose(event: Event) {
		const chosen = (event.currentTarget as HTMLInputElement).files?.[0] ?? null;
		file = chosen;
		// Suggest a title from the file name when none is typed yet.
		if (chosen && title.trim() === '') title = chosen.name.replace(/\.pdf$/i, '');
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy || !open) return;
		if (!file) { error = 'Choose a PDF to upload.'; return; }
		if (file.size > MAX_BYTES) { error = 'The PDF is larger than 25 MB.'; return; }
		const query = new URLSearchParams({ doc_type: docType, title: title.trim(), document_date: documentDate });
		const body = new FormData();
		body.append('file', file, file.name);
		const token = seq;
		busy = true; error = '';
		try {
			const saved = await api<StrataDocument>('POST', `/api/v1/orgs/${org}/strata-schemes/${schemeId}/documents?${query}`, body);
			if (token !== seq || !open) return;
			open = false;
			onsaved(saved);
		} catch (err) {
			if (token === seq) error = err instanceof ApiError ? err.message : 'Could not upload the document.';
		} finally {
			if (token === seq) busy = false;
		}
	}
</script>

<Dialog bind:open title="Upload strata document" {busy} {dirty}>
	{#snippet children({ close })}
	{#if open}
		<form class="flex flex-col gap-3" onsubmit={submit}>
			{#if error}<p class="alert alert-error alert-soft" role="alert">{error}</p>{/if}
			<fieldset disabled={busy} class="grid gap-3">
				<Field label="PDF document" id="strata-doc-file" hint="A StrataMax PDF, up to 25 MB and 300 pages.">
					<input bind:this={fileInput} class="file-input w-full" id="strata-doc-file" type="file" accept="application/pdf" onchange={choose} required />
				</Field>
				<Field label="Type" id="strata-doc-type">
					<select class="select w-full" id="strata-doc-type" bind:value={docType}>
						{#each STRATA_DOC_TYPES as t (t.value)}<option value={t.value}>{t.label}</option>{/each}
					</select>
				</Field>
				<Field label="Title" id="strata-doc-title">
					<input class="input w-full" id="strata-doc-title" bind:value={title} maxlength="200" required />
				</Field>
				<Field label="Document date" id="strata-doc-date" hint="The meeting, notice or statement date.">
					<input class="input w-full" id="strata-doc-date" type="date" bind:value={documentDate} required />
				</Field>
			</fieldset>
			<div class="modal-action">
				<Button variant="secondary" disabled={busy} onclick={close}>Cancel</Button>
				<Button type="submit" {busy}>{busy ? 'Uploading…' : 'Upload document'}</Button>
			</div>
		</form>
	{/if}
	{/snippet}
</Dialog>
