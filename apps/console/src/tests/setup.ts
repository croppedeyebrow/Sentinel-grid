import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// Vitest runs without injected globals, so Testing Library's automatic
// cleanup is not registered: without this, renders accumulate across tests and
// queries start matching elements from an earlier case.
afterEach(() => {
  cleanup()
})
