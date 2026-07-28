// [INPUT]: Untrusted registration source and redirect values from URL or session storage.
// [OUTPUT]: A normalized HVOY campaign source and optional safe same-origin path.
// [POS]: Shared security boundary for ordinary email registration attribution.

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
  if (source !== 'hvoy_partner') {
    return {}
  }

  const context: RegistrationContext = {
    campaign_source: 'hvoy_partner'
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
