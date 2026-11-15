import type { UnitStatus } from '../../entities/patrol-unit/model'
import { statusLabel, statusTone } from '../../entities/patrol-unit/uiModel'

/** Unit condition axis. Never used to express data quality. */
export function StatusBadge({ status }: { status: UnitStatus }) {
  return (
    <span className={`status-badge status-badge--${statusTone[status]}`} data-testid="status-badge">
      {statusLabel[status]}
    </span>
  )
}
