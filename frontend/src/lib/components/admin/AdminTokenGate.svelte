<script lang="ts">
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import LockKeyholeIcon from '@lucide/svelte/icons/lock-keyhole';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { ApiError } from '#lib/api/client.js';
	import { unlockAdminMutation } from '#lib/api/queries.js';

	const qc = useQueryClient();
	const unlock = createMutation(() => unlockAdminMutation(qc));

	let token = $state('');
	let error = $state<string | null>(null);

	function submit(e: SubmitEvent) {
		e.preventDefault();
		const value = token.trim();
		if (value === '') {
			error = 'Enter the admin token.';
			return;
		}
		error = null;
		unlock.mutate(value, {
			onError: (err) => {
				error =
					err instanceof ApiError && err.status === 401
						? 'Wrong token.'
						: `Could not verify the token: ${err.message}`;
			}
		});
	}
</script>

<div class="grid place-items-center py-16">
	<Card.Root class="glow-edge w-full max-w-sm">
		<Card.Header>
			<div
				class="mb-2 grid size-10 place-items-center rounded-xl bg-white/[0.06] ring-1 ring-white/10"
			>
				<LockKeyholeIcon class="size-5 text-sky-300" />
			</div>
			<Card.Title>Superadmin access</Card.Title>
			<Card.Description>Enter the admin token configured on the server.</Card.Description>
		</Card.Header>
		<Card.Content>
			<form class="grid gap-3" novalidate onsubmit={submit}>
				<div class="grid gap-1.5">
					<Label for="admin-token">Admin token</Label>
					<Input
						id="admin-token"
						type="password"
						autocomplete="off"
						aria-invalid={error ? true : undefined}
						aria-describedby={error ? 'admin-token-error' : undefined}
						bind:value={token}
						oninput={() => (error = null)}
					/>
					{#if error}
						<p id="admin-token-error" class="text-sm text-destructive" role="alert">{error}</p>
					{/if}
				</div>
				<Button type="submit" class="btn-glow" disabled={unlock.isPending}>
					{unlock.isPending ? 'Checking…' : 'Unlock'}
				</Button>
			</form>
		</Card.Content>
	</Card.Root>
</div>
