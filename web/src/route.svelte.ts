// The open folder lives in the address, /folder/<name>, so a reload or a shared link keeps it.

const prefix = '/folder/';

function folderFromPath(path: string): string {
  if (!path.startsWith(prefix)) return 'INBOX';
  try {
    return decodeURIComponent(path.slice(prefix.length)) || 'INBOX';
  } catch {
    return 'INBOX';
  }
}

export const route = $state({ folder: folderFromPath(location.pathname) });

export function openFolder(name: string): void {
  if (name === route.folder) return;
  route.folder = name;
  history.pushState(null, '', prefix + encodeURIComponent(name));
}

addEventListener('popstate', () => {
  route.folder = folderFromPath(location.pathname);
});
