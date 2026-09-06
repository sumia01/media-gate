import { readFile } from 'node:fs/promises'

const manifestPath = process.argv[2]
if (!manifestPath) throw new Error('manifest path is required')

const manifest = JSON.parse(await readFile(manifestPath, 'utf8'))
if (!manifest.seed) throw new Error('harness instance has not been seeded')
const apiRoot = `${manifest.apiUrl}/api/v1`

async function request(base, path, options = {}, expected = [200]) {
  const response = await fetch(`${base}${path}`, {
    ...options,
    signal: AbortSignal.timeout(5000),
  })
  const text = await response.text()
  if (!expected.includes(response.status)) {
    throw new Error(`${options.method ?? 'GET'} ${path} returned ${response.status}: ${text}`)
  }
  return text ? JSON.parse(text) : undefined
}

async function poll(label, callback, attempts = 80) {
  for (let attempt = 0; attempt < attempts; attempt++) {
    const result = await callback()
    if (result) return result
    await new Promise((resolve) => setTimeout(resolve, 250))
  }
  throw new Error(`timed out waiting for ${label}`)
}

const login = await request(apiRoot, '/auth/login', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(manifest.credentials),
})
const headers = {
  Authorization: `Bearer ${login.accessToken}`,
  'Content-Type': 'application/json',
}

const frontendResponse = await fetch(manifest.frontendUrl, { signal: AbortSignal.timeout(5000) })
if (!frontendResponse.ok) {
  throw new Error(`frontend returned ${frontendResponse.status}`)
}
const proxyStatus = await request(manifest.frontendUrl, '/api/v1/setup/status')
if (proxyStatus.needsSetup) {
  throw new Error('frontend API proxy reached an unseeded instance')
}
const initialFiles = await request(apiRoot, `/media/${manifest.seed.mediaItemId}/files`, { headers })
const initialPaths = new Set(initialFiles.files.map((file) => file.path))
const search = await request(apiRoot, '/indexers/search?query=Harness%20Movie&type=movie-search', { headers })
const result = search.results[0]
if (!result || result.indexerId !== manifest.seed.indexerId) {
  throw new Error(`fake tracker search returned no seeded result: ${JSON.stringify(search)}`)
}

const download = await request(apiRoot, '/downloads', {
  method: 'POST',
  headers,
  body: JSON.stringify({
    mediaItemId: manifest.seed.mediaItemId,
    indexerId: result.indexerId,
    indexerName: result.indexerName,
    title: result.title,
    downloadUrl: result.downloadUrl,
    detailsUrl: result.detailsUrl,
    size: result.size,
  }),
}, [201])

await poll('torrent submission', async () => {
  const downloads = await request(apiRoot, `/downloads?mediaItemId=${manifest.seed.mediaItemId}`, { headers })
  const current = downloads.downloads.find((entry) => entry.id === download.id)
  return current?.status === 'downloading' ? current : undefined
})

const activeState = await request(manifest.fakeUrl, '/_harness/state')
if (activeState.tracker_download_count !== 1 || activeState.torrents.length !== 1) {
  throw new Error(`unexpected fake state after submission: ${JSON.stringify(activeState)}`)
}

await request(manifest.fakeUrl, '/_harness/complete', { method: 'POST' })
await poll('download import', async () => {
  const downloads = await request(apiRoot, `/downloads?mediaItemId=${manifest.seed.mediaItemId}`, { headers })
  const current = downloads.downloads.find((entry) => entry.id === download.id)
  return current?.status === 'completed' ? current : undefined
})

const files = await request(apiRoot, `/media/${manifest.seed.mediaItemId}/files`, { headers })
const imported = files.files.some((file) => {
  return file.fileName === 'Harness.Movie.2026.1080p.WEB-DL.mkv'
    && !initialPaths.has(file.path)
    && file.path.includes('/Harness.Movie.2026.1080p.WEB-DL/')
})
if (!imported) {
  throw new Error(`imported harness payload not found: ${JSON.stringify(files)}`)
}

console.log(`Harness smoke test passed for download ${download.id} and media item ${manifest.seed.mediaItemId}.`)
