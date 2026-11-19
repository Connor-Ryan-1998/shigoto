<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '../../../../../../../lib/api';
	import PageHeader from '../../../../../../../lib/components/PageHeader.svelte';
	import EmptyState from '../../../../../../../lib/components/EmptyState.svelte';
	import Button from '../../../../../../../lib/components/Button.svelte';
	import Money from '../../../../../../../lib/components/Money.svelte';
	import DateText from '../../../../../../../lib/components/DateText.svelte';
	import { METHOD_LABEL, type Receipt } from '../../../../../../../lib/modules/property/portalRent';

	const back = $derived({ href: `/portal/${page.params.org}/rent`, label: 'Back to rent' });

	let loadStatus = $state<'loading' | 'error' | 'ready'>('loading');
	let errorMessage = $state('');
	let receipt = $state<Receipt | null>(null);
	let requestSeq = 0;

	async function load() {
		const seq = ++requestSeq;
		loadStatus = 'loading';
		try {
			const { org, leaseId, paymentId } = page.params;
			const result = await api<Receipt>('GET', `/api/v1/portal/${org}/leases/${leaseId}/payments/${paymentId}/receipt`);
			if (seq !== requestSeq) return;
			receipt = result;
			loadStatus = 'ready';
		} catch (e) {
			if (seq !== requestSeq) return;
			errorMessage = e instanceof ApiError ? e.message : 'Something went wrong.';
			loadStatus = 'error';
		}
	}

	$effect(() => {
		page.params.paymentId;
		load();
		return () => {
			requestSeq++;
		};
	});
</script>

<div class="print:hidden">
	<PageHeader title="Payment receipt" {back}>
		{#snippet actions()}
			{#if receipt}<Button onclick={() => window.print()}>Print</Button>{/if}
		{/snippet}
	</PageHeader>
</div>

{#if loadStatus === 'loading'}
	<p aria-live="polite">Loading receipt…</p>
{:else if loadStatus === 'error'}
	<EmptyState title="Couldn't load the receipt" description={errorMessage}>
		{#snippet action()}<Button onclick={load}>Retry</Button>{/snippet}
	</EmptyState>
{:else if receipt}
	{@const r = receipt}
	<article class="card max-w-xl bg-base-100 shadow-sm print:max-w-none print:shadow-none">
		<div class="card-body gap-4">
			<header>
				<p class="text-xl font-bold">{r.org_name}</p>
				{#if r.org_abn}<p class="text-sm text-base-content/70">ABN {r.org_abn}</p>{/if}
				<h2 class="mt-2 text-lg font-semibold">Payment receipt</h2>
			</header>
			<dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2">
				<dt class="text-base-content/70">Received from</dt><dd>{r.tenant_names.join(', ')}</dd>
				<dt class="text-base-content/70">Property</dt><dd>{r.property_address}</dd>
				<dt class="text-base-content/70">Amount</dt><dd class="font-semibold"><Money cents={r.payment.amount} /></dd>
				<dt class="text-base-content/70">Paid on</dt><dd><DateText iso={r.payment.paid_on} /></dd>
				<dt class="text-base-content/70">Method</dt><dd>{METHOD_LABEL[r.payment.method] ?? r.payment.method}</dd>
				{#if r.payment.reference}<dt class="text-base-content/70">Reference</dt><dd>{r.payment.reference}</dd>{/if}
				<dt class="text-base-content/70">For</dt><dd>{r.invoice.period}</dd>
				{#if r.invoice.number !== null}<dt class="text-base-content/70">Invoice</dt><dd>#{r.invoice.number}</dd>{/if}
				<dt class="text-base-content/70">Balance after payment</dt><dd><Money cents={r.balance_after} /></dd>
			</dl>
			<p class="text-xs text-base-content/70">Receipt no. {r.payment.id} · Generated <DateText iso={r.generated_at.slice(0, 10)} /></p>
		</div>
	</article>
{/if}
