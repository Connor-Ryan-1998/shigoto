<script lang="ts">
	import { untrack } from 'svelte';
	import { api, ApiError } from '../../api';
	import Dialog from '../../components/Dialog.svelte';
	import Field from '../../components/Field.svelte';
	import Button from '../../components/Button.svelte';
	import LoadState from '../../components/LoadState.svelte';
	import type { Ticket } from './maintenance';

	let { open = $bindable(false), org, ticket, onsaved }: { open?: boolean; org: string; ticket: Ticket; onsaved: (t: Ticket) => void } = $props();

	let suppliers = $state<{ id: number; name: string }[]>([]);
	let supplierId = $state('');
	let loaded = $state(false);
	let loadError = $state('');
	let error = $state('');
	let busy = $state(false);
	let seq = 0;

	async function load() {
		const token = ++seq;
		loaded = false; loadError = ''; error = '';
		try {
			const list = await api<{ id: number; name: string }[]>('GET', `/api/v1/orgs/${org}/contacts?kind=supplier`);
			if (token !== seq) return;
			suppliers = list;
			supplierId = String(ticket.supplier?.id ?? list[0]?.id ?? '');
			loaded = true;
		} catch (e) {
			if (token === seq) loadError = e instanceof ApiError ? e.message : 'Could not load suppliers.';
		}
	}
	$effect(() => { if (open) untrack(load); return () => { seq++; }; });

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy || !supplierId) return;
		busy = true; error = '';
		try {
			const t = await api<Ticket>('PUT', `/api/v1/orgs/${org}/maintenance/${ticket.id}/supplier`, { supplier_contact_id: Number(supplierId) });
			open = false;
			onsaved(t);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not assign the tradesperson.';
		} finally {
			busy = false;
		}
	}
</script>

<Dialog bind:open title="Assign tradesperson" {busy}>
	{#snippet children({ close })}
		{#if open}
			<LoadState loading={!loaded} error={loadError} loadingLabel="Loading suppliers…" onretry={load} empty={loaded && suppliers.length === 0} emptyTitle="No suppliers yet" emptyDescription="Add a supplier contact first.">
				<form class="flex flex-col gap-3" onsubmit={submit}>
					{#if error}<p class="alert alert-error alert-soft" role="alert">{error}</p>{/if}
					<fieldset disabled={busy}>
						<Field label="Tradesperson" id="ticket-supplier">
							<select class="select w-full" id="ticket-supplier" bind:value={supplierId}>
								{#each suppliers as s (s.id)}<option value={String(s.id)}>{s.name}</option>{/each}
							</select>
						</Field>
					</fieldset>
					<div class="modal-action">
						<Button variant="secondary" disabled={busy} onclick={close}>Cancel</Button>
						<Button type="submit" {busy}>{busy ? 'Saving…' : 'Assign'}</Button>
					</div>
				</form>
			</LoadState>
		{/if}
	{/snippet}
</Dialog>
