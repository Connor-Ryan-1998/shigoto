<script lang="ts">
	// One short text editor for every ticket message: an internal note or a
	// shared comment (events), or the note/reason closing a ticket.
	import { api, ApiError } from '../../api';
	import Dialog from '../../components/Dialog.svelte';
	import Field from '../../components/Field.svelte';
	import Button from '../../components/Button.svelte';

	export type MessageAction = 'note' | 'comment' | 'complete' | 'cancel' | 'cancel_visit';

	const COPY: Record<MessageAction, { title: string; label: string; submit: string; required: boolean; hint: string }> = {
		note: { title: 'Add note', label: 'Note', submit: 'Save note', required: true, hint: 'Only staff see notes.' },
		comment: { title: 'Message tenant', label: 'Message', submit: 'Send message', required: true, hint: 'Tenants on the lease see this.' },
		complete: { title: 'Mark complete', label: 'Completion note (optional)', submit: 'Mark complete', required: false, hint: 'Tenants see this note.' },
		cancel: { title: 'Cancel repair', label: 'Reason', submit: 'Cancel repair', required: true, hint: 'Tenants see this reason.' },
		cancel_visit: { title: 'Cancel visit', label: 'Reason (optional)', submit: 'Cancel visit', required: false, hint: 'Tenants see this reason.' }
	};

	let { open = $bindable(false), org, ticketId, visitId, action, onsaved }: { open?: boolean; org: string; ticketId: number; visitId?: number; action: MessageAction; onsaved: () => void } = $props();

	const copy = $derived(COPY[action]);
	let text = $state('');
	let error = $state('');
	let busy = $state(false);
	$effect(() => { if (open) { text = ''; error = ''; } });

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy) return;
		busy = true; error = '';
		const base = `/api/v1/orgs/${org}/maintenance/${ticketId}`;
		try {
			if (action === 'note' || action === 'comment') await api('POST', `${base}/events`, { kind: action, body: text });
			else if (action === 'complete') await api('POST', `${base}/complete`, { note: text });
			else if (action === 'cancel_visit') await api('POST', `${base}/visits/${visitId}/cancel`, { reason: text });
			else await api('POST', `${base}/cancel`, { reason: text });
			open = false;
			onsaved();
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not save.';
		} finally {
			busy = false;
		}
	}
</script>

<Dialog bind:open title={copy.title} {busy}>
	{#snippet children({ close })}
		<form class="flex flex-col gap-3" onsubmit={submit}>
			{#if error}<p class="alert alert-error alert-soft" role="alert">{error}</p>{/if}
			<fieldset disabled={busy}>
				<Field label={copy.label} id="ticket-message" hint={copy.hint}>
					<textarea class="textarea w-full" id="ticket-message" rows="4" maxlength="4000" bind:value={text} required={copy.required}></textarea>
				</Field>
			</fieldset>
			<div class="modal-action">
				<Button variant="secondary" disabled={busy} onclick={close}>Close</Button>
				<Button type="submit" variant={action === 'cancel' || action === 'cancel_visit' ? 'danger' : 'primary'} {busy}>{busy ? 'Saving…' : copy.submit}</Button>
			</div>
		</form>
	{/snippet}
</Dialog>
