import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

afterEach(() => {
  cleanup()
})

// jsdom implements neither; the theme hook and Nivo's responsive wrapper need them.
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string): Partial<MediaQueryList> => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  }),
})

class NoopResizeObserver {
  observe(): void {
    // jsdom has no layout, so there is nothing to observe
  }
  unobserve(): void {
    // as above
  }
  disconnect(): void {
    // as above
  }
}
Object.defineProperty(globalThis, 'ResizeObserver', { writable: true, value: NoopResizeObserver })
