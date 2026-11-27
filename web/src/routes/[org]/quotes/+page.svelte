<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '../../../lib/api';
	import { session } from '../../../lib/session.svelte';
	import PageHeader from '../../../lib/components/PageHeader.svelte';
	import Button from '../../../lib/components/Button.svelte';
	import EmptyState from '../../../lib/components/EmptyState.svelte';
	import Pagination from '../../../lib/components/Pagination.svelte';
	import DataTable from '../../../lib/components/DataTable.svelte';
	import Money from '../../../lib/components/Money.svelte';
	import DateText from '../../../lib/components/DateText.svelte';
	import StatusBadge from '../../../lib/components/StatusBadge.svelte';
	import { STATUS_LABELS, STATUS_TONES, type Quote } from '../../../lib/modules/quotes/types';

	const PAGE_SIZE = 50;
	// Expired isn't a stored status (it's a sent quote past its date), so
	// it shows as a badge under Sent rather than as its own tab.
	const TABS = [
		{ label: 'All', value: '' },
		{ label: 'Draft', value: 'draft' },
		{ label: 'Sent', value: 'sent' },
		{ label: 'Accepted', value: 'accepted' },
		{ label: 'Declined', value: 'declined' },
		{ label: 'Superseded', value: 'superseded' }
	];

	const org = $derived(page.params.org ?? '');
	const canWrite = $derived(session.currentOrg?.slug === org && (session.currentOrg?.perms.includes('quotes_write') ?? false));
	const activeTab = $derived(page.url.searchParams.get('status') ?? '');
	const offset = $derived(Math.max(0, Number(page.url.searchParams.get('offset') ?? '0') || 0));

	let loadStatus = $state<'loading' | 'error' | 'ready'>('loading');
	let errorMessage = $state('');
	let quotes = $state<Quote[]>([]);
	let total = $state(0);
	let requestSeq = 0;

	async function load() {
		const seq = ++requestSeq;
		loadStatus = 'loading';
		errorMessage = '';
		try {
			const params = new URLSearchParams();
			if (activeTab !== '') params.set('status', activeTab);
			params.set('limit', String(PAGE_SIZE));
			params.set('offset', String(offset));
			const res = await api<{ quotes: Quote[]; total: number }>('GET', `/api/v1/orgs/${org}/quotes?${params}`);
			if (seq !== requestSeq) return;
			quotes = res.quotes;
			total = res.total;
			loadStatus = 'ready';
		} catch (e) {
			if (seq !== requestSeq) return;
			errorMessage = e instanceof ApiError ? e.message : 'Something went wrong.';
			loadStatus = 'error';
		}
	}

	$effect(() => {
		page.url.search;
		org;
		load();
	});

	function tabHref(value: string): string {
		return value === '' ? `/${org}/quotes` : `/${org}/quotes?status=${value}`;
	}

	function gotoOffset(next: number) {
		const params = new URLSearchParams(page.url.search);
		if (next > 0) params.set('offset', String(next));
		else params.delete('offset');
		const qs = params.toString();
		goto(`/${org}/quotes${qs ? `?${qs}` : ''}`);
	}
</script>

{#snippet numberCell(row: Quote)}<a href={`/${org}/quotes/${row.id}`}>{row.number_label}</a>{/snippet}
{#snippet projectCell(row: Quote)}{row.project_name || '—'}{/snippet}
{#snippet statusCell(row: Quote)}<StatusBadge status={STATUS_LABELS[row.display_status]} tone={STATUS_TONES[row.display_status]} />{/snippet}
{#snippet totalCell(row: Quote)}<Money cents={row.total} />{/snippet}
{#snippet validCell(row: Quote)}<DateText iso={row.valid_until} />{/snippet}

<PageHeader title="Quotes">
	{#snippet actions()}
		{#if canWrite}<Button onclick={() => goto(`/${org}/quotes/new`)}>New quote</Button>{/if}
	{/snippet}
</PageHeader>

<nav class="tabs tabs-border mb-4" aria-label="Quote status">
	{#each TABS as tab (tab.value)}
		<a href={tabHref(tab.value)} class={['tab', activeTab === tab.value && 'tab-active']} aria-current={activeTab === tab.value ? 'page' : undefined}>{tab.label}</a>
	{/each}
</nav>

{#if loadStatus === 'loading'}
	<p aria-live="polite">Loading quotes…</p>
{:else if loadStatus === 'error'}
	<EmptyState title="Couldn't load quotes" description={errorMessage}>
		{#snippet action()}<Button onclick={load}>Retry</Button>{/snippet}
	</EmptyState>
{:else if quotes.length === 0 && offset > 0}
	<EmptyState title="No more quotes" description="You've gone past the last page.">
		{#snippet action()}<Button onclick={() => gotoOffset(0)}>Back to first page</Button>{/snippet}
	</EmptyState>
{:else if quotes.length === 0}
	<EmptyState title="No quotes in this view" />
{:else}
	<DataTable
		columns={[
			{ key: 'number_label', label: 'Number', cell: numberCell },
			{ key: 'title', label: 'Title' },
			{ key: 'contact_name', label: 'Customer' },
			{ key: 'project_name', label: 'Project', cell: projectCell },
			{ key: 'status', label: 'Status', cell: statusCell },
			{ key: 'total', label: 'Total', cell: totalCell, align: 'right' },
			{ key: 'valid_until', label: 'Valid until', cell: validCell }
		]}
		rows={quotes}
		rowKey={(row) => row.id}
	/>
	<Pagination {offset} pageSize={PAGE_SIZE} {total} count={quotes.length} onchange={gotoOffset} />
{/if}
