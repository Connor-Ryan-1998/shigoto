<script lang="ts">
	// StrataSchemeDialog creates a strata scheme for the organisation and
	// hands it back; linking it to a property is the caller's save.
	import { untrack } from 'svelte';
	import { api, ApiError } from '../../api';
	import Dialog from '../../components/Dialog.svelte';
	import Field from '../../components/Field.svelte';
	import Button from '../../components/Button.svelte';
	import { STRATA_POSITIONS, strataCtsError, type StrataScheme } from './types';

	let {
		open = $bindable(false),
		org,
		oncreated
	}: {
		open?: boolean;
		org: string;
		oncreated: (scheme: StrataScheme) => void;
	} = $props();

	let name = $state('');
	let cts = $state('');
	let positions = $state({ is_chairperson: false, is_secretary: false, is_treasurer: false });
	let error = $state('');
	let ctsError = $state('');
	let busy = $state(false);
	let seq = 0;
	$effect(() => { org; open; return () => { seq++; }; });

	$effect(() => {
		if (open) untrack(() => {
			name = ''; cts = ''; positions = { is_chairperson: false, is_secretary: false, is_treasurer: false };
			error = ''; ctsError = ''; busy = false;
		});
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy || !open) return;
		const token = seq;
		busy = true; error = ''; ctsError = '';
		try {
			const created = await api<StrataScheme>('POST', `/api/v1/orgs/${org}/strata-schemes`, { name, cts_number: cts, ...positions });
			if (token !== seq || !open) return;
			open = false;
			oncreated(created);
		} catch (err) {
			if (token !== seq) return;
			const message = err instanceof ApiError ? err.message : 'Could not create the scheme.';
			ctsError = strataCtsError(message) ?? '';
			error = message;
		} finally {
			if (token === seq) busy = false;
		}
	}
</script>

<Dialog bind:open title="New strata scheme" {busy}>
	{#snippet children({ close })}
	{#if open}
		<form class="flex flex-col gap-3" onsubmit={submit}>
			{#if error}<p class="alert alert-error alert-soft" role="alert">{error}</p>{/if}
			<fieldset disabled={busy} class="grid gap-3 sm:grid-cols-2">
				<Field label="Scheme name" id="new-scheme-name"><input class="input w-full" id="new-scheme-name" bind:value={name} required /></Field>
				<Field label="CTS number" id="new-scheme-cts" error={ctsError || undefined}><input class="input w-full" id="new-scheme-cts" bind:value={cts} /></Field>
				<div class="grid gap-1 sm:col-span-2" role="group" aria-label="Positions held">
					<span class="text-sm font-semibold">Positions held</span>
					{#each STRATA_POSITIONS as p (p.value)}
						<label class="label"><input class="checkbox" type="checkbox" bind:checked={positions[p.field]} /> {p.label}</label>
					{/each}
				</div>
			</fieldset>
			<div class="modal-action">
				<Button variant="secondary" disabled={busy} onclick={close}>Cancel</Button>
				<Button type="submit" {busy}>{busy ? 'Saving…' : 'Create scheme'}</Button>
			</div>
		</form>
	{/if}
	{/snippet}
</Dialog>
