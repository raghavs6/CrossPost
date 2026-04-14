const StarIcon = () => (
  <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
    <path
      d="M24 2L26.2 21.8L46 24L26.2 26.2L24 46L21.8 26.2L2 24L21.8 21.8Z"
      fill="white"
    />
  </svg>
)

const GoogleIcon = () => (
  <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
    <path
      d="M17.64 9.205c0-.639-.057-1.252-.164-1.841H9v3.481h4.844a4.14 4.14 0 0 1-1.796 2.716v2.259h2.908c1.702-1.567 2.684-3.875 2.684-6.615z"
      fill="#4285F4"
    />
    <path
      d="M9 18c2.43 0 4.467-.806 5.956-2.18l-2.908-2.259c-.806.54-1.837.86-3.048.86-2.344 0-4.328-1.584-5.036-3.711H.957v2.332A8.997 8.997 0 0 0 9 18z"
      fill="#34A853"
    />
    <path
      d="M3.964 10.71A5.41 5.41 0 0 1 3.682 9c0-.593.102-1.17.282-1.71V4.958H.957A8.996 8.996 0 0 0 0 9c0 1.452.348 2.827.957 4.042l3.007-2.332z"
      fill="#FBBC05"
    />
    <path
      d="M9 3.58c1.321 0 2.508.454 3.44 1.345l2.582-2.58C13.463.891 11.426 0 9 0A8.997 8.997 0 0 0 .957 4.958L3.964 7.29C4.672 5.163 6.656 3.58 9 3.58z"
      fill="#EA4335"
    />
  </svg>
)

export default function LoginPage() {
  return (
    <div
      className="relative overflow-hidden w-full h-screen"
      style={{
        backgroundColor: '#07070d',
        backgroundImage: [
          'radial-gradient(ellipse 60% 50% at 15% 35%, rgba(110,70,30,0.32) 0%, transparent 70%)',
          'radial-gradient(ellipse 50% 60% at 78% 55%, rgba(20,50,130,0.22) 0%, transparent 70%)',
          'radial-gradient(ellipse 40% 40% at 50% 88%, rgba(55,15,75,0.18) 0%, transparent 70%)',
        ].join(', '),
      }}
    >
      {/* Left tagline */}
      <div className="absolute top-1/2 left-10 -translate-y-1/2">
        <span className="text-xs font-semibold tracking-[0.3em] text-white/40 uppercase">
          Write Once
        </span>
      </div>

      {/* Right tagline */}
      <div className="absolute top-1/2 right-10 -translate-y-1/2 text-right">
        <span className="text-xs font-semibold tracking-[0.3em] text-white/40 uppercase">
          Post Everywhere
        </span>
      </div>

      {/* Centre column */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 flex flex-col items-center gap-6">
        <StarIcon />
        <h1 className="text-5xl font-black tracking-[0.3em] text-white uppercase mt-4">
          CrossPost
        </h1>
        {/*
          onClick does a full browser navigation — not a fetch() call.
          OAuth requires the browser to actually visit Google's login page,
          which can only happen via a real page navigation, not an API call.
        */}
        <button
          type="button"
          onClick={() => { window.location.href = '/api/auth/google' }}
          className="flex items-center gap-3 px-8 py-3 rounded-full border border-white/20 text-white/80 hover:border-white/50 hover:text-white transition-colors duration-200"
        >
          <GoogleIcon />
          <span className="text-sm font-medium">Sign in with Google</span>
        </button>
      </div>

      {/* Bottom tagline */}
      <div className="absolute bottom-12 w-full text-center">
        <span className="text-xs text-white/30 tracking-[0.25em] uppercase">
          One Post. Every Platform. Exactly On Time.
        </span>
      </div>
    </div>
  )
}
