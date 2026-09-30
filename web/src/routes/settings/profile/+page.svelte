<script lang="ts">
	import { api, ApiError } from '$lib/api/client';
	import { loadSession, session } from '$lib/session.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';

	let name = $state(session.user?.display_name ?? '');
	let avatar = $state<string | null>(null);
	let preview = $state(session.user?.avatar_url ?? '');
	let error = $state('');
	let saving = $state(false);

	async function choose(e: Event) {
		const file = (e.currentTarget as HTMLInputElement).files?.[0];
		if (!file) return;
		if (file.size > 8 * 1024 * 1024) { error = 'Choose an image smaller than 8 MB.'; return; }
		try {
			const bitmap = await createImageBitmap(file);
			const side = Math.min(bitmap.width, bitmap.height);
			const canvas = document.createElement('canvas');
			canvas.width = canvas.height = 128;
			canvas.getContext('2d')!.drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, 128, 128);
			bitmap.close();
			let quality = 0.86;
			let data = canvas.toDataURL('image/jpeg', quality);
			while (Math.ceil((data.length * 3) / 4) > 64 * 1024 && quality > 0.35) data = canvas.toDataURL('image/jpeg', (quality -= 0.1));
			avatar = data.split(',')[1]; preview = data; error = '';
		} catch { error = 'That image could not be read. Try a JPEG, PNG, or WebP file.'; }
	}

	async function save(e: SubmitEvent) {
		e.preventDefault(); saving = true; error = '';
		try {
			await api('PUT', '/me/profile', { display_name: name, ...(avatar !== null ? { avatar_jpeg: avatar } : {}) });
			await loadSession(); avatar = null; preview = session.user?.avatar_url ?? preview; toast('Profile saved', 'success');
		} catch (e) { error = e instanceof ApiError ? e.message : 'The profile could not be saved.'; }
		finally { saving = false; }
	}
	function remove() { avatar = ''; preview = ''; }
</script>

<svelte:head><title>Profile · BotForge</title></svelte:head>
<h2 class="text-section">Profile</h2>
<p class="mt-1 max-w-prose text-muted">Choose how your account appears around this panel. Your email remains the sign-in name.</p>
{#if error}<Notice tone="fail" class="mt-4">{error}</Notice>{/if}
<form class="mt-6 max-w-2xl border-y border-rule-soft bg-panel px-4 py-5 sm:px-6" onsubmit={save}>
	<div class="flex flex-col gap-5 sm:flex-row sm:items-center">
		{#if preview}<img src={preview} alt="Profile preview" class="size-24 rounded-full border border-rule object-cover" />{:else}<div class="grid size-24 place-items-center rounded-full bg-paper-2 text-section font-semibold text-muted">{(name || session.user?.email || '?').slice(0, 2).toUpperCase()}</div>{/if}
		<div>
			<label class="btn cursor-pointer">Choose picture<input class="sr-only" type="file" accept="image/jpeg,image/png,image/webp" onchange={choose} /></label>
			{#if preview}<button type="button" class="btn ml-2" onclick={remove}>Remove</button>{/if}
			<p class="mt-2 text-small text-muted">Cropped to a square and compressed to 128 × 128 pixels before upload.</p>
		</div>
	</div>
	<label class="mt-6 block max-w-md"><span class="label">Display name</span><input class="field" maxlength="64" bind:value={name} placeholder="How people should see you" /></label>
	<label class="mt-4 block max-w-md"><span class="label">Email</span><input class="field" disabled value={session.user?.email ?? ''} /></label>
	<div class="mt-5"><button class="btn btn-primary" disabled={saving}>{saving ? 'Saving…' : 'Save profile'}</button></div>
</form>
