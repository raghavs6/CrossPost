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
  const { login } = useAuth()

  useEffect(() => {
    // URLSearchParams is a browser built-in that parses ?key=value strings.
    const params = new URLSearchParams(window.location.search)
    const token = params.get('token')

    if (token) {
      login(token)
      navigate('/dashboard', { replace: true })
    } else {
      // No token means something went wrong in the OAuth flow.
      // Send the user back to the login page rather than leaving them stuck.
      navigate('/login', { replace: true })
    }
  }, [login, navigate])

  return (
    <div className="flex items-center justify-center h-screen" style={{ backgroundColor: '#07070d' }}>
      <p className="text-white/50 text-sm tracking-widest uppercase">Redirecting…</p>
    </div>
  )
}
