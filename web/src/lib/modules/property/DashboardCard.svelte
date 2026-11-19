<script lang="ts" module>
	import type { TicketStatus, Urgency } from './maintenance';
	// Shape of the dashboard response's "property" section
	// (internal/property/routes.go's propertyDashboard).
	export type PropertyDashboard = {
		arrears_total: number;
		unissued_total: number;
		bonds_unlodged_overdue: number;
		insurance_renewing_soon: number;
		repairs?: {
			open_count: number;
			items: { ticket_id: number; property_id: number; property_address: string; ref: string; title: string; urgency: Urgency; status: TicketStatus }[];
		};
	};
</script>

<script lang="ts">
	import { page } from '$app/state';
	import Money from '../../components/Money.svelte';
	import StatusBadge from '../../components/StatusBadge.svelte';
	import { STATUS_LABEL, URGENCY_LABEL, URGENCY_TONE } from './maintenance';

	let { data }: { data: PropertyDashboard } = $props();
</script>

<section class="card bg-base-100 p-5 shadow-sm">
	<h2 class="mb-3 text-lg font-bold">Property</h2>
	<dl class="m-0">
		<div class="flex justify-between gap-4 border-t border-base-300 py-2 first:border-t-0">
			<dt class="text-base-content/70">Arrears</dt>
			<dd class="m-0 font-semibold tabular-nums"><Money cents={data.arrears_total} /></dd>
		</div>
		<div class="flex justify-between gap-4 border-t border-base-300 py-2 first:border-t-0">
			<dt class="text-base-content/70">Unissued</dt>
			<dd class="m-0 font-semibold tabular-nums"><Money cents={data.unissued_total} /></dd>
		</div>
		<div class="flex justify-between gap-4 border-t border-base-300 py-2 first:border-t-0">
			<dt class="text-base-content/70">Bonds overdue to lodge</dt>
			<dd class="m-0 font-semibold tabular-nums">{data.bonds_unlodged_overdue}</dd>
		</div>
		<div class="flex justify-between gap-4 border-t border-base-300 py-2 first:border-t-0">
			<dt class="text-base-content/70">Insurance renewing within 30 days</dt>
			<dd class="m-0 font-semibold tabular-nums">{data.insurance_renewing_soon}</dd>
		</div>
	</dl>
</section>

{#if data.repairs && data.repairs.open_count > 0}
	<section class="card mt-4 bg-base-100 p-5 shadow-sm" aria-labelledby="dashboard-repairs">
		<div class="mb-3 flex items-baseline justify-between gap-2">
			<h2 id="dashboard-repairs" class="text-lg font-bold">Repairs</h2>
			<span class="text-base-content/70">{data.repairs.open_count} open</span>
		</div>
		<ul class="m-0 flex flex-col">
			{#each data.repairs.items as r (r.ticket_id)}
				<li class="flex flex-wrap items-center gap-2 border-t border-base-300 py-2 first:border-t-0">
					<StatusBadge status={URGENCY_LABEL[r.urgency]} tone={URGENCY_TONE[r.urgency]} />
					<a class="link link-primary" href={`/${page.params.org}/properties/${r.property_id}/maintenance/${r.ticket_id}`}>{r.ref} · {r.title}</a>
					<span class="text-sm text-base-content/70">{r.property_address} · {STATUS_LABEL[r.status]}</span>
				</li>
			{/each}
		</ul>
	</section>
{/if}
