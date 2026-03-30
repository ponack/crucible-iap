<script lang="ts">
	function login() {
		window.location.href = '/auth/login';
	}
</script>

<div class="min-h-screen w-screen flex items-center justify-center bg-zinc-950">
	<div class="w-full max-w-sm space-y-8">
		<div class="text-center">
			<img src="/logo-dark.png" alt="Crucible IAP" class="mx-auto h-36 w-auto" />
		</div>

		<div class="bg-zinc-900 border border-zinc-800 rounded-xl p-8 space-y-4">
			<p class="text-sm text-zinc-300 text-center">
				Sign in with your identity provider to continue.
			</p>
			<button
				onclick={login}
				class="w-full bg-indigo-600 hover:bg-indigo-500 text-white text-sm font-medium py-2.5 px-4 rounded-lg transition-colors"
			>
				Sign in with SSO
			</button>
		</div>

		<p class="text-center text-xs text-zinc-600">
			Crucible IAP — self-hosted infrastructure automation
		</p>
	</div>
</div>
