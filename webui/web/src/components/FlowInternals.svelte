<script lang="ts">
  import { Background, Controls, MiniMap, BackgroundVariant, useSvelteFlow, useStore } from '@xyflow/svelte';
  import type { Node as XyFlowNode, Viewport } from '@xyflow/svelte';
  import { tick, untrack } from 'svelte';

  interface Props {
    pendingFitView: boolean;
    pendingRestoreViewport: boolean;
    focusNodeId?: string | null;
    onFitViewDone: () => void;
    onRestoreViewportDone: () => void;
    containerWidth?: number;
    containerHeight?: number;
  }

  let { pendingFitView, pendingRestoreViewport, focusNodeId = null, onFitViewDone, onRestoreViewportDone, containerWidth, containerHeight }: Props = $props();

  const { fitView, getViewport, setViewport, getInternalNode } = useSvelteFlow();
  const store = useStore();

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
          const vp = untrack(() => getViewport());
          const internal = untrack(() => getInternalNode(nodeId));
          const absPos = internal?.internals?.positionAbsolute ?? internal?.position ?? { x: 0, y: 0 };
          const measured = internal?.measured;
          const w = (measured?.width ?? (internal as { width?: number } | undefined)?.width ?? 180) as number;
          const h = (measured?.height ?? (internal as { height?: number } | undefined)?.height ?? 60) as number;
          const nx = absPos.x * vp.zoom + vp.x;
          const ny = absPos.y * vp.zoom + vp.y;
          const domNode = untrack(() => store.domNode);
          const cw = (domNode?.clientWidth ?? 0) || untrack(() => containerWidth) || window.innerWidth;
          const ch = (domNode?.clientHeight ?? 0) || untrack(() => containerHeight) || window.innerHeight;
          const margin = 10;
          const visible =
            nx >= -margin &&
            nx + w * vp.zoom <= cw + margin &&
            ny >= -margin &&
            ny + h * vp.zoom <= ch + margin;
          if (!visible) {
            const currentZoom = vp.zoom;
            fitView({ nodes: [{ id: nodeId }], minZoom: currentZoom, maxZoom: currentZoom, padding: 0.3, duration: dur });
          }
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
