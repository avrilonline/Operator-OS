// ============================================================================
// Operator OS — Login Page
// Email + password sign-in with error display and register link.
// ============================================================================

import { useState } from 'react'
import { useNavigate, useLocation, Link } from 'react-router-dom'
import { useAuthStore } from '../stores/authStore'

export function LoginPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { login, isLoading, error, clearError } = useAuthStore()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  // Where to redirect after successful login
  const from = (location.state as { from?: string })?.from || '/chat'

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!email || !password) return

    try {
      await login({ email, password })
      navigate(from, { replace: true })
    } catch {
      // Error is captured in store
    }
  }

  return (
    <div className="h-full flex items-center justify-center bg-bg">
      <div className="w-full max-w-sm mx-4 animate-fade-slide">
        {/* Header */}
        <div className="text-center mb-8">
          <h1 className="text-2xl font-bold text-text tracking-tight">
            Operator OS
          </h1>
          <p className="text-sm text-text-secondary mt-1">
            Sign in to continue
          </p>
        </div>

        {/* Error banner */}
        {error && (
          <div className="mb-4 px-4 py-3 bg-error-subtle border border-error/20 rounded-[var(--radius-sm)] text-sm text-error flex items-start gap-2">
            <span className="shrink-0 mt-0.5">⚠</span>
            <span className="flex-1">{error}</span>
            <button
              onClick={clearError}
              className="shrink-0 text-error/60 hover:text-error transition-colors"
              aria-label="Dismiss error"
            >
              ✕
            </button>
          </div>
        )}

        {/* Form */}
        <form onSubmit={handleSubmit} className="flex flex-col gap-3">
          <input
            type="email"
            placeholder="Email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="email"
            autoFocus
            required
            disabled={isLoading}
            className="w-full px-4 py-3 bg-surface border border-border rounded-[var(--radius-sm)] text-text text-sm placeholder:text-text-dim outline-none focus:border-accent transition-colors disabled:opacity-50"
          />
          <input
            type="password"
            placeholder="Password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
            disabled={isLoading}
            className="w-full px-4 py-3 bg-surface border border-border rounded-[var(--radius-sm)] text-text text-sm placeholder:text-text-dim outline-none focus:border-accent transition-colors disabled:opacity-50"
          />
          <button
            type="submit"
            disabled={isLoading || !email || !password}
            className="w-full py-3 bg-accent text-white text-sm font-semibold rounded-[var(--radius-sm)] hover:opacity-90 transition-opacity mt-2 disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
          >
            {isLoading ? (
              <>
                <div className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                Signing in…
              </>
            ) : (
              'Sign In'
            )}
          </button>
        </form>

        {/* Footer */}
        <p className="text-center text-xs text-text-dim mt-6">
          Don't have an account?{' '}
          <Link
            to="/register"
            className="text-accent-text hover:underline"
          >
            Register
          </Link>
        </p>
      </div>
    </div>
  )
}
