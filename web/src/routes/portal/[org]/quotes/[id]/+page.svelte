<script lang="ts">
	// Client quote page: view only. Lines, totals and terms, and the PDF
	// exactly as it was sent. Attachments are staff records and never shown.
	import { onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '../../../../../lib/api';
	import { date, money } from '../../../../../lib/format';
	import PageHeader from '../../../../../lib/components/PageHeader.svelte';
	import StatusBadge from '../../../../../lib/components/StatusBadge.svelte';
	import EmptyState from '../../../../../lib/components/EmptyState.svelte';
	import Button from '../../../../../lib/components/Button.svelte';
	import { STATUS_LABELS, STATUS_TONES, type DisplayStatus, type QuoteLine } from '../../../../../lib/modules/quotes/types';

	type PortalQuoteLine = QuoteLine & { amount: number };
	type PortalQuote = {
		id: number; number_label: string; title: string; project_id: number | null; project_name: string;
		display_status: DisplayStatus; issue_date: string; valid_until: string;
		subtotal: number; gst_total: number; total: number; terms: string; lines: PortalQuoteLine[];
	};

	const org = $derived(page.params.org ?? '');
	const id = $derived(page.params.id ?? '');
	let quote = $state<PortalQuote | null>(null);
	let error = $state('');
	let seq = 0;
	onDestroy(() => { seq++; });

	async function load() {
		const s = ++seq;
		error = '';
		try {
			const q = await api<PortalQuote>('GET', `/api/v1/portal/${org}/quotes/${id}`);
			if (s === seq) quote = q;
		} catch (e) {
			if (s === seq) error = e instanceof ApiError ? (e.status === 404 ? 'Quote not found.' : e.message) : 'Something went wrong.';
		}
	}
	$effect(() => { if (org && id) load(); });

	const labour = $derived(quote?.lines.filter((l) => l.kind === 'labour') ?? []);
	const materials = $derived(quote?.lines.filter((l) => l.kind === 'material') ?? []);
</script>

{#snippet lineTable(title: string, qtyLabel: string, unitLabel: string, lines: PortalQuoteLine[])}
	{#if lines.length > 0}
		<section class="rounded-box bg-base-100 p-4 shadow-sm">
			<h2 class="mt-0 mb-3 text-lg font-semibold">{title}</h2>
			<div class="overflow-x-auto">
				<table class="table">
					<thead><tr><th scope="col">Description</th><th scope="col" class="text-right">{qtyLabel}</th><th scope="col" class="text-right">{unitLabel}</th><th scope="col" class="text-right">Amount</th></tr></thead>
					<tbody>
						{#each lines as l, i (i)}
							<tr><td>{l.description}</td><td class="text-right tabular-nums">{l.quantity_milli / 1000}</td><td class="text-right tabular-nums">{money(l.unit_amount)}</td><td class="text-right tabular-nums">{money(l.amount)}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}
{/snippet}

{#if error}
	<PageHeader title="Quote" back={{ href: `/portal/${org}/hub`, label: 'Back to hub' }} />
	<EmptyState title={error}>
		{#snippet action()}<Button onclick={load}>Retry</Button>{/snippet}
	</EmptyState>
{:else if !quote}
	<p aria-live="polite">Loading…</p>
{:else}
	<PageHeader title={`${quote.number_label} · ${quote.title}`} back={{ href: `/portal/${org}/hub`, label: 'Back to hub' }}>
		{#snippet actions()}
			<a class="btn btn-primary" href={`/api/v1/portal/${org}/quotes/${quote!.id}/pdf`}>Download PDF</a>
		{/snippet}
	</PageHeader>
	<div class="flex max-w-4xl flex-col gap-5">
		<div class="flex flex-wrap items-center gap-3">
			<StatusBadge status={STATUS_LABELS[quote.display_status]} tone={STATUS_TONES[quote.display_status]} />
			<span>Issued {date(quote.issue_date)}</span>
			<span>Valid until {date(quote.valid_until)}</span>
			{#if quote.project_id != null}<a class="link" href={`/portal/${org}/projects/${quote.project_id}`}>{quote.project_name}</a>{/if}
		</div>
		{@render lineTable('Labour', 'Hours', 'Rate', labour)}
		{@render lineTable('Materials', 'Qty', 'Unit price', materials)}
		<dl class="ml-auto grid w-full max-w-xs grid-cols-2 gap-1 tabular-nums">
			<dt>Subtotal</dt><dd class="text-right">{money(quote.subtotal)}</dd>
			{#if quote.gst_total !== 0}<dt>GST</dt><dd class="text-right">{money(quote.gst_total)}</dd>{/if}
			<dt class="font-semibold">Total</dt><dd class="text-right font-semibold">{money(quote.total)}</dd>
		</dl>
		{#if quote.terms.trim()}
			<section class="rounded-box bg-base-100 p-4 shadow-sm">
				<h2 class="mt-0 mb-2 text-lg font-semibold">Terms</h2>
				<p class="whitespace-pre-line">{quote.terms}</p>
			</section>
		{/if}
	</div>
{/if}
