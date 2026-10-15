import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { StackView } from './pages/StackView.jsx'

/** Preserve `?env=` / `?focus=` when rewriting `/` → `/stack/default`. */
function HomeRedirect() {
  const location = useLocation()
  return <Navigate to={`/stack/default${location.search}`} replace />
}

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<HomeRedirect />} />
        <Route path="/stack/:stackName" element={<StackView />} />
        <Route path="/stack/:stackName/:nodeId" element={<StackView />} />
      </Routes>
    </BrowserRouter>
  )
}
