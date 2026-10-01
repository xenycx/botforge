<script lang="ts">
	import { goto } from '$app/navigation';
	import { session } from '$lib/session.svelte';
	import SubNav from '$lib/components/ui/SubNav.svelte';
	let { children } = $props();
	const links = [
		{ href: '/admin/users', label: 'Users', icon: 'users' as const, match: '/admin/users/' },
		{ href: '/admin/workspaces', label: 'Workspaces', icon: 'building' as const, match: '/admin/workspaces/' },
		{ href: '/admin/sites', label: 'Sites and domains', icon: 'globe' as const },
		{ href: '/admin/host', label: 'Host', icon: 'chart' as const },
		{ href: '/admin/settings', label: 'Panel settings', icon: 'gear' as const },
		{ href: '/admin/mail', label: 'Announcements', icon: 'send' as const },
		{ href: '/admin/environment', label: 'Environment', icon: 'sliders' as const },
		{ href: '/admin/diagnostics', label: 'Diagnostics', icon: 'shield' as const }
	];
	$effect(() => {
		if (session.user && session.user.role !== 'admin') goto('/dashboard');
	});
</script>

{#if session.user?.role === 'admin'}
	<header class="border-b border-rule-soft pb-5">
		<p class="eyebrow">Administration</p>
		<h1 class="mt-1 text-page">This installation<span class="text-action">.</span></h1>
		<p class="mt-0.5 text-muted">Accounts, workspaces, hosted sites, the host and its health.</p>
	</header>
	<div class="mt-6 flex flex-col gap-6 md:flex-row md:gap-10">
		<SubNav {links} label="Administration sections" />
		<div class="min-w-0 flex-1">{@render children()}</div>
	</div>
{/if}
