import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mediaRequesterNames } from '../mediaRequests.ts'

const requests = [
  { scope: 'media', requester: { id: 1, name: 'Attila Sumi' }, requestedAt: '2026-09-07T10:00:00Z' },
  { scope: 'whole_series', requester: { id: 1, name: 'Attila Sumi' }, requestedAt: '2026-09-07T10:00:00Z' },
  { scope: 'future_seasons', requester: { id: 1, name: 'Attila Sumi' }, requestedAt: '2026-09-07T10:00:00Z' },
  { scope: 'season', seasonNumber: 2, requester: { id: 1, name: 'Attila Sumi' }, requestedAt: '2026-09-07T10:00:00Z' },
  { scope: 'season', seasonNumber: 2, requester: { id: 2, name: 'Agnes Sumi' }, requestedAt: '2026-09-07T11:00:00Z' },
  {
    scope: 'episode',
    seasonNumber: 3,
    episodeNumber: 4,
    requester: { id: 2, name: 'Agnes Sumi' },
    requestedAt: '2026-09-07T11:00:00Z',
  },
]

test('media requester names are deduplicated and can be scoped', () => {
  assert.deepEqual(mediaRequesterNames(requests), ['Attila Sumi', 'Agnes Sumi'])
  assert.deepEqual(mediaRequesterNames(requests, 'media'), ['Attila Sumi'])
  assert.deepEqual(mediaRequesterNames(requests, 'whole_series'), ['Attila Sumi'])
  assert.deepEqual(mediaRequesterNames(requests, 'future_seasons'), ['Attila Sumi'])
  assert.deepEqual(mediaRequesterNames(requests, 'season', 2), ['Attila Sumi', 'Agnes Sumi'])
  assert.deepEqual(mediaRequesterNames(requests, 'episode', 3, 4), ['Agnes Sumi'])
})
