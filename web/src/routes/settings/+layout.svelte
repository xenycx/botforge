<script lang="ts">
	import { page } from '$app/state';
	import Icon from '$lib/components/ui/Icon.svelte';
	let { children } = $props();
	const links = [
		{ href: '/settings/profile', label: 'Profile', icon: 'users' as const },
		{ href: '/settings/appearance', label: 'Appearance', icon: 'sliders' as const },
		{ href: '/settings/connected-accounts', label: 'Connected accounts', icon: 'link' as const },
		{ href: '/settings/security', label: 'Security', icon: 'shield' as const },
		{ href: '/settings/sftp', label: 'SFTP and API keys', icon: 'key' as const }
	];
</script>

<h1 class="text-page">Settings</h1>
<p class="mt-0.5 text-muted">Your account on this panel.</p>
<div class="mt-6 flex flex-col gap-6 md:flex-row md:gap-8">
	<nav class="min-w-0 max-w-full overflow-x-auto md:w-56 md:shrink-0 md:overflow-visible" aria-label="Settings sections">
		<ul class="flex w-max min-w-full gap-1 md:w-auto md:flex-col">
			{#each links as l (l.href)}
				<li class="shrink-0">
					<a
						href={l.href}
						class="flex items-center gap-2.5 rounded-control px-2 py-1.5 whitespace-nowrap {page.url.pathname === l.href ? 'bg-panel font-medium shadow-[inset_3px_0_0_var(--color-action)]' : 'text-ink/75 hover:bg-panel/70'}"
						aria-current={page.url.pathname === l.href ? 'page' : undefined}><Icon name={l.icon} class={page.url.pathname === l.href ? 'text-action' : 'text-muted'} />{l.label}</a
					>
				</li>
			{/each}
		</ul>
	</nav>
	<div class="min-w-0 flex-1 overflow-x-hidden">{@render children()}</div>
</div>
