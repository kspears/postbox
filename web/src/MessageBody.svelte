<script lang="ts">
  import { allowImages, withImages, type Message } from './api';
  import { strings } from './strings';

  interface Props {
    message: Message;
    // The reader chose "Always load from this sender" on another message of the conversation.
    senderAllowed: boolean;
    onAllowSender: (sender: string) => void;
  }
  const { message, senderAllowed, onAllowSender }: Props = $props();

  let frame = $state<HTMLIFrameElement>();
  let height = $state(80);
  let blocked = $state(0);
  let loadOnce = $state(false);

  // The body comes from its own address, so the frame never inherits the app's page, and its
  // response carries a policy that stops every request but inline images. The sandbox lets
  // links open a new tab and lets the app read the frame's height and blocked-image count,
  // which only works because the frame shares the app's origin; scripts stay off.
  const src = $derived(message.body && (loadOnce || senderAllowed) && !message.imagesAllowed ? withImages(message.body) : message.body);

  function measure(): void {
    const doc = frame?.contentDocument;
    if (!doc?.documentElement) return;
    height = Math.max(40, doc.documentElement.scrollHeight);
  }

  function loaded(): void {
    const doc = frame?.contentDocument;
    if (!doc) return;
    const meta = doc.querySelector('meta[name="postbox-remote-images"]');
    blocked = Number(meta?.getAttribute('content') ?? 0) || 0;
    measure();
    // Images and fonts arrive after load and change the height.
    const observer = new ResizeObserver(measure);
    if (doc.body) observer.observe(doc.body);
    frame?.addEventListener('load', () => observer.disconnect(), { once: true });
  }

  async function always(): Promise<void> {
    if (!message.sender) return;
    const result = await allowImages(message.sender);
    if (result.ok) onAllowSender(message.sender);
  }
</script>

{#if blocked > 0}
  <div class="images-bar" role="status">
    <span>{strings.reading.remoteBlocked}</span>
    <button type="button" onclick={() => (loadOnce = true)}>{strings.reading.loadImages}</button>
    {#if message.sender}
      <button type="button" onclick={always}>{strings.reading.alwaysLoad}</button>
    {/if}
  </div>
{/if}

{#if src}
  <iframe
    bind:this={frame}
    {src}
    title={message.subject}
    sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
    referrerpolicy="no-referrer"
    style:height="{height}px"
    onload={loaded}
  ></iframe>
{:else}
  <p class="note">{strings.reading.noContent}</p>
{/if}

<style>
  iframe {
    display: block;
    width: 100%;
    border: none;
    border-radius: 0.5rem;
    background: #fff;
  }

  .images-bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem 0.75rem;
    margin: 0 0 0.5rem;
    padding: 0.5rem 0.75rem;
    border-radius: 0.5rem;
    background: var(--hover);
    font-size: 0.9rem;
  }

  .images-bar span {
    flex: 1;
    min-width: 12rem;
  }

  .images-bar button {
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.25rem 0.75rem;
    background: var(--surface);
    color: var(--text);
    cursor: pointer;
  }
</style>
