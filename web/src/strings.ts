// Every piece of text a person reads lives here, so a translation replaces one file.
export const strings = {
  appName: 'Postbox',

  signIn: {
    title: 'Sign in',
    email: 'Email',
    password: 'Password',
    keep: 'Keep me signed in',
    submit: 'Sign in',
    working: 'Signing in…',
    passwordChanged: 'Your password has changed. Sign in again.',
  },

  // Keyed by the error codes the server sends.
  errors: {
    wrong_credentials: 'Wrong email or password.',
    throttled: 'Too many attempts. Try again in 15 minutes.',
    unreachable: "Can't reach the mail server.",
    internal: 'Something went wrong. Try again.',
  },

  unreachableBanner: "Can't reach the mail server. Retrying…",
  failedBanner: 'Something went wrong. Retrying…',

  account: {
    menu: 'Account',
    signOut: 'Sign out',
    signOutEverywhere: 'Sign out everywhere',
    signOutFailed: "Couldn't sign out. Try again.",
  },

  folders: {
    heading: 'Folders',
    unread: (n: number) => `${n} unread`,
    missing: 'This folder no longer exists.',
  },

  list: {
    empty: 'No conversations here.',
    loading: 'Loading…',
    noSubject: '(no subject)',
    unread: 'Unread',
    flagged: 'Flagged',
    attachment: 'Has attachments',
    messages: (n: number) => `${n} messages`,
  },

  reading: {
    nothingSelected: 'Select a conversation to read it.',
  },
} as const;
