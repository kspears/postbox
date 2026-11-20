<script lang="ts">
  import mark from './postbox.svg?inline';
  import { strings } from './strings';
</script>

<span class="brand">
  <img src={mark} width="32" height="32" alt="" />
  {strings.appName}
</span>

<style>
  .brand {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 1.125rem;
    font-weight: 700;
    letter-spacing: -0.035em;
  }
</style>
