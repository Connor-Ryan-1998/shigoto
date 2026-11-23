<script lang="ts">
	// All of a client's documents across projects plus general ones (MSA,
	// NDA), filterable by To sign, Signed and Shared.
	import { onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '../../../../lib/api';
	import PageHeader from '../../../../lib/components/PageHeader.svelte';
	import EmptyState from '../../../../lib/components/EmptyState.svelte';
	import Button from '../../../../lib/components/Button.svelte';
	import { isToSign, portalStatus, type PortalDocument } from '../../../../lib/modules/clients/portal';

	type Filter = 'all' | 'to_sign' | 'signed' | 'shared';
	const FILTERS: [Filter, string][] = [['all', 'All'], ['to_sign', 'To sign'], ['signed', 'Signed'], ['shared', 'Shared']];

	const org = $derived(page.params.org ?? '');
	const initial = page.url?.searchParams.get('filter');
	let filter = $state<Filter>(FILTERS.some(([f]) => f === initial) ? (initial as Filter) : 'all');
	let docs = $state<PortalDocument[] | null>(null);
	let error = $state('');
	let seq = 0;
	onDestroy(() => { seq++; });

	const shown = $derived(
		(docs ?? []).filter((d) => {
			switch (filter) {
				case 'to_sign':
					return isToSign(d);
				case 'signed':
					return d.status === 'signed';
				case 'shared':
					return d.kind === 'shared';
				default:
					return true;
			}
		})
	);

	async function load() {
		const s = ++seq;
		error = '';
		try {
			const d = await api<PortalDocument[]>('GET', `/api/v1/portal/${org}/documents`);
			if (s === seq) docs = d;
		} catch (e) {
			if (s === seq) error = e instanceof ApiError ? e.message : 'Something went wrong.';
		}
	}
	$effect(() => { if (org) load(); });
</script>

<PageHeader title="Documents" />
{#if error}
	<EmptyState title="Couldn't load your documents" description={error}>
		{#snippet action()}<Button onclick={load}>Retry</Button>{/snippet}
	</EmptyState>
{:else if !docs}
	<p aria-live="polite">Loading…</p>
{:else}
	<div role="tablist" class="tabs tabs-border mb-4">
		{#each FILTERS as [f, label] (f)}
			<button type="button" role="tab" class={['tab', filter === f && 'tab-active']} aria-selected={filter === f} onclick={() => (filter = f)}>{label}</button>
		{/each}
	</div>
	<div class="max-w-5xl overflow-x-auto rounded-box border border-base-300 bg-base-100">
		<table class="table">
			<thead><tr><th>Document</th><th>Project</th><th>Status</th><th><span class="sr-only">Actions</span></th></tr></thead>
			<tbody>
				{#each shown as d (d.id)}
					<tr class={d.status === 'withdrawn' || d.status === 'superseded' ? 'opacity-60' : ''}>
						<td class="font-medium">{d.title}</td>
						<td>{d.project_name || 'General'}</td>
						<td>{#if isToSign(d)}<span class="badge badge-warning badge-soft">✍ To sign</span>{:else}{portalStatus(d)}{/if}</td>
						<td class="text-right">
							<a class={['btn btn-sm', isToSign(d) ? 'btn-primary' : 'btn-ghost']} href={`/portal/${org}/documents/${d.id}`} aria-label={`${isToSign(d) ? 'Review and sign' : 'View'} ${d.title}`}>
								{isToSign(d) ? 'Sign' : 'View'}
							</a>
						</td>
					</tr>
				{:else}
					<tr><td colspan="4" class="text-base-content/70">No documents here.</td></tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
