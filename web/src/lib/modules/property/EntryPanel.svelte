<script lang="ts">
	import StatusBadge from '../../components/StatusBadge.svelte';
	import { ENTRY_LABEL, ENTRY_TONE, visitWindow, type Visit } from './maintenance';

	// What staff (and the tradesperson they brief) may rely on for getting
	// in. A ticket's preference is never permission: it shows only until the
	// tenant confirms the actual visit.
	let { visit, preference, accessNotes }: { visit: Visit | null; preference: string | null; accessNotes: string } = $props();

	const PREFERENCE: Record<string, string> = { permitted: 'OK to enter if out', tenant_home: "I'll be home" };
	const entry = $derived(visit?.entry_state ?? 'unconfirmed');
</script>

<div class="flex flex-col gap-2" data-entry={entry}>
	{#if visit}
		<div><StatusBadge status={ENTRY_LABEL[entry]} tone={ENTRY_TONE[entry]} /></div>
		{#if entry === 'permitted' && visit.entry_granted_at}
			<p class="text-sm">Granted {new Date(visit.entry_granted_at).toLocaleString()} for {visitWindow(visit.starts_at, visit.ends_at)}</p>
		{/if}
	{/if}
	{#if preference && entry === 'unconfirmed'}
		<p class="text-sm text-base-content/70">Tenant preference: {PREFERENCE[preference] ?? preference}</p>
	{/if}
	{#if accessNotes}
		<p class={['whitespace-pre-line', entry === 'permitted' ? 'rounded-box bg-success/10 p-2 font-semibold' : 'text-sm']}>{accessNotes}</p>
	{/if}
</div>
