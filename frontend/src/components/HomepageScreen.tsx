import { useEffect } from 'react'

import homepageHtml from '../../homepage.html?raw'

export function HomepageScreen({ isLab = false }: { isLab?: boolean }) {
  useEffect(() => {
    document.title = 'ChatAPI'
  }, [])

  const homepageBaseUrl = (import.meta.env.VITE_HOMEPAGE_API_BASE_URL || window.location.origin).replace(/\/$/, '')
  const resolvedHomepageHtml = homepageHtml
    .replace('https://api.kirari.fun', homepageBaseUrl)
    .replace('data-statistics-link', isLab ? 'hidden' : '')

  return (
    <div className="homepage-shell">
      <iframe
        title="ChatAPI Homepage"
        srcDoc={resolvedHomepageHtml}
        className="homepage-frame"
      />
    </div>
  )
}
