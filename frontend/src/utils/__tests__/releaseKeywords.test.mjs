import assert from 'node:assert/strict'
import { test } from 'node:test'
import { insertReleaseKeyword, parseReleaseKeywords, releaseKeywordToken } from '../releaseKeywords.ts'

test('keywords are lowercase, trimmed, deduplicated and empty lists stay empty', () => {
  assert.deepEqual(parseReleaseKeywords(' Ethel, ETHEL, EtHeL, FLUX , '), ['ethel', 'flux'])
  for (const value of ['', ' ', ', ,']) assert.deepEqual(parseReleaseKeywords(value), [])
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
