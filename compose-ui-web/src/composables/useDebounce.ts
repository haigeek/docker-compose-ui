import { ref, watch, type Ref } from 'vue'

export function useDebounce<T>(value: Ref<T>, delay = 300): Ref<T> {
  const debounced = ref(value.value) as Ref<T>
  let timeoutId: ReturnType<typeof setTimeout> | null = null

  watch(
    value,
    (newVal) => {
      if (timeoutId) {
        clearTimeout(timeoutId)
      }
      timeoutId = setTimeout(() => {
        debounced.value = newVal
        timeoutId = null
      }, delay)
    },
    { immediate: true }
  )

  return debounced
}

export function useDebouncedCallback<F extends (...args: unknown[]) => unknown>(
  fn: F,
  delay = 300
): F {
  let timeoutId: ReturnType<typeof setTimeout> | null = null

  const debounced = function (this: unknown, ...args: Parameters<F>) {
    if (timeoutId) {
      clearTimeout(timeoutId)
    }
    timeoutId = setTimeout(() => {
      fn.apply(this, args)
      timeoutId = null
    }, delay)
  } as F

  return debounced
}
