import { Outlet, Link } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

export default function Layout() {
  const { logout } = useAuth()
  return (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white shadow-sm">
        <div className="max-w-5xl mx-auto px-4 py-3 flex items-center justify-between">
          <Link to="/dashboard" className="font-bold text-lg text-indigo-600">
            CrossPost
          </Link>
          <div className="flex gap-4 items-center">
            <Link
              to="/posts/new"
              className="text-sm text-gray-600 hover:text-indigo-600"
            >
              New Post
            </Link>
            <button
              onClick={logout}
              className="text-sm text-gray-600 hover:text-red-600"
            >
              Logout
            </button>
          </div>
        </div>
      </nav>
      <main className="max-w-5xl mx-auto px-4 py-8">
        <Outlet />
      </main>
    </div>
  )
}
