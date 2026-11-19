<script lang="ts" module>
  import { Node } from '@tiptap/core';
  import Image from '@tiptap/extension-image';

  // A signature: a block of its own, so changing From can find and replace it.
  const Signature = Node.create({
    name: 'signature',
    group: 'block',
    content: 'block+',
    defining: true,
    parseHTML: () => [{ tag: 'div[data-signature]' }],
    renderHTML: () => ['div', { 'data-signature': '' }, 0],
  });

  // An image in the body is a part of the message (cid:). In the editor it shows from a local
  // address, and data-cid says which part it is.
  const InlineImage = Image.extend({
    addAttributes() {
      return {
        ...this.parent?.(),
        cid: {
          default: null,
          parseHTML: (el: HTMLElement) => el.getAttribute('data-cid'),
          renderHTML: (attrs: { cid?: string | null }) => (attrs.cid ? { 'data-cid': attrs.cid } : {}),
        },
      };
    },
  });
</script>

<script lang="ts">
  import { untrack } from 'svelte';
  import { Editor } from '@tiptap/core';
  import StarterKit from '@tiptap/starter-kit';
  import type { EditorView } from '@tiptap/pm/view';
  import { richSignature } from './rich';
  import { strings } from './strings';

  interface Props {
    // The starting HTML, with images as cid:<contentId>.
    content: string;
    label: string;
    // Where each image part shows from, by Content-ID.
    images: Map<string, string>;
    onChange: () => void;
    // Images dropped or pasted into the body; the answer is those accepted, to place inline.
    onImages: (files: File[]) => Promise<{ contentId: string; url: string }[]>;
    // Other files dropped into the body, to attach.
    onFiles: (files: File[]) => void;
  }
  const { content, label, images, onChange, onImages, onFiles }: Props = $props();

  let element = $state<HTMLDivElement>();
  let editor = $state<Editor>();
  // Bumped on every change, so the toolbar follows the selection.
  let tick = $state(0);

  function forEditor(html: string): string {
    const t = document.createElement('template');
    t.innerHTML = html;
    for (const img of Array.from(t.content.querySelectorAll('img'))) {
      const src = img.getAttribute('src') ?? '';
      if (!src.startsWith('cid:')) continue;
      const id = src.slice(4);
      img.setAttribute('data-cid', id);
      img.setAttribute('src', images.get(id) ?? '');
    }
    return t.innerHTML;
  }

  /** The body as it is saved and sent: images as cid:<contentId>. */
  export function html(): string {
    if (!editor || editor.isEmpty) return '';
    const t = document.createElement('template');
    t.innerHTML = editor.getHTML();
    for (const img of Array.from(t.content.querySelectorAll('img[data-cid]'))) {
      img.setAttribute('src', `cid:${img.getAttribute('data-cid')}`);
      img.removeAttribute('data-cid');
    }
    return t.innerHTML;
  }

  /** Replaces the signature with signature (HTML), or adds it below the person's own words
   * when there is none: above a quoted original and the line that introduces it, else at the
   * end. An empty one removes it. */
  export function setSignature(signature: string): void {
    if (!editor) return;
    let range: { from: number; to: number } | undefined;
    editor.state.doc.descendants((node, pos) => {
      if (range) return false;
      if (node.type.name === 'signature') range = { from: pos, to: pos + node.nodeSize };
      return !range;
    });
    const block = richSignature(signature);
    if (range) {
      if (block) editor.chain().insertContentAt(range, block).run();
      else editor.chain().deleteRange(range).run();
      return;
    }
    if (!block) return;
    const doc = editor.state.doc;
    let at = doc.content.size;
    let previous = 0;
    doc.forEach((node, offset) => {
      if (node.type.name === 'blockquote' && at === doc.content.size) at = previous;
      previous = offset;
    });
    editor.chain().insertContentAt(at, block).run();
  }

  function place(view: EditorView, files: File[], at: number): void {
    const pictures = files.filter((f) => f.type.startsWith('image/'));
    const others = files.filter((f) => !f.type.startsWith('image/'));
    if (others.length > 0) onFiles(others);
    if (pictures.length === 0) return;
    void onImages(pictures).then((accepted) => {
      if (!editor || editor.isDestroyed) return;
      const nodes = accepted.map((a) => ({ type: 'image', attrs: { src: a.url, cid: a.contentId } }));
      if (nodes.length > 0) editor.chain().insertContentAt(Math.min(at, view.state.doc.content.size), nodes).run();
    });
  }

  // Made once per element: the content and images it starts from are read untracked, so adding
  // an image does not rebuild the editor and lose what was typed.
  $effect(() => {
    if (!element) return;
    const target = element;
    const e = untrack(() => new Editor({
      element: target,
      extensions: [
        StarterKit.configure({ link: { openOnClick: false, autolink: true } }),
        InlineImage.configure({ inline: false, allowBase64: false }),
        Signature,
      ],
      content: forEditor(content),
      editorProps: {
        attributes: { 'aria-label': label, 'aria-multiline': 'true', role: 'textbox', class: 'body' },
        handleDrop: (view, event, _slice, moved) => {
          const files = Array.from(event.dataTransfer?.files ?? []);
          if (moved || files.length === 0) return false;
          event.preventDefault();
          const at = view.posAtCoords({ left: event.clientX, top: event.clientY })?.pos ?? view.state.selection.from;
          place(view, files, at);
          return true;
        },
        handlePaste: (view, event) => {
          const files = Array.from(event.clipboardData?.files ?? []);
          if (files.length === 0) return false;
          place(view, files, view.state.selection.from);
          return true;
        },
      },
      onUpdate: () => onChange(),
      onTransaction: () => tick++,
    }));
    editor = e;
    return () => e.destroy();
  });

  function active(name: string): boolean {
    void tick;
    return editor?.isActive(name) ?? false;
  }

  function link(): void {
    if (!editor) return;
    if (editor.isActive('link')) {
      editor.chain().focus().unsetLink().run();
      return;
    }
    const href = prompt(strings.compose.linkAddress, 'https://')?.trim();
    if (!href || !/^(https?:|mailto:)/i.test(href)) return;
    editor.chain().focus().extendMarkRange('link').setLink({ href }).run();
  }

  const tools = [
    { name: 'bold', label: strings.compose.bold, text: 'B', run: () => editor?.chain().focus().toggleBold().run() },
    { name: 'italic', label: strings.compose.italic, text: 'I', run: () => editor?.chain().focus().toggleItalic().run() },
    { name: 'underline', label: strings.compose.underline, text: 'U', run: () => editor?.chain().focus().toggleUnderline().run() },
    { name: 'bulletList', label: strings.compose.bulletList, text: '•', run: () => editor?.chain().focus().toggleBulletList().run() },
    { name: 'orderedList', label: strings.compose.orderedList, text: '1.', run: () => editor?.chain().focus().toggleOrderedList().run() },
    { name: 'blockquote', label: strings.compose.quote, text: '❝', run: () => editor?.chain().focus().toggleBlockquote().run() },
    { name: 'link', label: strings.compose.link, text: '🔗', run: link },
  ];
