export const connectionState = $state<{ status: 'idle' | 'connecting' | 'open' | 'error' }>({
  status: 'idle',
});
