import { createContext, useContext } from 'react'
import type { Session } from '../api'

interface SessionValue {
  session: Session
  /** Re-reads the session after the account changed (username, password, Google). */
  refresh: () => Promise<void>
}

export const SessionContext = createContext<SessionValue | null>(null)

export function useSession(): SessionValue {
  const value = useContext(SessionContext)
  if (!value) throw new Error('useSession outside of a signed-in workspace')
  return value
}
