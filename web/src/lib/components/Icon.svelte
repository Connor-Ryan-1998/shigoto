<script lang="ts" module>
	// Outline icons as path data only (no icon library, no {@html}): 24x24
	// grid, drawn with the current text colour so they follow the theme.
	const paths = {
		dashboard: ['M4 4h6v6H4z', 'M14 4h6v6h-6z', 'M4 14h6v6H4z', 'M14 14h6v6h-6z'],
		contacts: ['M12 11a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7z', 'M5 20v-1a7 7 0 0 1 14 0v1'],
		invoices: ['M6 3h12v18l-3-2-3 2-3-2-3 2z', 'M9 8h6', 'M9 12h6', 'M9 16h3'],
		reports: ['M4 20h16', 'M7 16v-5', 'M12 16V5', 'M17 16V9'],
		settings: [
			'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
			'M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z'
		],
		members: [
			'M9 11a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7z',
			'M2.5 20v-1a6.5 6.5 0 0 1 13 0v1',
			'M16 4.5a3.5 3.5 0 0 1 0 6.5',
			'M18.5 14a6.5 6.5 0 0 1 3 5.5v.5'
		],
		properties: ['M3 10l9-7 9 7', 'M5 9v12h14V9', 'M10 21v-7h4v7'],
		expenses: ['M3 8a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z', 'M3 11h18', 'M7 15h3'],
		reconciliation: ['M4 7h13', 'M14 4l3 3-3 3', 'M20 17H7', 'M10 14l-3 3 3 3'],
		projects: ['M3 9a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z', 'M8 7V4h8v3'],
		time: ['M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z', 'M12 7v5l3 2'],
		menu: ['M4 6h16', 'M4 12h16', 'M4 18h16'],
		sun: [
			'M12 16a4 4 0 1 0 0-8 4 4 0 0 0 0 8z',
			'M12 2v2', 'M12 20v2', 'M4.9 4.9l1.4 1.4', 'M17.7 17.7l1.4 1.4',
			'M2 12h2', 'M20 12h2', 'M4.9 19.1l1.4-1.4', 'M17.7 6.3l1.4-1.4'
		],
		moon: ['M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z'],
		auto: ['M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z', 'M12 3v18', 'M12 3a9 9 0 0 0 0 18z'],
		'sign-out': ['M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3', 'M10 8l-4 4 4 4', 'M6 12h10'],
		back: ['M15 18l-6-6 6-6'],
		inbox: ['M3 13l3-8h12l3 8', 'M3 13v5a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-5', 'M3 13h5l1 2h6l1-2h5']
	} satisfies Record<string, string[]>;

	export type IconName = keyof typeof paths;
</script>

<script lang="ts">
	let {
		name,
		size = 18,
		label,
		class: className
	}: { name: IconName; size?: number; label?: string; class?: string } = $props();
</script>

<svg
	xmlns="http://www.w3.org/2000/svg"
	viewBox="0 0 24 24"
	width={size}
	height={size}
	fill="none"
	stroke="currentColor"
	stroke-width="1.5"
	stroke-linecap="round"
	stroke-linejoin="round"
	class={['shrink-0', className]}
	aria-hidden={label ? undefined : 'true'}
	role={label ? 'img' : undefined}
	aria-label={label}
	focusable="false"
>
	{#each paths[name] as d (d)}<path {d} />{/each}
</svg>
