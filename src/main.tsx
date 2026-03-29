import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createRouter, RouterProvider, createHashHistory } from '@tanstack/react-router'
import { routeTree } from './routeTree.gen'
import './styles/globals.css'

// US-4 & US-8: Hash routing — required for go:embed static file serving.
// go:embed serves a single index.html; history mode would 404 on deep links.
const hashHistory = createHashHistory()

const router = createRouter({
  routeTree,
  history: hashHistory,
  defaultPreload: 'intent',
  scrollRestoration: true,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

const rootElement = document.getElementById('root')!
createRoot(rootElement).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>
)
