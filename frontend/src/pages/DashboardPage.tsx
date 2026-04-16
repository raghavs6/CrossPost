import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import type { Platform } from '../types'

// ── SVG Icons ────────────────────────────────────────────────────────────────

const XIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
    <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-4.714-6.231-5.401 6.231H2.744l7.734-8.835L2.5 2.25h6.985l4.26 5.632 4.499-5.632zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
  </svg>
)

const LinkedInIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
    <path d="M20.447 20.452h-3.554v-5.569c0-1.328-.027-3.037-1.852-3.037-1.853 0-2.136 1.445-2.136 2.939v5.667H9.351V9h3.414v1.561h.046c.477-.9 1.637-1.85 3.37-1.85 3.601 0 4.267 2.37 4.267 5.455v6.286zM5.337 7.433a2.062 2.062 0 0 1-2.063-2.065 2.064 2.064 0 1 1 2.063 2.065zm1.782 13.019H3.555V9h3.564v11.452zM22.225 0H1.771C.792 0 0 .774 0 1.729v20.542C0 23.227.792 24 1.771 24h20.451C23.2 24 24 23.227 24 22.271V1.729C24 .774 23.2 0 22.222 0h.003z" />
  </svg>
)

const InstagramIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 2.163c3.204 0 3.584.012 4.85.07 3.252.148 4.771 1.691 4.919 4.919.058 1.265.069 1.645.069 4.849 0 3.205-.012 3.584-.069 4.849-.149 3.225-1.664 4.771-4.919 4.919-1.266.058-1.644.07-4.85.07-3.204 0-3.584-.012-4.849-.07-3.26-.149-4.771-1.699-4.919-4.92-.058-1.265-.07-1.644-.07-4.849 0-3.204.013-3.583.07-4.849.149-3.227 1.664-4.771 4.919-4.919 1.266-.057 1.645-.069 4.849-.069zM12 0C8.741 0 8.333.014 7.053.072 2.695.272.273 2.69.073 7.052.014 8.333 0 8.741 0 12c0 3.259.014 3.668.072 4.948.2 4.358 2.618 6.78 6.98 6.98C8.333 23.986 8.741 24 12 24c3.259 0 3.668-.014 4.948-.072 4.354-.2 6.782-2.618 6.979-6.98.059-1.28.073-1.689.073-4.948 0-3.259-.014-3.667-.072-4.947-.196-4.354-2.617-6.78-6.979-6.98C15.668.014 15.259 0 12 0zm0 5.838a6.162 6.162 0 1 0 0 12.324 6.162 6.162 0 0 0 0-12.324zM12 16a4 4 0 1 1 0-8 4 4 0 0 1 0 8zm6.406-11.845a1.44 1.44 0 1 0 0 2.881 1.44 1.44 0 0 0 0-2.881z" />
  </svg>
)

const FacebookIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
    <path d="M24 12.073c0-6.627-5.373-12-12-12s-12 5.373-12 12c0 5.99 4.388 10.954 10.125 11.854v-8.385H7.078v-3.47h3.047V9.43c0-3.007 1.792-4.669 4.533-4.669 1.312 0 2.686.235 2.686.235v2.953H15.83c-1.491 0-1.956.925-1.956 1.874v2.25h3.328l-.532 3.47h-2.796v8.385C19.612 23.027 24 18.062 24 12.073z" />
  </svg>
)

const UploadIcon = () => (
  <svg
    width="32"
    height="32"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.5"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
    <polyline points="17 8 12 3 7 8" />
    <line x1="12" y1="3" x2="12" y2="15" />
  </svg>
)

// ── Platform config ───────────────────────────────────────────────────────────

type PlatformConfig = {
  id: Platform
  label: string
  Icon: () => JSX.Element
}

const PLATFORMS: PlatformConfig[] = [
  { id: 'twitter', label: 'X', Icon: XIcon },
  { id: 'linkedin', label: 'LinkedIn', Icon: LinkedInIcon },
  { id: 'instagram', label: 'Instagram', Icon: InstagramIcon },
  { id: 'facebook', label: 'Facebook', Icon: FacebookIcon },
]

// ── Component ─────────────────────────────────────────────────────────────────

