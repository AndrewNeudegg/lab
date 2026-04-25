<script lang="ts">
  export let title = 'Homelab';
  export let subtitle = 'homelabd chat';
  export let apiBase = '';
</script>

<header>
  <div>
    <p>{subtitle}</p>
    <h1>{title}</h1>
  </div>

  {#if apiBase}
    <span>{apiBase}</span>
  {/if}
</header>

<style>
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 1.25rem 1.5rem;
    border-bottom: 1px solid #d8dee7;
    background: #ffffff;
  }

  div {
    display: grid;
    gap: 0.2rem;
  }

  p,
  h1 {
    margin: 0;
  }

  p {
    color: #6b7280;
    font-size: 0.82rem;
    font-weight: 700;
    letter-spacing: 0;
    text-transform: uppercase;
  }

  h1 {
    color: #111827;
    font-size: clamp(1.35rem, 4vw, 2rem);
    line-height: 1.1;
  }

  span {
    min-width: 0;
    overflow: hidden;
    padding: 0.45rem 0.65rem;
    border: 1px solid #cfd7e3;
    border-radius: 999px;
    color: #374151;
    background: #f7fafc;
    font-size: 0.78rem;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  @media (max-width: 640px) {
    header {
      align-items: flex-start;
      flex-direction: column;
      padding: 1rem;
    }

    span {
      max-width: 100%;
      border-radius: 0.5rem;
      white-space: normal;
      overflow-wrap: anywhere;
    }
  }
</style>
