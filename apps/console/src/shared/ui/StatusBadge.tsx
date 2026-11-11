import { statusLabel, type PatrolUnitStatus } from '../../entities/patrol-unit/model'

export function StatusBadge({ status }: { status: PatrolUnitStatus }) {
  return <span className={`status-badge status-badge--${status.toLowerCase()}`}>{statusLabel[status]}</span>
}

