// What is open lives in the address, so a reload, the back button or a shared link keeps it:
// /folder/<name> for a folder, /folder/<name>/<id> for a conversation in it.

const prefix = '/folder/';

interface Route {
  folder: string;
  conversation?: number;
}

function parse(path: string): Route {
  if (!path.startsWith(prefix)) return { folder: 'INBOX' };
  const [name = '', id] = path.slice(prefix.length).split('/');
  try {
    const folder = decodeURIComponent(name) || 'INBOX';
    const n = Number(id);
    return Number.isInteger(n) && n > 0 ? { folder, conversation: n } : { folder };
  } catch {
    return { folder: 'INBOX' };
  }
}

export function path(r: Route): string {
  return prefix + encodeURIComponent(r.folder) + (r.conversation ? `/${r.conversation}` : '');
}

export const route = $state<Route>(parse(location.pathname));

function go(next: Route): void {
  if (next.folder === route.folder && next.conversation === route.conversation) return;
  route.folder = next.folder;
  route.conversation = next.conversation;
  history.pushState(null, '', path(next));
}

export function openFolder(name: string): void {
  go({ folder: name });
}

export function openConversation(id: number): void {
  go({ folder: route.folder, conversation: id });
}

export function closeConversation(): void {
  go({ folder: route.folder });
}

addEventListener('popstate', () => {
  const next = parse(location.pathname);
  route.folder = next.folder;
  route.conversation = next.conversation;
});
