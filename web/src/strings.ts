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
  },

  // Keyed by the error codes the server sends.
  errors: {
    wrong_credentials: 'Wrong email or password.',
    throttled: 'Too many attempts. Try again in 15 minutes.',
    unreachable: "Can't reach the mail server.",
    internal: 'Something went wrong. Try again.',
  },

  account: {
    menu: 'Account',
    signOut: 'Sign out',
    signOutEverywhere: 'Sign out everywhere',
    signOutFailed: "Couldn't sign out. Try again.",
  },

  folders: {
    inbox: 'Inbox',
  },

  inboxComingSoon: 'Your mail will appear here.',
} as const;
