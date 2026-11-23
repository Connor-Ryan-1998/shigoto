<script lang="ts">
	// ChecklistPanel is a client's kickoff checklist for staff: add, edit
	// and delete items, and tick one once staff confirm the client did it.
	import { untrack } from 'svelte';
	import { api, ApiError } from '../../api';
	import { date } from '../../format';
	import ChecklistItemDialog from './ChecklistItemDialog.svelte';
	import type { ChecklistItem } from './types';

	let { org, contactId, canWrite }: { org: string; contactId: number; canWrite: boolean } = $props();

	const base = $derived(`/api/v1/orgs/${org}/contacts/${contactId}/checklist`);
	const key = $derived(`${org}/${contactId}`);
	let items = $state<ChecklistItem[]>([]);
	let loaded = $state('');
	let error = $state('');
	let busy = $state(false);
	let dialogOpen = $state(false);
	let editing = $state<ChecklistItem | null>(null);
	let seq = 0;

	async function load() {
		const s = ++seq;
		const k = key;
		try {
			const result = await api<ChecklistItem[]>('GET', base);
			if (s !== seq || k !== key) return;
			items = result;
			loaded = k;
		} catch (e) {
			if (s === seq && k === key) error = e instanceof ApiError ? e.message : 'Could not load the checklist.';
		}
	}
	$effect(() => {
		key;
		untrack(() => { loaded = ''; error = ''; load(); });
		return () => { seq++; };
	});

	async function toggle(it: ChecklistItem, e: Event) {
		const box = e.currentTarget as HTMLInputElement;
		const done = box.checked;
		if (busy || !canWrite) { box.checked = !done; return; }
		busy = true;
		error = '';
		try {
			const saved = await api<ChecklistItem>('POST', `${base}/${it.id}/done`, { done });
			items = items.map((x) => (x.id === saved.id ? saved : x));
		} catch (err) {
			box.checked = !done;
			error = err instanceof ApiError ? err.message : 'Something went wrong.';
		} finally {
			busy = false;
		}
	}

	async function remove(it: ChecklistItem) {
		if (busy || !canWrite || !confirm(`Delete "${it.action}"?`)) return;
		busy = true;
		error = '';
		try {
			await api('DELETE', `${base}/${it.id}`);
			await load();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Something went wrong.';
		} finally {
			busy = false;
		}
	}

	function open(it: ChecklistItem | null) {
		editing = it;
		dialogOpen = true;
	}
</script>

<section aria-labelledby="checklist-heading">
	<div class="mb-2 flex items-center justify-between">
		<h2 id="checklist-heading" class="text-lg font-semibold">Client action checklist</h2>
		{#if canWrite}<button type="button" class="btn btn-ghost btn-sm text-primary" disabled={busy || loaded !== key} onclick={() => open(null)}>+ Add item</button>{/if}
	</div>
	<p class="mb-2 text-sm text-base-content/70">Files or system permissions needed before kickoff. Tick an item once you've confirmed it's done.</p>
	{#if error}<p class="alert alert-error alert-soft mb-2" role="alert">{error}</p>{/if}
	{#if loaded !== key}
		{#if !error}<p role="status">Loading checklist…</p>{/if}
	{:else}
		<div class="overflow-x-auto rounded-box border border-base-300 bg-base-100">
			<table class="table">
				<thead><tr><th><span class="sr-only">Confirmed</span></th><th>Action</th><th>Owner</th><th>Due</th><th><span class="sr-only">Actions</span></th></tr></thead>
				<tbody>
					{#each items as it (it.id)}
						<tr>
							<td><input type="checkbox" class="checkbox checkbox-sm" checked={it.done_at !== null} disabled={!canWrite || busy}
								aria-label={`Confirmed: ${it.action}`} onchange={(e) => toggle(it, e)} /></td>
							<td>
								{it.action}
								{#if it.done_at}<div class="text-sm text-success">Confirmed {date(it.done_at.slice(0, 10))}</div>{/if}
							</td>
							<td>{it.owner || '—'}</td>
							<td>{it.due_date ? date(it.due_date) : '—'}</td>
							<td class="whitespace-nowrap text-right">
								{#if canWrite}
									<button type="button" class="btn btn-ghost btn-xs" disabled={busy} aria-label={`Edit ${it.action}`} onclick={() => open(it)}>Edit</button>
									<button type="button" class="btn btn-ghost btn-xs" disabled={busy} aria-label={`Delete ${it.action}`} onclick={() => remove(it)}>Delete</button>
								{/if}
							</td>
						</tr>
					{:else}
						<tr><td colspan="5" class="text-base-content/70">No checklist items yet.</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
	{#if canWrite}<ChecklistItemDialog bind:open={dialogOpen} {org} {contactId} item={editing} onsaved={load} />{/if}
</section>
