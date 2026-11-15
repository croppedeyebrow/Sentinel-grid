import type { Freshness } from '../../entities/patrol-unit/model'
import { freshnessLabel } from '../../entities/patrol-unit/uiModel'

const description: Record<Freshness, string> = {
  FRESH: '관측 기준 시간 이내',
  STALE: '관측 기준 시간을 초과해 현재 값으로 신뢰할 수 없음',
  MISSING: '사용할 수 있는 관측이 없음',
}

/**
 * Data quality axis, shown next to a value rather than instead of it: a stale
 * reading stays on screen, labelled as not current.
 */
export function FreshnessChip({ freshness, age }: { freshness: Freshness; age?: string | null }) {
  return (
    <span
      className={`freshness-chip freshness-chip--${freshness.toLowerCase()}`}
      title={description[freshness]}
      data-testid="freshness-chip"
    >
      {freshnessLabel[freshness]}
      {age ? <span className="freshness-chip__age">{age}</span> : null}
    </span>
  )
}
