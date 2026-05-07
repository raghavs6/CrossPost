import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import DashboardPage from '../../pages/DashboardPage'
import * as postsApi from '../../api/posts'
import * as connectionsApi from '../../api/connections'
import * as AuthContextModule from '../../context/AuthContext'
import type { Post, SocialConnection } from '../../types'

// Replace the entire posts API module with Vitest fakes.
// Every function in posts.ts (listPosts, createPost, deletePost) becomes a
// vi.fn() that we control per-test with mockResolvedValue / mockRejectedValue.
vi.mock('../../api/posts')

// Replace the connections API module with Vitest fakes.
// listConnections() is called on every mount; we set a default empty array in
// beforeEach so existing tests are not affected.
vi.mock('../../api/connections')

// Replace useAuth so DashboardPage gets a logged-in user without needing a
// real auth token or a running backend.
vi.mock('../../context/AuthContext', () => ({
  useAuth: vi.fn(),
  AuthProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

// A reusable fake post for assertions.
const mockPost: Post = {
  id: 1,
  content: 'Hello world from the test',
  platforms: ['linkedin'],
  scheduledAt: '2025-01-15T14:30:00.000Z',
  status: 'draft',
  createdAt: '2025-01-01T00:00:00.000Z',
}

// Renders DashboardPage inside a MemoryRouter (required because the component
// calls useNavigate) with a mock authenticated user.
function renderDashboard() {
  vi.mocked(AuthContextModule.useAuth).mockReturnValue({
    isAuthenticated: true,
    user: null,
    login: vi.fn(),
    logout: vi.fn(),
  })
  return render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>,
  )
}

describe('DashboardPage', () => {
  beforeEach(() => {
    // Reset all mock state between tests so one test's mocks don't bleed into
    // the next.
    vi.clearAllMocks()
    // Default: no linked social accounts.  Individual tests can override this.
    vi.mocked(connectionsApi.listConnections).mockResolvedValue([])
  })

  it('shows loading state initially', () => {
    // Return a Promise that never resolves — the component stays in isLoading=true.
    vi.mocked(postsApi.listPosts).mockReturnValue(new Promise(() => {}))
    renderDashboard()
    expect(screen.getByText('Loading…')).toBeInTheDocument()
  })

  it('shows posts after fetch resolves', async () => {
    vi.mocked(postsApi.listPosts).mockResolvedValue([mockPost])
    renderDashboard()
    // findByText waits for the component to re-render after the Promise resolves.
    expect(await screen.findByText('Hello world from the test')).toBeInTheDocument()
    expect(screen.getByText('draft')).toBeInTheDocument()
    expect(screen.getByText('linkedin')).toBeInTheDocument()
  })

  it('shows empty state when no posts exist', async () => {
    vi.mocked(postsApi.listPosts).mockResolvedValue([])
    renderDashboard()
    expect(
      await screen.findByText('No posts yet. Create one above.'),
    ).toBeInTheDocument()
  })

  it('calls createPost with correct payload on form submit', async () => {
    vi.mocked(postsApi.listPosts).mockResolvedValue([])
    const newPost: Post = { ...mockPost, id: 2, content: 'My new post' }
    vi.mocked(postsApi.createPost).mockResolvedValue(newPost)

    const { container } = renderDashboard()
    // Wait for the initial load to finish before interacting with the form.
    await screen.findByText('No posts yet. Create one above.')

    // Fill in the content textarea.
    fireEvent.change(screen.getByPlaceholderText('Write your post here…'), {
      target: { value: 'My new post' },
    })

    // Select the LinkedIn platform toggle.
    fireEvent.click(screen.getByText('LinkedIn'))

    // Set the scheduled datetime.  datetime-local inputs have no ARIA label in
    // this component, so we target by input type via container.querySelector.
    const datetimeInput = container.querySelector('input[type="datetime-local"]')!
    fireEvent.change(datetimeInput, { target: { value: '2025-01-15T14:30' } })

    fireEvent.click(screen.getByText('Schedule Post'))

    await waitFor(() => {
      expect(postsApi.createPost).toHaveBeenCalledWith({
        content: 'My new post',
        platforms: ['linkedin'],
        // The component converts the datetime-local string to a full ISO string.
        scheduled_at: new Date('2025-01-15T14:30').toISOString(),
      })
    })

    // The newly created post should appear in the list immediately.
    expect(await screen.findByText('My new post')).toBeInTheDocument()
  })

  it('calls deletePost with the correct id on delete click', async () => {
    vi.mocked(postsApi.listPosts).mockResolvedValue([mockPost])
    vi.mocked(postsApi.deletePost).mockImplementation(() => Promise.resolve())
    // The component calls window.confirm before deleting; return true to confirm.
    vi.spyOn(window, 'confirm').mockReturnValue(true)

    renderDashboard()
    await screen.findByText('Hello world from the test')

    fireEvent.click(screen.getByText('Delete'))

    await waitFor(() => {
      expect(postsApi.deletePost).toHaveBeenCalledWith(mockPost.id)
    })

    // The deleted post should no longer appear in the list.
    expect(screen.queryByText('Hello world from the test')).not.toBeInTheDocument()
  })

  it('shows error message when createPost fails', async () => {
    vi.mocked(postsApi.listPosts).mockResolvedValue([])
    vi.mocked(postsApi.createPost).mockRejectedValue(new Error('Network error'))

    const { container } = renderDashboard()
    await screen.findByText('No posts yet. Create one above.')

    fireEvent.change(screen.getByPlaceholderText('Write your post here…'), {
      target: { value: 'Some content' },
    })
    fireEvent.click(screen.getByText('X'))
    const datetimeInput = container.querySelector('input[type="datetime-local"]')!
    fireEvent.change(datetimeInput, { target: { value: '2025-01-15T14:30' } })

    fireEvent.click(screen.getByText('Schedule Post'))

    expect(
      await screen.findByText('Failed to schedule post. Please try again.'),
    ).toBeInTheDocument()
  })

  // ---------------------------------------------------------------------------
  // Platform Connections section
  // ---------------------------------------------------------------------------

  it('shows Connect X button when X is not connected', async () => {
    vi.mocked(postsApi.listPosts).mockResolvedValue([])
    // beforeEach already mocks listConnections to return [] — no override needed.

    renderDashboard()

    expect(await screen.findByText('Connect X')).toBeInTheDocument()
  })

  it('shows Connect Facebook button when Facebook is not connected', async () => {
    vi.mocked(postsApi.listPosts).mockResolvedValue([])

    renderDashboard()

    expect(await screen.findByText('Connect Facebook')).toBeInTheDocument()
  })

  it('starts X connection by fetching the auth URL and redirecting the browser', async () => {
    vi.mocked(postsApi.listPosts).mockResolvedValue([])
    vi.mocked(connectionsApi.beginTwitterConnection).mockResolvedValue(
      'https://twitter.example/i/oauth2/authorize?state=test',
    )
    vi.mocked(connectionsApi.redirectToExternalURL).mockImplementation(() => {})

    renderDashboard()

    fireEvent.click(await screen.findByText('Connect X'))

    await waitFor(() => {
      expect(connectionsApi.beginTwitterConnection).toHaveBeenCalledTimes(1)
      expect(connectionsApi.redirectToExternalURL).toHaveBeenCalledWith(
        'https://twitter.example/i/oauth2/authorize?state=test',
      )
    })
  })

  it('shows X connected status when account is linked', async () => {
    vi.mocked(postsApi.listPosts).mockResolvedValue([])

    const mockConnection: SocialConnection = {
      platform: 'twitter',
      display_name: 'My Twitter',
      username: 'mytwitterhandle',
      connected_at: '2025-01-01T00:00:00.000Z',
    }
    vi.mocked(connectionsApi.listConnections).mockResolvedValue([mockConnection])

    renderDashboard()

    // findByText waits for the async listConnections call to resolve.
    expect(await screen.findByText(/Connected ✓ @mytwitterhandle/)).toBeInTheDocument()
    // The Connect X button should not appear when already linked.
    expect(screen.queryByText('Connect X')).not.toBeInTheDocument()
  })

  it('starts Facebook connection by fetching the auth URL and redirecting the browser', async () => {
    vi.mocked(postsApi.listPosts).mockResolvedValue([])
    vi.mocked(connectionsApi.beginFacebookConnection).mockResolvedValue(
      'https://facebook.example/dialog/oauth?state=test',
    )
    vi.mocked(connectionsApi.redirectToExternalURL).mockImplementation(() => {})

    renderDashboard()

    fireEvent.click(await screen.findByText('Connect Facebook'))

    await waitFor(() => {
      expect(connectionsApi.beginFacebookConnection).toHaveBeenCalledTimes(1)
      expect(connectionsApi.redirectToExternalURL).toHaveBeenCalledWith(
        'https://facebook.example/dialog/oauth?state=test',
      )
    })
  })

  it('shows Facebook connected status when account is linked', async () => {
    vi.mocked(postsApi.listPosts).mockResolvedValue([])

    const mockConnection: SocialConnection = {
      platform: 'facebook',
      display_name: 'Ada Lovelace',
      connected_at: '2025-01-01T00:00:00.000Z',
    }
    vi.mocked(connectionsApi.listConnections).mockResolvedValue([mockConnection])

    renderDashboard()

    expect(await screen.findByText(/Connected Facebook ✓ Ada Lovelace/)).toBeInTheDocument()
    expect(screen.queryByText('Connect Facebook')).not.toBeInTheDocument()
  })
})
