// Moving between the composer's two modes: rich text (HTML) and plain text.

function escape(s: string): string {
  return s.replace(/[&<>"]/g, (c) => `&#${c.charCodeAt(0)};`);
}

/** Plain text as the rich-text editor's HTML: a paragraph per line, quoted lines ("> ") in a
 * block quote. */
export function textToHtml(text: string): string {
  const out: string[] = [];
  let quote: string[] | null = null;
  for (const line of text.replace(/\r\n/g, '\n').split('\n')) {
    if (line.startsWith('>')) {
      quote ??= [];
      quote.push(line.replace(/^> ?/, ''));
      continue;
    }
    if (quote) {
      out.push(`<blockquote>${textToHtml(quote.join('\n'))}</blockquote>`);
      quote = null;
    }
    out.push(`<p>${escape(line)}</p>`);
  }
  if (quote) out.push(`<blockquote>${textToHtml(quote.join('\n'))}</blockquote>`);
  return out.join('');
}

const blocks = new Set(['P', 'DIV', 'H1', 'H2', 'H3', 'PRE', 'UL', 'OL', 'HR']);

/** The text of the rich-text editor's HTML, by the server's rules (compose.HTMLToText): a line
 * per paragraph, quotes marked "> ", list items "- " or "1. ", and a link's address after its
 * text when the two differ. */
export function htmlToText(html: string): string {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  const lines: string[] = [];
  let line = '';
  const end = (depth: number) => {
    lines.push('> '.repeat(depth) + line);
    line = '';
  };
  const walk = (node: Node, depth: number) => {
    for (const child of Array.from(node.childNodes)) {
      if (child.nodeType === Node.TEXT_NODE) {
        line += child.textContent ?? '';
        continue;
      }
      if (!(child instanceof HTMLElement)) continue;
      if (child.tagName === 'BR') {
        end(depth);
      } else if (child.tagName === 'BLOCKQUOTE') {
        if (line) end(depth);
        walk(child, depth + 1);
      } else if (child.tagName === 'LI') {
        if (line) end(depth);
        line = child.parentElement?.tagName === 'OL' ? `${Array.from(child.parentElement.children).indexOf(child) + 1}. ` : '- ';
        const marker = line;
        walk(child, depth);
        if (line !== marker) end(depth);
        line = '';
      } else if (blocks.has(child.tagName)) {
        // A list item's marker waits for the paragraph inside it.
        if (line && !/^(- |\d+\. )$/.test(line)) end(depth);
        walk(child, depth);
        if (child.tagName === 'P' || line) end(depth);
      } else if (child.tagName === 'A') {
        const before = line.length;
        walk(child, depth);
        const href = child.getAttribute('href') ?? '';
        if (href && !href.startsWith('mailto:') && href !== line.slice(before).trim()) line += ` <${href}>`;
      } else {
        walk(child, depth);
      }
    }
  };
  walk(doc.body, 0);
  if (line) end(0);
  return lines.map((l) => l.trimEnd()).join('\n');
}

/** A signature as it ends a plain-text message: the usual "-- " line, then its text. */
export function plainSignature(html: string): string {
  const text = htmlToText(html).trim();
  return text ? `\n-- \n${text}` : '';
}

/** A signature as the rich-text editor holds it. */
export function richSignature(html: string): string {
  return html ? `<div data-signature="">${html}</div>` : '';
}
