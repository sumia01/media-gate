import assert from 'node:assert/strict'
import { test } from 'node:test'
import { filterLibraryItems, libraryGenres } from '../media.ts'

const items = [
  { title: 'House of the Dragon', metadata: { genres: '["Drama","Fantasy"]' } },
  { title: 'Dragon Ball', metadata: { genres: 'Animation, Action' } },
  { title: 'The Last of Us', metadata: { genres: '["Drama","Sci-Fi & Fantasy"]' } },
  { title: 'Unmatched Folder' },
]

test('library title filtering is live substring matching regardless of case or surrounding whitespace', () => {
  assert.deepEqual(
    filterLibraryItems(items, ' agon ', []).map((item) => item.title),
    ['House of the Dragon', 'Dragon Ball'],
  )
  assert.deepEqual(
    filterLibraryItems(items, 'LAST OF', []).map((item) => item.title),
    ['The Last of Us'],
  )
})

test('library genres are unique across JSON and comma-separated metadata', () => {
  assert.deepEqual(libraryGenres([...items, { title: 'Duplicate', metadata: { genres: 'drama' } }]), [
    'Action',
    'Animation',
    'Drama',
    'Fantasy',
    'Sci-Fi & Fantasy',
  ])
})

test('genre filtering matches any selected genre and combines with the title filter', () => {
  assert.deepEqual(
    filterLibraryItems(items, '', ['Fantasy', 'Animation']).map((item) => item.title),
    ['House of the Dragon', 'Dragon Ball'],
  )
  assert.deepEqual(
    filterLibraryItems(items, 'dragon', ['drama']).map((item) => item.title),
    ['House of the Dragon'],
  )
  assert.deepEqual(filterLibraryItems(items, '', ['Comedy']), [])
})
