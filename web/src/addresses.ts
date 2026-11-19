import type { Person } from './api';

// Recipient fields hold a list such as `Bob <bob@example.test>, carol@example.test`.

const simple = /^[^\s@<>",;]+@[^\s@<>",;]+\.[^\s@<>",;]+$/;

/** The addresses in text, or the first part that is not one. */
export function parseAddresses(text: string): { ok: true; people: Person[] } | { ok: false; bad: string } {
  const people: Person[] = [];
  for (const raw of splitList(text)) {
    const item = raw.trim();
    if (!item) continue;
    const angle = /^(.*)<([^<>]+)>$/.exec(item);
    const email = (angle ? angle[2]! : item).trim();
    if (!simple.test(email)) return { ok: false, bad: item };
    const name = angle ? angle[1]!.trim().replace(/^"(.*)"$/, '$1') : '';
    people.push({ name, email });
  }
  return { ok: true, people };
}

/** Splits on commas and semicolons outside quotes and angle brackets. */
export function splitList(text: string): string[] {
  const out: string[] = [];
  let current = '';
  let quoted = false;
  let angle = false;
  for (const ch of text) {
    if (ch === '"') quoted = !quoted;
    else if (ch === '<' && !quoted) angle = true;
    else if (ch === '>' && !quoted) angle = false;
    if ((ch === ',' || ch === ';') && !quoted && !angle) {
      out.push(current);
      current = '';
    } else {
      current += ch;
    }
  }
  out.push(current);
  return out;
}

export function formatAddresses(people: Person[]): string {
  return people.map((p) => (p.name ? `${/[,;"<>]/.test(p.name) ? JSON.stringify(p.name) : p.name} <${p.email}>` : p.email)).join(', ');
}
