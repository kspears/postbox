import type { Person } from './api';

const time = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' });
const dayMonth = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' });
const full = new Intl.DateTimeFormat(undefined, { dateStyle: 'short' });
const long = new Intl.DateTimeFormat(undefined, { dateStyle: 'full', timeStyle: 'short' });

/** A list row's date: the time today, the day this year, else the short date. */
export function when(iso: string, today = new Date()): string {
  const d = new Date(iso);
  if (d.toDateString() === today.toDateString()) return time.format(d);
  if (d.getFullYear() === today.getFullYear()) return dayMonth.format(d);
  return full.format(d);
}

const medium = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' });

/** A date on its own, such as when a passkey was added. */
export const mediumDate = (iso: string): string => medium.format(new Date(iso));

/** A row's date in full, for its tooltip. */
export const longDate = (iso: string): string => long.format(new Date(iso));

/** Up to three of people by name, "me" for account. */
export function who(people: Person[], account: string): string {
  const names = people.map((p) => (p.email === account ? 'me' : p.name || p.email.split('@')[0] || p.email));
  return names.slice(0, 3).join(', ') + (names.length > 3 ? ' …' : '');
}
