<script lang="ts">
	import type { Component } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '../../lib/api';
	import { session } from '../../lib/session.svelte';
	import PageHeader from '../../lib/components/PageHeader.svelte';
	import EmptyState from '../../lib/components/EmptyState.svelte';
	import Button from '../../lib/components/Button.svelte';
	import DashboardCore, { type CoreDashboard } from '../../lib/modules/core/DashboardCore.svelte';
	import { property } from '../../lib/modules/property/manifest';
	import { timesheets } from '../../lib/modules/timesheets/manifest';
	import { reconciliation } from '../../lib/modules/reconciliation/manifest';
	import CashFlowCard from '../../lib/modules/reconciliation/CashFlowCard.svelte';

	// Dashboard response shape (GET /api/v1/orgs/{org}/dashboard): "modules"
	// only contains the sections the caller's role may see, keyed by module
	// name - each value's real shape is the matching DashboardCard's own
	// exported type, but this page only passes it through unopened.
	type DashboardResponse = {
		core: CoreDashboard;
		modules: Record<string, unknown>;
	};

	// Order matches the sidebar's module group order (registry.ts).
	const moduleManifests = [property, timesheets, reconciliation];

	type LoadedCard = { name: string; Component: Component; data: unknown };

	let status = $state<'loading' | 'error' | 'ready'>('loading');
	let errorMessage = $state('');
	let core = $state<CoreDashboard | null>(null);
	let cards = $state<LoadedCard[]>([]);
	let generation = 0;

	async function load(org: string) {
		const request = ++generation;
		status = 'loading';
		errorMessage = '';
		core = null;
		cards = [];
		try {
			const res = await api<DashboardResponse>('GET', `/api/v1/orgs/${org}/dashboard`);
			if (request !== generation) return;

			const loaded: LoadedCard[] = [];
			for (const manifest of moduleManifests) {
				const section = res.modules[manifest.name];
				// A section is absent when its module is disabled, or the
				// caller lacks the read permission for it - render nothing
				// rather than a card for data that isn't there.
				if (section === undefined || !manifest.dashboardCard) continue;
				const mod = await manifest.dashboardCard();
				if (request !== generation) return;
				loaded.push({ name: manifest.name, Component: mod.default, data: section });
			}
			cards = loaded;
			core = res.core;
			status = 'ready';
		} catch (e) {
			if (request !== generation) return;
			errorMessage = e instanceof ApiError ? e.message : 'Something went wrong.';
			status = 'error';
		}
	}

	$effect(() => {
		const org = page.params.org ?? '';
		const membership = session.currentOrg;
		const permissions = membership?.perms.join(',') ?? '';
		if (org && membership?.slug === org && permissions.split(',').includes('invoices_read')) {
			void load(org);
		} else {
			core = null;
			cards = [];
			status = 'loading';
		}
		return () => { generation++; };
	});

	// An org with zero invoices of any status (draft, open, paid, void,
	// uncollectible) has nothing to work with yet, so show the onboarding
	// empty state instead of zero receivables. invoice_count (unlike
	// open/overdue/draft_count) isn't scoped to one status, so a settled
	// org (every invoice paid or void) correctly stays on the normal tiles
	// view instead of being mistaken for a brand-new one.
	const isEmpty = $derived(core !== null && core.invoice_count === 0);
</script>

<PageHeader title="Dashboard" />

{#if status === 'loading'}
	<div role="status" aria-live="polite">
		<span class="sr-only">Loading dashboard…</span>
		<div class="grid grid-cols-[repeat(auto-fit,minmax(14rem,1fr))] gap-4" aria-hidden="true">
			<div class="skeleton h-24"></div>
			<div class="skeleton h-24"></div>
			<div class="skeleton h-24"></div>
		</div>
	</div>
{:else if status === 'error'}
	<EmptyState title="Couldn't load the dashboard" description={errorMessage}>
		{#snippet action()}
			<Button onclick={() => load(page.params.org ?? '')}>Retry</Button>
		{/snippet}
	</EmptyState>
{:else if core}
	{#if isEmpty}
		<EmptyState title="No invoices yet — create one" />
	{:else}
		<DashboardCore {core} org={page.params.org ?? ''} />
	{/if}
	{#if cards.some((card) => card.name === 'reconciliation')}
		<div class="mt-6"><CashFlowCard org={page.params.org ?? ''} /></div>
	{/if}
	<!-- Module cards show even before the first invoice: an open urgent
	     repair must never hide behind the onboarding state. -->
	{#if cards.length > 0}
		<div class="mt-6 grid grid-cols-[repeat(auto-fit,minmax(min(100%,20rem),1fr))] items-start gap-6">
			{#each cards as { name, Component, data } (name)}
				<Component {data} org={page.params.org ?? ''} />
			{/each}
		</div>
	{/if}
{/if}
