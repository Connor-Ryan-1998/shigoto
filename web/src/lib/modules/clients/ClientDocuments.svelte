<script lang="ts">
	// ClientDocuments lists a client's documents (optionally one project's)
	// with the staff actions each status allows: send/share and delete a
	// draft, withdraw or replace a sent one. Signed documents are immutable.
	import { untrack } from 'svelte';
	import { api, ApiError } from '../../api';
	import { date } from '../../format';
	import Button from '../../components/Button.svelte';
	import StatusBadge from '../../components/StatusBadge.svelte';
	import DocumentUploadDialog from './DocumentUploadDialog.svelte';
	import {
		CATEGORY_LABELS,
		DOCUMENT_STATUS_TONES,
		documentActions,
		statusLabel,
		type ClientDocument
	} from './types';

	let {
		org,
		contactId,
		projectId = null,
		projects,
		canWrite
	}: {
		org: string;
		contactId: number;
		projectId?: number | null;
		projects: { id: number; name: string }[];
		canWrite: boolean;
	} = $props();

	const NO_LOGIN = 'Invite this client to the portal first';
	const base = $derived(`/api/v1/orgs/${org}`);
	const key = $derived(`${org}/${contactId}/${projectId ?? ''}`);
	let docs = $state<ClientDocument[]>([]);
	let loaded = $state('');
	let error = $state('');
	let busy = $state(false);
	let uploadOpen = $state(false);
	let replacing = $state<ClientDocument | null>(null);
	let seq = 0;

	const projectName = (id: number | null) => projects.find((p) => p.id === id)?.name ?? 'General';

	async function load() {
		const s = ++seq;
		const k = key;
		try {
			const q = projectId ? `?project=${projectId}` : '';
			const result = await api<ClientDocument[]>('GET', `${base}/contacts/${contactId}/documents${q}`);
			if (s !== seq || k !== key) return;
			docs = result;
			loaded = k;
		} catch (e) {
			if (s === seq && k === key) error = e instanceof ApiError ? e.message : 'Could not load documents.';
		}
	}

	$effect(() => {
		key;
		untrack(() => { loaded = ''; error = ''; load(); });
		return () => { seq++; };
	});

	async function act(d: ClientDocument, action: 'send' | 'withdraw' | 'delete') {
		if (busy || !canWrite) return;
		if (action === 'delete' && !confirm(`Delete draft ${d.title}?`)) return;
		if (action === 'withdraw' && !confirm(`Withdraw ${d.title}? The client will still see it, marked withdrawn.`)) return;
		busy = true;
		error = '';
		try {
			if (action === 'delete') await api('DELETE', `${base}/documents/${d.id}`);
			else await api('POST', `${base}/documents/${d.id}/${action}`);
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Something went wrong.';
		} finally {
			busy = false;
		}
	}

	function replace(d: ClientDocument) {
		replacing = d;
		uploadOpen = true;
	}
</script>

<section aria-labelledby="client-documents-heading">
	<div class="mb-2 flex items-center justify-between">
		<h2 id="client-documents-heading" class="text-lg font-semibold">Documents</h2>
		{#if canWrite}
			<Button size="sm" disabled={busy || loaded !== key} onclick={() => { replacing = null; uploadOpen = true; }}>Upload document</Button>
		{/if}
	</div>
	{#if error}
		<p class="alert alert-error alert-soft mb-2" role="alert">
			{error}
			{#if error === NO_LOGIN}<a class="link" href={`/${org}/settings/members`}>Invite to portal</a>{/if}
		</p>
	{/if}
	{#if loaded !== key}
		{#if !error}<p role="status">Loading documents…</p>{/if}
	{:else}
		<div class="overflow-x-auto rounded-box border border-base-300 bg-base-100">
			<table class="table">
				<thead><tr><th>Document</th>{#if !projectId}<th>Project</th>{/if}<th>Category</th><th>Status</th><th><span class="sr-only">Actions</span></th></tr></thead>
				<tbody>
					{#each docs as d (d.id)}
						<tr class={d.status === 'withdrawn' || d.status === 'superseded' ? 'opacity-60' : ''}>
							<td>
								<div class="font-medium">{d.title}</div>
								{#if d.signature}
									<div class="text-sm text-base-content/70">Signed by {d.signature.typed_name} on {date(d.signature.signed_at.slice(0, 10))}</div>
								{/if}
							</td>
							{#if !projectId}<td>{projectName(d.project_id)}</td>{/if}
							<td>{CATEGORY_LABELS[d.category]}</td>
							<td><StatusBadge status={statusLabel(d)} tone={DOCUMENT_STATUS_TONES[d.status]} /></td>
							<td class="whitespace-nowrap text-right">
								<a class="btn btn-ghost btn-xs" href={`${base}/documents/${d.id}/file`} download aria-label={`Download ${d.title}`}>PDF</a>
								{#if d.signature}
									<a class="btn btn-ghost btn-xs" href={`${base}/documents/${d.id}/certificate`} download aria-label={`Certificate for ${d.title}`}>Certificate</a>
								{/if}
								{#if canWrite}
									{#each documentActions(d) as action (action)}
										{#if action === 'send'}
											<button type="button" class="btn btn-ghost btn-xs text-primary" disabled={busy} aria-label={`Send ${d.title}`} onclick={() => act(d, 'send')}>{d.kind === 'shared' ? 'Share' : 'Send'}</button>
										{:else if action === 'delete'}
											<button type="button" class="btn btn-ghost btn-xs" disabled={busy} aria-label={`Delete ${d.title}`} onclick={() => act(d, 'delete')}>Delete</button>
										{:else if action === 'withdraw'}
											<button type="button" class="btn btn-ghost btn-xs" disabled={busy} aria-label={`Withdraw ${d.title}`} onclick={() => act(d, 'withdraw')}>Withdraw</button>
										{:else}
											<button type="button" class="btn btn-ghost btn-xs" disabled={busy} aria-label={`Replace ${d.title}`} onclick={() => replace(d)}>Replace</button>
										{/if}
									{/each}
								{/if}
							</td>
						</tr>
					{:else}
						<tr><td colspan={projectId ? 4 : 5} class="text-base-content/70">No documents yet.</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
	{#if canWrite}
		<DocumentUploadDialog bind:open={uploadOpen} {org} {contactId} {projects} {projectId}
			replace={replacing ? { id: replacing.id, title: replacing.title } : null} onsaved={load} />
	{/if}
</section>
