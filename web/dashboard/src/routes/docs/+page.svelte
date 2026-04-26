<script lang="ts">
  import DocsLibrary from '../../lib/docs/DocsLibrary.svelte';
  import type { PageData } from './$types';

  export let data: PageData;
</script>

<DocsLibrary docs={data.docs} selectedDoc={data.selectedDoc} selectedSlug={data.selectedSlug} />
