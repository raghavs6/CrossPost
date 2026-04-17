import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

/**
 * AuthCallbackPage handles the final step of the Google OAuth flow.
 *
 * After the user logs in with Google, our backend redirects here:
 *   /auth/callback?token=<jwt>
 *
 * This page:
 *   1. Reads the JWT from the URL query parameter.
 *   2. Calls login(token) to store it in localStorage via AuthContext.
 *   3. Navigates to /dashboard with replace:true so the callback URL is
 *      not left in browser history (hitting Back won't re-run this logic).
 *
 * No visible UI is needed — this page is active for milliseconds.
 */
export default function AuthCallbackPage() {
  const navigate = useNavigate()
  const { login, isAuthenticated } = useAuth()

  // Step 1: extract the token from the URL and store it
  useEffect(() => {
    const token = new URLSearchParams(window.location.search).get('token')
    if (token) {
      login(token)
    } else {
      navigate('/login', { replace: true })
    }
  }, [login, navigate])

  // Step 2: navigate only after React has committed the state update
  useEffect(() => {
    if (isAuthenticated) {
      navigate('/dashboard', { replace: true })
    }
  }, [isAuthenticated, navigate])

  return (
    <div className="flex items-center justify-center h-screen" style={{ backgroundColor: '#07070d' }}>
      <p className="text-white/50 text-sm tracking-widest uppercase">Redirecting…</p>
    </div>
  )
}
