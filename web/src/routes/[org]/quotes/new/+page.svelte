<script lang="ts">
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '../../../../lib/api';
	import { session } from '../../../../lib/session.svelte';
	import { useUnsavedChanges } from '../../../../lib/forms/unsaved.svelte';
	import PageHeader from '../../../../lib/components/PageHeader.svelte';
	import LoadState from '../../../../lib/components/LoadState.svelte';
	import QuoteForm, { type ClientOption, type ProjectOption } from '../../../../lib/modules/quotes/QuoteForm.svelte';
	import { loadQuoteOptions } from '../../../../lib/modules/quotes/options';
	import type { QuoteRequest } from '../../../../lib/modules/quotes/types';

	const org = $derived(page.params.org ?? '');
	const member = $derived(session.currentOrg?.slug === org ? session.currentOrg : null);
	const allowed = $derived(!!member?.perms.includes('quotes_write'));

	let clients = $state<ClientOption[]>([]);
	let projects = $state<ProjectOption[]>([]);
	let defaultTerms = $state('');
	let gstRegistered = $state(true);
	let loaded = $state('');
	let error = $state('');
	let saveError = $state('');
	let saving = $state(false);
	let dirty = $state(false);
	let seq = 0;
	const { allowLeave } = useUnsavedChanges(() => dirty, () => saving);

	async function load() {
		const token = ++seq;
		const slug = org;
		error = '';
		try {
			const o = await loadQuoteOptions(slug, member?.perms ?? []);
			if (token !== seq || slug !== org) return;
			({ clients, projects, defaultTerms, gstRegistered } = o);
			loaded = slug;
		} catch (e) {
			if (token === seq) error = e instanceof ApiError ? e.message : 'Could not load quote options.';
		}
	}

	$effect(() => {
		org; allowed;
		untrack(() => { loaded = ''; dirty = false; saving = false; saveError = ''; if (allowed) load(); });
		return () => { seq++; };
	});

	async function save(body: QuoteRequest) {
		if (!allowed || saving) return;
		const token = seq;
		const slug = org;
		saving = true;
		saveError = '';
		try {
			const created = await api<{ id: number }>('POST', `/api/v1/orgs/${slug}/quotes`, body);
			if (token !== seq || slug !== org) return;
			dirty = false;
			await goto(`/${slug}/quotes/${created.id}`);
		} catch (e) {
			if (token === seq) saveError = e instanceof ApiError ? e.message : 'Could not save the quote.';
		} finally {
			if (token === seq) saving = false;
		}
	}
</script>

<PageHeader title="New quote" back={{ href: `/${org}/quotes`, label: 'Back to quotes' }} />
{#if !allowed}
	<p role="alert">You do not have permission to create quotes.</p>
{:else}
	<LoadState loading={loaded !== org} {error} loadingLabel="Loading quote options…" onretry={load}>
		{#if clients.length === 0}
			<p>Add a client contact before creating a quote.</p>
		{:else}
			{#key loaded}
				<QuoteForm {clients} {projects} {defaultTerms} {gstRegistered} busy={saving} error={saveError} onSubmit={save}
					onchange={() => (dirty = true)}
					oncancel={() => { if (allowLeave()) { dirty = false; goto(`/${org}/quotes`); } }} />
			{/key}
		{/if}
	</LoadState>
{/if}
