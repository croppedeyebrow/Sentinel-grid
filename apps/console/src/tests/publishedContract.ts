import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Reads the repository's published contracts from disk.
 *
 * The schemas and examples are never copied into the tests: the Go service is
 * verified against these same files, so a change on one side fails the other
 * side's tests instead of drifting quietly.
 */
function contractsPath(...segments: string[]): string {
  let directory = process.cwd()

  for (let depth = 0; depth < 6; depth += 1) {
    const candidate = resolve(directory, 'contracts', ...segments)
    if (existsSync(candidate)) return candidate
    directory = resolve(directory, '..')
  }
  throw new Error(`contracts/${segments.join('/')} not found from ${process.cwd()}`)
}

function readJson(...segments: string[]): Record<string, unknown> {
  return JSON.parse(readFileSync(contractsPath(...segments), 'utf8')) as Record<string, unknown>
}

/** Reads `contracts/examples/<name>.json`. */
export function readExample(name: string): Record<string, unknown> {
  return readJson('examples', `${name}.json`)
}

/** Reads `contracts/schemas/<name>.schema.json`. */
export function readSchema(name: string): Record<string, unknown> {
  return readJson('schemas', `${name}.schema.json`)
}

/** The Operational Picture example, kept for the earlier slice's tests. */
export function readPublishedExample(): Record<string, unknown> {
  return readExample('operational-picture')
}
