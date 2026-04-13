import { useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import client from '../api/client'
import type { Platform } from '../types'

const PLATFORMS: Platform[] = ['twitter', 'linkedin', 'facebook']

export default function NewPostPage() {
  const navigate = useNavigate()
  const [content, setContent] = useState('')
  const [selectedPlatforms, setSelectedPlatforms] = useState<Platform[]>([])
  const [scheduledAt, setScheduledAt] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  function togglePlatform(p: Platform) {
    setSelectedPlatforms((prev) =>
      prev.includes(p) ? prev.filter((x) => x !== p) : [...prev, p],
    )
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (selectedPlatforms.length === 0) {
      setError('Select at least one platform.')
      return
    }
    setError('')
    setLoading(true)
    try {
      await client.post('/api/posts', {
        content,
        platforms: selectedPlatforms,
        scheduledAt,
      })
      navigate('/dashboard')
    } catch {
      setError('Failed to schedule post. Please try again.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="max-w-lg">
      <h1 className="text-2xl font-bold mb-6">Schedule a New Post</h1>
      {error && <p className="text-red-500 text-sm mb-4">{error}</p>}
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <textarea
          placeholder="What do you want to say?"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          required
          rows={5}
          className="border rounded px-3 py-2 text-sm resize-none focus:outline-none focus:ring-2 focus:ring-indigo-400"
        />
        <div>
          <p className="text-sm font-medium mb-2">Platforms</p>
          <div className="flex gap-3">
            {PLATFORMS.map((p) => (
              <label
                key={p}
                className="flex items-center gap-1 text-sm cursor-pointer capitalize"
              >
                <input
                  type="checkbox"
                  checked={selectedPlatforms.includes(p)}
                  onChange={() => togglePlatform(p)}
                  className="accent-indigo-600"
                />
                {p}
              </label>
            ))}
          </div>
        </div>
        <div>
          <p className="text-sm font-medium mb-2">Schedule for</p>
          <input
            type="datetime-local"
            value={scheduledAt}
            onChange={(e) => setScheduledAt(e.target.value)}
            required
            className="border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-400"
          />
        </div>
        <button
          type="submit"
          disabled={loading}
          className="bg-indigo-600 text-white py-2 rounded hover:bg-indigo-700 disabled:opacity-50"
        >
          {loading ? 'Scheduling...' : 'Schedule Post'}
        </button>
      </form>
    </div>
  )
}
