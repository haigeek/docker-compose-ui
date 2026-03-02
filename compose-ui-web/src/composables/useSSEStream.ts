import { ref, readonly } from 'vue'

export function isAbortError(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}

export type StreamStatus = 'idle' | 'connecting' | 'connected' | 'disconnected' | 'error'

export interface UseSSEStreamOptions {
  maxRetries?: number
  retryDelay?: number
  onRetry?: (attempt: number) => void
}

export function useSSEStream(options: UseSSEStreamOptions = {}) {
  const {
    maxRetries = 3,
    retryDelay = 1000,
    onRetry,
  } = options

  const active = ref(false)
  const status = ref<StreamStatus>('idle')
  const error = ref<string | null>(null)
  let controller: AbortController | null = null
  let retryCount = 0
  let reconnectTimeout: ReturnType<typeof setTimeout> | null = null

  async function start(run: (signal: AbortSignal) => Promise<void>): Promise<void> {
    stop()
    const current = new AbortController()
    controller = current
    active.value = true
    status.value = 'connecting'
    error.value = null
    retryCount = 0

    try {
      await run(current.signal)
    } catch (e) {
      if (isAbortError(e)) {
        status.value = 'disconnected'
        return
      }

      error.value = e instanceof Error ? e.message : String(e)
      status.value = 'error'

      if (retryCount < maxRetries && active.value) {
        retryCount++
        onRetry?.(retryCount)
        reconnectTimeout = setTimeout(() => {
          if (active.value) {
            start(run).catch(() => {})
          }
        }, retryDelay * retryCount)
      }
    } finally {
      if (controller === current) {
        controller = null
        // 只有在非错误且 active 仍为 true 时，才设置为 disconnected
        if (active.value && status.value !== 'error') {
          status.value = 'disconnected'
        }
      }
    }
  }

  function stop() {
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout)
      reconnectTimeout = null
    }
    if (controller) {
      controller.abort()
      controller = null
    }
    active.value = false
    status.value = 'disconnected'
  }

  function reconnect(run: (signal: AbortSignal) => Promise<void>) {
    if (active.value) {
      stop()
    }
    void start(run)
  }

  // 手动设置状态（供外部调用，比如连接成功后）
  function setStatus(newStatus: StreamStatus) {
    status.value = newStatus
  }

  return {
    active: readonly(active),
    status: readonly(status),
    error: readonly(error),
    start,
    stop,
    reconnect,
    setStatus,
  }
}
