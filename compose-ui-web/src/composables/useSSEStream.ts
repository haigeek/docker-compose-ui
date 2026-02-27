import { ref } from 'vue'

export function isAbortError(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}

export function useSSEStream() {
  const active = ref(false)
  let controller: AbortController | null = null

  async function start(run: (signal: AbortSignal) => Promise<void>): Promise<void> {
    stop()
    const current = new AbortController()
    controller = current
    active.value = true
    try {
      await run(current.signal)
    } finally {
      if (controller === current) {
        controller = null
        active.value = false
      }
    }
  }

  function stop() {
    if (controller) {
      controller.abort()
      controller = null
    }
    active.value = false
  }

  return {
    active,
    start,
    stop,
  }
}
