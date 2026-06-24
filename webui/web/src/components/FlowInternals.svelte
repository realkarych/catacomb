<script lang="ts">
  import { Background, Controls, MiniMap, BackgroundVariant, useSvelteFlow } from '@xyflow/svelte';
  import type { Node as XyFlowNode } from '@xyflow/svelte';
  import { tick } from 'svelte';

  interface Props {
    pendingFitView: boolean;
    onFitViewDone: () => void;
  }

  let { pendingFitView, onFitViewDone }: Props = $props();

  const { fitView } = useSvelteFlow();

  $effect(() => {
    if (pendingFitView) {
      tick().then(() => {
        fitView({ duration: 300 });
        onFitViewDone();
      });
    }
  });
</script>

<Background variant={BackgroundVariant.Dots} gap={20} size={1} patternColor="var(--border)" />
<Controls />
<MiniMap
  nodeColor={(node: XyFlowNode) => `var(--node-${(node.data as { catNode?: { type?: string } } | undefined)?.catNode?.type ?? 'marker'})`}
  maskColor="var(--bg)"
  style="background: var(--surface);"
/>
