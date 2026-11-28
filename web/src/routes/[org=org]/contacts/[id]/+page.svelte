<script lang="ts">
	// Client page: what a client contact sees in their portal hub - welcome
	// notes, kickoff checklist and documents - managed by staff.
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '../../../../lib/api';
	import { session } from '../../../../lib/session.svelte';
	import PageHeader from '../../../../lib/components/PageHeader.svelte';
	import Button from '../../../../lib/components/Button.svelte';
	import EmptyState from '../../../../lib/components/EmptyState.svelte';
	import WelcomeDialog from '../../../../lib/modules/clients/WelcomeDialog.svelte';
	import ChecklistPanel from '../../../../lib/modules/clients/ChecklistPanel.svelte';
	import ClientDocuments from '../../../../lib/modules/clients/ClientDocuments.svelte';
	import type { Welcome } from '../../../../lib/modules/clients/types';

	type Contact = { id: number; kind: string; name: string; email: string };

	const org = $derived(page.params.org ?? '');
	const id = $derived(Number(page.params.id ?? ''));
	const base = $derived(`/api/v1/orgs/${org}`);
	const membership = $derived(session.currentOrg?.slug === org ? session.currentOrg : null);
	const canRead = $derived(membership?.perms.includes('invoices_read') ?? false);
	const canWrite = $derived(membership?.perms.includes('contacts_write') ?? false);
	const canProjects = $derived(
		session.modules.includes('timesheets') && (membership?.perms.includes('timesheets_read') ?? false)
	);

	let contact = $state<Contact | null>(null);
	let welcome = $state<Welcome>({ notes: '', updated_at: null });
	let projects = $state<{ id: number; name: string }[]>([]);
	let loaded = $state('');
	let error = $state('');
	let welcomeOpen = $state(false);
	let seq = 0;
	const key = $derived(`${org}/${id}`);

	async function load() {
		const s = ++seq;
		const k = key;
		try {
			const all = await api<Contact[]>('GET', `${base}/contacts`);
			if (s !== seq || k !== key) return;
			const c = all.find((x) => x.id === id) ?? null;
			if (c?.kind === 'client') {
				const [w, p] = await Promise.all([
					api<Welcome>('GET', `${base}/contacts/${id}/welcome`),
					canProjects
						? api<{ id: number; name: string }[]>('GET', `${base}/projects?client_id=${id}`)
						: Promise.resolve([])
				]);
				if (s !== seq || k !== key) return;
				welcome = w;
				projects = p;
			}
			contact = c;
			loaded = k;
		} catch (e) {
			if (s === seq && k === key) error = e instanceof ApiError ? e.message : 'Could not load this client.';
		}
	}

	$effect(() => {
		key;
		if (!canRead) return;
		untrack(() => { loaded = ''; error = ''; contact = null; load(); });
		return () => { seq++; };
	});
</script>

{#if !canRead}
	<p role="alert">You do not have permission to view contacts.</p>
{:else if error}
	<p class="alert alert-error alert-soft" role="alert">{error}</p>
{:else if loaded !== key}
	<p role="status">Loading client…</p>
{:else if !contact || contact.kind !== 'client'}
	<EmptyState title="Not a client" description="Only client contacts have a portal page with welcome notes, a checklist and documents.">
		{#snippet action()}<a class="link" href={`/${org}/contacts`}>Back to contacts</a>{/snippet}
	</EmptyState>
{:else}
	<PageHeader title={contact.name} back={{ href: `/${org}/contacts`, label: 'Back to contacts' }} />
	<div class="flex flex-col gap-6">
		<section class="card bg-base-100 shadow-sm" aria-labelledby="welcome-heading">
			<div class="card-body">
				<div class="flex items-center justify-between">
					<h2 id="welcome-heading" class="text-lg font-semibold">Welcome notes</h2>
					{#if canWrite}<Button variant="secondary" size="sm" onclick={() => (welcomeOpen = true)}>Edit notes</Button>{/if}
				</div>
				<p class="text-sm text-base-content/70">Communication rules shown at the top of the client's portal hub.</p>
				{#if welcome.notes}
					<p class="whitespace-pre-wrap">{welcome.notes}</p>
				{:else}
					<p class="text-base-content/70">No welcome notes yet.</p>
				{/if}
			</div>
		</section>
		<ChecklistPanel {org} contactId={contact.id} {canWrite} />
		<ClientDocuments {org} contactId={contact.id} {projects} {canWrite} />
	</div>
	{#if canWrite}
		<WelcomeDialog bind:open={welcomeOpen} {org} contactId={contact.id} {welcome} onsaved={(w) => (welcome = w)} />
	{/if}
{/if}
