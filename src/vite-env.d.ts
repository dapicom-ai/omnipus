/// <reference types="vite/client" />

// SVG URL imports
declare module '*.svg?url' {
  const src: string
  export default src
}

// SVG component imports (for inline usage)
declare module '*.svg?react' {
  import * as React from 'react'
  const ReactComponent: React.FunctionComponent<React.SVGProps<SVGSVGElement>>
  export default ReactComponent
}
