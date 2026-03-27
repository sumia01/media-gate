import createClient from 'openapi-fetch'
import type { paths } from './schema'

const client = createClient<paths>({ baseUrl: '/api/v1' })

export default client
