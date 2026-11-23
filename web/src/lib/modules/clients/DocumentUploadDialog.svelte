<script lang="ts">
	// DocumentUploadDialog uploads a PDF for a client as a draft document, or
	// - with `replace` - uploads a new version of a sent, unsigned document.
	// The studio signs a document before uploading it; the client signs in
	// the portal once it's sent.
	import { untrack } from 'svelte';
	import { api, ApiError } from '../../api';
	import Dialog from '../../components/Dialog.svelte';
	import Field from '../../components/Field.svelte';
	import Button from '../../components/Button.svelte';
	import { CATEGORY_LABELS, KIND_LABELS, type DocumentCategory, type DocumentKind } from './types';

	let {
		open = $bindable(false),
		org,
		contactId,
		projects,
		projectId = null,
		replace = null,
		onsaved
	}: {
		open?: boolean;
		org: string;
		contactId: number;
		projects: { id: number; name: string }[];
		projectId?: number | null;
		replace?: { id: number; title: string } | null;
		onsaved: () => void;
	} = $props();

	let title = $state('');
	let kind = $state<DocumentKind>('signature');
	let category = $state<DocumentCategory>('general');
	let project = $state('');
	let file = $state<File | null>(null);
	let error = $state('');
	let busy = $state(false);
	let actionSeq = 0;
	$effect(() => { org; contactId; open; return () => { actionSeq++; }; });
	$effect(() => {
		if (open) untrack(() => {
			title = '';
			kind = 'signature';
			category = 'general';
			project = projectId ? String(projectId) : '';
			file = null;
			error = '';
			busy = false;
		});
	});

	function pick(e: Event) {
		file = (e.currentTarget as HTMLInputElement).files?.[0] ?? null;
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy || !open) return;
		if (!file) {
			error = 'Choose a PDF file.';
			return;
		}
		const seq = actionSeq;
		let url: string;
		if (replace) {
			url = `/api/v1/orgs/${org}/documents/${replace.id}/replace`;
		} else {
			const q = new URLSearchParams({ title, kind, category });
			if (project) q.set('project_id', project);
			url = `/api/v1/orgs/${org}/contacts/${contactId}/documents?${q}`;
		}
		const form = new FormData();
		form.append('file', file, file.name);
		busy = true;
		error = '';
		try {
			await api('POST', url, form);
			if (seq !== actionSeq || !open) return;
			open = false;
			onsaved();
		} catch (err) {
			if (seq === actionSeq && open) error = err instanceof ApiError ? err.message : 'Something went wrong.';
		} finally {
			if (seq === actionSeq) busy = false;
		}
	}
</script>

<Dialog bind:open title={replace ? `Replace ${replace.title}` : 'Upload document'} {busy}>
	{#snippet children({ close })}
	{#if open}
		<form class="flex flex-col gap-3" onsubmit={submit}>
			{#if replace}
				<p class="text-sm text-base-content/70">The new version starts as a draft. The current version stays live until you send it; the client then sees the old one marked "Replaced".</p>
			{:else}
				<Field label="Title" id="doc-title">
					<input class="input w-full" id="doc-title" maxlength="200" bind:value={title} disabled={busy} required />
				</Field>
				<div class="grid grid-cols-2 gap-3">
					<Field label="Type" id="doc-kind">
						<select class="select w-full" id="doc-kind" bind:value={kind} disabled={busy}>
							{#each Object.entries(KIND_LABELS) as [value, label] (value)}<option {value}>{label}</option>{/each}
						</select>
					</Field>
					<Field label="Category" id="doc-category">
						<select class="select w-full" id="doc-category" bind:value={category} disabled={busy}>
							{#each Object.entries(CATEGORY_LABELS) as [value, label] (value)}<option {value}>{label}</option>{/each}
						</select>
					</Field>
				</div>
				<Field label="Project" id="doc-project" hint="Leave as General for agreements covering every project, like an MSA.">
					<select class="select w-full" id="doc-project" bind:value={project} disabled={busy || projectId !== null}>
						<option value="">General (no project)</option>
						{#each projects as p (p.id)}<option value={String(p.id)}>{p.name}</option>{/each}
					</select>
				</Field>
				{#if kind === 'signature'}
					<p class="text-sm text-base-content/70">Sign the PDF on your side before uploading. The client signs it in their portal.</p>
				{/if}
			{/if}
			<Field label="PDF file" id="doc-file">
				<input class="file-input w-full" id="doc-file" type="file" accept="application/pdf,.pdf" onchange={pick} disabled={busy} />
			</Field>
			{#if error}<p class="alert alert-error alert-soft" role="alert">{error}</p>{/if}
			<div class="modal-action">
				<Button variant="secondary" disabled={busy} onclick={close}>Cancel</Button>
				<Button type="submit" disabled={busy}>{replace ? 'Replace' : 'Upload'}</Button>
			</div>
		</form>
	{/if}
	{/snippet}
</Dialog>
