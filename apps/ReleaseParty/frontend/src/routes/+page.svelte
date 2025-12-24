<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchInstallURL } from '$lib/api';

  let installURL = '';
  let error = '';

  onMount(async () => {
    try {
      installURL = await fetchInstallURL();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  });
</script>

<main style="max-width: 720px; margin: 40px auto; font-family: system-ui, -apple-system, Segoe UI, Roboto, sans-serif;">
  <h1>ReleaseParty</h1>
  <p>Automated release blog posts via the ReleaseParty Acolyte GitHub App.</p>

  {#if error}
    <div style="padding: 12px; border: 1px solid #fca5a5; background: #fee2e2;">
      <strong>Backend not reachable</strong>
      <div>{error}</div>
    </div>
  {:else if installURL}
    <a
      href={installURL}
      target="_blank"
      rel="noreferrer"
      style="display: inline-block; padding: 10px 14px; border-radius: 8px; background: #111827; color: white; text-decoration: none;"
    >
      Install GitHub App
    </a>
    <p style="margin-top: 12px; color: #6b7280;">
      After installation, configure destination repo/path and ReleaseParty will open a PR for each release.
    </p>
  {:else}
    <p>Loading…</p>
  {/if}
</main>

