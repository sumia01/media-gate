import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  insertReleaseKeyword,
  parseReleaseKeywords,
  releaseKeywordPopupPosition,
  releaseKeywordToken,
} from '../releaseKeywords.ts'

test('keywords are lowercase, trimmed, deduplicated and empty lists stay empty', () => {
  assert.deepEqual(parseReleaseKeywords(' Ethel, ETHEL, EtHeL, FLUX , '), ['ethel', 'flux'])
  for (const value of ['', ' ', ', ,']) assert.deepEqual(parseReleaseKeywords(value), [])
})

test('floating suggestions show at most four rows and stay in the visible viewport', () => {
  assert.deepEqual(
    releaseKeywordPopupPosition(
      { top: 100, bottom: 138, left: 20, width: 300 },
      { top: 0, left: 0, width: 600, height: 600 },
      10,
    ),
    { left: '20px', top: '142px', width: '300px', maxHeight: '154px' },
  )
  const above = releaseKeywordPopupPosition(
    { top: 260, bottom: 298, left: 500, width: 300 },
    { top: 0, left: 0, width: 600, height: 320 },
    8,
  )
  assert.deepEqual(above, { left: '292px', top: '102px', width: '300px', maxHeight: '154px' })
  assert.equal(
    releaseKeywordPopupPosition(
      { top: 50, bottom: 88, left: 20, width: 300 },
      { top: 0, left: 0, width: 600, height: 100 },
      10,
    ),
    null,
    'do not open an unusable popup when even one row cannot fit',
  )
  assert.equal(
    releaseKeywordPopupPosition(
      { top: 400, bottom: 438, left: 20, width: 300 },
      { top: 0, left: 0, width: 600, height: 320 },
      8,
    ),
    null,
    'hide suggestions when the anchor is outside the visible viewport',
  )
})

test('suggestions use the comma token at the caret and exclude other selected tokens', () => {
  assert.deepEqual(releaseKeywordToken('ethel, fl', 9), {
    start: 6,
    end: 9,
    prefix: 'fl',
    selected: ['ethel'],
  })
  assert.deepEqual(releaseKeywordToken('et, flux', 2), {
    start: 0,
    end: 2,
    prefix: 'et',
    selected: ['flux'],
  })
  assert.equal(releaseKeywordToken('ethel, ', 7).prefix, '')
  assert.equal(releaseKeywordToken('ethel', 0).prefix, '')
})

test('choosing a keyword replaces only the active token and makes another token easy to enter', () => {
  assert.deepEqual(insertReleaseKeyword('ET', 2, 'Ethel'), { value: 'ethel, ', caret: 7 })
  assert.deepEqual(insertReleaseKeyword('ethel, FL', 9, 'Flux'), { value: 'ethel, flux, ', caret: 13 })
  assert.deepEqual(insertReleaseKeyword('ET, flux', 2, 'Ethel'), { value: 'ethel, flux', caret: 5 })
})
