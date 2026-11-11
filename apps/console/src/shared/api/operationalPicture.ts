import { operationalPictureSchema } from '../../entities/patrol-unit/model'
import { API_BASE_URL } from './config'

export async function getOperationalPicture() {
  const response = await fetch(`${API_BASE_URL}/operational-picture`)
  if (!response.ok) throw new Error(`Operational Picture 요청 실패: ${response.status}`)
  return operationalPictureSchema.parse(await response.json())
}

