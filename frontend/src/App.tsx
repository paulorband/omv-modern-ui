import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'
import { useAuth } from './lib/auth'
import { Layout } from './components/Layout'
import { Spinner } from './components/ui'
import { Login } from './pages/Login'
import { Home } from './pages/Home'
import { Files } from './pages/Files'
import { Store } from './pages/Store'
import { StoreApp } from './pages/StoreApp'

function RequireAuth({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  const location = useLocation()
  if (user === undefined) return <Spinner />
  if (user === null) return <Navigate to="/login" replace state={{ from: location.pathname }} />
  return <>{children}</>
}

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        element={
          <RequireAuth>
            <Layout />
          </RequireAuth>
        }
      >
        <Route index element={<Home />} />
        <Route path="store" element={<Store />} />
        <Route path="store/:id" element={<StoreApp />} />
        <Route path="files" element={<Files />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
