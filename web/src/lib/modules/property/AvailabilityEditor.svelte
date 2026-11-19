<script lang="ts">
	import Field from '../../components/Field.svelte';
	import Button from '../../components/Button.svelte';
	import { windowLabel, type Window } from './maintenance';

	const MAX = 10;

	// Tenant availability windows: a day plus morning, afternoon or a custom
	// range. Reports the whole list through onchange.
	let { windows, today, onchange, disabled = false }: { windows: Window[]; today: string; onchange: (w: Window[]) => void; disabled?: boolean } = $props();

	let list = $state<Window[]>([]);
	$effect.pre(() => { list = [...windows]; });
	let day = $state('');
	let slot = $state<Window['slot']>('morning');
	let start = $state('');
	let end = $state('');
	let error = $state('');

	function add() {
		error = '';
		if (!day || day < today) { error = 'Choose today or a later day.'; return; }
		if (slot === 'custom' && (!start || !end || start >= end)) { error = 'Choose a start time before the end time.'; return; }
		list = [...list, { date: day, slot, start: slot === 'custom' ? start : null, end: slot === 'custom' ? end : null }];
		onchange(list);
		day = ''; start = ''; end = '';
	}

	function remove(i: number) {
		list = list.filter((_, j) => j !== i);
		onchange(list);
	}
</script>

<div class="flex flex-col gap-2">
	{#if list.length}
		<ul class="flex flex-col gap-1">
			{#each list as w, i (i)}
				<li class="flex items-center justify-between gap-2 rounded-box bg-base-200 px-3 py-1 text-sm">
					<span>{windowLabel(w)}</span>
					<button type="button" class="btn btn-ghost btn-xs" {disabled} onclick={() => remove(i)} aria-label={`Remove ${windowLabel(w)}`}>Remove</button>
				</li>
			{/each}
		</ul>
	{/if}
	<div class="grid grid-cols-2 gap-2">
		<Field label="Day" id="avail-day"><input class="input w-full" id="avail-day" type="date" min={today} bind:value={day} {disabled} /></Field>
		<Field label="Time of day" id="avail-slot">
			<select class="select w-full" id="avail-slot" bind:value={slot} {disabled}>
				<option value="morning">Morning (8am–12pm)</option>
				<option value="afternoon">Afternoon (12–5pm)</option>
				<option value="custom">Specific times</option>
			</select>
		</Field>
		{#if slot === 'custom'}
			<Field label="From" id="avail-start"><input class="input w-full" id="avail-start" type="time" bind:value={start} {disabled} /></Field>
			<Field label="Until" id="avail-end"><input class="input w-full" id="avail-end" type="time" bind:value={end} {disabled} /></Field>
		{/if}
	</div>
	{#if error}<p class="text-sm text-error" role="alert">{error}</p>{/if}
	<div><Button size="sm" variant="secondary" disabled={disabled || list.length >= MAX} onclick={add}>Add time</Button></div>
</div>
