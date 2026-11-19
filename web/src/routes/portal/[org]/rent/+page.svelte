<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '../../../../lib/api';
	import PageHeader from '../../../../lib/components/PageHeader.svelte';
	import EmptyState from '../../../../lib/components/EmptyState.svelte';
	import Button from '../../../../lib/components/Button.svelte';
	import Field from '../../../../lib/components/Field.svelte';
	import Select from '../../../../lib/components/Select.svelte';
	import Money from '../../../../lib/components/Money.svelte';
	import DateText from '../../../../lib/components/DateText.svelte';
	import { todayLocal } from '../../../../lib/format';
	import { currentFY, fyLabel } from '../../../../lib/modules/property/fy';
	import {
		fyRange,
		statementHref,
		type LeaseAccount,
		type PortalLease
	} from '../../../../lib/modules/property/portalRent';

	// Entries shown before "Show earlier"; the API returns the whole account.
	const PAGE_SIZE = 20;

	const org = $derived(page.params.org ?? '');
	const thisFY = currentFY(todayLocal());
	const periodOptions = [
		...[0, 1, 2].map((i) => ({ value: String(thisFY - i), label: fyLabel(thisFY - i) })),
		{ value: 'custom', label: 'Custom dates' }
	];

	let loadStatus = $state<'loading' | 'error' | 'ready'>('loading');
	let errorMessage = $state('');
	let leases = $state<PortalLease[]>([]);
	let leaseId = $state('');
	let account = $state<LeaseAccount | null>(null);
	let accountStatus = $state<'loading' | 'error' | 'ready'>('loading');
	let accountError = $state('');
	let shown = $state(PAGE_SIZE);

	let period = $state(String(thisFY));
	let customFrom = $state('');
	let customTo = $state('');

	const range = $derived.by(() => {
		if (period !== 'custom') return fyRange(Number(period));
		if (!customFrom || !customTo || customFrom > customTo) return null;
		return { from: customFrom, to: customTo };
	});
	const rangeError = $derived(
		period === 'custom' && customFrom && customTo && customFrom > customTo ? 'From must be on or before To.' : ''
	);

	// Only the latest request's response is applied (ui-standards: accept
	// only the latest response).
	let requestSeq = 0;

	async function loadLeases() {
		const seq = ++requestSeq;
		loadStatus = 'loading';
		errorMessage = '';
		try {
			const result = await api<PortalLease[]>('GET', `/api/v1/portal/${org}/leases`);
			if (seq !== requestSeq) return;
			// Current leases first, then most recent.
			leases = [...result].sort(
				(a, b) => Number(b.active) - Number(a.active) || b.start_date.localeCompare(a.start_date)
			);
			loadStatus = 'ready';
			if (leases.length) selectLease(String(leases[0].id));
		} catch (e) {
			if (seq !== requestSeq) return;
			errorMessage = e instanceof ApiError ? e.message : 'Something went wrong.';
			loadStatus = 'error';
		}
	}

	async function selectLease(id: string) {
		leaseId = id;
		const seq = ++requestSeq;
		accountStatus = 'loading';
		accountError = '';
		shown = PAGE_SIZE;
		try {
			const result = await api<LeaseAccount>('GET', `/api/v1/portal/${org}/leases/${id}/account`);
			if (seq !== requestSeq) return;
			account = result;
			accountStatus = 'ready';
		} catch (e) {
			if (seq !== requestSeq) return;
			accountError = e instanceof ApiError ? e.message : 'Something went wrong.';
			accountStatus = 'error';
		}
	}

	$effect(() => {
		page.params.org;
		loadLeases();
		return () => {
			requestSeq++;
		};
	});
</script>

<PageHeader title="Rent" />

