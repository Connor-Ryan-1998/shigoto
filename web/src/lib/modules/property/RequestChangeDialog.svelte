<script lang="ts">
	import { api, ApiError } from '../../api';
	import Dialog from '../../components/Dialog.svelte';
	import Field from '../../components/Field.svelte';
	import Button from '../../components/Button.svelte';
	import AvailabilityEditor from './AvailabilityEditor.svelte';
	import type { Window } from './maintenance';

	let { open = $bindable(false), apiBase, visitId, today, onsaved }: { open?: boolean; apiBase: string; visitId: number; today: string; onsaved: () => void } = $props();

	let note = $state('');
	let windows = $state<Window[]>([]);
	let error = $state('');
	let busy = $state(false);
	$effect(() => { if (open) { note = ''; windows = []; error = ''; } });

	async function submit(e: SubmitEvent | MouseEvent) {
		e.preventDefault();
		if (busy) return;
		if (!note.trim()) { error = 'Tell us what would suit you better.'; return; }
		busy = true; error = '';
		try {
			await api('POST', `${apiBase}/visits/${visitId}/request-change`, { note, windows: windows.length ? windows : undefined });
			open = false;
			onsaved();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not send your request.';
		} finally {
			busy = false;
		}
	}
</script>

<Dialog bind:open title="Request a different time" {busy}>
	{#snippet children({ close })}
		<form class="flex flex-col gap-3" onsubmit={submit}>
			{#if error}<p class="alert alert-error alert-soft" role="alert">{error}</p>{/if}
			<fieldset disabled={busy} class="flex flex-col gap-3">
				<Field label="What would suit you better?" id="change-note">
					<textarea class="textarea w-full" id="change-note" rows="3" maxlength="2000" bind:value={note} required></textarea>
				</Field>
				<p class="text-sm font-semibold">Times that suit you (optional)</p>
				<AvailabilityEditor {windows} {today} disabled={busy} onchange={(w) => (windows = w)} />
			</fieldset>
			<div class="modal-action">
				<Button variant="secondary" disabled={busy} onclick={close}>Cancel</Button>
				<Button type="submit" {busy}>{busy ? 'Sending…' : 'Send request'}</Button>
			</div>
		</form>
	{/snippet}
</Dialog>
