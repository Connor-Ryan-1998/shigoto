<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '../../../../../lib/api';
	import { session } from '../../../../../lib/session.svelte';
	import Button from '../../../../../lib/components/Button.svelte';
	import LoadState from '../../../../../lib/components/LoadState.svelte';
	import StatusBadge from '../../../../../lib/components/StatusBadge.svelte';
	import DateText from '../../../../../lib/components/DateText.svelte';
	import NewTicketDialog from '../../../../../lib/modules/property/NewTicketDialog.svelte';
	import { STATUS_LABEL, STATUS_TONE, URGENCY_LABEL, URGENCY_TONE, type Ticket } from '../../../../../lib/modules/property/maintenance';

	const org = $derived(page.params.org ?? '');
	const id = $derived(page.params.id ?? '');
	const showAll = $derived(page.url?.searchParams.get('status') === 'all');
	const member = $derived(session.currentOrg?.slug === org ? session.currentOrg : null);
	const canWrite = $derived(member?.perms.includes('property_write') ?? false);
	const base = $derived(`/${org}/properties/${id}/maintenance`);

	let tickets = $state<Ticket[]>([]);
	let loading = $state(true);
	let error = $state('');
	let creating = $state(false);
	let seq = 0;

	async function load() {
		const token = ++seq;
		loading = true; error = '';
		try {
			const list = await api<Ticket[]>('GET', `/api/v1/orgs/${org}/properties/${id}/maintenance?status=${showAll ? 'all' : 'open'}`);
			if (token !== seq) return;
			tickets = list;
		} catch (e) {
			if (token === seq) error = e instanceof ApiError ? e.message : 'Could not load repairs.';
		} finally {
			if (token === seq) loading = false;
		}
	}
	$effect(() => { org; id; showAll; load(); return () => { seq++; }; });
</script>

<div class="mb-4 flex flex-wrap items-center gap-2">
	<div class="join" role="group" aria-label="Show">
		<a class={['btn join-item btn-sm', !showAll && 'btn-active']} href={base} aria-current={!showAll ? 'page' : undefined}>Open</a>
		<a class={['btn join-item btn-sm', showAll && 'btn-active']} href={`${base}?status=all`} aria-current={showAll ? 'page' : undefined}>All</a>
	</div>
	{#if canWrite}
		<div class="ml-auto"><Button onclick={() => (creating = true)}>New repair</Button></div>
	{/if}
</div>

<LoadState {loading} {error} loadingLabel="Loading repairs…" onretry={load} empty={tickets.length === 0} emptyTitle={showAll ? 'No repairs yet' : 'No open repairs'}>
	<div class="overflow-x-auto">
		<table class="table">
			<thead><tr><th>Ref</th><th>Issue</th><th>Urgency</th><th>Status</th><th>Raised</th></tr></thead>
			<tbody>
				{#each tickets as t (t.id)}
					<tr>
						<td class="whitespace-nowrap">{t.ref}</td>
						<td><a class="link link-primary" href={`${base}/${t.id}`}>{t.title}</a>{#if t.supplier}<div class="text-sm text-base-content/70">{t.supplier.name}</div>{/if}</td>
						<td><StatusBadge status={URGENCY_LABEL[t.urgency]} tone={URGENCY_TONE[t.urgency]} /></td>
						<td><StatusBadge status={STATUS_LABEL[t.status]} tone={STATUS_TONE[t.status]} /></td>
						<td class="whitespace-nowrap"><DateText iso={t.created_at.slice(0, 10)} /></td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
</LoadState>

{#if canWrite}
	<NewTicketDialog bind:open={creating} {org} propertyId={id} oncreated={(t) => goto(`${base}/${t.id}`)} />
{/if}
