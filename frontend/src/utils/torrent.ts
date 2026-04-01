export interface ParsedSeasonEpisode {
  season: number | null
  episode: number | null
  episodeRange: [number, number] | null
}

export type MatchLevel = 'full' | 'season' | 'none'

const MATCH_ORDER: Record<MatchLevel, number> = { full: 0, season: 1, none: 2 }

// Patterns in priority order — first match wins.
// S02E01-E10  (episode range)
const RE_SXEX_RANGE = /S(\d{1,2})E(\d{1,3})-E(\d{1,3})/i
// S02E05      (standard single episode)
const RE_SXEX = /S(\d{1,2})E(\d{1,3})/i
// S02 alone   (word-boundary guarded to avoid false positives like DTS, ABS)
const RE_S_ONLY = /(?:^|[\s.\-_[(])S(\d{1,2})(?=[\s.\-_\])]|$)/i
// Season 2 Episode 5
const RE_SEASON_EP = /Season[\s._-]*(\d{1,2})[\s._-]*Episode[\s._-]*(\d{1,3})/i
// Season 2
const RE_SEASON_ONLY = /Season[\s._-]*(\d{1,2})/i
// 2x05
const RE_NXN = /\b(\d{1,2})x(\d{1,3})\b/i

export function parseTitleSeasonEpisode(title: string): ParsedSeasonEpisode {
  let m: RegExpMatchArray | null

  m = title.match(RE_SXEX_RANGE)
  if (m) return { season: +m[1]!, episode: null, episodeRange: [+m[2]!, +m[3]!] }

  m = title.match(RE_SXEX)
  if (m) return { season: +m[1]!, episode: +m[2]!, episodeRange: null }

  m = title.match(RE_S_ONLY)
  if (m) return { season: +m[1]!, episode: null, episodeRange: null }

  m = title.match(RE_SEASON_EP)
  if (m) return { season: +m[1]!, episode: +m[2]!, episodeRange: null }

  m = title.match(RE_SEASON_ONLY)
  if (m) return { season: +m[1]!, episode: null, episodeRange: null }

  m = title.match(RE_NXN)
  if (m) return { season: +m[1]!, episode: +m[2]!, episodeRange: null }

  return { season: null, episode: null, episodeRange: null }
}

export function classifyMatch(
  parsed: ParsedSeasonEpisode,
  userSeason: number | null,
  userEpisode: number | null,
): MatchLevel {
  if (userSeason === null) return 'none'
  if (parsed.season !== userSeason) return 'none'

  // Season matches — check episode
  if (userEpisode === null) return 'season'

  if (parsed.episode === userEpisode) return 'full'

  if (parsed.episodeRange) {
    const [start, end] = parsed.episodeRange
    if (userEpisode >= start && userEpisode <= end) return 'full'
  }

  return 'season'
}

export function matchLevelOrder(level: MatchLevel): number {
  return MATCH_ORDER[level]
}

// --- Quality profile matching ---

export interface ParsedTorrentQuality {
  resolution: string // "2160p", "1080p", "720p", "480p", or ""
  source: string     // "bluray", "webdl", "webrip", "hdtv", "dvdrip", or ""
}

const RES_RE = /(?:^|[\s.\-_[(])(2160|1080|720|480)p/i

const SOURCE_ALIASES: Record<string, string> = {
  'bluray': 'bluray',
  'blu-ray': 'bluray',
  'bdrip': 'bluray',
  'brrip': 'bluray',
  'web-dl': 'webdl',
  'webdl': 'webdl',
  'webrip': 'webrip',
  'web-rip': 'webrip',
  'hdtv': 'hdtv',
  'pdtv': 'hdtv',
  'dvdrip': 'dvdrip',
  'dvd-rip': 'dvdrip',
  'dvd': 'dvd',
}

export function parseTorrentQuality(title: string): ParsedTorrentQuality {
  const result: ParsedTorrentQuality = { resolution: '', source: '' }

  const resMatch = title.match(RES_RE)
  if (resMatch) result.resolution = resMatch[1] + 'p'

  const lower = title.toLowerCase()
  let bestAlias = ''
  let bestCanonical = ''
  for (const [alias, canonical] of Object.entries(SOURCE_ALIASES)) {
    if (lower.includes(alias) && alias.length > bestAlias.length) {
      bestAlias = alias
      bestCanonical = canonical
    }
  }
  if (bestCanonical) result.source = bestCanonical

  return result
}

export interface ProfileMatchInput {
  resolutions: string[]
  sources?: string[]
}

export function matchesProfile(quality: ParsedTorrentQuality, profile: ProfileMatchInput): boolean {
  // Resolution must match
  if (!quality.resolution || !profile.resolutions.includes(quality.resolution)) return false

  // If profile has sources defined, source must also match
  if (profile.sources && profile.sources.length > 0) {
    if (!quality.source || !profile.sources.includes(quality.source)) return false
  }

  return true
}
