<script lang="ts">
	// One client document. A sent signature document shows the sign panel:
	// open the PDF, type your full legal name, tick consent, sign. Signed
	// documents show who signed and link to the signing certificate;
	// withdrawn and replaced ones can be read but not signed.
	import { onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '../../../../../lib/api';
	import { useUnsavedChanges } from '../../../../../lib/forms/unsaved.svelte';
	import PageHeader from '../../../../../lib/components/PageHeader.svelte';
	import EmptyState from '../../../../../lib/components/EmptyState.svelte';
	import Field from '../../../../../lib/components/Field.svelte';
	import Button from '../../../../../lib/components/Button.svelte';
	import {
		certificateUrl,
		documentFileUrl,
		isToSign,
		type PortalDocument
	} from '../../../../../lib/modules/clients/portal';

	const org = $derived(page.params.org ?? '');
	const id = $derived(Number(page.params.id ?? ''));
	const url = $derived(`/api/v1/portal/${org}/documents/${id}`);
	const back = $derived(`/portal/${org}/documents`);

	let doc = $state<PortalDocument | null>(null);
	let loadError = $state('');
	let opened = $state(false);
	let name = $state('');
	let consent = $state(false);
	let saving = $state(false);
	let error = $state('');
	let seq = 0;
	onDestroy(() => { seq++; });

	const dirty = $derived(name.trim() !== '' || consent);
	const { allowLeave } = useUnsavedChanges(() => dirty && !saving && !!doc && isToSign(doc), () => saving);
	const canSign = $derived(opened && name.trim() !== '' && consent && !saving);

	async function load() {
		const s = ++seq;
		loadError = '';
		try {
			const d = await api<PortalDocument>('GET', url);
			if (s === seq) doc = d;
		} catch (e) {
			if (s === seq) loadError = e instanceof ApiError ? (e.status === 404 ? 'Document not found.' : e.message) : 'Something went wrong.';
		}
	}
	$effect(() => { if (org && id) load(); });

	function openPdf() {
		window.open(documentFileUrl(org, id), '_blank', 'noopener');
		opened = true;
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!canSign || !doc) return;
		saving = true;
		error = '';
		try {
			doc = await api<PortalDocument>('POST', `${url}/sign`, { typed_name: name, consent: true });
			name = '';
			consent = false;
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Something went wrong.';
			if (err instanceof ApiError && err.status === 409) {
				// Signed in another tab, withdrawn or replaced meanwhile: show
				// the current state. A conflict that leaves it still to sign
				// (e.g. the file changed) keeps the form and the message.
				await load();
				if (doc && !isToSign(doc)) {
					name = '';
					consent = false;
					error = '';
				}
			}
		} finally {
			saving = false;
		}
	}

	function cancel() {
		if (allowLeave()) goto(back);
	}
</script>

{#if loadError}
	<EmptyState title="Couldn't load this document" description={loadError}>
		{#snippet action()}<a class="btn" href={back}>Back to documents</a>{/snippet}
	</EmptyState>
{:else if !doc}
	<p aria-live="polite">Loading…</p>
{:else}
	<PageHeader title={doc.title} back={{ href: back, label: 'Back to documents' }} />
	<div class="grid max-w-4xl gap-6 md:grid-cols-[1fr_22rem]">
		<section class="card bg-base-100 shadow-sm" aria-label="Document">
			<div class="card-body gap-3">
				<p class="text-sm text-base-content/70">{doc.project_name || 'General document'} · {doc.file.original_name}</p>
				{#if isToSign(doc)}
					<p>Read the full document before signing. It opens in a new tab.</p>
					<div><button type="button" class="btn btn-secondary" onclick={openPdf}>Open {doc.title} (PDF)</button></div>
					{#if opened}<p class="text-sm text-success">Opened. Come back here to sign.</p>{/if}
				{:else}
					<div><a class="btn btn-secondary" href={documentFileUrl(org, doc.id)} download>Download PDF</a></div>
				{/if}
				{#if doc.status === 'withdrawn'}
					<p class="alert alert-soft">This document was withdrawn and no longer needs your signature.</p>
				{:else if doc.status === 'superseded'}
					<p class="alert alert-soft">This document was replaced by a newer version.</p>
				{/if}
			</div>
		</section>

		{#if doc.signature}
			<section class="card bg-base-100 shadow-sm" aria-labelledby="signed-heading">
				<div class="card-body gap-2">
					<h2 id="signed-heading" class="text-lg font-semibold text-success">Signed</h2>
					<p>Signed by {doc.signature.typed_name} ({doc.signature.email}) on {new Date(doc.signature.signed_at).toLocaleString()}.</p>
					<a class="link" href={certificateUrl(org, doc.id)} download>Download signing certificate</a>
				</div>
			</section>
		{:else if isToSign(doc)}
			<section class="card bg-base-100 shadow-sm" aria-labelledby="sign-heading">
				<form class="card-body gap-3" onsubmit={submit}>
					<h2 id="sign-heading" class="text-lg font-semibold">Sign</h2>
					<Field label="Full legal name" id="sign-name">
						<input class="input w-full" id="sign-name" autocomplete="name" maxlength="200" bind:value={name} disabled={saving} />
					</Field>
					<label class="flex items-start gap-2">
						<input type="checkbox" class="checkbox checkbox-sm mt-1" bind:checked={consent} disabled={saving} aria-label={doc.consent_text} />
						<span>{doc.consent_text}</span>
					</label>
					{#if !opened}<p class="text-sm text-base-content/70">Open the document first.</p>{/if}
					{#if error}<p class="alert alert-error alert-soft" role="alert">{error}</p>{/if}
					<p class="text-xs text-base-content/70">We record your name, email, the time, your IP address and browser, and a fingerprint of this exact file.</p>
					<div class="flex gap-2">
						<Button type="submit" disabled={!canSign}>Sign document</Button>
						<Button variant="secondary" disabled={saving} onclick={cancel}>Cancel</Button>
					</div>
				</form>
			</section>
		{/if}
	</div>
{/if}
