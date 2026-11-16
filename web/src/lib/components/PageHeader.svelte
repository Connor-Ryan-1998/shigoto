<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	let {
		title,
		back,
		heading,
		actions
	}: {
		title: string;
		back?: { href: string; label: string };
		heading?: Snippet;
		actions?: Snippet;
	} = $props();
</script>

<header class="mb-8 flex flex-wrap items-end justify-between gap-4">
	<div class="min-w-0">
		{#if back}<a class="mb-2 inline-flex items-center gap-1 text-sm text-base-content/70 no-underline hover:text-primary" href={back.href}><Icon name="back" size={16} />{back.label}</a>{/if}
		{#if heading}{@render heading()}{:else}<h1 class="m-0 text-3xl font-semibold tracking-tight">{title}</h1>{/if}
	</div>
	{#if actions}
		<div class="flex flex-wrap gap-2">{@render actions()}</div>
	{/if}
</header>