export default function DashboardPage() {
  const { logout } = useAuth()
  const navigate = useNavigate()

  const [content, setContent] = useState('')
  const [connected, setConnected] = useState<Set<Platform>>(new Set())
  const [isDragOver, setIsDragOver] = useState(false)

  function handleLogout() {
    logout()
    navigate('/login')
  }

  function togglePlatform(id: Platform) {
    setConnected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  return (
    <div
      className="relative min-h-screen overflow-hidden"
      style={{
        backgroundColor: '#07070d',
        backgroundImage: [
          'radial-gradient(ellipse 60% 50% at 15% 35%, rgba(110,70,30,0.32) 0%, transparent 70%)',
          'radial-gradient(ellipse 50% 60% at 78% 55%, rgba(20,50,130,0.22) 0%, transparent 70%)',
          'radial-gradient(ellipse 40% 40% at 50% 88%, rgba(55,15,75,0.18) 0%, transparent 70%)',
        ].join(', '),
      }}
    >
      {/* Nav bar */}
      <nav className="flex items-center justify-between px-8 py-5 border-b border-white/10">
        <span className="text-sm font-black tracking-[0.3em] text-white uppercase">
          CrossPost
        </span>
        <button
          type="button"
          onClick={handleLogout}
          className="text-sm text-white/40 hover:text-white transition-colors duration-200"
        >
          Log out
        </button>
      </nav>

      {/* Main content */}
      <div className="mx-auto max-w-2xl px-6 py-12 flex flex-col gap-8">

        {/* Section A — Write your post */}
        <div className="flex flex-col gap-3">
          <label className="text-xs font-semibold tracking-[0.2em] text-white/60 uppercase">
            What do you want to say?
          </label>
          <textarea
            rows={5}
            value={content}
            onChange={(e) => setContent(e.target.value)}
            placeholder="Write your post here…"
            className="w-full bg-white/5 border border-white/10 text-white placeholder-white/30 rounded-xl px-4 py-3 resize-none focus:outline-none focus:border-white/30 transition-colors duration-200"
          />
          <span className="text-xs text-white/30 text-right">
            {content.length} / 280
          </span>
        </div>

        {/* Section B — Media drag & drop */}
        <div
          onDragOver={(e) => { e.preventDefault(); setIsDragOver(true) }}
          onDragLeave={() => setIsDragOver(false)}
          onDrop={(e) => { e.preventDefault(); setIsDragOver(false) }}
          className={[
            'flex flex-col items-center justify-center gap-3 rounded-xl px-6 py-10',
            'border-2 border-dashed transition-colors duration-200 cursor-default',
            isDragOver
              ? 'border-white/30 bg-white/5'
              : 'border-white/15 hover:border-white/30 hover:bg-white/5',
          ].join(' ')}
        >
          <span className="text-white/30">
            <UploadIcon />
          </span>
          <p className="text-sm text-white/50">Drag &amp; drop images or videos</p>
          <p className="text-xs text-white/25">PNG, JPG, GIF, MP4 — up to 50 MB</p>
        </div>

        {/* Section C — Connect platforms */}
        <div className="flex flex-col gap-3">
          <label className="text-xs font-semibold tracking-[0.2em] text-white/60 uppercase">
            Post to
          </label>
          <div className="flex flex-wrap gap-3">
            {PLATFORMS.map(({ id, label, Icon }) => {
              const isConnected = connected.has(id)
              return (
                <button
                  key={id}
                  type="button"
                  onClick={() => togglePlatform(id)}
                  className={[
                    'flex items-center gap-2 px-5 py-2 rounded-full border text-sm transition-colors duration-200',
                    isConnected
                      ? 'border-white/60 bg-white/10 text-white'
                      : 'border-white/20 text-white/70 hover:border-white/50 hover:text-white',
                  ].join(' ')}
                >
                  <Icon />
                  {label}
                </button>
              )
            })}
          </div>
        </div>

        {/* Section D — Schedule */}
        <div className="flex flex-col gap-3">
          <label className="text-xs font-semibold tracking-[0.2em] text-white/60 uppercase">
            Schedule for
          </label>
          <input
            type="datetime-local"
            className="w-full appearance-none bg-white/5 border border-white/10 text-white/70 rounded-xl px-4 py-3 focus:outline-none focus:border-white/30 transition-colors duration-200"
          />
        </div>

        {/* Section E — Submit */}
        <div className="flex justify-end">
          <button
            type="button"
            className="bg-white text-black font-semibold rounded-full px-8 py-3 hover:bg-white/90 transition-colors duration-200"
          >
            Schedule Post
          </button>
        </div>

      </div>
    </div>
  )
}
