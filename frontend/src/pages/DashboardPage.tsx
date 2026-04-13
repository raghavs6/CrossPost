import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import client from '../api/client'
import type { Post } from '../types'
import LoadingSpinner from '../components/LoadingSpinner'

export default function DashboardPage() {
  const [posts, setPosts] = useState<Post[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    client
      .get<Post[]>('/api/posts')
      .then((res) => setPosts(res.data))
      .catch(() => setError('Failed to load posts.'))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <LoadingSpinner />

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Your Scheduled Posts</h1>
        <Link
          to="/posts/new"
          className="bg-indigo-600 text-white px-4 py-2 rounded hover:bg-indigo-700 text-sm"
        >
          + New Post
        </Link>
      </div>
      {error && <p className="text-red-500 text-sm mb-4">{error}</p>}
      {posts.length === 0 ? (
        <p className="text-gray-500 text-sm">No posts yet. Create one above.</p>
      ) : (
        <ul className="flex flex-col gap-4">
          {posts.map((post) => (
            <li key={post.id} className="bg-white rounded-lg shadow p-4">
              <p className="text-sm text-gray-800 mb-2">{post.content}</p>
              <div className="flex gap-2 text-xs text-gray-400">
                <span>Platforms: {post.platforms.join(', ')}</span>
                <span>·</span>
                <span>
                  Scheduled: {new Date(post.scheduledAt).toLocaleString()}
                </span>
                <span>·</span>
                <span className="capitalize">{post.status}</span>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
