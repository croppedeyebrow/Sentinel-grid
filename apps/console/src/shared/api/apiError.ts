import { apiErrorSchema, type ApiError, type ErrorCode } from '../../entities/mission/model'

/**
 * A refusal the API published as contract: a code the console keys off and the
 * facts needed to act on it. The server's own sentence is English and is kept
 * only for logs; what the operator reads is derived from the code.
 */
export class ApiRefusal extends Error {
  readonly code: ErrorCode | 'UNKNOWN'
  readonly status: number
  readonly details: ApiError['details']

  constructor(status: number, payload: ApiError | null, fallback: string) {
    super(payload?.error ?? fallback)
    this.name = 'ApiRefusal'
    this.status = status
    this.code = payload?.code ?? 'UNKNOWN'
    this.details = payload?.details
  }
}

/** Turns a failed response into a refusal, whatever shape the body has. */
export async function refusalFrom(response: Response): Promise<ApiRefusal> {
  let payload: ApiError | null = null
  try {
    payload = apiErrorSchema.parse(await response.json())
  } catch {
    payload = null
  }
  return new ApiRefusal(response.status, payload, `요청이 실패했습니다 (HTTP ${response.status})`)
}
