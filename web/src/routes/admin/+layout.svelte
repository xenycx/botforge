<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { session } from '$lib/session.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	let { children } = $props();
	const links = [
		{ href: '/admin/users', label: 'Users', icon: 'users' as const },
		{ href: '/admin/host', label: 'Host', icon: 'chart' as const },
		{ href: '/admin/settings', label: 'Panel settings', icon: 'gear' as const },
		{ href: '/admin/diagnostics', label: 'Diagnostics', icon: 'shield' as const }
	];
	$effect(() => {
		if (session.user && session.user.role !== 'admin') goto('/dashboard');
	});
</script>

{#if session.user?.role === 'admin'}
	<p class="eyebrow">Administration</p>
	<h1 class="mt-1 text-page">This installation<span class="text-action">.</span></h1>
	<p class="mt-0.5 text-muted">Accounts, sign-in providers, the host and its health.</p>
	<div class="mt-6 flex flex-col gap-6 md:flex-row md:gap-8">
		<nav class="min-w-0 max-w-full overflow-x-auto md:w-56 md:shrink-0 md:overflow-visible" aria-label="Administration sections">
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
{/if}
