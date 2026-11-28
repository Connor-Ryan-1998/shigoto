<script lang="ts">
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '../../../../../lib/api';
	import { session } from '../../../../../lib/session.svelte';
	import { useUnsavedChanges } from '../../../../../lib/forms/unsaved.svelte';
	import PageHeader from '../../../../../lib/components/PageHeader.svelte';
	import LoadState from '../../../../../lib/components/LoadState.svelte';
	import QuoteForm, { type ClientOption, type ProjectOption } from '../../../../../lib/modules/quotes/QuoteForm.svelte';
	import { loadQuoteOptions } from '../../../../../lib/modules/quotes/options';
	import type { QuoteDetail, QuoteRequest } from '../../../../../lib/modules/quotes/types';

	const org = $derived(page.params.org ?? '');
	const id = $derived(page.params.id ?? '');
	const scope = $derived(`${org}:${id}`);
	const member = $derived(session.currentOrg?.slug === org ? session.currentOrg : null);
	const allowed = $derived(!!member?.perms.includes('quotes_write'));

	let quote = $state<QuoteDetail | null>(null);
	let clients = $state<ClientOption[]>([]);
	let projects = $state<ProjectOption[]>([]);
	let gstRegistered = $state(true);
	let loaded = $state('');
	let error = $state('');
	let saveError = $state('');
	let saving = $state(false);
	let dirty = $state(false);
	let seq = 0;
	const { allowLeave } = useUnsavedChanges(() => dirty, () => saving);
	const detailHref = $derived(`/${org}/quotes/${id}`);

	async function load() {
		const token = ++seq;
		const s = scope;
		error = '';
		try {
			const [q, o] = await Promise.all([
				api<QuoteDetail>('GET', `/api/v1/orgs/${org}/quotes/${id}`),
				loadQuoteOptions(org, member?.perms ?? [])
			]);
			if (token !== seq || s !== scope) return;
			quote = q;
			({ clients, projects, gstRegistered } = o);
			// The quote's own customer stays selectable even without contact access.
			if (!clients.some((c) => c.id === q.contact_id)) clients = [...clients, { id: q.contact_id, name: q.contact_name }];
			loaded = s;
		} catch (e) {
			if (token === seq) error = e instanceof ApiError ? e.message : 'Could not load the quote.';
		}
	}

	$effect(() => {
		scope; allowed;
		untrack(() => { loaded = ''; quote = null; dirty = false; saving = false; saveError = ''; if (allowed) load(); });
		return () => { seq++; };
	});

	async function save(body: QuoteRequest) {
		if (!allowed || saving) return;
		const token = seq;
		const s = scope;
		saving = true;
		saveError = '';
		try {
			await api('PUT', `/api/v1/orgs/${org}/quotes/${id}`, body);
			if (token !== seq || s !== scope) return;
			dirty = false;
			// Not busy any more: the unsaved-changes guard blocks leaving
			// while a save is in flight.
			saving = false;
			await goto(detailHref);
		} catch (e) {
			if (token === seq) saveError = e instanceof ApiError ? e.message : 'Could not save the quote.';
		} finally {
			if (token === seq) saving = false;
		}
	}
</script>

<PageHeader title={quote ? `Edit ${quote.number_label}` : 'Edit quote'} back={{ href: detailHref, label: 'Back to quote' }} />
{#if !allowed}
	<p role="alert">You do not have permission to edit quotes.</p>
{:else}
	<LoadState loading={loaded !== scope} {error} loadingLabel="Loading quote…" onretry={load}>
		{#if quote && quote.status !== 'draft'}
			<p role="alert">Only drafts can be edited. <a class="link" href={detailHref}>Back to the quote</a> to revise it instead.</p>
		{:else if quote}
			{#key loaded}
				<QuoteForm {clients} {projects} {gstRegistered} initial={quote} busy={saving} error={saveError} onSubmit={save}
					onchange={() => (dirty = true)}
					oncancel={() => { if (allowLeave()) { dirty = false; goto(detailHref); } }} />
			{/key}
		{/if}
	</LoadState>
{/if}
