import Ajv2020 from 'ajv/dist/2020'
import addFormats from 'ajv-formats'
import { describe, expect, it } from 'vitest'
import {
  abortReasonSchema,
  apiErrorSchema,
  checkpointStateSchema,
  createMissionRequestSchema,
  errorCodeSchema,
  missionListSchema,
  missionStatusSchema,
  missionTransitionSchema,
  transitionRequestSchema,
} from '../entities/mission/model'
import { readExample, readSchema } from './publishedContract'

/**
 * The Patrol Mission contract is pinned from both directions: every published
 * example is validated against the JSON Schema the Go service is tested with,
 * and against the Zod schema the console parses responses with. A field added
 * to one representation alone fails here.
 */

function validator() {
  const ajv = new Ajv2020({ strict: false, allErrors: true })
  addFormats(ajv)
  ajv.addSchema(readSchema('patrol-mission'))
  ajv.addSchema(readSchema('patrol-mission-list'))
  ajv.addSchema(readSchema('patrol-mission-create-request'))
  ajv.addSchema(readSchema('patrol-mission-transition-request'))
  ajv.addSchema(readSchema('api-error'))
  return ajv
}

function assertValid(schemaId: string, instance: unknown) {
  const ajv = validator()
  const validate = ajv.getSchema(`https://sentinel-grid.local/schemas/${schemaId}.schema.json`)
  if (validate === undefined) throw new Error(`schema ${schemaId} did not compile`)

  if (!validate(instance)) {
    const facts = JSON.stringify(validate.errors?.map((error) => error.params))
    throw new Error(`${schemaId}: ${ajv.errorsText(validate.errors)} ${facts}`)
  }
}

describe('published mission schemas', () => {
  it('accept the mission list example', () => {
    expect(() => assertValid('patrol-mission-list', readExample('patrol-mission-list'))).not.toThrow()
  })

  it('accept the create request example', () => {
    expect(() =>
      assertValid('patrol-mission-create-request', readExample('patrol-mission-create-request')),
    ).not.toThrow()
  })

  it('accept the transition request example', () => {
    expect(() =>
      assertValid(
        'patrol-mission-transition-request',
        readExample('patrol-mission-transition-request'),
      ),
    ).not.toThrow()
  })

  it('accept the error example', () => {
    expect(() => assertValid('api-error', readExample('api-error'))).not.toThrow()
  })

  it('reject a mission that carries an undeclared field', () => {
    const list = readExample('patrol-mission-list') as { missions: Record<string, unknown>[] }
    const tampered = {
      ...list,
      missions: [{ ...list.missions[0], coverageRatio: 0.4 }, ...list.missions.slice(1)],
    }

    expect(() => assertValid('patrol-mission-list', tampered)).toThrow(/coverageRatio/)
  })

  it('reject a transition request without the version the caller saw', () => {
    expect(() => assertValid('patrol-mission-transition-request', { reason: 'BATTERY' })).toThrow(
      /version/,
    )
  })
})

describe('console schemas and published examples agree', () => {
  it('parses the mission list example', () => {
    const picture = missionListSchema.parse(readExample('patrol-mission-list'))

    expect(picture.missions).not.toHaveLength(0)
    expect(new Set(picture.missions.map((mission) => mission.status))).toEqual(
      new Set(['ACTIVE', 'DRAFT', 'COMPLETED', 'ABORTED']),
    )
  })

  it('parses the request and error examples', () => {
    expect(() =>
      createMissionRequestSchema.parse(readExample('patrol-mission-create-request')),
    ).not.toThrow()
    expect(() =>
      transitionRequestSchema.parse(readExample('patrol-mission-transition-request')),
    ).not.toThrow()
    expect(() => apiErrorSchema.parse(readExample('api-error'))).not.toThrow()
  })

  it('covers every checkpoint verdict in the examples', () => {
    const picture = missionListSchema.parse(readExample('patrol-mission-list'))
    const states = picture.missions.flatMap((mission) =>
      mission.checkpoints.map((checkpoint) => checkpoint.state),
    )

    expect(new Set(states)).toEqual(new Set(['PLANNED', 'VISITED', 'MISSED', 'UNKNOWN']))
  })

  it('never reports a missed checkpoint on a running mission', () => {
    const picture = missionListSchema.parse(readExample('patrol-mission-list'))

    for (const mission of picture.missions.filter((candidate) => candidate.status === 'ACTIVE')) {
      const judged = mission.checkpoints.filter((checkpoint) =>
        ['MISSED', 'UNKNOWN'].includes(checkpoint.state),
      )
      expect(judged).toHaveLength(0)
    }
  })

  it('keeps coverage counts consistent with the checkpoints they summarise', () => {
    const picture = missionListSchema.parse(readExample('patrol-mission-list'))

    for (const mission of picture.missions) {
      const count = (state: string) =>
        mission.checkpoints.filter((checkpoint) => checkpoint.state === state).length

      expect(mission.coverage).toEqual({
        visited: count('VISITED'),
        missed: count('MISSED'),
        unknown: count('UNKNOWN'),
        planned: count('PLANNED'),
        total: mission.checkpoints.length,
      })
    }
  })

  it('credits a pass only where a checkpoint is visited', () => {
    const picture = missionListSchema.parse(readExample('patrol-mission-list'))

    for (const mission of picture.missions) {
      for (const checkpoint of mission.checkpoints) {
        if (checkpoint.state === 'VISITED') {
          expect(checkpoint.creditedObservedAt).not.toBeNull()
        } else {
          expect(checkpoint.creditedObservedAt).toBeNull()
        }
      }
    }
  })
})

describe('enums match between the JSON Schema and the console', () => {
  function schemaEnum(path: string[]): string[] {
    let node = readSchema('patrol-mission') as Record<string, unknown>
    for (const segment of path) {
      node = node[segment] as Record<string, unknown>
    }
    return node.enum as string[]
  }

  it.each([
    ['missionStatus', () => missionStatusSchema.options],
    ['transition', () => missionTransitionSchema.options],
    ['abortReason', () => abortReasonSchema.options],
    ['checkpointState', () => checkpointStateSchema.options],
  ])('%s', (name, consoleOptions) => {
    expect(new Set(consoleOptions())).toEqual(new Set(schemaEnum(['$defs', name])))
  })

  it('error codes', () => {
    const published = (readSchema('api-error').$defs as Record<string, { enum: string[] }>).errorCode
      .enum

    expect(new Set(errorCodeSchema.options)).toEqual(new Set(published))
  })
})
