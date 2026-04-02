import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createRouter, RouterProvider, createHashHistory } from '@tanstack/react-router'
import { QueryClientProvider } from '@tanstack/react-query'
import { queryClient } from './lib/queryClient'
import { routeTree } from './routeTree.gen'
import './styles/globals.css'

// Hash routing — required for go:embed static file serving.
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

const rootElement = document.getElementById('root')
if (!rootElement) {
  throw new Error('Root element #root not found in DOM. Ensure index.html contains <div id="root"></div>.')
}
createRoot(rootElement).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>
)
