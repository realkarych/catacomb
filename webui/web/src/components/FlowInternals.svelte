<script lang="ts">
  import { Background, Controls, MiniMap, BackgroundVariant, useSvelteFlow } from '@xyflow/svelte';
  import type { Node as XyFlowNode, Viewport } from '@xyflow/svelte';
  import { tick, untrack } from 'svelte';

  interface Props {
    pendingFitView: boolean;
    pendingRestoreViewport: boolean;
    focusNodeId?: string | null;
    onFitViewDone: () => void;
    onRestoreViewportDone: () => void;
  }

  let { pendingFitView, pendingRestoreViewport, focusNodeId = null, onFitViewDone, onRestoreViewportDone }: Props = $props();

  const { fitView, getViewport, setViewport } = useSvelteFlow();

  let prevFocusNodeId: string | null = null;

  function motionDuration(): number {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return 300;
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 300;
  }

  $effect(() => {
    if (pendingFitView) {
      const nodeId = untrack(() => focusNodeId);
      const dur = motionDuration();
      tick().then(() => {
        if (nodeId) {
          fitView({ nodes: [{ id: nodeId }], duration: dur, maxZoom: 1.0, padding: 0.3 });
        } else {
          fitView({ duration: dur, maxZoom: 1.0 });
        }
        onFitViewDone();
      });
    }
  });

  $effect(() => {
    if (pendingRestoreViewport) {
      const captured: Viewport = getViewport();
      tick().then(() => {
        setViewport(captured, { duration: 0 });
        onRestoreViewportDone();
      });
    }
  });

  $effect(() => {
    const nodeId = focusNodeId;
    const busy = untrack(() => pendingFitView);
    if (nodeId && nodeId !== prevFocusNodeId) {
      prevFocusNodeId = nodeId;
      if (!busy) {
        const dur = motionDuration();
        tick().then(() => {
          fitView({ nodes: [{ id: nodeId }], duration: dur, maxZoom: 1.0, padding: 0.3 });
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
