export interface MediaItem {
  id: number
  title: string
  year: number
  type: 'movie' | 'series'
  posterColor: string
  posterUrl: string
  rating?: number
}

// TMDB poster paths for real movie/show posters (w342 size)
const tmdb = (path: string) => `https://image.tmdb.org/t/p/w342${path}`

export const recentlyAdded: MediaItem[] = [
  { id: 1, title: 'Dune: Part Two', year: 2024, type: 'movie', posterColor: '#7c3aed', posterUrl: tmdb('/md6VWaPq3uE5Ghw78BPabtjKFtK.jpg'), rating: 8.2 },
  { id: 2, title: 'Shogun', year: 2024, type: 'series', posterColor: '#0ea5e9', posterUrl: tmdb('/cltj0OIDwxxu2n82vMUAeKFJfhU.jpg'), rating: 8.7 },
  { id: 3, title: 'Civil War', year: 2024, type: 'movie', posterColor: '#e11d48', posterUrl: tmdb('/oCzzgAJcl52ntF0RZrW6hvPk93d.jpg'), rating: 7.0 },
  { id: 4, title: 'The Bear', year: 2024, type: 'series', posterColor: '#059669', posterUrl: tmdb('/y1BoozfWnHPI3aTRLzZ1bHSaZiq.jpg'), rating: 8.5 },
  { id: 5, title: 'Furiosa', year: 2024, type: 'movie', posterColor: '#d97706', posterUrl: tmdb('/kLRK25kSphppMGB2ag00NAl1j54.jpg'), rating: 7.6 },
  { id: 6, title: 'Fallout', year: 2024, type: 'series', posterColor: '#6366f1', posterUrl: tmdb('/c15BtJxCXMrISLVmysdsnZUPQft.jpg'), rating: 8.3 },
  { id: 7, title: 'Challengers', year: 2024, type: 'movie', posterColor: '#dc2626', posterUrl: tmdb('/5n62DVirTazDk4NvzO2uLvxeJbb.jpg'), rating: 7.8 },
]

export const trending: MediaItem[] = [
  { id: 8, title: 'Oppenheimer', year: 2023, type: 'movie', posterColor: '#9333ea', posterUrl: tmdb('/89fbqq5nnnzroLpD13T4TJ55Llf.jpg'), rating: 8.9 },
  { id: 9, title: 'Severance', year: 2024, type: 'series', posterColor: '#0891b2', posterUrl: tmdb('/rm6ET9oocRwD9xoo4442dlV7rtP.jpg'), rating: 8.8 },
  { id: 10, title: 'Poor Things', year: 2023, type: 'movie', posterColor: '#b45309', posterUrl: tmdb('/rrlR4tfI1KMT3IAipHIUExZAe4D.jpg'), rating: 8.0 },
  { id: 11, title: 'The Penguin', year: 2024, type: 'series', posterColor: '#4f46e5', posterUrl: tmdb('/2rkXHbaHWfCOHWWEmVdxFA8NL4E.jpg'), rating: 8.4 },
  { id: 12, title: 'Alien: Romulus', year: 2024, type: 'movie', posterColor: '#0d9488', posterUrl: tmdb('/2uSWRTtCG336nuBiG8jOTEUKSy8.jpg'), rating: 7.3 },
  { id: 13, title: 'Killers of the Flower Moon', year: 2023, type: 'movie', posterColor: '#c2410c', posterUrl: tmdb('/6cJuZds6dBfLgx8dn6VLoGhs6pQ.jpg'), rating: 8.6 },
  { id: 14, title: 'Slow Horses', year: 2024, type: 'series', posterColor: '#7c3aed', posterUrl: tmdb('/qBLksHQn8dedP1TwItRYDwQGlLp.jpg'), rating: 8.1 },
]

export interface NavItem {
  icon: string
  label: string
  active?: boolean
  badge?: number
}

export const navItems: NavItem[] = [
  { icon: '◈', label: 'Discover', active: true },
  { icon: '◻', label: 'Movies' },
  { icon: '▤', label: 'Series' },
  { icon: '↗', label: 'Requests', badge: 3 },
  { icon: '⚙', label: 'Settings' },
]
