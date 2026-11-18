<script lang="ts">
  import { strings } from './strings';
</script>

<main>
  <h1>{strings.appName}</h1>
  <p>{strings.comingSoon}</p>
</main>

<style>
  main {
    min-height: 100dvh;
    display: grid;
    place-content: center;
    gap: 0.5rem;
    text-align: center;
  }

  h1 {
    margin: 0;
    font-size: 2rem;
    font-weight: 650;
    letter-spacing: -0.02em;
  }

  p {
    margin: 0;
    color: var(--text-muted);
  }
</style>
