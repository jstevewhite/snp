import { afterEach, describe, expect, it, vi } from 'vitest'
import { watchServiceWorkerUpdates } from './sw'

type Handler = (e: Event) => void

/** A stand-in for navigator.serviceWorker that records its listeners. */
function stubServiceWorker(controller: unknown) {
  const handlers = new Map<string, Handler[]>()
  const update = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'serviceWorker', {
    configurable: true,
    value: {
      controller,
      addEventListener: (type: string, fn: Handler) => {
        handlers.set(type, [...(handlers.get(type) ?? []), fn])
      },
      getRegistration: () => Promise.resolve({ update }),
    },
  })
  return {
    update,
    fire: (type: string): void => {
      for (const fn of handlers.get(type) ?? []) fn(new Event(type))
    },
  }
}

/** Let the getRegistration().then(reg => reg.update()) chain settle. */
const flush = (): Promise<void> => new Promise((r) => setTimeout(r, 0))

afterEach(() => {
  Reflect.deleteProperty(navigator, 'serviceWorker')
  vi.restoreAllMocks()
})

describe('watchServiceWorkerUpdates', () => {
  it('is inert where service workers are unavailable', () => {
    // jsdom defines no navigator.serviceWorker, so this is the browser case.
    expect(() => watchServiceWorkerUpdates(vi.fn())).not.toThrow()
  })

  it('checks for an update at startup', async () => {
    const { update } = stubServiceWorker({})
    watchServiceWorkerUpdates(vi.fn())
    await flush()
    expect(update).toHaveBeenCalledTimes(1)
  })

  it('re-checks on focus and on becoming visible again', async () => {
    const { update } = stubServiceWorker({})
    watchServiceWorkerUpdates(vi.fn())
    await flush()
    update.mockClear()

    window.dispatchEvent(new Event('focus'))
    await flush()
    expect(update).toHaveBeenCalledTimes(1)

    document.dispatchEvent(new Event('visibilitychange'))
    await flush()
    expect(update).toHaveBeenCalledTimes(2)
  })

  it('reloads once when a new worker takes over an already-controlled page', () => {
    const { fire } = stubServiceWorker({})
    const reload = vi.fn()
    watchServiceWorkerUpdates(reload)

    fire('controllerchange')
    expect(reload).toHaveBeenCalledTimes(1)
    // Later controller changes must not reload again on top of a reload.
    fire('controllerchange')
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('does not reload on the very first install', () => {
    // No controller yet: the worker claiming clients here is the first one.
    const { fire } = stubServiceWorker(null)
    const reload = vi.fn()
    watchServiceWorkerUpdates(reload)

    fire('controllerchange')
    expect(reload).not.toHaveBeenCalled()
  })

  it('survives an update check that rejects', async () => {
    const handlers = new Map<string, Handler[]>()
    Object.defineProperty(navigator, 'serviceWorker', {
      configurable: true,
      value: {
        controller: {},
        addEventListener: (type: string, fn: Handler) => {
          handlers.set(type, [...(handlers.get(type) ?? []), fn])
        },
        getRegistration: () => Promise.reject(new Error('offline')),
      },
    })
    expect(() => watchServiceWorkerUpdates(vi.fn())).not.toThrow()
    await flush()
  })
})
