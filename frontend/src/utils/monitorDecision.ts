import type { components } from '../api/schema'

export function monitorDecisionFreshness(
  decision: Pick<components['schemas']['MonitorDecision'], 'inputUpdatedAt'> | null,
  updatedAt: string,
): 'unknown' | 'stale' | 'unchanged' | null {
  if (!decision) return null
  if (!decision.inputUpdatedAt) return 'unknown'
  const inputTime = Date.parse(decision.inputUpdatedAt)
  const itemTime = Date.parse(updatedAt)
  if (!Number.isFinite(inputTime) || !Number.isFinite(itemTime)) return 'unknown'
  return itemTime > inputTime ? 'stale' : 'unchanged'
}
