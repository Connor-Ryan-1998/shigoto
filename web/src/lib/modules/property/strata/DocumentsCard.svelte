<script lang="ts">
	// DocumentsCard lists a strata scheme's uploaded StrataMax documents and
	// their indexing state, and (with property_write) uploads, reindexes and
	// deletes them. It polls while any document is still being read.
	import { untrack } from 'svelte';
	import { api, ApiError } from '../../../api';
	import Button from '../../../components/Button.svelte';
	import DataTable from '../../../components/DataTable.svelte';
	import DateText from '../../../components/DateText.svelte';
	import LoadState from '../../../components/LoadState.svelte';
	import StatusBadge from '../../../components/StatusBadge.svelte';
	import StrataDocumentDialog from './StrataDocumentDialog.svelte';
	import { strataDocTypeLabel, type StrataDocument, type StrataScheme } from '../types';

	let { org, scheme, canWrite }: { org: string; scheme: StrataScheme; canWrite: boolean } = $props();

	const POLL_MS = 5000;
	const base = $derived(`/api/v1/orgs/${org}/strata-schemes/${scheme.id}/documents`);
	const headingId = $derived(`strata-docs-${org}-${scheme.id}`);

	let docs = $state<StrataDocument[]>([]);
	let loaded = $state('');
	let error = $state('');
	let actionError = $state('');
	let busyId = $state(0);
	let uploadOpen = $state(false);
	let seq = 0;

	async function load(background = false) {
		const token = ++seq;
		const url = base;
		if (!background) error = '';
		try {
			const got = await api<StrataDocument[]>('GET', url);
			if (token !== seq || url !== base) return;
			docs = got;
			loaded = url;
			error = '';
		} catch (e) {
			if (token !== seq || url !== base) return;
			// A failed background refresh keeps the rows on screen.
			const msg = e instanceof ApiError ? e.message : 'Could not load documents.';
			if (background && loaded === url) actionError = msg;
			else error = msg;
		}
	}

	$effect(() => {
		base;
		untrack(() => { loaded = ''; docs = []; actionError = ''; load(); });
		return () => { seq++; };
	});

	const indexing = $derived(docs.some((d) => d.status === 'pending' || d.status === 'extracting'));
	$effect(() => {
		if (!indexing) return;
		const timer = setInterval(() => load(true), POLL_MS);
		return () => clearInterval(timer);
	});

	async function act(doc: StrataDocument, method: 'POST' | 'DELETE', path: string, failure: string) {
		if (busyId) return;
		const token = seq;
		busyId = doc.id; actionError = '';
		try {
			await api(method, path);
			if (token === seq) await load(true);
		} catch (e) {
			if (token === seq) actionError = e instanceof ApiError ? e.message : failure;
		} finally {
			busyId = 0;
		}
	}

	function remove(doc: StrataDocument) {
		if (busyId || !window.confirm(`Delete "${doc.title}" and everything read from it?`)) return;
		act(doc, 'DELETE', `${base}/${doc.id}`, 'Could not delete the document.');
	}

	const statusLabel = (d: StrataDocument) =>
		d.status === 'indexed' ? 'Indexed' : d.status === 'failed' ? 'Failed' : 'Indexing…';
	const statusTone = (d: StrataDocument) =>
		d.status === 'indexed' ? 'success' : d.status === 'failed' ? 'danger' : 'info';

	const columns = $derived([
		{ key: 'title', label: 'Title', cell: titleCell },
		{ key: 'type', label: 'Type', cell: typeCell },
		{ key: 'date', label: 'Date', cell: dateCell },
		{ key: 'pages', label: 'Pages', cell: pagesCell, align: 'right' as const },
		{ key: 'status', label: 'Status', cell: statusCell },
		...(canWrite ? [{ key: 'actions', label: 'Actions', cell: actionsCell }] : [])
	]);
</script>

{#snippet titleCell(d: StrataDocument)}<a class="link" href={`${base}/${d.id}/file`} data-sveltekit-reload>{d.title}</a>{/snippet}
{#snippet typeCell(d: StrataDocument)}{strataDocTypeLabel(d.doc_type)}{/snippet}
{#snippet dateCell(d: StrataDocument)}<DateText iso={d.document_date} />{/snippet}
{#snippet pagesCell(d: StrataDocument)}{d.page_count}{/snippet}
{#snippet statusCell(d: StrataDocument)}
	<StatusBadge status={statusLabel(d)} tone={statusTone(d)} />
	{#if d.status === 'failed' && d.error}<p class="mt-1 text-sm text-base-content/70">{d.error}</p>{/if}
{/snippet}
{#snippet actionsCell(d: StrataDocument)}
	<div class="flex flex-wrap gap-2">
		<Button size="sm" variant="secondary" disabled={busyId !== 0} onclick={() => act(d, 'POST', `${base}/${d.id}/reindex`, 'Could not reindex the document.')}>
			<span aria-hidden="true">Reindex</span><span class="sr-only">Reindex {d.title}</span>
		</Button>
		<Button size="sm" variant="danger" disabled={busyId !== 0} onclick={() => remove(d)}>
			<span aria-hidden="true">Delete</span><span class="sr-only">Delete {d.title}</span>
		</Button>
	</div>
{/snippet}

<section class="card card-border bg-base-100 p-4" aria-labelledby={headingId}>
	<div class="mb-2 flex flex-wrap items-center justify-between gap-2">
		<h3 id={headingId} class="text-lg font-bold">Documents</h3>
		{#if canWrite}<Button size="sm" onclick={() => (uploadOpen = true)}>Upload document</Button>{/if}
	</div>
	{#if actionError}<p class="alert alert-error alert-soft mb-2" role="alert">{actionError}</p>{/if}
	<LoadState loading={loaded !== base} {error} empty={docs.length === 0} loadingLabel="Loading documents…"
		emptyTitle="No documents yet" emptyDescription="Upload minutes, by-laws, levy notices, budgets and financial statements from StrataMax."
		onretry={() => load()}>
		<DataTable caption="Strata documents" {columns} rows={docs} rowKey={(d) => d.id} />
	</LoadState>
</section>

{#if canWrite}
	<StrataDocumentDialog bind:open={uploadOpen} {org} schemeId={scheme.id} onsaved={() => load(true)} />
{/if}
