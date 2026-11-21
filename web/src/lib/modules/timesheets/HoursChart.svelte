<script lang="ts">
	// One week's hours per day (Mon-Sun), stacked billed / unbilled /
	// fixed-fee, with previous/next week navigation. Plain CSS bars; the
	// sr-only summary carries the numbers. Not a <figure>: DaisyUI cards
	// style `.card figure` as a centred flex row, which collapses the chart.
	import { date, todayLocal } from '../../format';
	import { billingStatus, hhmm, periodRange, type BillingStatus, type Entry } from './types';

	let { entries, today = todayLocal() }: { entries: Entry[]; today?: string } = $props();

	const SERIES: { key: BillingStatus; label: string; cls: string }[] = [
		{ key: 'billed', label: 'Billed hours', cls: 'bg-primary' },
		{ key: 'unbilled', label: 'Unbilled hours', cls: 'bg-warning' },
		{ key: 'fixed', label: 'Fixed-fee hours', cls: 'bg-base-content/30' }
	];
	const headingId = `hours-chart-${Math.random().toString(36).slice(2)}`;

	const iso = (day: Date) =>
		`${day.getFullYear()}-${String(day.getMonth() + 1).padStart(2, '0')}-${String(day.getDate()).padStart(2, '0')}`;
	const addDays = (ymd: string, n: number) => {
		const [y, m, d] = ymd.split('-').map(Number);
		return iso(new Date(y, m - 1, d + n));
	};

	// Weeks back from the current week; 0 is the week containing `today`.
	let offset = $state(0);
	const range = $derived(periodRange('this_week', addDays(today, -7 * offset))!);

	const days = $derived(
		Array.from({ length: 7 }, (_, i) => {
			const dayIso = addDays(range.from, i);
			const [y, m, d] = dayIso.split('-').map(Number);
			const minutes: Record<BillingStatus, number> = { billed: 0, unbilled: 0, fixed: 0 };
			for (const e of entries) if (e.work_date === dayIso) minutes[billingStatus(e)] += e.minutes;
			return {
				iso: dayIso,
				label: new Date(y, m - 1, d).toLocaleDateString(undefined, { weekday: 'short', day: '2-digit' }),
				minutes,
				total: minutes.billed + minutes.unbilled + minutes.fixed
			};
		})
	);
	const weekTotal = $derived(days.reduce((sum, d) => sum + d.total, 0));
	// Round the scale up to whole hours (at least 1h) so bars have headroom.
	const maxHours = $derived(Math.max(1, Math.ceil(Math.max(...days.map((d) => d.total)) / 60)));
</script>

<div role="figure" aria-labelledby={headingId} class="w-full">
	<div class="mb-2 flex flex-wrap items-center justify-between gap-2">
		<h3 id={headingId} class="text-sm font-semibold">
			Project hours, {date(range.from)} – {date(range.to)}
			<span class="font-normal text-base-content/60 tabular-nums">({hhmm(weekTotal)})</span>
		</h3>
		<div class="join">
			<button type="button" class="btn btn-sm btn-outline join-item" onclick={() => offset++}>
				<span aria-hidden="true">‹</span><span class="sr-only">Previous week</span>
			</button>
			<button type="button" class="btn btn-sm btn-outline join-item" disabled={offset === 0} onclick={() => (offset = 0)}>
				<span aria-hidden="true">Today</span><span class="sr-only">Current week</span>
			</button>
			<button type="button" class="btn btn-sm btn-outline join-item" disabled={offset === 0} onclick={() => (offset = Math.max(0, offset - 1))}>
				<span aria-hidden="true">›</span><span class="sr-only">Next week</span>
			</button>
		</div>
	</div>
	<div class="flex h-40 gap-2">
		<div class="flex w-8 shrink-0 flex-col justify-between text-right text-xs text-base-content/60 tabular-nums" aria-hidden="true">
			<span>{maxHours}h</span><span>{maxHours / 2}h</span><span>0</span>
		</div>
		<div class="relative flex min-w-0 flex-1 gap-2">
			{#each days as day (day.iso)}
				<div class="flex min-w-0 flex-1 flex-col justify-end border-b border-base-300" title={`${day.label}: ${hhmm(day.total)}`} aria-hidden="true">
					{#each [...SERIES].reverse() as s (s.key)}
						{#if day.minutes[s.key] > 0}
							<div class={['w-full first:rounded-t', s.cls]} style:height="{(day.minutes[s.key] / 60 / maxHours) * 100}%"></div>
						{/if}
					{/each}
				</div>
			{/each}
			{#if weekTotal === 0}
				<p class="absolute inset-0 flex items-center justify-center text-sm text-base-content/60">No time logged this week.</p>
			{/if}
		</div>
	</div>
	<div class="ml-10 flex gap-2 text-center text-xs text-base-content/60" aria-hidden="true">
		{#each days as day (day.iso)}<span class="min-w-0 flex-1">{day.label}</span>{/each}
	</div>
	<div class="mt-2 flex flex-wrap justify-center gap-4 text-xs" aria-hidden="true">
		{#each SERIES as s (s.key)}
			<span class="flex items-center gap-1"><span class={['inline-block h-2 w-4 rounded', s.cls]}></span>{s.label}</span>
		{/each}
	</div>
	<ul class="sr-only">
		{#each days as day (day.iso)}
			<li>{day.label}: {hhmm(day.minutes.billed)} billed, {hhmm(day.minutes.unbilled)} unbilled, {hhmm(day.minutes.fixed)} fixed fee</li>
		{/each}
	</ul>
</div>
