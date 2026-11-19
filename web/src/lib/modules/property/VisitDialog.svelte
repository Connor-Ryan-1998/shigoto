<script lang="ts">
	import { untrack } from 'svelte';
	import { api, ApiError } from '../../api';
	import Dialog from '../../components/Dialog.svelte';
	import Field from '../../components/Field.svelte';
	import Button from '../../components/Button.svelte';
	import { localParts, type Visit } from './maintenance';

	// Books a visit (visit null) or reschedules one. Times are the org's
	// wall-clock times; the server reads them in the org timezone.
	let {
		open = $bindable(false),
		org,
		ticket,
		visit,
		onsaved
	}: {
		open?: boolean;
		org: string;
		ticket: { id: number; supplier: { id: number; name: string } | null };
		visit: Pick<Visit, 'id' | 'starts_at' | 'ends_at'> | null;
		onsaved: () => void;
	} = $props();

	let suppliers = $state<{ id: number; name: string }[]>([]);
	let supplierId = $state('');
	let date = $state('');
	let from = $state('');
	let to = $state('');
	let error = $state('');
	let busy = $state(false);

	$effect(() => {
		if (!open) return;
		untrack(() => {
			error = '';
			if (visit) {
				({ date, time: from } = localParts(visit.starts_at));
				to = localParts(visit.ends_at).time;
			} else {
				date = from = to = '';
				supplierId = String(ticket.supplier?.id ?? '');
				api<{ id: number; name: string }[]>('GET', `/api/v1/orgs/${org}/contacts?kind=supplier`)
					.then((list) => { suppliers = list; if (!supplierId && list[0]) supplierId = String(list[0].id); })
					.catch((e) => { error = e instanceof ApiError ? e.message : 'Could not load suppliers.'; });
			}
		});
	});

	async function submit(e: SubmitEvent | MouseEvent) {
		e.preventDefault();
		if (busy) return;
		if (!date || !from || !to) { error = 'Choose a date and arrival window.'; return; }
		if (to <= from) { error = 'The arrival window must end after it starts.'; return; }
		busy = true; error = '';
		const times = { starts_at: `${date}T${from}`, ends_at: `${date}T${to}` };
		try {
			if (visit) await api('PUT', `/api/v1/orgs/${org}/maintenance/${ticket.id}/visits/${visit.id}`, times);
			else await api('POST', `/api/v1/orgs/${org}/maintenance/${ticket.id}/visits`, { supplier_contact_id: Number(supplierId), ...times });
			open = false;
			onsaved();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not save the visit.';
		} finally {
			busy = false;
		}
	}
</script>

<Dialog bind:open title={visit ? 'Reschedule visit' : 'Book visit'} {busy}>
	{#snippet children({ close })}
		<form class="flex flex-col gap-3" onsubmit={submit}>
			{#if visit}<p class="alert alert-warning alert-soft">Rescheduling clears the tenant's confirmation and entry permission.</p>{/if}
			{#if error}<p class="alert alert-error alert-soft" role="alert">{error}</p>{/if}
			<fieldset disabled={busy} class="grid gap-3">
				{#if !visit}
					<Field label="Tradesperson" id="visit-supplier">
						<select class="select w-full" id="visit-supplier" bind:value={supplierId} required>
							{#if ticket.supplier && !suppliers.some((s) => s.id === ticket.supplier?.id)}<option value={String(ticket.supplier.id)}>{ticket.supplier.name}</option>{/if}
							{#each suppliers as s (s.id)}<option value={String(s.id)}>{s.name}</option>{/each}
						</select>
					</Field>
				{/if}
				<Field label="Date" id="visit-date"><input class="input w-full" id="visit-date" type="date" bind:value={date} required /></Field>
				<div class="grid grid-cols-2 gap-2">
					<Field label="Arrive from" id="visit-from"><input class="input w-full" id="visit-from" type="time" bind:value={from} required /></Field>
					<Field label="Arrive by" id="visit-to"><input class="input w-full" id="visit-to" type="time" bind:value={to} required /></Field>
				</div>
			</fieldset>
			<div class="modal-action">
				<Button variant="secondary" disabled={busy} onclick={close}>Cancel</Button>
				<Button type="submit" {busy}>{busy ? 'Saving…' : visit ? 'Reschedule' : 'Book visit'}</Button>
			</div>
		</form>
	{/snippet}
</Dialog>
