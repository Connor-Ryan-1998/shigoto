<script lang="ts">
	import { api, ApiError } from '../../api';
	import Button from '../../components/Button.svelte';
	import Dialog from '../../components/Dialog.svelte';
	import { visitWindow, type Visit } from './maintenance';

	// The tenant's view of the booked visit: confirm it (choosing whether
	// they'll be home or allow entry), ask for another time, or withdraw
	// entry permission until the visit starts.
	let { visit, preference, apiBase, onchanged, onrequestchange }: {
		visit: Visit;
		preference: string | null;
		apiBase: string;
		onchanged: () => void;
		onrequestchange?: () => void;
	} = $props();

	let entryMode = $state('tenant_home');
	$effect.pre(() => { entryMode = preference === 'permitted' ? 'permitted' : 'tenant_home'; });
	let busy = $state(false);
	let error = $state('');
	let withdrawOpen = $state(false);
	const started = $derived(Date.now() >= new Date(visit.starts_at).getTime());
	const awaitingTenant = $derived(visit.status === 'booked');

	async function post(path: string, body?: unknown) {
		if (busy) return;
		busy = true; error = '';
		try {
			await api('POST', `${apiBase}/visits/${visit.id}/${path}`, body);
			withdrawOpen = false;
			onchanged();
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Something went wrong.';
		} finally {
			busy = false;
		}
	}
</script>

<section class="card border-2 border-primary bg-base-100 shadow-sm" aria-labelledby="visit-heading">
	<div class="card-body gap-2">
		<h2 id="visit-heading" class="font-semibold">Visit</h2>
		<p class="text-lg font-bold">{visitWindow(visit.starts_at, visit.ends_at)}</p>
		<p class="text-sm">Tradesperson: <span class="font-semibold">{visit.supplier.name}</span></p>
		{#if error}<p class="text-sm text-error" role="alert">{error}</p>{/if}

		{#if awaitingTenant}
			<fieldset class="fieldset" disabled={busy}>
				<legend class="fieldset-legend">Access for this visit</legend>
				<label class="label"><input class="radio" type="radio" name="entry" value="tenant_home" bind:group={entryMode} /> I'll be home</label>
				<label class="label"><input class="radio" type="radio" name="entry" value="permitted" bind:group={entryMode} /> OK to enter if I'm out</label>
			</fieldset>
			<div class="grid grid-cols-2 gap-2">
				<Button busy={busy} onclick={() => post('confirm', { entry_mode: entryMode })}>Confirm time</Button>
				<Button variant="secondary" disabled={busy} onclick={() => onrequestchange?.()}>Request change</Button>
			</div>
		{:else if visit.status === 'change_requested'}
			<p class="text-sm">Change requested — waiting for your property manager</p>
		{:else if visit.status === 'confirmed'}
			{#if visit.entry_state === 'permitted'}
				<p class="text-sm font-semibold text-success">You've allowed entry if you're out</p>
				{#if !started}
					<div><Button size="sm" variant="secondary" disabled={busy} onclick={() => (withdrawOpen = true)}>Withdraw permission</Button></div>
				{/if}
			{:else if visit.entry_state === 'withdrawn'}
				<p class="text-sm">You've withdrawn entry permission. Please be home or arrange access.</p>
				{#if !started}
					<div><Button size="sm" variant="secondary" busy={busy} onclick={() => post('confirm', { entry_mode: 'permitted' })}>Allow entry again</Button></div>
				{/if}
			{:else}
				<p class="text-sm">You've confirmed you'll be home.</p>
			{/if}
			{#if !started}<div><Button size="sm" variant="secondary" disabled={busy} onclick={() => onrequestchange?.()}>Request change</Button></div>{/if}
		{/if}
	</div>
</section>

<Dialog bind:open={withdrawOpen} title="Withdraw entry permission" busy={busy}>
	{#snippet children({ close })}
		<form class="flex flex-col gap-3" onsubmit={(e) => { e.preventDefault(); post('withdraw-entry'); }}>
			<p>The tradesperson won't be able to enter while you're out. You'll need to be home or arrange access.</p>
			<div class="modal-action">
				<Button variant="secondary" disabled={busy} onclick={close}>Keep permission</Button>
				<Button type="submit" variant="danger" {busy}>Withdraw</Button>
			</div>
		</form>
	{/snippet}
</Dialog>
