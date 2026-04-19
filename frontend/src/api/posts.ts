import apiClient from './client'
import type { Post, Platform } from '../types'

// The shape the backend actually sends over the wire (snake_case JSON keys).
// We keep this private to this module — the rest of the app uses the camelCase
// Post interface from types/index.ts.
interface RawPost {
  id: number
  content: string
  platforms: string[]
  scheduled_at: string
  status: string
  created_at: string
}

// Converts the raw backend response into the camelCase Post type used by React
// components.  All snake_case → camelCase translation happens here, in one place.
function mapPost(raw: RawPost): Post {
  return {
    id: raw.id,
    content: raw.content,
    platforms: raw.platforms as Platform[],
    scheduledAt: raw.scheduled_at,
    status: raw.status as Post['status'],
    createdAt: raw.created_at,
  }
}

// CreatePostPayload matches the JSON body the backend's CreatePostRequest expects.
// scheduled_at must be an ISO 8601 string — JSON has no native Date type,
// so we serialize a JS Date to a string before sending.
export interface CreatePostPayload {
  content: string
  platforms: string[]
  scheduled_at: string
}

// listPosts fetches all posts belonging to the logged-in user.
// GET /api/posts → PostResponse[] (backend sends a plain JSON array, no wrapper).
export async function listPosts(): Promise<Post[]> {
  const res = await apiClient.get<RawPost[]>('/api/posts')
  return res.data.map(mapPost)
}

// createPost sends a new post to the backend and returns the created post.
// POST /api/posts → PostResponse
export async function createPost(payload: CreatePostPayload): Promise<Post> {
  const res = await apiClient.post<RawPost>('/api/posts', payload)
  return mapPost(res.data)
}

// deletePost removes a post by ID.
// DELETE /api/posts/{id} → 204 No Content (no response body).
export async function deletePost(id: number): Promise<void> {
  await apiClient.delete(`/api/posts/${id}`)
}
