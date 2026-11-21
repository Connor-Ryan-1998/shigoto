<script lang="ts">
	import { BarChart } from 'layerchart';
	import { api, ApiError } from '../../api';
	import { session } from '../../session.svelte';
	import { date, money } from '../../format';
	import Money from '../../components/Money.svelte';
	import Button from '../../components/Button.svelte';

	type CashFlowResponse = {
		from: string;
		to: string;
		months: { month: string; incoming: number; outgoing: number }[];
	};

	let { org }: { org: string } = $props();
	let period = $state('current');
	let retry = $state(0);
	let status = $state<'loading' | 'ready' | 'error'>('loading');
	let error = $state('');
	let data = $state<CashFlowResponse | null>(null);
	const allowed = $derived(session.currentOrg?.slug === org && session.currentOrg.perms.includes('reports_read'));
	const incoming = $derived(data?.months.reduce((sum, m) => sum + m.incoming, 0) ?? 0);
	const outgoing = $derived(data?.months.reduce((sum, m) => sum + m.outgoing, 0) ?? 0);
	const monthFormat = new Intl.DateTimeFormat('en-AU', { month: 'short', timeZone: 'UTC' });
	const monthLabel = (month: string) => monthFormat.format(new Date(`${month}T00:00:00Z`));
	const headingId = $props.id();
	const series = [
		{ key: 'incoming', label: 'Incoming', color: 'var(--color-success)' },
		{ key: 'outgoing', label: 'Outgoing', color: 'var(--color-error)' }
	];

	$effect(() => {
		const slug = org;
		const selected = period;
		retry;
		data = null;
		error = '';
		status = 'loading';
		if (!allowed) return;
		let active = true;
		api<CashFlowResponse>('GET', `/api/v1/orgs/${slug}/cash-flow?period=${selected}`)
			.then((response) => {
				if (!active) return;
				data = response;
				status = 'ready';
			})
			.catch((e) => {
				if (!active) return;
				error = e instanceof ApiError ? e.message : 'Could not load cash flow.';
				status = 'error';
			});
		return () => { active = false; };
	});
</script>

{#if allowed}
	<section class="card min-w-0 overflow-hidden border border-base-300 bg-base-100 shadow-sm" aria-labelledby={headingId}>
		<header class="flex flex-wrap items-center justify-between gap-3 border-b border-base-300 bg-base-200/50 px-5 py-3">
			<h2 id={headingId} class="m-0 text-lg font-semibold">Cash Flow</h2>
			<label class="min-w-0">
				<span class="sr-only">Cash flow period</span>
				<select class="select select-ghost select-sm max-w-full" bind:value={period}>
					<option value="current">This fiscal year</option>
					<option value="previous">Previous fiscal year</option>
				</select>
			</label>
		</header>
		<div class="p-5">
			{#if status === 'loading'}
				<div role="status"><span class="sr-only">Loading cash flow…</span><div class="skeleton h-72" aria-hidden="true"></div></div>
			{:else if status === 'error'}
				<p class="text-sm" role="alert">{error}</p>
				<Button onclick={() => retry++}>Retry cash flow</Button>
			{:else if data}
				<p class="m-0 mb-4 text-sm text-base-content/70">{date(data.from)} – {date(data.to)}</p>
				<div class="grid min-w-0 gap-6 lg:grid-cols-[minmax(0,1fr)_12rem]">
					<div class="h-72 min-w-0" aria-label="Monthly incoming and outgoing cash flow">
						<BarChart
							data={data.months}
							x="month"
							{series}
							seriesLayout="group"
							yDomain={[0, incoming + outgoing === 0 ? 1 : null]}
							yNice
							height={288}
							padding={{ left: 64, right: 8, top: 8, bottom: 28 }}
							props={{
								xAxis: { format: monthLabel, tickSpacing: 40, tickOcclusion: true },
								yAxis: { format: money, ticks: 4 },
								tooltip: { item: { format: money }, header: { format: date }, hideTotal: true,
									root: { class: 'border border-base-300 bg-base-100 text-base-content shadow-sm' } }
							}}
						/>
					</div>
					<div role="group" aria-label="Cash flow totals" class="grid grid-cols-1 gap-5 sm:grid-cols-3 lg:grid-cols-1 lg:content-center lg:text-right">
						{#each [
							{ label: 'Incoming (+)', value: incoming, colour: 'bg-success' },
							{ label: 'Outgoing (−)', value: outgoing, colour: 'bg-error' },
							{ label: 'Net (=)', value: incoming - outgoing, colour: 'bg-primary' }
						] as total}
							<div>
								<p class="m-0 text-sm text-base-content/70"><span class={`mr-1 inline-block size-2 rounded-sm ${total.colour}`} aria-hidden="true"></span>{total.label}</p>
								<p class="m-0 mt-1 text-lg font-medium"><Money cents={total.value} /></p>
							</div>
						{/each}
					</div>
				</div>
				{#if incoming + outgoing === 0}
					<p class="mb-0 text-sm text-base-content/70">No imported bank activity for this period.</p>
				{/if}
				<table class="sr-only">
					<caption>Monthly cash flow</caption>
					<thead><tr><th scope="col">Month</th><th scope="col">Incoming</th><th scope="col">Outgoing</th><th scope="col">Net</th></tr></thead>
					<tbody>
						{#each data.months as month (month.month)}
							<tr><th scope="row">{date(month.month)}</th><td><Money cents={month.incoming} /></td><td><Money cents={month.outgoing} /></td><td><Money cents={month.incoming - month.outgoing} /></td></tr>
						{/each}
					</tbody>
				</table>
			{/if}
			<p class="mb-0 mt-4 text-xs text-base-content/70">Imported bank activity only; ignored transactions excluded. Not a verified bank balance.</p>
		</div>
	</section>
{/if}
