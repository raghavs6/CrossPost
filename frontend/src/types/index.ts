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

export interface ApiResponse<T> {
  data?: T
  message?: string
  error?: string
}
