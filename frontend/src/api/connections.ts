import apiClient from './client'
import type { SocialConnection } from '../types'

export interface TwitterAuthorizationResponse {
  authorization_url: string
}

// listConnections fetches all social accounts linked by the logged-in user.
// GET /api/connections → ConnectionResponse[] (plain JSON array, no wrapper).
// Returns [] when no accounts are linked or when Twitter OAuth is not configured.
export async function listConnections(): Promise<SocialConnection[]> {
  const res = await apiClient.get<SocialConnection[]>('/api/connections')
  return res.data
}

// beginTwitterConnection fetches the X consent URL from the protected backend
// route. The caller is responsible for redirecting the browser to that URL.
export async function beginTwitterConnection(): Promise<string> {
  const res = await apiClient.get<TwitterAuthorizationResponse>('/api/auth/twitter')
  return res.data.authorization_url
}

// redirectToExternalURL performs the browser navigation after the authenticated
// API call has returned the X consent URL.
export function redirectToExternalURL(url: string): void {
  window.location.assign(url)
}
