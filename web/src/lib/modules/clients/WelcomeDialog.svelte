<script lang="ts">
	// WelcomeDialog edits a client's welcome note: communication rules shown
	// at the top of their portal hub.
	import { untrack } from 'svelte';
	import { api, ApiError } from '../../api';
	import Dialog from '../../components/Dialog.svelte';
	import Field from '../../components/Field.svelte';
	import Button from '../../components/Button.svelte';
	import type { Welcome } from './types';

	let {
		open = $bindable(false),
		org,
		contactId,
		welcome,
		onsaved
	}: {
		open?: boolean;
		org: string;
		contactId: number;
		welcome: Welcome;
		onsaved: (w: Welcome) => void;
	} = $props();

	let notes = $state('');
	let error = $state('');
	let busy = $state(false);
	let actionSeq = 0;
	$effect(() => { org; contactId; open; return () => { actionSeq++; }; });
	$effect(() => {
		if (open) untrack(() => { notes = welcome.notes; error = ''; busy = false; });
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy || !open) return;
		const seq = actionSeq;
		busy = true;
		error = '';
		try {
			const saved = await api<Welcome>('PUT', `/api/v1/orgs/${org}/contacts/${contactId}/welcome`, { notes });
			if (seq !== actionSeq || !open) return;
			open = false;
			onsaved(saved);
		} catch (err) {
			if (seq === actionSeq && open) error = err instanceof ApiError ? err.message : 'Something went wrong.';
		} finally {
			if (seq === actionSeq) busy = false;
		}
	}
</script>

<Dialog bind:open title="Edit welcome notes" {busy}>
	{#snippet children({ close })}
	{#if open}
		<form class="flex flex-col gap-3" onsubmit={submit}>
			<Field label="Welcome notes" id="welcome-notes" hint="Shown at the top of the client's portal: which channel to use and expected response times.">
				<textarea class="textarea min-h-40 w-full" id="welcome-notes" maxlength="4000" bind:value={notes} disabled={busy}></textarea>
			</Field>
			{#if error}<p class="alert alert-error alert-soft" role="alert">{error}</p>{/if}
			<div class="modal-action">
				<Button variant="secondary" disabled={busy} onclick={close}>Cancel</Button>
				<Button type="submit" disabled={busy}>Save notes</Button>
			</div>
		</form>
	{/if}
	{/snippet}
</Dialog>
