// [INPUT]: Untrusted registration source and redirect values from URL or session storage.
// [OUTPUT]: An optional normalized campaign source and independently validated same-origin path.
// [POS]: Shared security boundary for email registration attribution and post-verification routing.

export interface RegistrationContext {
  campaign_source?: 'hvoy_partner'
  redirect?: string
}

function hasUnsafeRedirectCharacter(value: string): boolean {
  return Array.from(value).some((character) => {
    const code = character.charCodeAt(0)
    return character === '\\' || code <= 0x1f || code === 0x7f
  })
}

export function sanitizeRegistrationContext(
  source: unknown,
  redirect: unknown
): RegistrationContext {
  const context: RegistrationContext = {}
  if (source === 'hvoy_partner') {
    context.campaign_source = 'hvoy_partner'
  }

  if (
    typeof redirect === 'string' &&
    redirect.startsWith('/') &&
    !redirect.startsWith('//') &&
    !hasUnsafeRedirectCharacter(redirect)
  ) {
    context.redirect = redirect
  }
  return context
}
