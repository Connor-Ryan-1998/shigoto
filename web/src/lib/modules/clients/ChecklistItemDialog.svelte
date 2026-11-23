<script lang="ts">
	// ChecklistItemDialog adds an action the client must complete before
	// kickoff, or edits one when `item` is set.
	import { untrack } from 'svelte';
	import { api, ApiError } from '../../api';
	import Dialog from '../../components/Dialog.svelte';
	import Field from '../../components/Field.svelte';
	import Button from '../../components/Button.svelte';
	import type { ChecklistItem } from './types';

	let {
		open = $bindable(false),
		org,
		contactId,
		item = null,
		onsaved
	}: {
		open?: boolean;
		org: string;
		contactId: number;
		item?: ChecklistItem | null;
		onsaved: (it: ChecklistItem) => void;
	} = $props();

	let action = $state('');
	let owner = $state('');
	let due = $state('');
	let error = $state('');
	let busy = $state(false);
	let actionSeq = 0;
	$effect(() => { org; contactId; open; return () => { actionSeq++; }; });
	$effect(() => {
		if (open) untrack(() => {
			action = item?.action ?? '';
			owner = item?.owner ?? '';
			due = item?.due_date ?? '';
			error = '';
			busy = false;
		});
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy || !open) return;
		const seq = actionSeq;
		const base = `/api/v1/orgs/${org}/contacts/${contactId}/checklist`;
		const body = { action, owner, due_date: due || null };
		busy = true;
		error = '';
		try {
			const saved = item
				? await api<ChecklistItem>('PUT', `${base}/${item.id}`, body)
				: await api<ChecklistItem>('POST', base, body);
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

<Dialog bind:open title={item ? 'Edit checklist item' : 'Add checklist item'} {busy}>
	{#snippet children({ close })}
	{#if open}
		<form class="flex flex-col gap-3" onsubmit={submit}>
			<Field label="Action" id="checklist-action" hint="The exact file or system permission needed before kickoff.">
				<input class="input w-full" id="checklist-action" maxlength="500" bind:value={action} disabled={busy} required />
			</Field>
			<div class="grid grid-cols-2 gap-3">
				<Field label="Owner" id="checklist-owner">
					<input class="input w-full" id="checklist-owner" maxlength="500" bind:value={owner} disabled={busy} />
				</Field>
				<Field label="Due date" id="checklist-due">
					<input class="input w-full" id="checklist-due" type="date" bind:value={due} disabled={busy} />
				</Field>
			</div>
			{#if error}<p class="alert alert-error alert-soft" role="alert">{error}</p>{/if}
			<div class="modal-action">
				<Button variant="secondary" disabled={busy} onclick={close}>Cancel</Button>
				<Button type="submit" disabled={busy}>{item ? 'Save item' : 'Add item'}</Button>
			</div>
		</form>
	{/if}
	{/snippet}
</Dialog>
