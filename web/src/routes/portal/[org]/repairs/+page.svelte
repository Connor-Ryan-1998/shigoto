<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '../../../../lib/api';
	import PageHeader from '../../../../lib/components/PageHeader.svelte';
	import LoadState from '../../../../lib/components/LoadState.svelte';
	import StatusBadge from '../../../../lib/components/StatusBadge.svelte';
	import DateText from '../../../../lib/components/DateText.svelte';
	import { STATUS_LABEL, STATUS_TONE, URGENCY_LABEL, type Ticket } from '../../../../lib/modules/property/maintenance';

	type List = { emergency_phone: string; leases: { id: number; property_address: string; active: boolean }[]; tickets: Ticket[] };

	const org = $derived(page.params.org ?? '');
	let data = $state<List | null>(null);
	let error = $state('');
	let seq = 0;

	async function load() {
		const token = ++seq;
		error = ''; data = null;
		try {
			const d = await api<List>('GET', `/api/v1/portal/${org}/maintenance`);
			if (token === seq) data = d;
		} catch (e) {
			if (token === seq) error = e instanceof ApiError ? e.message : 'Could not load repairs.';
		}
	}
	$effect(() => { org; load(); return () => { seq++; }; });
	const canReport = $derived(data?.leases.some((l) => l.active) ?? false);
</script>

<PageHeader title="Repairs">
	{#snippet actions()}
		{#if canReport}<a class="btn btn-primary" href={`/portal/${org}/repairs/new`}>Report a repair</a>{/if}
	{/snippet}
</PageHeader>

<LoadState loading={!data && !error} {error} loadingLabel="Loading repairs…" onretry={load}>
	{#if data}
		{#if !canReport}<p class="mb-4 text-base-content/70">You can report repairs for a current lease.</p>{/if}
		{#if data.tickets.length === 0}
			<p class="text-base-content/70">No repairs yet</p>
		{:else}
			<ul class="flex flex-col gap-3">
				{#each data.tickets as t (t.id)}
					<li class="card bg-base-100 shadow-sm">
						<div class="card-body gap-1 p-4">
							<div class="flex flex-wrap items-center justify-between gap-2">
								<a class="link link-primary font-semibold" href={`/portal/${org}/repairs/${t.id}`}>{t.title}</a>
								<StatusBadge status={STATUS_LABEL[t.status]} tone={STATUS_TONE[t.status]} />
							</div>
							<p class="text-sm text-base-content/70">{t.ref} · {URGENCY_LABEL[t.urgency]} · {t.property_address} · <DateText iso={t.created_at.slice(0, 10)} /></p>
						</div>
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
</LoadState>
