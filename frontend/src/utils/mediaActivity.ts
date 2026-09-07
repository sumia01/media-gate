import type { components } from '@/api/schema'

export type MediaActivity = components['schemas']['MediaActivity']
export type MediaActivityDetails = components['schemas']['MediaActivityDetails']
export type MediaActivityTarget = components['schemas']['MediaActivityTarget']
export type MediaActivityMonitoringChange = components['schemas']['MediaActivityMonitoringChange']
export type MediaActivityFieldChange = components['schemas']['MediaActivityFieldChange']

export const MEDIA_ACTIVITY_DETAILS_VERSION = 1

const knownActions = new Set([
  'request.made',
  'monitoring.changed',
  'media.settings_changed',
  'download.queued',
  'download.status_changed',
  'download.removal_requested',
  'download.removed',
  'media.match_changed',
  'media.unmatched',
  'metadata.changed',
  'media.resync_requested',
  'media.resync_completed',
  'media.removal_requested',
  'subtitle.downloaded',
  'subtitle.removed',
  'watched.marked',
  'watched.unmarked',
])

const fieldLabels: Record<string, string> = {
  media_profile: 'Media profile',
  preferred_release: 'Preferred release',
  title: 'Title',
  year: 'Year',
  status: 'Status',
  season_count: 'Season count',
  runtime: 'Runtime',
  'download.status': 'Download status',
}

export function hasKnownActivityDetails(activity: MediaActivity): boolean {
  return (
    activity.detailsVersion === MEDIA_ACTIVITY_DETAILS_VERSION &&
    knownActions.has(activity.action) &&
    activity.details != null
  )
}

export function activityActionLabel(activity: MediaActivity): string {
  if (!knownActions.has(activity.action) || activity.detailsVersion !== MEDIA_ACTIVITY_DETAILS_VERSION) {
    return 'Recorded activity'
  }
  if (activity.action === 'monitoring.changed') {
    const changes = activity.details?.monitoringChanges ?? []
    const enabled = changes.some((change) => !change.effectiveBefore && change.effectiveAfter)
    const disabled = changes.some((change) => change.effectiveBefore && !change.effectiveAfter)
    if (enabled && !disabled) return 'Monitoring enabled'
    if (disabled && !enabled) return 'Monitoring disabled'
    return 'Monitoring changed'
  }
  const labels: Record<string, string> = {
    'request.made': 'Request made',
    'media.settings_changed': 'Media settings changed',
    'download.queued': activity.details?.automatic ? 'Automatic grab queued' : 'Manual grab queued',
    'download.status_changed': 'Download status changed',
    'download.removal_requested': 'Download removal requested',
    'download.removed': 'Download removed',
    'media.match_changed': 'Media match changed',
    'media.unmatched': 'Media unmatched',
    'metadata.changed': 'Media refreshed',
    'media.resync_requested': 'File resync requested',
    'media.resync_completed': 'File resync completed',
    'media.removal_requested': 'Media removal requested',
    'subtitle.downloaded': 'Subtitle downloaded',
    'subtitle.removed': 'Subtitle removed',
    'watched.marked': 'Marked as watched',
    'watched.unmarked': 'Unmarked as watched',
  }
  return labels[activity.action] ?? 'Recorded activity'
}

export function activityTargetLabel(target: MediaActivityTarget): string {
  switch (target.scope) {
    case 'media':
      return 'Media'
    case 'whole_series':
      return 'Whole series'
    case 'future_seasons':
      return 'Future seasons'
    case 'season':
      return target.seasonNumber == null ? 'Season' : `Season ${target.seasonNumber}`
    case 'episode':
      if (target.seasonNumber != null && target.episodeNumber != null) {
        return `Season ${target.seasonNumber}, episode ${target.episodeNumber}`
      }
      return target.episodeNumber == null ? 'Episode' : `Episode ${target.episodeNumber}`
    default:
      return 'Unknown scope'
  }
}

