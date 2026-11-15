import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Locates the repository's `contracts/` directory from the test runner's
 * working directory. The published example is read from disk rather than
 * copied into the test, so the console cannot drift away from the contract the
 * Go service is verified against.
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

export function readPublishedExample(): Record<string, unknown> {
  const path = contractsPath('examples', 'operational-picture.json')
  return JSON.parse(readFileSync(path, 'utf8')) as Record<string, unknown>
}
