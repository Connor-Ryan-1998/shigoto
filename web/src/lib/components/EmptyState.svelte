<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon, { type IconName } from './Icon.svelte';

	let {
		title,
		description,
		icon,
		action
	}: {
		title: string;
		description?: string;
		icon?: IconName;
		action?: Snippet;
	} = $props();
</script>

<div class="flex flex-col items-center gap-2 px-4 py-12 text-center text-base-content/70">
	{#if icon}<Icon name={icon} size={32} class="mb-1 text-base-content/50" />{/if}
	<p class="m-0 font-semibold text-base-content">{title}</p>
	{#if description}
		<p class="m-0">{description}</p>
	{/if}
	{#if action}
		<div>{@render action()}</div>
	{/if}
</div>