function activityTargets(activity: MediaActivity): MediaActivityTarget[] {
  if (!hasKnownActivityDetails(activity)) return []
  const details = activity.details!
  const candidates = [
    ...(details.target ? [details.target] : []),
    ...(details.targets ?? []),
    ...(details.monitoringChanges ?? []).map((change) => change.target),
    ...(details.fieldChanges ?? []).flatMap((change) => (change.target ? [change.target] : [])),
  ]
  const seen = new Set<string>()
  return candidates.filter((target) => {
    const key = `${target.scope}:${target.seasonNumber ?? ''}:${target.episodeNumber ?? ''}:${target.objectId ?? ''}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

export function activityTargetSummary(activity: MediaActivity): string {
  const targets = activityTargets(activity)
  if (!targets.length || (targets.length === 1 && targets[0]?.scope === 'media')) return activity.mediaTitle
  return `${activity.mediaTitle} · ${targets.map(activityTargetLabel).join(', ')}`
}

function settingValue(value: boolean | null | undefined): string {
  if (value == null) return 'inherited'
  return value ? 'on' : 'off'
}

export function monitoringChangeText(change: MediaActivityMonitoringChange): string {
  const target = activityTargetLabel(change.target)
  const before = settingValue(change.before)
  const after = settingValue(change.after)
  if (before === after && change.effectiveBefore !== change.effectiveAfter) {
    return `${target}: effective ${change.effectiveBefore ? 'on' : 'off'} -> ${change.effectiveAfter ? 'on' : 'off'}`
  }
  if (change.effectiveBefore === change.effectiveAfter && before !== after) {
    return `${target}: ${before} -> ${after} (effective state remains ${change.effectiveAfter ? 'on' : 'off'})`
  }
  return `${target}: ${before} -> ${after}`
}

export function fieldChangeText(change: MediaActivityFieldChange): string {
  const label = fieldLabels[change.field] ?? 'Setting'
  const target = change.target ? `${activityTargetLabel(change.target)}: ` : ''
  return `${target}${label}: ${change.before || 'none'} -> ${change.after || 'none'}`
}

function providerLabel(provider: MediaActivityDetails['oldProvider']): string {
  if (!provider) return 'none'
  return `${provider.source.toUpperCase()} #${provider.externalId}${provider.title ? ` (${provider.title})` : ''}`
}

function codeLabel(value: string): string {
  return value.replaceAll('_', ' ')
}

export function activityDetailLines(activity: MediaActivity): string[] {
  if (!hasKnownActivityDetails(activity)) return []
  const details = activity.details!
  const lines: string[] = []

  if (details.mediaAdded === true) lines.push('Media added to the library.')
  else if (details.mediaAdded === false && activity.action === 'request.made')
    lines.push('Media was already in the library.')
  if (details.reason === 'future_season_policy') lines.push('Enabled under the future-season monitoring policy.')
  if (details.reason === 'manual_retry') lines.push('A manual retry was requested.')
  for (const change of details.monitoringChanges ?? []) lines.push(monitoringChangeText(change))
  for (const change of details.fieldChanges ?? []) lines.push(fieldChangeText(change))
  if (details.releaseName) lines.push(`Release: ${details.releaseName}`)
  if (details.indexerName) lines.push(`Indexer: ${details.indexerName}`)
  if (Number.isSafeInteger(details.downloadId) && details.downloadId! > 0) {
    lines.push(`Download: #${details.downloadId}`)
  }
  if (details.oldStatus || details.newStatus)
    lines.push(`Status: ${details.oldStatus || 'none'} -> ${details.newStatus || 'none'}`)
  if (details.oldProvider || details.newProvider) {
    lines.push(`Match: ${providerLabel(details.oldProvider)} -> ${providerLabel(details.newProvider)}`)
  }
  if (details.language) lines.push(`Language: ${details.language}`)
  if (details.provider) lines.push(`Provider: ${details.provider}`)
  if (details.fileName) lines.push(`File: ${details.fileName}`)
  if (details.requestedDeleteFiles != null) {
    lines.push(details.requestedDeleteFiles ? 'File removal was requested.' : 'Only record removal was requested.')
  }
  if (details.recordRemoved) {
    lines.push(
      activity.action === 'subtitle.removed' ? 'The subtitle record was removed.' : 'The download record was removed.',
    )
  }
  if (details.cleanupOutcome) lines.push(`Cleanup: ${codeLabel(details.cleanupOutcome)}`)
  if (details.torrentCleanupOutcome) lines.push(`Torrent cleanup: ${codeLabel(details.torrentCleanupOutcome)}`)
  if (details.fileCleanupOutcome) lines.push(`File cleanup: ${codeLabel(details.fileCleanupOutcome)}`)

  const changes =
    activity.action === 'media.resync_completed'
      ? [`${details.added ?? 0} added`, `${details.updated ?? 0} updated`, `${details.removed ?? 0} removed`]
      : [
          details.added ? `${details.added} added` : '',
          details.updated ? `${details.updated} updated` : '',
          details.removed ? `${details.removed} removed` : '',
        ].filter(Boolean)
  if (changes.length) lines.push(`Changes: ${changes.join(', ')}`)
  const cleanupCounts = [
    details.physicalFilesRemoved ? `${details.physicalFilesRemoved} physical files removed` : '',
    details.physicalFilesFailed ? `${details.physicalFilesFailed} physical file removals failed` : '',
    details.databaseRecordsRemoved ? `${details.databaseRecordsRemoved} database records removed` : '',
  ].filter(Boolean)
  if (cleanupCounts.length) lines.push(cleanupCounts.join(', '))
  if (details.partial) {
    lines.push(
      `Partial provider refresh: ${details.successfulProviderWindows ?? 0} succeeded, ${details.failedProviderWindows ?? 0} failed.`,
    )
  }
  if (details.truncated && details.total) {
    const collectionCount =
      (details.targets?.length ?? 0) + (details.monitoringChanges?.length ?? 0) + (details.fieldChanges?.length ?? 0)
    const shown = Math.min(details.total, collectionCount)
    lines.push(
      shown
        ? `Showing ${shown} of ${details.total} recorded changes.`
        : `${details.total} changes were recorded; details were truncated.`,
    )
  }
  return lines
}

export function activityAbsoluteTime(value: string): string {
  return new Date(value).toLocaleString(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

export function activityRelativeTime(value: string, now = Date.now()): string {
  const difference = new Date(value).getTime() - now
  const absolute = Math.abs(difference)
  const divisions: [number, Intl.RelativeTimeFormatUnit][] = [
    [1000 * 60 * 60 * 24, 'day'],
    [1000 * 60 * 60, 'hour'],
    [1000 * 60, 'minute'],
  ]
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
  for (const [milliseconds, unit] of divisions) {
    if (absolute >= milliseconds) return formatter.format(Math.round(difference / milliseconds), unit)
  }
  return formatter.format(Math.round(difference / 1000), 'second')
}
