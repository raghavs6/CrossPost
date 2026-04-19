export interface User {
  id: number
  email: string
  createdAt: string
}

export interface Post {
  id: number
  content: string
  scheduledAt: string
  status: 'draft' | 'queued' | 'published' | 'failed'
  platforms: Platform[]
  createdAt: string
}

export type Platform = 'twitter' | 'linkedin' | 'facebook' | 'instagram'

// SocialConnection represents a linked social media account returned by
// GET /api/connections.  Field names match the backend's snake_case JSON keys.
export interface SocialConnection {
  platform: Platform
  username: string
  connected_at: string
}

export interface ApiResponse<T> {
  data?: T
  message?: string
  error?: string
}
