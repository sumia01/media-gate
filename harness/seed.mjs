import { readFile, writeFile } from 'node:fs/promises'

const manifestPath = process.argv[2]
if (!manifestPath) throw new Error('manifest path is required')

const manifest = JSON.parse(await readFile(manifestPath, 'utf8'))
const apiRoot = `${manifest.apiUrl}/api/v1`

async function request(path, options = {}, expected = [200]) {
  const response = await fetch(`${apiRoot}${path}`, {
    ...options,
    signal: AbortSignal.timeout(5000),
  })
  const text = await response.text()
  if (!expected.includes(response.status)) {
    throw new Error(`${options.method ?? 'GET'} ${path} returned ${response.status}: ${text}`)
  }
  return text ? JSON.parse(text) : undefined
}

const login = await request('/auth/login', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    email: manifest.credentials.email,
    password: manifest.credentials.password,
  }),
})
const headers = {
  Authorization: `Bearer ${login.accessToken}`,
  'Content-Type': 'application/json',
}

await request('/settings', {
  method: 'PUT',
  headers,
  body: JSON.stringify({
    qbitUrl: manifest.fakeUrl,
    qbitUsername: 'harness',
    qbitPassword: 'harness',
    qbitDownloadPath: manifest.paths.downloads,
    qbitCategory: 'media-gate-harness',
    workerDownloadInterval: 1,
    workerImporterInterval: 1,
    workerMonitorInterval: 3600,
    workerMetadataRefreshInterval: 3600,
    libraryBasePath: manifest.paths.libraryBase,
    onboardingStep: 6,
    onboardingCompleted: true,
    subtitleAutoSearch: false,
  }),
})

const indexer = await request('/indexers', {
  method: 'POST',
  headers,
  body: JSON.stringify({
    name: 'Harness Tracker',
    definitionId: 'media-gate-harness',
    settings: {},
    priority: 1,
  }),
}, [201])

const library = await request('/libraries', {
  method: 'POST',
  headers,
  body: JSON.stringify({
    name: 'Harness Movies',
    path: manifest.paths.movieLibrary,
    mediaType: 'movie',
  }),
}, [201])

await request(`/libraries/${library.id}/sync`, { method: 'POST', headers }, [202])

let mediaItem
for (let attempt = 0; attempt < 80; attempt++) {
  const media = await request(`/libraries/${library.id}/media`, { headers })
  mediaItem = media.items[0]
  if (mediaItem) break
  await new Promise((resolve) => setTimeout(resolve, 250))
}
if (!mediaItem) throw new Error('library sync did not create the harness media item')

manifest.seed = {
  indexerId: indexer.id,
  libraryId: library.id,
  mediaItemId: mediaItem.id,
}
await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, { mode: 0o600 })

console.log(`Seeded harness library ${library.id}, media item ${mediaItem.id}, indexer ${indexer.id}.`)
