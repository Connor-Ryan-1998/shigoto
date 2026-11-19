<script lang="ts">
	// Staff raise a repair on a property (e.g. a phoned-in urgent repair).
	import { api, ApiError } from '../../api';
	import Dialog from '../../components/Dialog.svelte';
	import Field from '../../components/Field.svelte';
	import Button from '../../components/Button.svelte';
	import { CATEGORY_OPTIONS, URGENCY_LABEL, type Ticket } from './maintenance';

	let { open = $bindable(false), org, propertyId, oncreated }: { open?: boolean; org: string; propertyId: string; oncreated: (t: Ticket) => void } = $props();

	let title = $state('');
	let description = $state('');
	let category = $state('plumbing');
	let urgency = $state('routine');
	let error = $state('');
	let busy = $state(false);

	$effect(() => {
		if (open) { title = ''; description = ''; category = 'plumbing'; urgency = 'routine'; error = ''; }
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy) return;
		busy = true; error = '';
		try {
			const t = await api<Ticket>('POST', `/api/v1/orgs/${org}/properties/${propertyId}/maintenance`, { title, description, category, urgency });
			open = false;
			oncreated(t);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not create the repair.';
		} finally {
			busy = false;
		}
	}
</script>

<Dialog bind:open title="New repair" {busy}>
	{#snippet children({ close })}
		<form class="flex flex-col gap-3" onsubmit={submit}>
			{#if error}<p class="alert alert-error alert-soft" role="alert">{error}</p>{/if}
			<fieldset disabled={busy} class="grid gap-3">
				<Field label="Title" id="ticket-title"><input class="input w-full" id="ticket-title" maxlength="120" bind:value={title} required /></Field>
				<Field label="Description" id="ticket-description"><textarea class="textarea w-full" id="ticket-description" rows="4" maxlength="4000" bind:value={description} required></textarea></Field>
				<Field label="Type" id="ticket-category">
					<select class="select w-full" id="ticket-category" bind:value={category}>
						{#each CATEGORY_OPTIONS as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
					</select>
				</Field>
				<Field label="Urgency" id="ticket-urgency">
					<select class="select w-full" id="ticket-urgency" bind:value={urgency}>
						{#each Object.entries(URGENCY_LABEL) as [v, l] (v)}<option value={v}>{l}</option>{/each}
					</select>
				</Field>
			</fieldset>
			<div class="modal-action">
				<Button variant="secondary" disabled={busy} onclick={close}>Cancel</Button>
				<Button type="submit" {busy}>{busy ? 'Saving…' : 'Create repair'}</Button>
			</div>
		</form>
	{/snippet}
</Dialog>
