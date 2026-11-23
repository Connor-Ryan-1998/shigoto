<script lang="ts" module>
	// Shape of the dashboard response's "core" section (GET /dashboard):
	// always present, regardless of which modules are enabled.
	export type CoreDashboard = {
		open_count: number;
		open_outstanding: number;
		overdue_count: number;
		overdue_outstanding: number;
		draft_count: number;
		// Every invoice in the org regardless of status - unlike the counts
		// above, which are each scoped to one status. Lets the dashboard page
		// tell "no invoices at all" apart from "fully settled" (open/overdue/
		// draft all 0, but some are paid/void).
		invoice_count: number;
	};
</script>

<script lang="ts">
	import Money from '../../components/Money.svelte';
	import { session } from '../../session.svelte';

	// org is the current org's slug: links must be absolute, since a relative
	// href resolves against /<org> to a path without the org in it.
	let { core, org }: { core: CoreDashboard; org: string } = $props();
	const current = $derived(core.open_outstanding - core.overdue_outstanding);
	const currentPercent = $derived(core.open_outstanding > 0 ? current / core.open_outstanding * 100 : 0);
	const overduePercent = $derived(core.open_outstanding > 0 ? core.overdue_outstanding / core.open_outstanding * 100 : 0);
	const canCreate = $derived(session.currentOrg?.slug === org && session.currentOrg.perms.includes('invoices_write'));
</script>

<section class="card min-w-0 overflow-hidden border border-base-300 bg-base-100 shadow-sm">
	<header class="flex flex-wrap items-center justify-between gap-2 border-b border-base-300 bg-base-200/50 px-5 py-3">
		<h2 class="m-0 text-lg font-semibold">Total Receivables</h2>
		{#if canCreate}
			<a class="btn btn-ghost btn-sm" href={`/${org}/invoices/new`} aria-label="New invoice"><span class="text-primary" aria-hidden="true">+</span> New</a>
		{/if}
	</header>
	<div class="space-y-4 p-5">
		<div>
			<p class="m-0 text-sm text-base-content/70">Total Unpaid Invoices</p>
			<p class="m-0 mt-1 text-2xl font-semibold"><Money cents={core.open_outstanding} /></p>
		</div>
		<div class="flex h-3 overflow-hidden rounded-sm bg-base-200" aria-hidden="true">
			<div class="bg-primary" style:width={`${currentPercent}%`}></div>
			<div class="bg-warning" style:width={`${overduePercent}%`}></div>
		</div>
		<div class="flex flex-wrap gap-x-6 gap-y-2 text-sm">
			<a class="link-hover inline-flex items-center gap-1.5" href={`/${org}/invoices?status=open`}>
				<span class="size-2 rounded-sm bg-primary" aria-hidden="true"></span>
				<span class="text-base-content/70">Current:</span> <Money cents={current} />
			</a>
			<a class="link-hover inline-flex items-center gap-1.5" href={`/${org}/invoices?status=overdue`}>
				<span class="size-2 rounded-sm bg-warning" aria-hidden="true"></span>
				<span class="text-base-content/70">Overdue:</span> <Money cents={core.overdue_outstanding} />
			</a>
		</div>
	</div>
</section>
