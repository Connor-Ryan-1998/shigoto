<script lang="ts">
	// ProjectCosts compares what a project's accepted quotes quoted with
	// what has been invoiced and what was actually spent (expense lines
	// tagged to the project). Everything is ex GST so the figures compare.
	import { api, ApiError } from '../../api';
	import { money } from '../../format';
	import LoadState from '../../components/LoadState.svelte';
	import DateText from '../../components/DateText.svelte';

	type Costs = {
		quoted_labour: number;
		quoted_materials: number;
		quoted_total: number;
		invoiced: number;
		expenses_total: number;
		expenses: { expense_id: number; spent_on: string; description: string; category: string; supplier: string; amount: number }[];
	};

	let { org, projectId }: { org: string; projectId: number } = $props();

	const scope = $derived(`${org}:${projectId}`);
	let costs = $state<Costs | null>(null);
	let loaded = $state('');
	let error = $state('');
	let seq = 0;

	async function load() {
		const token = ++seq;
		const s = scope;
		error = '';
		try {
			const result = await api<Costs>('GET', `/api/v1/orgs/${org}/projects/${projectId}/costs`);
			if (token !== seq || s !== scope) return;
			costs = result;
			loaded = s;
		} catch (e) {
			if (token === seq) error = e instanceof ApiError ? e.message : 'Could not load project costs.';
		}
	}

	$effect(() => {
		scope;
		costs = null;
		loaded = '';
		load();
		return () => { seq++; };
	});

	const remaining = $derived(costs ? costs.quoted_total - costs.expenses_total : 0);
</script>

<LoadState loading={loaded !== scope} {error} loadingLabel="Loading costs…" onretry={load}>
	{#if costs}
		<p class="mb-3 text-sm text-base-content/70">All amounts exclude GST.</p>
		<div class="stats stats-vertical mb-4 w-full bg-base-100 shadow-sm sm:stats-horizontal">
			<div class="stat">
				<div class="stat-title">Quoted</div>
				<div class="stat-value text-2xl tabular-nums">{money(costs.quoted_total)}</div>
				<div class="stat-desc">Labour {money(costs.quoted_labour)} · Materials {money(costs.quoted_materials)}</div>
			</div>
			<div class="stat">
				<div class="stat-title">Invoiced</div>
				<div class="stat-value text-2xl tabular-nums">{money(costs.invoiced)}</div>
			</div>
			<div class="stat">
				<div class="stat-title">Spent</div>
				<div class="stat-value text-2xl tabular-nums">{money(costs.expenses_total)}</div>
			</div>
			<div class="stat">
				<div class="stat-title">Remaining vs quote</div>
				{#if remaining < 0}
					<div class="stat-value text-2xl tabular-nums text-error">{money(-remaining)} over quote</div>
				{:else}
					<div class="stat-value text-2xl tabular-nums">{money(remaining)}</div>
				{/if}
			</div>
		</div>

		<section class="rounded-box bg-base-100 shadow-sm" aria-labelledby="project-costs-expenses">
			<h2 id="project-costs-expenses" class="m-0 p-4 text-lg font-semibold">Expenses</h2>
			{#if costs.expenses.length === 0}
				<p class="px-4 pb-4 text-base-content/70">No expenses tagged to this project yet.</p>
			{:else}
				<div class="overflow-x-auto">
					<table class="table">
						<thead><tr><th scope="col">Date</th><th scope="col">Description</th><th scope="col">Category</th><th scope="col">Supplier</th><th scope="col" class="text-right">Amount</th></tr></thead>
						<tbody>
							{#each costs.expenses as e, i (i)}
								<tr>
									<td><DateText iso={e.spent_on} /></td>
									<td><a class="link" href={`/${org}/expenses/${e.expense_id}`}>{e.description}</a></td>
									<td>{e.category}</td>
									<td>{e.supplier || '—'}</td>
									<td class="text-right tabular-nums">{money(e.amount)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>
	{/if}
</LoadState>
