/**
 * URL scheme safety helpers — shared across chat and iframe preview components.
 *
 * Allow-list: http, https, mailto, tel.
 * Everything else (javascript:, data:, vbscript:, file:, ftp:, schemeless
 * absolute URLs that browser engines resolve to javascript:, etc.) is rejected.
 */

const SAFE_PROTOCOLS = new Set(['http:', 'https:', 'mailto:', 'tel:'])

/**
 * Returns true only when `href` is parseable by the URL constructor and its
 * scheme is in the allow-list. Relative URLs (which throw in `new URL`) and
 * any disallowed scheme both return false.
 */
export function isSafeHref(href: string): boolean {
  try {
    const parsed = new URL(href)
    return SAFE_PROTOCOLS.has(parsed.protocol)
  } catch {
    return false
  }
}
