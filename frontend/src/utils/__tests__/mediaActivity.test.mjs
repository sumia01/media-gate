import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import {
  activityAbsoluteTime,
  activityActionLabel,
  activityDetailLines,
  activityRelativeTime,
  activityTargetLabel,
  activityTargetSummary,
  fieldChangeText,
  monitoringChangeText,
} from '../mediaActivity.ts'

const base = {
  id: 1,
  recordedAt: '2026-09-01T12:00:00Z',
  actor: { kind: 'user', name: 'Alice' },
  operationId: 'operation',
  mediaTitle: 'Example Show',
  detailsVersion: 1,
  details: {},
}

test('every current activity action has a specific past-tense label', () => {
  const activities = [
    ['request.made', {}, 'Request made'],
    [
      'monitoring.changed',
      { monitoringChanges: [{ target: { scope: 'media' }, effectiveBefore: true, effectiveAfter: false }] },
      'Monitoring disabled',
    ],
    ['media.settings_changed', {}, 'Media settings changed'],
    ['download.queued', { automatic: false }, 'Manual grab queued'],
    ['download.status_changed', {}, 'Download status changed'],
    ['download.removal_requested', {}, 'Download removal requested'],
    ['download.removed', {}, 'Download removed'],
    ['media.match_changed', {}, 'Media match changed'],
    ['media.unmatched', {}, 'Media unmatched'],
    ['metadata.changed', {}, 'Media refreshed'],
    ['media.resync_requested', {}, 'File resync requested'],
    ['media.resync_completed', {}, 'File resync completed'],
    ['media.removal_requested', {}, 'Media removal requested'],
    ['subtitle.downloaded', {}, 'Subtitle downloaded'],
    ['subtitle.removed', {}, 'Subtitle removed'],
    ['watched.marked', {}, 'Marked as watched'],
    ['watched.unmarked', {}, 'Unmarked as watched'],
  ]
  for (const [action, details, expected] of activities) {
    assert.equal(activityActionLabel({ ...base, action, details }), expected)
  }
  assert.equal(
    activityActionLabel({ ...base, action: 'download.queued', details: { automatic: true } }),
    'Automatic grab queued',
  )
})

test('targets and typed changes use natural safe wording', () => {
  const activity = {
    ...base,
    action: 'request.made',
    details: {
      targets: [
        { scope: 'season', seasonNumber: 2 },
        { scope: 'episode', seasonNumber: 2, episodeNumber: 4 },
      ],
      mediaAdded: false,
      releaseName: 'Example.Show.S02E04',
      total: 120,
      truncated: true,
    },
  }
  assert.equal(activityTargetSummary(activity), 'Example Show · Season 2, Season 2, episode 4')
  assert.deepEqual(activityDetailLines(activity), [
    'Media was already in the library.',
    'Release: Example.Show.S02E04',
    'Showing 2 of 120 recorded changes.',
  ])
  assert.equal(
    monitoringChangeText({
      target: { scope: 'season', seasonNumber: 2 },
      before: false,
      after: true,
      effectiveBefore: false,
      effectiveAfter: false,
    }),
    'Season 2: off -> on (effective state remains off)',
  )
  assert.equal(fieldChangeText({ field: 'media_profile', before: 'HD', after: 'UHD' }), 'Media profile: HD -> UHD')
  assert.equal(activityTargetLabel({ scope: 'unknown', title: 'Misleading title' }), 'Unknown scope')
})

test('download IDs and mixed truncated collections are rendered with bounded values', () => {
  const details = {
    downloadId: 42,
    targets: [
      { scope: 'season', seasonNumber: 1 },
      { scope: 'season', seasonNumber: 2 },
    ],
    monitoringChanges: [
      { target: { scope: 'season', seasonNumber: 1 }, effectiveBefore: false, effectiveAfter: true },
      { target: { scope: 'season', seasonNumber: 2 }, effectiveBefore: false, effectiveAfter: true },
    ],
    fieldChanges: [
      { field: 'title', before: 'Old', after: 'New' },
      { field: 'year', before: '2025', after: '2026' },
    ],
    total: 5,
    truncated: true,
  }
  const lines = activityDetailLines({ ...base, action: 'monitoring.changed', details })
  assert.ok(lines.includes('Download: #42'))
  assert.equal(lines.at(-1), 'Showing 5 of 5 recorded changes.')

  for (const downloadId of ['<img src=x onerror=alert(1)>', Number.MAX_SAFE_INTEGER + 1, -1]) {
    const unsafeLines = activityDetailLines({
      ...base,
      action: 'download.queued',
      details: { downloadId },
    })
    assert.equal(
      unsafeLines.some((line) => line.startsWith('Download:')),
      false,
    )
  }
})

test('unknown actions and details versions never expose arbitrary payloads', () => {
  for (const activity of [
    { ...base, action: 'future.action', details: { releaseName: '<secret>' } },
    { ...base, action: 'request.made', detailsVersion: 2, details: { releaseName: '<secret>' } },
  ]) {
    assert.equal(activityActionLabel(activity), 'Recorded activity')
    assert.deepEqual(activityDetailLines(activity), [])
    assert.equal(activityTargetSummary(activity), 'Example Show')
  }
})

test('removal outcomes name the deleted child record accurately', () => {
  assert.ok(
    activityDetailLines({ ...base, action: 'subtitle.removed', details: { recordRemoved: true } }).includes(
      'The subtitle record was removed.',
    ),
  )
  assert.ok(
    activityDetailLines({ ...base, action: 'download.removed', details: { recordRemoved: true } }).includes(
      'The download record was removed.',
    ),
  )
})

test('timestamp helpers provide relative text and an absolute accessible label', () => {
  const time = '2026-09-01T12:00:00Z'
  assert.match(activityRelativeTime(time, Date.parse('2026-09-01T12:02:00Z')), /2 minutes ago/)
  assert.ok(activityAbsoluteTime(time).length > 5)
})

test('the activity disclosure and timestamps expose native accessibility relationships', () => {
  const source = readFileSync(new URL('../../components/media/MediaActivityPanel.vue', import.meta.url), 'utf8')
  assert.match(source, /<h2>[\s\S]*?<button[\s\S]*?:aria-expanded="expanded"[\s\S]*?:aria-controls="panelId"/)
  assert.match(source, /<time[\s\S]*?:datetime="entry\.recordedAt"[\s\S]*?:aria-label="activityAbsoluteTime/)
  assert.match(source, /role="status"/)
  assert.match(source, /role="alert"/)
  assert.match(source, /motion-reduce:/)
})
