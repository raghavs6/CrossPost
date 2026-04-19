import apiClient from './client'
import type { SocialConnection } from '../types'

// listConnections fetches all social accounts linked by the logged-in user.
// GET /api/connections → ConnectionResponse[] (plain JSON array, no wrapper).
// Returns [] when no accounts are linked or when Twitter OAuth is not configured.
export async function listConnections(): Promise<SocialConnection[]> {
  const res = await apiClient.get<SocialConnection[]>('/api/connections')
  return res.data
}
