<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchTasks, spawnDyad, type DyadTask } from '$lib/api';

  let tasks: DyadTask[] = [];
  let loading = true;
  let error = '';

  let name = '';
  let role = 'infra';
  let dept = 'infra';
  let spawnResult = '';

  async function load() {
    loading = true;
    error = '';
    try {
      tasks = await fetchTasks();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function handleSpawn() {
    spawnResult = '';
    try {
      const out = await spawnDyad(name.trim(), role.trim(), dept.trim());
      spawnResult = out;
      await load();
    } catch (e) {
      spawnResult = e instanceof Error ? e.message : String(e);
    }
  }

  onMount(load);
</script>

<style>
  body {
    margin: 0;
    font-family: system-ui, -apple-system, Segoe UI, sans-serif;
    background: #0b1021;
    color: #e5e7eb;
  }
  main {
    max-width: 1100px;
    margin: 32px auto;
    padding: 0 20px;
  }
  .card {
    background: #111827;
    border: 1px solid #1f2937;
    border-radius: 12px;
    padding: 16px;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25);
  }
  table {
    width: 100%;
    border-collapse: collapse;
  }
  th, td {
    padding: 8px 10px;
    border-bottom: 1px solid #1f2937;
    text-align: left;
    font-size: 14px;
  }
  th {
    color: #9ca3af;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-size: 12px;
  }
  .pill {
    padding: 4px 8px;
    border-radius: 999px;
    font-size: 12px;
    background: #1f2937;
  }
  .row {
    display: flex;
    gap: 16px;
    flex-wrap: wrap;
  }
  .col {
    flex: 1;
    min-width: 320px;
  }
  input, select, button, textarea {
    background: #0f172a;
    border: 1px solid #1f2937;
    color: #e5e7eb;
    padding: 8px 10px;
    border-radius: 8px;
    font-size: 14px;
    width: 100%;
  }
  button {
    cursor: pointer;
    background: linear-gradient(120deg, #6366f1, #22d3ee);
    border: none;
    color: #0b1021;
    font-weight: 700;
  }
  .status-blocked { color: #f87171; }
  .status-done { color: #34d399; }
  .status-in_progress { color: #fbbf24; }
  .status-review { color: #a78bfa; }
  .status-todo { color: #9ca3af; }
</style>

<main>
  <h1>Dyad Dashboard</h1>
  <p style="color:#9ca3af; max-width: 760px;">Monitor dyad tasks and spawn dyads. Data is proxied through the backend to Manager.</p>

  <div class="row" style="margin-top:16px;">
    <div class="col card">
      <h3>Spawn Dyad</h3>
      <div style="display:grid; gap:8px; margin-top:8px;">
        <label> Name <input bind:value={name} placeholder="infra" /></label>
        <label> Role <input bind:value={role} placeholder="infra" /></label>
        <label> Department <input bind:value={dept} placeholder="infra" /></label>
        <button on:click|preventDefault={handleSpawn}>Spawn</button>
        {#if spawnResult}<div style="color:#a78bfa; font-size:13px;">{spawnResult}</div>{/if}
      </div>
    </div>
  </div>

  <div class="card" style="margin-top:20px;">
    <div style="display:flex; justify-content:space-between; align-items:center;">
      <h3>Dyad Tasks</h3>
      <button on:click={load} style="width:auto; padding:6px 10px;">Refresh</button>
    </div>
    {#if loading}
      <p>Loading…</p>
    {:else if error}
      <p style="color:#f87171;">{error}</p>
    {:else}
      <table>
        <thead>
          <tr>
            <th>ID</th><th>Title</th><th>Kind</th><th>Status</th><th>Dyad</th><th>Priority</th><th>Owner</th><th>Updated</th>
          </tr>
        </thead>
        <tbody>
          {#if tasks.length === 0}
            <tr><td colspan="8">No tasks</td></tr>
          {:else}
            {#each tasks as t}
              <tr>
                <td>{t.id}</td>
                <td>{t.title}</td>
                <td>{t.kind}</td>
                <td class={"status-"+t.status}>{t.status}</td>
                <td>{t.dyad}</td>
                <td>{t.priority}</td>
                <td>{t.claimed_by || '-'}</td>
                <td>{t.updated_at || ''}</td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    {/if}
  </div>
</main>

