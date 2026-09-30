<script lang="ts">
	import { accents, setAccent, setTheme, theme, type Accent, type ThemePref } from '$lib/ui/theme.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';

	const modes: { id: ThemePref; label: string; text: string; icon: 'sun' | 'moon' | 'monitor' }[] = [
		{ id: 'light', label: 'Light', text: 'Bright working surfaces', icon: 'sun' },
		{ id: 'dark', label: 'Dark', text: 'Low-light operations', icon: 'moon' },
		{ id: 'system', label: 'System', text: 'Match this device', icon: 'monitor' }
	];
</script>

<svelte:head><title>Appearance · BotForge</title></svelte:head>

<h2 class="text-section">Appearance</h2>
<p class="mt-1 max-w-prose text-muted">Choose how BotForge looks in this browser. Accent colors are balanced independently for light and dark surfaces.</p>

<section class="mt-6" aria-labelledby="mode-title">
	<h3 id="mode-title" class="font-semibold">Theme</h3>
	<div class="mt-3 grid gap-3 sm:grid-cols-3">
		{#each modes as mode (mode.id)}
			<button class="flex items-center gap-3 rounded-[10px] border p-4 text-left {theme.pref === mode.id ? 'border-action bg-action/8 shadow-[inset_0_0_0_1px_var(--color-action)]' : 'border-rule-soft bg-panel hover:border-rule'}" aria-pressed={theme.pref === mode.id} onclick={() => setTheme(mode.id)}>
				<span class="grid size-9 place-items-center rounded-control bg-paper-2 text-action"><Icon name={mode.icon} /></span>
				<span><span class="block font-semibold">{mode.label}</span><span class="block text-small text-muted">{mode.text}</span></span>
			</button>
		{/each}
	</div>
</section>

<section class="mt-8" aria-labelledby="accent-title">
	<h3 id="accent-title" class="font-semibold">Accent color</h3>
	<p class="mt-1 text-small text-muted">Used for selected navigation, links, focus rings, and primary actions.</p>
	<div class="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4">
		{#each accents as accent (accent.id)}
			<button class="flex items-center gap-3 rounded-[10px] border bg-panel p-3 text-left {theme.accent === accent.id ? 'border-action shadow-[inset_0_0_0_1px_var(--color-action)]' : 'border-rule-soft hover:border-rule'}" aria-pressed={theme.accent === accent.id} onclick={() => setAccent(accent.id as Accent)}>
				<span class="relative size-8 shrink-0 overflow-hidden rounded-full border border-black/10" aria-hidden="true">
					<span class="absolute inset-y-0 left-0 w-1/2" style="background:{accent.light}"></span>
					<span class="absolute inset-y-0 right-0 w-1/2" style="background:{accent.dark}"></span>
				</span>
				<span class="min-w-0 flex-1 font-medium">{accent.name}</span>
				{#if theme.accent === accent.id}<Icon name="check" class="text-action" />{/if}
			</button>
		{/each}
	</div>
</section>
