<script lang="ts">
  import { Background, Controls, MiniMap, BackgroundVariant, useSvelteFlow } from '@xyflow/svelte';
  import type { Node as XyFlowNode } from '@xyflow/svelte';
  import { tick, untrack } from 'svelte';

  interface Props {
    pendingFitView: boolean;
    focusNodeId?: string | null;
    onFitViewDone: () => void;
  }

  let { pendingFitView, focusNodeId = null, onFitViewDone }: Props = $props();

  const { fitView } = useSvelteFlow();

  let prevFocusNodeId: string | null = null;

  $effect(() => {
    if (pendingFitView) {
      const nodeId = untrack(() => focusNodeId);
      tick().then(() => {
        if (nodeId) {
          fitView({ nodes: [{ id: nodeId }], duration: 300, maxZoom: 1.0, padding: 0.3 });
        } else {
          fitView({ duration: 300, maxZoom: 1.0 });
        }
        onFitViewDone();
      });
    }
  });

  $effect(() => {
    const nodeId = focusNodeId;
    const busy = untrack(() => pendingFitView);
    if (nodeId && nodeId !== prevFocusNodeId) {
      prevFocusNodeId = nodeId;
      if (!busy) {
        tick().then(() => {
          fitView({ nodes: [{ id: nodeId }], duration: 300, maxZoom: 1.0, padding: 0.3 });
        });
      }
    } else if (!nodeId) {
      prevFocusNodeId = null;
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
