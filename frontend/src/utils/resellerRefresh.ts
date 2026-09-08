// Reauthorization replaces keys and product access. Reload the application so
// all page-local lists and settings are fetched from the committed state.
export function reloadAfterResellerEnrollment(): void {
  window.location.reload()
}
