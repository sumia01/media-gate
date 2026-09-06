import type { components } from '../api/schema'

export type TimelineItem = components['schemas']['EpisodeTimelineItem']
export const WINDOW_DAYS = 14
export const DAY_WIDTH = 224
export const RAIL_DAYS = WINDOW_DAYS * 5
const DAY_MS = 86_400_000

// Calendar days are integers, not local-midnight timestamps: DST never changes
// window lengths, and formatting explicitly in UTC preserves the supplied date.
export function timelineDay(value: string): number {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) throw new Error('Invalid calendar date')
  const date = new Date(`${value}T00:00:00Z`)
  if (!Number.isFinite(date.getTime()) || date.toISOString().slice(0, 10) !== value) {
    throw new Error('Invalid calendar date')
  }
  return date.getTime() / DAY_MS
}

export function timelineDate(day: number): string {
  return new Date(day * DAY_MS).toISOString().slice(0, 10)
}

export function localToday(now = new Date()): number {
  return timelineDay(
    `${now.getFullYear().toString().padStart(4, '0')}-${(now.getMonth() + 1).toString().padStart(2, '0')}-${now.getDate().toString().padStart(2, '0')}`,
  )
}

export function formatTimelineDay(day: number, options: Intl.DateTimeFormatOptions): string {
  return new Intl.DateTimeFormat('en', { ...options, timeZone: 'UTC' }).format(new Date(day * DAY_MS))
}

export function timelineWindow(day: number): number {
  return Math.floor(day / WINDOW_DAYS) * WINDOW_DAYS
}

export function timelineViewport(start: number, left: number, width: number) {
  const first = Math.max(0, Math.floor(left / DAY_WIDTH) - 2)
  const end = Math.min(RAIL_DAYS, Math.ceil((left + width) / DAY_WIDTH) + 2)
  return { first: start + first, end: start + end }
}

// Move the finite native scroll rail under the same calendar date. Whole-window
// shifts keep fetch keys stable; the caller applies left after Vue's nextTick.
export function recenterTimelineRail(start: number, left: number, width: number) {
  const index = Math.floor(left / DAY_WIDTH)
  const visible = Math.ceil(width / DAY_WIDTH)
  if (index >= WINDOW_DAYS && index + visible <= RAIL_DAYS - WINDOW_DAYS) return { start, left }
  const shift = Math.floor((index - WINDOW_DAYS * 2) / WINDOW_DAYS) * WINDOW_DAYS
  return { start: start + shift, left: left - shift * DAY_WIDTH }
}

export function timelineAvailability(item: TimelineItem, today: number): string {
  const ep = item.episode
  if (ep.hasFile) return 'Available'
  const labels: Record<string, string> = {
    pending: 'Queued',
    downloading: 'Downloading',
    downloaded: 'Downloaded',
    importing: 'Importing',
    seeding: 'Seeding',
  }
  if (ep.downloadStatus && labels[ep.downloadStatus]) return labels[ep.downloadStatus]!
  if (ep.airDate && ep.airDate > timelineDate(today)) return 'Upcoming'
  return 'Not available'
}

export interface TimelineWindow {
  from: number
  status: 'loading' | 'ready' | 'error'
  items: TimelineItem[]
  error?: string
}

export function createTimelineCache(
  load: (from: string, to: string, signal: AbortSignal) => Promise<TimelineItem[]>,
  changed: () => void,
) {
  const windows = new Map<number, TimelineWindow>()
  const requests = new Map<number, AbortController>()
  let disposed = false

  async function ensure(from: number, retry = false) {
    const previous = windows.get(from)
    if (disposed || (previous && (!retry || previous.status !== 'error'))) return
    const controller = new AbortController()
    const entry: TimelineWindow = { from, status: 'loading', items: [] }
    requests.set(from, controller)
    windows.set(from, entry)
    changed()
    try {
      const items = await load(timelineDate(from), timelineDate(from + WINDOW_DAYS), controller.signal)
      if (disposed || controller.signal.aborted || windows.get(from) !== entry) return
      windows.set(from, { from, status: 'ready', items })
    } catch (error) {
      if (disposed || controller.signal.aborted || windows.get(from) !== entry) return
      windows.set(from, {
        from,
        status: 'error',
        items: [],
        error: error instanceof Error ? error.message : 'Could not load episodes.',
      })
    } finally {
      if (requests.get(from) === controller) {
        requests.delete(from)
        if (!disposed) changed()
      }
    }
  }

  function invalidate(matches: (window: TimelineWindow) => boolean): boolean {
    let invalidated = false
    for (const [from, window] of windows) {
      if (matches(window)) {
        requests.get(from)?.abort()
        requests.delete(from)
        windows.delete(from)
        invalidated = true
      }
    }
    if (invalidated && !disposed) changed()
    return invalidated
  }

  function retain(first: number, end: number) {
    invalidate(({ from }) => from + WINDOW_DAYS <= first || from >= end)
  }

  function reset() {
    for (const controller of requests.values()) controller.abort()
    requests.clear()
    windows.clear()
  }

  return {
    windows,
    ensure,
    invalidate,
    retain,
    reset,
    dispose() {
      disposed = true
      reset()
    },
  }
}
