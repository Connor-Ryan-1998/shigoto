<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '../../../../../lib/api';
	import PageHeader from '../../../../../lib/components/PageHeader.svelte';
	import Field from '../../../../../lib/components/Field.svelte';
	import Button from '../../../../../lib/components/Button.svelte';
	import Dialog from '../../../../../lib/components/Dialog.svelte';
	import LoadState from '../../../../../lib/components/LoadState.svelte';
	import StatusBadge from '../../../../../lib/components/StatusBadge.svelte';
	import MediaGallery from '../../../../../lib/modules/property/MediaGallery.svelte';
	import MediaPicker from '../../../../../lib/modules/property/MediaPicker.svelte';
	import {
		CATEGORY_LABEL, STATUS_LABEL, STATUS_TONE, URGENCY_LABEL, eventLabel, isOpen,
		type Ticket, type TicketEvent, type TicketFile
	} from '../../../../../lib/modules/property/maintenance';

	type Detail = { ticket: Ticket; files: TicketFile[]; events: TicketEvent[] };

	const org = $derived(page.params.org ?? '');
	const id = $derived(page.params.id ?? '');
	const apiBase = $derived(`/api/v1/portal/${org}/maintenance/${id}`);

	let detail = $state<Detail | null>(null);
	let error = $state('');
	let message = $state('');
	let sending = $state(false);
	let sendError = $state('');
	let cancelOpen = $state(false);
	let reason = $state('');
	let cancelling = $state(false);
	let cancelError = $state('');
	let seq = 0;

	async function load() {
		const token = ++seq;
		error = '';
		try {
			const d = await api<Detail>('GET', apiBase);
			if (token === seq) detail = d;
		} catch (e) {
			if (token === seq) error = e instanceof ApiError ? e.message : 'Could not load the repair.';
		}
	}
	$effect(() => { apiBase; detail = null; load(); return () => { seq++; }; });

	async function send(e: SubmitEvent) {
		e.preventDefault();
		if (sending || !message.trim()) return;
		sending = true; sendError = '';
		try {
			await api('POST', `${apiBase}/comments`, { body: message });
			message = '';
			await load();
		} catch (err) {
			sendError = err instanceof ApiError ? err.message : 'Could not send.';
		} finally {
			sending = false;
		}
	}

	async function cancelRequest(e: SubmitEvent) {
		e.preventDefault();
		if (cancelling) return;
		cancelling = true; cancelError = '';
		try {
			await api('POST', `${apiBase}/cancel`, { reason });
			cancelOpen = false;
			await load();
		} catch (err) {
			cancelError = err instanceof ApiError ? err.message : 'Could not cancel.';
		} finally {
			cancelling = false;
		}
	}
</script>

<LoadState loading={!detail && !error} {error} loadingLabel="Loading repair…" onretry={load}>
	{#if detail}
		{@const t = detail.ticket}
		<PageHeader title={t.title} back={{ href: `/portal/${org}/repairs`, label: 'Back to repairs' }}>
			{#snippet actions()}<StatusBadge status={STATUS_LABEL[t.status]} tone={STATUS_TONE[t.status]} />{/snippet}
		</PageHeader>
		<p class="mb-4 text-sm text-base-content/70">{t.ref} · {CATEGORY_LABEL[t.category] ?? t.category} · {URGENCY_LABEL[t.urgency]} · {t.property_address}</p>

		<div class="grid max-w-3xl gap-4">
			<section class="card bg-base-100 shadow-sm" aria-labelledby="repair-progress">
				<div class="card-body">
					<h2 id="repair-progress" class="font-semibold">Progress</h2>
					{#if t.supplier}<p>Tradesperson: <span class="font-semibold">{t.supplier.name}</span></p>{/if}
					<ol class="flex flex-col gap-3">
						{#each [...detail.events].reverse() as e (e.id)}
							<li class="rounded-box bg-base-200 p-3">
								<p class="text-sm"><span class="font-semibold">{eventLabel(e)}</span> <span class="text-base-content/70">· {new Date(e.created_at).toLocaleString()}</span></p>
								{#if e.body}<p class="mt-1 whitespace-pre-line">{e.body}</p>{/if}
							</li>
						{/each}
					</ol>
				</div>
			</section>

			{#if isOpen(t)}
				<section class="card bg-base-100 shadow-sm" aria-labelledby="repair-message">
					<div class="card-body">
						<h2 id="repair-message" class="font-semibold">Messages</h2>
						<form class="flex flex-col gap-2" onsubmit={send}>
							{#if sendError}<p class="text-sm text-error" role="alert">{sendError}</p>{/if}
							<Field label="Add a message" id="repair-comment">
								<textarea class="textarea w-full" id="repair-comment" rows="2" maxlength="4000" bind:value={message} disabled={sending}></textarea>
							</Field>
							<div><Button type="submit" busy={sending} disabled={!message.trim()}>Send</Button></div>
						</form>
					</div>
				</section>
			{/if}

			<section class="card bg-base-100 shadow-sm" aria-labelledby="repair-media">
				<div class="card-body">
					<h2 id="repair-media" class="font-semibold">Photos and recordings</h2>
					<MediaGallery base={`${apiBase}/files`} files={detail.files} />
					{#if isOpen(t)}<MediaPicker uploadUrl={`${apiBase}/files`} onuploaded={load} />{/if}
				</div>
			</section>

			<section class="card bg-base-100 shadow-sm" aria-labelledby="repair-details">
				<div class="card-body">
					<h2 id="repair-details" class="font-semibold">Details</h2>
					<p class="whitespace-pre-line">{t.description}</p>
					{#if t.access_notes}<p class="text-sm"><span class="font-semibold">Access:</span> {t.access_notes}</p>{/if}
				</div>
			</section>

			{#if t.status === 'new' || t.status === 'viewed'}
				<div><Button variant="secondary" onclick={() => { reason = ''; cancelError = ''; cancelOpen = true; }}>Cancel request</Button></div>
			{/if}
		</div>

		<Dialog bind:open={cancelOpen} title="Cancel request" busy={cancelling}>
			{#snippet children({ close })}
				<form class="flex flex-col gap-3" onsubmit={cancelRequest}>
					{#if cancelError}<p class="alert alert-error alert-soft" role="alert">{cancelError}</p>{/if}
					<Field label="Reason" id="repair-cancel-reason">
						<textarea class="textarea w-full" id="repair-cancel-reason" rows="2" maxlength="4000" bind:value={reason} required disabled={cancelling}></textarea>
					</Field>
					<div class="modal-action">
						<Button variant="secondary" disabled={cancelling} onclick={close}>Keep request</Button>
						<Button type="submit" variant="danger" busy={cancelling}>Cancel request</Button>
					</div>
				</form>
			{/snippet}
		</Dialog>
	{/if}
</LoadState>
