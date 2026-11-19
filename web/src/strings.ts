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
    newFolder: 'New folder',
    newSubfolder: 'New subfolder',
    rename: 'Rename',
    delete: 'Delete folder',
    name: 'Folder name',
    actionsFor: (name: string) => `Actions for ${name}`,
    exists: (name: string) => `A folder named “${name}” already exists.`,
    confirmDelete: (name: string, messages: number) =>
      `Delete “${name}” and the ${messages} messages in it? This cannot be undone.`,
    confirmDeleteWithSubfolders: (name: string, subfolders: number, messages: number) =>
      `Delete “${name}”, its ${subfolders} subfolders, and the ${messages} messages in them? This cannot be undone.`,
  },

  actions: {
    archive: 'Archive',
    delete: 'Delete',
    spam: 'Report spam',
    notSpam: 'Not spam',
    moveTo: 'Move to…',
    markRead: 'Mark read',
    markUnread: 'Mark unread',
    flag: 'Flag',
    unflag: 'Unflag',
    select: (subject: string) => `Select ${subject}`,
    selected: (n: number) => `${n} selected`,
    clearSelection: 'Clear selection',
    moveHeading: 'Move to folder',
    cancel: 'Cancel',
    confirmDelete: 'Delete',
    deleteForever: 'Delete permanently? This cannot be undone.',
    failed: "Couldn't do that. Try again.",
    undo: 'Undo',
    archived: (n: number) => (n === 1 ? 'Conversation archived.' : `${n} conversations archived.`),
    trashed: (n: number) => (n === 1 ? 'Conversation moved to Trash.' : `${n} conversations moved to Trash.`),
    spammed: (n: number) => (n === 1 ? 'Conversation reported as spam.' : `${n} conversations reported as spam.`),
    unspammed: (n: number) => (n === 1 ? 'Conversation moved to Inbox.' : `${n} conversations moved to Inbox.`),
    moved: (n: number, folder: string) =>
      n === 1 ? `Conversation moved to ${folder}.` : `${n} conversations moved to ${folder}.`,
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
    back: 'Back',
    folders: 'Folders',
    to: 'To',
    cc: 'Cc',
    noContent: 'This message has no content.',
    failed: "This message couldn't be loaded.",
    gone: 'This conversation is no longer in this folder.',
    remoteBlocked: 'Remote images are blocked.',
    loadImages: 'Load images',
    alwaysLoad: 'Always load from this sender',
    attachments: 'Attachments',
    download: (name: string) => `Download ${name}`,
    me: 'me',
  },
} as const;