{#if loadStatus === 'loading'}
	<p aria-live="polite">Loading rent…</p>
{:else if loadStatus === 'error'}
	<EmptyState title="Couldn't load your rent" description={errorMessage}>
		{#snippet action()}<Button onclick={loadLeases}>Retry</Button>{/snippet}
	</EmptyState>
{:else if leases.length === 0}
	<EmptyState title="No leases yet" />
{:else}
	{#if leases.length > 1}
		<div class="mb-4 max-w-md">
			<Field label="Lease" id="rent-lease">
				{#snippet children()}
					<Select
						id="rent-lease"
						value={leaseId}
						options={leases.map((l) => ({
							value: String(l.id),
							label: `${l.property_address || l.property_name}${l.active ? '' : ' (past)'}`
						}))}
						onchange={selectLease}
					/>
				{/snippet}
			</Field>
		</div>
	{/if}

	{#if accountStatus === 'loading'}
		<p aria-live="polite">Loading account…</p>
	{:else if accountStatus === 'error'}
		<EmptyState title="Couldn't load this lease" description={accountError}>
			{#snippet action()}<Button onclick={() => selectLease(leaseId)}>Retry</Button>{/snippet}
		</EmptyState>
	{:else if account}
		{@const a = account}
		<div class="grid gap-4 md:grid-cols-2">
			<section class="card bg-base-100 shadow-sm" aria-labelledby="rent-summary">
				<div class="card-body gap-3">
					<h2 id="rent-summary" class="text-base-content/70">{a.lease.property_address}</h2>
					<dl class="grid grid-cols-2 gap-3">
						<div>
							<dt class="text-sm text-base-content/70">Paid to</dt>
							<dd class="font-semibold">{#if a.summary.paid_to}<DateText iso={a.summary.paid_to} />{:else}—{/if}</dd>
						</div>
						<div>
							<dt class="text-sm text-base-content/70">Owing now</dt>
							<dd class={['font-semibold', a.summary.owing_now > 0 && 'text-error']}><Money cents={a.summary.owing_now} /></dd>
						</div>
					</dl>
					{#if a.summary.next_due}
						<p class="text-sm">
							Next due <DateText iso={a.summary.next_due.date} />: <Money cents={a.summary.next_due.amount} />
							<span class="text-base-content/70">({a.lease.frequency})</span>
						</p>
					{/if}
					{#if a.summary.balance < 0}
						<p class="text-sm font-semibold text-success">You're <Money cents={-a.summary.balance} /> in credit</p>
					{/if}
				</div>
			</section>

			<section class="card bg-base-100 shadow-sm" aria-labelledby="rent-statement">
				<div class="card-body gap-3">
					<h2 id="rent-statement" class="font-semibold">Statement</h2>
					<Field label="Statement period" id="rent-period">
						{#snippet children()}
							<Select id="rent-period" bind:value={period} options={periodOptions} />
						{/snippet}
					</Field>
					{#if period === 'custom'}
						<div class="grid grid-cols-2 gap-2">
							<Field label="From" id="rent-from">
								{#snippet children({ describedBy })}
									<input id="rent-from" type="date" class="input" bind:value={customFrom} aria-describedby={describedBy} />
								{/snippet}
							</Field>
							<Field label="To" id="rent-to" error={rangeError}>
								{#snippet children({ describedBy, invalid })}
									<input id="rent-to" type="date" class="input" bind:value={customTo} aria-describedby={describedBy} aria-invalid={invalid} />
								{/snippet}
							</Field>
						</div>
					{/if}
					{#if range}
						<div class="grid grid-cols-2 gap-2">
							<a class="btn" href={statementHref(org, a.lease.id, 'pdf', range)} download>Download PDF</a>
							<a class="btn" href={statementHref(org, a.lease.id, 'csv', range)} download>Download CSV</a>
						</div>
					{/if}
				</div>
			</section>
		</div>

		<section class="card mt-4 bg-base-100 shadow-sm" aria-labelledby="rent-history">
			<div class="card-body">
				<h2 id="rent-history" class="font-semibold">History</h2>
				{#if a.entries.length === 0}
					<p class="text-base-content/70">No rent has been charged yet.</p>
				{:else}
					<div class="overflow-x-auto">
						<table class="table">
							<thead>
								<tr>
									<th scope="col">Date</th>
									<th scope="col">Details</th>
									<th scope="col" class="text-right">Amount</th>
									<th scope="col" class="text-right">Balance</th>
								</tr>
							</thead>
							<tbody>
								{#each a.entries.slice(0, shown) as e, i (i)}
									<tr>
										<td class="whitespace-nowrap"><DateText iso={e.date} /></td>
										<td>
											{e.description}
											{#if e.kind === 'payment' && e.payment_id}
												<a class="link ml-2 text-sm" href={`/portal/${org}/rent/${a.lease.id}/receipts/${e.payment_id}`}>Receipt<span class="sr-only"> for payment on {e.date}</span></a>
											{/if}
										</td>
										<td class={['text-right', e.credit > 0 && 'text-success']}>
											{#if e.credit > 0}−<Money cents={e.credit} />{:else}<Money cents={e.debit} />{/if}
										</td>
										<td class="text-right"><Money cents={e.balance} /></td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
					{#if shown < a.entries.length}
						<div class="mt-2 text-center">
							<Button variant="secondary" onclick={() => (shown += PAGE_SIZE)}>Show earlier</Button>
						</div>
					{/if}
				{/if}
			</div>
		</section>
	{/if}
{/if}