</script>

<div class="rich">
  <div class="toolbar" role="toolbar" aria-label={strings.compose.formatting}>
    {#each tools as t (t.name)}
      <button type="button" class={t.name} aria-label={t.label} aria-pressed={active(t.name)} onclick={t.run}>{t.text}</button>
    {/each}
  </div>
  <div class="editor" bind:this={element}></div>
</div>

<style>
  .rich {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .toolbar {
    display: flex;
    gap: 0.15rem;
    padding: 0.35rem 0;
    border-bottom: 1px solid var(--border);
  }

  .toolbar button {
    min-width: 2rem;
    padding: 0.2rem 0.4rem;
    border: none;
    border-radius: 0.35rem;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
  }

  .toolbar button[aria-pressed='true'] {
    background: var(--selected);
    color: var(--text);
  }

  .bold {
    font-weight: 700;
  }

  .italic {
    font-style: italic;
  }

  .underline {
    text-decoration: underline;
  }

  .editor {
    flex: 1;
    min-height: 8rem;
    overflow: auto;
    margin-top: 0.5rem;
  }

  .editor :global(.body) {
    min-height: 100%;
    outline: none;
    line-height: 1.5;
  }

  .editor :global(.body p) {
    margin: 0;
  }

  .editor :global(.body blockquote) {
    margin: 0.25rem 0 0.25rem 0.4rem;
    padding-left: 0.75rem;
    border-left: 2px solid var(--border);
    color: var(--text-muted);
  }

  .editor :global(.body img) {
    max-width: 100%;
  }

  .editor :global(.body [data-signature]) {
    color: var(--text-muted);
  }

  .editor :global(.body a) {
    color: var(--accent);
  }
</style>
