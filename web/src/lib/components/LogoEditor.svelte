<script lang="ts">
	import { squarePng } from '$lib/image';
	import Icon from '$lib/components/ui/Icon.svelte';

	let {
		src,
		custom,
		fallbackLabel,
		disabled = false,
		busy = false,
		onUpload,
		onRemove,
		children
	}: {
		/** What is shown now (custom, Discord avatar, favicon...); "" = initials. */
		src: string;
		custom: boolean;
		fallbackLabel: string;
		disabled?: boolean;
		busy?: boolean;
		onUpload: (base64: string) => Promise<void>;
		onRemove: () => Promise<void>;
		children?: import('svelte').Snippet;
	} = $props();

	let error = $state('');
	let broken = $state(false);
	let input: HTMLInputElement | undefined = $state();
	$effect(() => {
		void src;
		broken = false;
	});

	async function choose(e: Event) {
		const file = (e.currentTarget as HTMLInputElement).files?.[0];
		(e.currentTarget as HTMLInputElement).value = '';
		if (!file) return;
		error = '';
		try {
			const img = await squarePng(file);
			await onUpload(img.base64);
		} catch (err) {
			error = err instanceof Error && err.message ? err.message : 'That image could not be read. Try a PNG, JPEG or WebP file.';
		}
	}
</script>

<div class="flex flex-wrap items-center gap-4">
	{#if src && !broken}
		<img {src} alt="" class="size-16 shrink-0 rounded-tile border border-rule-soft object-cover" referrerpolicy="no-referrer" onerror={() => (broken = true)} />
	{:else}
		<span class="grid size-16 shrink-0 place-items-center rounded-tile bg-paper-2 font-mono text-title font-semibold text-action" aria-hidden="true">{fallbackLabel}</span>
	{/if}
	<div class="flex flex-wrap gap-2">
		<input bind:this={input} type="file" accept="image/png,image/jpeg,image/webp,image/gif" class="sr-only" onchange={choose} {disabled} />
		<button type="button" class="btn btn-sm" disabled={disabled || busy} onclick={() => input?.click()}><Icon name="upload" size={14} />{custom ? 'Replace logo' : 'Upload logo'}</button>
		{#if custom}<button type="button" class="btn btn-sm btn-quiet" disabled={disabled || busy} onclick={onRemove}><Icon name="trash" size={14} />Remove custom logo</button>{/if}
		{@render children?.()}
	</div>
</div>
{#if error}<p class="mt-2 text-small text-fail" role="alert">{error}</p>{/if}
