<script lang="ts">
	// PortalQuotes lists a client's quotes in the portal (optionally one
	// project's). Superseded, declined and expired quotes stay listed, greyed.
	import { api, ApiError } from '../../api';
	import { money } from '../../format';
	import StatusBadge from '../../components/StatusBadge.svelte';
	import { STATUS_LABELS, STATUS_TONES, type DisplayStatus } from './types';

	type Item = { id: number; number_label: string; title: string; project_name: string; display_status: DisplayStatus; valid_until: string; total: number };

	let { org, projectId }: { org: string; projectId?: number } = $props();

	const url = $derived(`/api/v1/portal/${org}/quotes${projectId != null ? `?project_id=${projectId}` : ''}`);
	let quotes = $state<Item[] | null>(null);
	let error = $state('');
	let seq = 0;

	async function load() {
		const token = ++seq;
		const u = url;
		error = '';
		try {
			const res = await api<{ quotes: Item[] }>('GET', u);
			if (token === seq && u === url) quotes = res.quotes;
		} catch (e) {
			if (token === seq) error = e instanceof ApiError ? e.message : 'Could not load quotes.';
		}
	}

	$effect(() => {
		url;
		quotes = null;
		load();
		return () => { seq++; };
	});

	const inactive = (q: Item) => q.display_status === 'superseded' || q.display_status === 'declined' || q.display_status === 'expired';
</script>

{#if error}
	<p role="alert" class="alert alert-error alert-soft">{error}</p>
{:else if quotes === null}
	<p aria-live="polite">Loading quotes…</p>
{:else if quotes.length === 0}
	<p class="text-base-content/70">No quotes yet.</p>
{:else}
	<ul class="menu w-full rounded-box bg-base-100 shadow-sm">
		{#each quotes as q (q.id)}
			<li class={inactive(q) ? 'opacity-60' : ''}>
				<a href={`/portal/${org}/quotes/${q.id}`} class="flex flex-wrap items-center gap-2">
					<span class="font-medium">{q.number_label}</span>
					<span>{q.title}</span>
					{#if q.project_name}<span class="text-sm text-base-content/70">{q.project_name}</span>{/if}
					<span class="ml-auto flex items-center gap-2">
						<span class="tabular-nums">{money(q.total)}</span>
						<StatusBadge status={STATUS_LABELS[q.display_status]} tone={STATUS_TONES[q.display_status]} />
					</span>
				</a>
			</li>
		{/each}
	</ul>
{/if}
